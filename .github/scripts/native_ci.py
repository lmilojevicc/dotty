"""One hosted-VM native CI sequence; no retries, cleanup, or resource controller."""
import errno
from contextlib import contextmanager, redirect_stdout, redirect_stderr
import hashlib
import io
import json
import math
import os
from pathlib import Path
import platform
import re
import selectors
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import time

assert sys.version_info >= (3, 9), 'Python 3.9+ required'
sys.dont_write_bytecode = True

# This sibling contains source-bound case data, not an executable acquisition hook.
sys.path.insert(0, str(Path(__file__).parent))
import native_evidence as evidence


ROOT = Path(os.environ['RUNNER_TEMP']) / (
    'dotty-native-' + os.environ['GITHUB_RUN_ID'] + '-' + os.environ['GITHUB_RUN_ATTEMPT'])
WORK = Path(os.environ['GITHUB_WORKSPACE'])
LOGS = ROOT / 'evidence'
PRIVATE = {
    'HOME': 'home', 'XDG_CONFIG_HOME': 'config', 'XDG_CACHE_HOME': 'cache',
    'XDG_DATA_HOME': 'data', 'XDG_STATE_HOME': 'state', 'XDG_RUNTIME_DIR': 'runtime',
    'TMPDIR': 'tmp', 'DOTTY_REPO': 'fake-repo', 'GOPATH': 'go',
    'GOMODCACHE': 'modules', 'GOCACHE': 'go-build', 'MISE_DATA_DIR': 'mise-data',
    'MISE_CACHE_DIR': 'mise-cache', 'MISE_CONFIG_DIR': 'mise-config',
    'MISE_STATE_DIR': 'mise-state',
}
FIXED_ENV = {
    'CGO_ENABLED': '0', 'GOENV': 'off', 'GOWORK': 'off', 'GOTOOLCHAIN': 'local',
    'GOCACHEPROG': '', 'GOFLAGS': '', 'GOTELEMETRY': 'off',
    'DOTTY_NATIVE_ACCEPTANCE': '1', 'MISE_YES': 'true', 'LC_ALL': 'C', 'LANG': 'C',
}
NATIVE = [
    ('regular-focused', 'TestRegularObservationBoundary'),
    ('coordinator-focused', 'TestCoordinatorBoundary'),
    ('flock-focused', 'TestFlockBoundary'),
    ('private-focused', 'TestPrivateCreateOpenBoundary'),
    ('authority-common', 'TestAuthorityBoundary'),
    ('authority-linux', 'TestLinuxAuthority'),
    ('native-focused', 'TestNativeHandleBoundary'),
]
TEST_LANES = evidence.PROTOCOL_LANES + [name for name, _ in NATIVE] + ['verify']
COMMAND_LANES = (
    ['source-before-' + suffix for suffix in ('head', 'commit', 'tree', 'git-dir')]
    + ['workflow-fetch', 'workflow-commit', 'workflow-tree-0', 'workflow-tree-1',
       'workflow-tree-2', 'workflow-blob']
    + ['source-after-workflow-' + suffix for suffix in ('head', 'commit', 'tree', 'git-dir')]
    + ['filesystem-0', 'filesystem-1', 'filesystem-2', 'process-status', 'mounts', 'reaping',
       'install', 'mise-version', 'tool-inventory', 'go-env', 'yamlfmt-version',
       'golangci-version', 'goreleaser-version', 'govulncheck-version', 'modules', 'format']
    + TEST_LANES + ['compile-freebsd', 'compile-windows', 'vuln']
    + ['source-after-' + suffix for suffix in ('head', 'commit', 'tree', 'git-dir')]
)


# Authority is retained across reopenings, never refreshed from a replacement name.
BOUND_DIRECTORIES = {}
COLLECTION_END = None


def check_collection_deadline():
    if COLLECTION_END is not None and time.monotonic() >= COLLECTION_END:
        raise CollectionDeadline('collection deadline; partial evidence retained')


def require_bindings(anchors):
    current = revalidate_ancestry(anchors)
    for path, observed in current.items():
        if path in BOUND_DIRECTORIES:
            assert observed == BOUND_DIRECTORIES[path], 'recorded directory binding drift: ' + path
    return current


class PrivateOutput:
    """Exclusive bounded writer holding current ancestor and leaf capabilities."""
    def __init__(self, path, limit, text=False):
        check_collection_deadline()
        self.digest = hashlib.sha256()
        self.closed_record = None
        assert path.is_relative_to(ROOT) and '..' not in path.parts, 'output outside private root'
        assert str(path.parent) in BOUND_DIRECTORIES, 'unbound output directory: ' + str(path.parent)
        self.path, self.limit, self.text, self.count = path, limit, text, 0
        self.context = trusted_ancestry(path.parent)
        self.anchors = self.context.__enter__()
        self.stream = None
        try:
            require_bindings(self.anchors)
            self.parent = self.anchors[-1][2]
            check_collection_deadline()
            fd = os.open(path.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                         0o600, dir_fd=self.parent)
            self.stream = os.fdopen(fd, 'wb', buffering=0)
            self.initial = os.fstat(fd)
            self.expected = evidence.export_metadata(self.initial)
            self.check()
        except BaseException:
            if self.stream is not None:
                self.stream.close()
            self.context.__exit__(*sys.exc_info())
            raise

    def check(self):
        check_collection_deadline()
        require_bindings(self.anchors)
        current = os.fstat(self.stream.fileno())
        assert stat.S_ISREG(current.st_mode) and current.st_uid == os.geteuid() and current.st_nlink == 1, 'unsafe output leaf'
        assert (current.st_dev, current.st_ino) == (self.initial.st_dev, self.initial.st_ino), 'output descriptor drift'
        assert evidence.export_metadata(os.stat(self.path.name, dir_fd=self.parent, follow_symlinks=False)) == evidence.export_metadata(current), 'output name drift'
        assert current.st_size == self.count, 'output size drift'
        assert evidence.export_metadata(current) == self.expected, 'output metadata drift'

    def write(self, value):
        data = value.encode('utf-8') if self.text else value
        assert self.count + len(data) <= self.limit, 'output byte budget: ' + str(self.path)
        self.check()
        remaining = memoryview(data)
        while remaining:
            self.check()
            check_collection_deadline()  # Immediately before each unbuffered data write.
            written = self.stream.write(remaining)
            assert written and written > 0, 'incomplete output write'
            self.digest.update(remaining[:written])
            self.count += written
            remaining = remaining[written:]
            current = os.fstat(self.stream.fileno())
            assert all(getattr(current, 'st_' + key) == self.expected[key]
                       for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink')), 'output security drift'
            self.expected = evidence.export_metadata(current)  # Only this owned write may advance timestamps/size.
        self.check()
        return len(value)

    def tell(self):
        return self.count

    def flush(self):
        self.check()
        self.stream.flush()

    def abort(self):
        # FileIO is unbuffered; closing owned descriptors adds no completion data.
        if not self.stream.closed:
            self.stream.close()
            self.context.__exit__(None, None, None)

    def close(self):
        if self.stream.closed:
            return
        try:
            self.check()
            record = {'bytes': self.count, 'sha256': self.digest.hexdigest(),
                      'metadata': dict(self.expected)}
        finally:
            self.abort()
        self.closed_record = record  # Authority only from a successfully closed writer.

    def __enter__(self):
        return self

    def __exit__(self, *error):
        if isinstance(error[1], CollectionDeadline):
            self.abort()
        else:
            self.close()


def write_json(path, value):
    data = json.dumps(value, sort_keys=True, indent=2) + '\n'
    with PrivateOutput(path, evidence.EXPORT_LIMITS['wire'], text=True) as stream:
        stream.write(data)
    return stream.closed_record


def identity(path):
    st = path.lstat()
    assert stat.S_ISDIR(st.st_mode) and st.st_uid == os.geteuid()
    assert stat.S_IMODE(st.st_mode) == 0o700, 'nonprivate directory: ' + str(path)
    return {key: getattr(st, 'st_' + key) for key in ('dev', 'ino', 'uid', 'gid', 'mode')}


def read_private(path, limit):
    """Bounded, nofollow observation with current name/descriptor bindings."""
    check_collection_deadline()
    assert path.is_relative_to(ROOT), 'read outside private root'
    with trusted_ancestry(path.parent) as anchors:
        require_bindings(anchors)
        fd = anchors[-1][2]
        name = path.name
        before = os.stat(name, dir_fd=fd, follow_symlinks=False)
        assert stat.S_ISREG(before.st_mode) and before.st_size <= limit, 'unsafe evidence file'
        assert before.st_uid == os.geteuid() and before.st_nlink == 1, 'unresolved evidence ownership/hardlinks: ' + str(path)
        leaf = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        try:
            assert evidence.export_metadata(os.fstat(leaf)) == evidence.export_metadata(before), 'evidence descriptor drift'
            require_bindings(anchors)
            data = bytearray()
            while len(data) < before.st_size:
                check_collection_deadline()
                block = os.read(leaf, min(65536, before.st_size - len(data)))
                check_collection_deadline()
                assert block, 'short evidence read'
                data.extend(block)
            assert evidence.export_metadata(os.fstat(leaf)) == evidence.export_metadata(before), 'evidence descriptor drift'
            assert evidence.export_metadata(os.stat(name, dir_fd=fd, follow_symlinks=False)) == evidence.export_metadata(before), 'evidence name drift'
            require_bindings(anchors)
            return bytes(data)
        finally:
            os.close(leaf)


def bind():
    assert ROOT.is_absolute() and ROOT.resolve() == ROOT, 'nonphysical CI root'
    identity(ROOT)
    binding = json.loads(read_private(ROOT / 'binding.json', 16384))
    assert binding['root'] == str(ROOT) and binding['uid'] == os.geteuid() > 0
    with trusted_ancestry(ROOT) as anchors:
        assert require_bindings(anchors) == binding['ancestry'], 'recorded ancestor binding drift'
    expected_directories = {str(ROOT / name): expected for name, expected in binding['directories'].items()}
    for name, expected in binding['directories'].items():
        assert identity(ROOT / name) == expected, 'private directory binding drift: ' + name
    for path, expected in (binding['ancestry'] | expected_directories).items():
        assert path not in BOUND_DIRECTORIES or BOUND_DIRECTORIES[path] == expected, 'recorded directory binding drift: ' + path
    BOUND_DIRECTORIES.update(binding['ancestry'] | expected_directories)
    if evidence.PRIVATE_TMP is None:
        evidence.configure(ROOT / 'tmp', os.geteuid())
    else:
        assert evidence.PRIVATE_TMP == ROOT / 'tmp' and evidence.PRIVATE_UID == os.geteuid(), 'evidence grammar binding drift'
    return binding


def ancestor_policy(fd, path):
    st = os.fstat(fd)
    assert stat.S_ISDIR(st.st_mode) and st.st_uid in (0, os.geteuid()), 'unsafe ancestor owner: ' + str(path)
    assert not st.st_mode & 0o022, 'writable ancestor: ' + str(path)
    for attribute in ('system.posix_acl_access', 'system.posix_acl_default'):
        try:
            os.getxattr(fd, attribute)
        except OSError as error:
            assert error.errno == errno.ENODATA, 'unsupported/unknown ancestor ACL: ' + str(path)
        else:
            raise AssertionError('ancestor ACL present: ' + str(path))


def revalidate_ancestry(anchors):
    bindings = {}
    for parent, name, fd, initial, path in anchors:
        ancestor_policy(fd, path)
        current = os.fstat(fd)
        keys = ('dev', 'ino', 'mode', 'uid', 'gid')
        observed = {key: getattr(current, 'st_' + key) for key in keys}
        assert observed == initial, 'ancestor identity drift: ' + str(path)
        named = os.stat(name, dir_fd=parent, follow_symlinks=False)
        assert all(getattr(named, 'st_' + key) == observed[key] for key in keys), 'ancestor binding drift: ' + str(path)
        bindings[str(path)] = observed
    return bindings


@contextmanager
def trusted_ancestry(path):
    assert path.is_absolute() and '..' not in path.parts, 'unsafe ancestor path'
    anchors = []
    try:
        parent = None
        current = Path('/')
        for name in ('/',) + path.parts[1:]:
            if name != '/':
                current /= name
            fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
            initial = {key: getattr(os.fstat(fd), 'st_' + key) for key in ('dev', 'ino', 'mode', 'uid', 'gid')}
            anchors.append((parent, name, fd, initial, current))
            ancestor_policy(fd, current)
            parent = fd
        revalidate_ancestry(anchors)
        yield anchors
    finally:
        for _, _, fd, _, _ in reversed(anchors):
            os.close(fd)


def create_root():
    with trusted_ancestry(ROOT.parent) as anchors:
        bindings = revalidate_ancestry(anchors)
        parent = anchors[-1][2]
        os.mkdir(ROOT.name, mode=0o700, dir_fd=parent)
        created = os.stat(ROOT.name, dir_fd=parent, follow_symlinks=False)
        fd = os.open(ROOT.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        try:
            st = os.fstat(fd)
            assert evidence.export_metadata(st) == evidence.export_metadata(created), 'new root open drift'
            assert st.st_uid == os.geteuid() and stat.S_IMODE(st.st_mode) == 0o700, 'new root ownership/mode'
            initial = {key: getattr(st, 'st_' + key) for key in ('dev', 'ino', 'mode', 'uid', 'gid')}
            bindings = revalidate_ancestry(anchors + [(parent, ROOT.name, fd, initial, ROOT)])
        finally:
            os.close(fd)
        BOUND_DIRECTORIES.update(bindings)
        return bindings


def create_private_directory(name):
    assert name and '/' not in name and name not in ('.', '..')
    with trusted_ancestry(ROOT) as anchors:
        require_bindings(anchors)
        parent = anchors[-1][2]
        os.mkdir(name, mode=0o700, dir_fd=parent)
        created = os.stat(name, dir_fd=parent, follow_symlinks=False)
        fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        try:
            assert evidence.export_metadata(os.fstat(fd)) == evidence.export_metadata(created), 'new directory open drift'
            initial = {key: getattr(os.fstat(fd), 'st_' + key) for key in ('dev', 'ino', 'mode', 'uid', 'gid')}
            assert initial['uid'] == os.geteuid() and stat.S_IMODE(initial['mode']) == 0o700
            chain = anchors + [(parent, name, fd, initial, ROOT / name)]
            require_bindings(chain)
            BOUND_DIRECTORIES[str(ROOT / name)] = initial
        finally:
            os.close(fd)


def append_github_env(values, require_empty=False):
    """Runner file-command capability, not arbitrary environment pathname authority."""
    path = Path(os.environ['GITHUB_ENV'])
    parent = Path(os.environ['RUNNER_TEMP']) / '_runner_file_commands'
    assert path.parent == parent and re.fullmatch(r'set_env_[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}', path.name), 'unexpected GITHUB_ENV role path'
    data = ''.join(key + '=' + value + '\n' for key, value in values.items())
    assert all(re.fullmatch('[A-Z][A-Z0-9_]*', key) and '\n' not in value and '\r' not in value
               for key, value in values.items()), 'invalid environment record'
    data = data.encode('utf-8')
    assert len(data) <= 65536, 'environment append budget'
    with trusted_ancestry(parent) as anchors:
        require_bindings(anchors)
        directory = anchors[-1][2]
        before = os.stat(path.name, dir_fd=directory, follow_symlinks=False)
        assert stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1, 'unsafe GITHUB_ENV leaf'
        assert not require_empty or before.st_size == 0, 'preexisting publication environment data'
        assert before.st_size + len(data) <= 1024**2, 'environment file budget'
        fd = os.open(path.name, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            expected = evidence.export_metadata(before)
            remaining = memoryview(data)
            while remaining:
                require_bindings(anchors)
                assert evidence.export_metadata(os.fstat(fd)) == expected, 'GITHUB_ENV descriptor drift'
                assert evidence.export_metadata(os.stat(path.name, dir_fd=directory, follow_symlinks=False)) == expected, 'GITHUB_ENV name drift'
                check_collection_deadline()
                written = os.write(fd, remaining)
                assert written > 0, 'incomplete environment append'
                remaining = remaining[written:]
                after = os.fstat(fd)
                assert after.st_size == expected['size'] + written and after.st_nlink == 1, 'GITHUB_ENV append drift'
                assert all(getattr(after, 'st_' + key) == expected[key]
                           for key in ('dev', 'ino', 'mode', 'uid', 'gid')), 'GITHUB_ENV security drift'
                expected = evidence.export_metadata(after)
            require_bindings(anchors)
            assert evidence.export_metadata(os.stat(path.name, dir_fd=directory, follow_symlinks=False)) == expected, 'GITHUB_ENV name drift'
        finally:
            os.close(fd)


def prepare():
    os.umask(0o077)
    assert re.fullmatch('[0-9]+', os.environ['GITHUB_RUN_ID'])
    assert re.fullmatch('[1-9][0-9]*', os.environ['GITHUB_RUN_ATTEMPT'])
    assert platform.system() == 'Linux' and platform.machine() == 'aarch64'
    assert os.geteuid() == os.getuid() > 0 and os.getegid() == os.getgid()
    assert ROOT.parent.resolve() == ROOT.parent and WORK.resolve() == WORK
    ancestry = create_root()  # Exclusive descriptor-relative creation; no adoption.
    for name in list(PRIVATE.values()) + ['evidence', 'compile']:
        create_private_directory(name)
    binding = {'root': str(ROOT), 'workspace': str(WORK), 'uid': os.geteuid(), 'ancestry': ancestry,
               'directories': {name: identity(ROOT / name)
                               for name in ['.'] + list(PRIVATE.values()) + ['evidence', 'compile']}}
    write_json(ROOT / 'binding.json', binding)
    write_json(LOGS / 'binding.json', binding)
    context = {key: os.environ.get(key) for key in (
        'CANDIDATE_SHA', 'GITHUB_SHA', 'GITHUB_REF', 'GITHUB_EVENT_NAME',
        'WORKFLOW_SHA', 'WORKFLOW_REF', 'GITHUB_REPOSITORY', 'GITHUB_RUN_ID',
        'GITHUB_RUN_ATTEMPT', 'RUNNER_NAME', 'RUNNER_OS', 'RUNNER_ARCH',
        'ImageOS', 'ImageVersion')}
    event_path = Path(os.environ['GITHUB_EVENT_PATH'])
    assert event_path.stat().st_size <= 4 * 1024**2, 'event payload budget'
    event_bytes = event_path.read_bytes()
    event = json.loads(event_bytes)
    pr = event['pull_request']
    assert event['repository']['full_name'] == pr['head']['repo']['full_name'] == 'lmilojevicc/dotty'
    assert pr['head']['sha'] == context['CANDIDATE_SHA']
    context['event'] = {'sha256': hashlib.sha256(event_bytes).hexdigest(),
                        'action': event['action'], 'number': event['number'],
                        'head_sha': pr['head']['sha'], 'head_ref': pr['head']['ref'],
                        'base_sha': pr['base']['sha'], 'base_ref': pr['base']['ref'],
                        'merge_commit_sha': pr['merge_commit_sha']}
    context.update({'uname': list(platform.uname()), 'uid': os.getuid(),
                    'euid': os.geteuid(), 'gid': os.getgid(), 'groups': os.getgroups(),
                    'python': sys.version, 'python_executable': sys.executable,
                    'trust': 'ephemeral hosted VM; network and sudo available, sudo unused'})
    write_json(LOGS / 'context.json', context)
    env = {key: str(ROOT / name) for key, name in PRIVATE.items()}
    env.update(FIXED_ENV)
    env.update({'MISE_GLOBAL_CONFIG_FILE': str(ROOT / 'mise-config/config.toml'),
                'MISE_SYSTEM_CONFIG_FILE': str(ROOT / 'mise-config/system.toml'),
                'MISE_TRUSTED_CONFIG_PATHS': str(WORK / 'mise.toml')})
    append_github_env(env)


# Linux WNOWAIT reserves the directly spawned session leader until the LAST signal.
# No poll(), Popen context manager, or wait() may reap it while group signals remain.
COMMAND_SECONDS = {'workflow-fetch': 60, 'install': 900, 'modules': 600, 'verify': 1800}
TERM_GRACE, KILL_GRACE, DRAIN_SECONDS, ABSENCE_SECONDS = 2.0, 2.0, 2.0, 1.0
RUN_DEADLINE = None
RETAINED_COMMANDS = []  # Unknown live leaders remain owned; never signal after release.


def observed_exit(proc):
    result = os.waitid(os.P_PID, proc.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
    if result is None:
        return None
    assert result.si_pid == proc.pid, 'command child identity unknown'
    return result.si_status if result.si_code == os.CLD_EXITED else 128 + result.si_status


def signal_owned(proc, number):
    # A direct unreaped child cannot have its PID/PGID reused. Check session/group too.
    observed_exit(proc)  # ECHILD is refusal, not permission to signal a historical ID.
    assert proc.returncode is None and os.getsid(proc.pid) == os.getpgid(proc.pid) == proc.pid, 'command group ownership unknown'
    os.killpg(proc.pid, number)


def session_observations(proc):
    """Bounded diagnostic only: never grants signal authority over observed peer IDs."""
    peers = []
    deadline = time.monotonic() + 0.25
    with os.scandir('/proc') as scan:
        count = 0
        for entry in scan:
            count += 1
            assert count <= 65536 and time.monotonic() < deadline, 'process observation budget'
            if not entry.name.isdecimal() or int(entry.name) == proc.pid:
                continue
            pid = int(entry.name)
            try:
                if os.getsid(pid) == proc.pid:
                    peers.append({'pid': pid, 'pgid': os.getpgid(pid)})
            except OSError as error:
                if error.errno != errno.ESRCH:
                    raise
    return peers


def group_absent(pgid):
    try:
        os.killpg(pgid, 0)  # Observation only after release; never a destructive signal.
    except OSError as error:
        assert error.errno == errno.ESRCH, 'command group absence unknown'
        return True
    return False


def observe_group_absence(pgid, until):
    while True:
        absent = group_absent(pgid)
        if absent or time.monotonic() >= until:
            return absent
        time.sleep(min(0.05, max(0, until - time.monotonic())))


def command(lane, argv, env=None, binary=False, seconds=None):
    # Keep cancellation local and active through capture, termination AND receipts.
    cancelled, previous = [], {}
    def cancellation(number, frame):
        if not cancelled:
            cancelled.append(number)
    try:
        for number in (signal.SIGTERM, signal.SIGINT):
            previous[number] = signal.signal(number, cancellation)
        result = capture_command(lane, argv, env, binary, seconds, cancelled)
        if cancelled:
            raise CommandFailure(128 + cancelled[0], lane + ': cancellation after capture')
        return result
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)


def capture_command(lane, argv, env, binary, seconds, cancelled):
    """Bounded local session capture. Only the reserved owned group can be signalled."""
    seconds = COMMAND_SECONDS.get(lane, 120 if lane in TEST_LANES else 300) if seconds is None else seconds
    end = time.monotonic() + seconds
    if RUN_DEADLINE is not None:
        end = min(end, RUN_DEADLINE)
    write_json(LOGS / (lane + '.command.json'), {
        'argv': argv, 'cwd': str(WORK), 'env_overrides': env or {}, 'seconds': seconds,
        'term_grace': TERM_GRACE, 'kill_grace': KILL_GRACE, 'drain_seconds': DRAIN_SECONDS})
    status, discarded, kept = None, 0, 0
    sizes, failures, files, peers = [0, 0], [], [], []
    proc, selector, exited_at = None, None, None
    def fail(reason):
        if reason not in failures:
            failures.append(reason)
    def pump(wait):
        nonlocal kept, discarded
        for key, _ in selector.select(max(0, min(0.05, wait))):
            chunk = os.read(key.fileobj.fileno(), 65536)
            if not chunk:
                selector.unregister(key.fileobj)
                key.fileobj.close()
                continue
            accepted = chunk[:max(0, evidence.OUTPUT_LIMIT - kept)]
            discarded += len(chunk) - len(accepted)
            kept += len(accepted)
            sizes[key.data] += len(accepted)
            files[key.data].write(accepted)
            files[2].write(accepted)
            for stream in files:
                stream.flush()
            if discarded:
                fail('output overflow')
    drained, absent, released = False, False, False
    try:
        assert hasattr(os, 'WNOWAIT') and hasattr(os, 'waitid'), 'Linux WNOWAIT required'
        assert time.monotonic() < end, 'overall command deadline'
        for suffix in ('stdout', 'stderr', 'log'):
            files.append(PrivateOutput(LOGS / (lane + '.' + suffix), evidence.OUTPUT_LIMIT))
        proc = subprocess.Popen(argv, cwd=WORK, env=os.environ | (env or {}),
                                start_new_session=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        selector = selectors.DefaultSelector()
        for index, pipe in enumerate((proc.stdout, proc.stderr)):
            os.set_blocking(pipe.fileno(), False)
            selector.register(pipe, selectors.EVENT_READ, index)
        while True:
            status = observed_exit(proc)
            now = time.monotonic()
            if status is not None and exited_at is None:
                exited_at = now
            if cancelled:
                fail('cancelled by signal ' + str(cancelled[0]))
            if now >= end:
                fail('command deadline')
            if exited_at is not None and selector.get_map() and now >= exited_at + DRAIN_SECONDS:
                fail('stream drain deadline')
            if failures or (status is not None and not selector.get_map()):
                break
            pump(end - now)
    except BaseException as error:
        fail(type(error).__name__ + ': ' + str(error))
    finally:
        # Even normal exit may have descendants that closed their pipes. Terminate
        # the owned group while its leader is still reserved, then release it.
        if proc is not None:
            shutdown_end = time.monotonic() + TERM_GRACE + KILL_GRACE + ABSENCE_SECONDS
            try:
                observed = observed_exit(proc)
                if status is None and observed is not None:
                    status = observed
                try:
                    peers = session_observations(proc)
                    if peers:
                        fail('remaining session peers; only owned group signalled, other groups retained: ' + repr(peers))
                except BaseException as error:
                    # Diagnostic uncertainty does not release or invalidate our child.
                    fail('incomplete session diagnostics: ' + type(error).__name__ + ': ' + str(error))
                signal_owned(proc, signal.SIGTERM)
                grace = min(time.monotonic() + TERM_GRACE, shutdown_end)
                while time.monotonic() < grace:
                    try:
                        if selector is not None and selector.get_map():
                            pump(grace - time.monotonic())
                        else:
                            time.sleep(min(0.05, max(0, grace - time.monotonic())))
                    except BaseException as error:
                        fail('termination drain: ' + type(error).__name__ + ': ' + str(error))
                        break
                signal_owned(proc, signal.SIGKILL)  # LAST mutating signal, before any reap.
                grace = min(time.monotonic() + KILL_GRACE, shutdown_end)
                while time.monotonic() < grace:
                    observed = observed_exit(proc)
                    if status is None and observed is not None:
                        status = observed
                    if observed is not None and (selector is None or not selector.get_map()):
                        break
                    try:
                        if selector is not None and selector.get_map():
                            pump(grace - time.monotonic())
                        else:
                            time.sleep(min(0.05, max(0, grace - time.monotonic())))
                    except BaseException as error:
                        fail('kill drain: ' + type(error).__name__ + ': ' + str(error))
                        break
                if observed_exit(proc) is not None:
                    # WNOWAIT has proven waitpid will not wait on a running child.
                    waited, wait_status = os.waitpid(proc.pid, os.WNOHANG)
                    assert waited == proc.pid, 'leader release unknown'
                    proc.returncode = os.waitstatus_to_exitcode(wait_status)
                    released = True
                    if status is None:
                        status = 128 - proc.returncode if proc.returncode < 0 else proc.returncode
                    until = min(time.monotonic() + ABSENCE_SECONDS, shutdown_end)
                    absent = observe_group_absence(proc.pid, until)
                    if not absent:
                        fail('released group present/reused; retained, no further signals')
                else:
                    fail('leader exit unknown; retained unreaped')
            except BaseException as error:
                fail('group ownership/termination unknown: ' + type(error).__name__ + ': ' + str(error))
            if not released:
                RETAINED_COMMANDS.append(proc)
        drained = selector is not None and not selector.get_map()
        if selector is not None:
            try:
                selector.close()
            except BaseException as error:
                fail('selector close: ' + str(error))
        if proc is not None:
            for pipe in (proc.stdout, proc.stderr):
                try:
                    if pipe is not None and not pipe.closed:
                        pipe.close()
                except BaseException as error:
                    fail('pipe close: ' + str(error))
        for stream in files:
            try:
                stream.close()
            except BaseException as error:
                fail('capture close: ' + str(error))
    if cancelled:
        fail('cancelled by signal ' + str(cancelled[0]))
    if not drained:
        fail('incomplete stream drain')
    if discarded:
        fail('output overflow')
    receipt = {'exit': status, 'drained': drained, 'discarded': discarded, 'streams': sizes,
               'failures': failures, 'group_absent': absent, 'leader_released': released,
               'session_peers': peers,
               'process_scope': 'owned session leader/group only; escaped groups are not absence-proven',
               'complete': not failures and status is not None and absent}
    raw = b''
    try:
        raw = read_private(LOGS / (lane + '.log'), evidence.OUTPUT_LIMIT)
        receipt.update({'stream_sha256': {
            suffix: hashlib.sha256(read_private(LOGS / (lane + '.' + suffix), evidence.OUTPUT_LIMIT)).hexdigest()
            for suffix in ('stdout', 'stderr')}, 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)})
    except BaseException as error:
        fail('capture hash: ' + str(error))
        receipt['complete'] = False
    if cancelled:
        fail('cancelled by signal ' + str(cancelled[0]))
        receipt['complete'] = False
    try:
        write_json(LOGS / (lane + '.result.json'), receipt)
    except BaseException as error:
        raise CommandFailure(status or 1, lane + ': incomplete capture receipt; ' + repr(failures)) from error
    if status or failures:
        raise CommandFailure(status or 1, lane + ': ' + '; '.join(failures))
    return raw if binary else raw.decode('utf-8')


class CommandFailure(Exception):
    def __init__(self, status, lane):
        self.status = status
        super().__init__(lane + ': exit ' + str(status))


def worktree_inventory(expected, allow_build=False):
    """Actual names, including ignored/untracked; only root Git administration is excluded."""
    paths, total = [], 0
    directories = {str(parent) for name in expected for parent in Path(name).parents if str(parent) != '.'}
    def walk(parent, relative=''):
        nonlocal total
        with os.scandir(parent) as scan:
            names = []
            for entry in scan:
                assert len(names) < 20000, 'source directory entry budget'
                names.append(entry.name)
        for name in sorted(names):
            path = relative + '/' + name if relative else name
            if path == '.git':
                # Checkout must have its actual administration here, not an arbitrary exclusion.
                st = os.stat(name, dir_fd=parent, follow_symlinks=False)
                assert stat.S_ISDIR(st.st_mode), 'unsupported Git administration binding'
                continue
            assert len(paths) < 20000 and len(Path(path).parts) <= 32, 'source inventory budget'
            before = os.stat(name, dir_fd=parent, follow_symlinks=False)
            record = {'path': path, 'metadata': evidence.export_metadata(before)}
            paths.append(record)
            if stat.S_ISDIR(before.st_mode):
                assert path in directories, 'unexpected source directory: ' + path
                fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
                try:
                    assert evidence.export_metadata(os.fstat(fd)) == record['metadata'], 'source directory drift: ' + path
                    walk(fd, path)
                    assert evidence.export_metadata(os.fstat(fd)) == record['metadata'], 'source directory drift: ' + path
                finally:
                    os.close(fd)
            else:
                artifact = allow_build and path == 'dotty' and path not in expected
                assert path in expected or artifact, 'unexpected source file: ' + path
                assert stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1, 'unsafe source file: ' + path
                limit = 64 * 1024**2 if artifact else 8 * 1024**2
                assert before.st_size <= limit, 'source file byte budget: ' + path
                fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
                try:
                    assert evidence.export_metadata(os.fstat(fd)) == record['metadata'], 'source file drift: ' + path
                    data = bytearray()
                    while True:
                        chunk = os.read(fd, min(65536, limit + 1 - len(data)))
                        if not chunk:
                            break
                        data.extend(chunk)
                        assert len(data) <= limit, 'source file byte budget: ' + path
                    total += len(data)
                    assert total <= 128 * 1024**2, 'source total byte budget'
                    assert len(data) == before.st_size and evidence.export_metadata(os.fstat(fd)) == record['metadata'], 'source file drift: ' + path
                finally:
                    os.close(fd)
                record['sha256'] = hashlib.sha256(data).hexdigest()
                if artifact:
                    assert before.st_mode & 0o111 and not before.st_mode & 0o7022, 'unsafe build artifact mode: ' + path
                    record['role'] = 'expected build artifact'
                else:
                    mode, oid = expected[path]
                    assert bool(before.st_mode & 0o111) == (mode == '100755'), 'source executable mode mismatch: ' + path
                    blob = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
                    assert blob == oid, 'source blob mismatch: ' + path
                    record.update({'git_mode': mode, 'git_blob': oid})
            assert evidence.export_metadata(os.stat(name, dir_fd=parent, follow_symlinks=False)) == record['metadata'], 'source name drift: ' + path
        with os.scandir(parent) as scan:
            after = []
            for entry in scan:
                assert len(after) < 20000, 'source directory entry budget'
                after.append(entry.name)
        assert sorted(after) == sorted(names), 'source namespace drift'
    fd = os.open(WORK, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        before = evidence.export_metadata(os.fstat(fd))
        assert before['uid'] == os.geteuid(), 'workspace owner mismatch'
        assert evidence.export_metadata(WORK.lstat()) == before, 'workspace name drift'
        walk(fd)
        assert evidence.export_metadata(os.fstat(fd)) == before and evidence.export_metadata(WORK.lstat()) == before, 'workspace binding drift'
    finally:
        os.close(fd)
    assert expected.keys() <= {record['path'] for record in paths}, 'missing tracked source'
    return paths


def source_record(label):
    """Read-only Git object inventory plus every checked-out blob/mode, no exclusions."""
    sha = os.environ['CANDIDATE_SHA']
    assert re.fullmatch('[0-9a-f]{40}', sha)
    head = command(label + '-head', ['git', '--no-replace-objects', 'rev-parse', 'HEAD']).strip()
    assert head == sha, 'checkout is not the requested PR head'
    raw = command(label + '-commit', ['git', '--no-replace-objects', 'cat-file', 'commit', sha]).encode()
    assert hashlib.sha1(b'commit ' + str(len(raw)).encode() + b'\0' + raw).hexdigest() == sha
    tree = raw.splitlines()[0].removeprefix(b'tree ').decode('ascii')
    listing = command(label + '-tree', ['git', '--no-replace-objects', 'ls-tree', '-rz', '--full-tree', sha])
    expected = {}
    for item in listing.rstrip('\0').split('\0'):
        header, name = item.split('\t', 1)
        mode, kind, oid = header.split()
        assert kind == 'blob' and mode in ('100644', '100755'), 'unsupported source object'
        assert not Path(name).is_absolute() and '..' not in Path(name).parts and name not in expected
        expected[name] = (mode, oid)
    administration = command(label + '-git-dir', ['git', '--no-replace-objects', 'rev-parse', '--absolute-git-dir']).strip()
    assert administration == str(WORK / '.git'), 'unsupported actual Git administration location'
    files = worktree_inventory(expected, allow_build=label == 'source-after')
    if label != 'source-before':
        baseline = json.loads(read_private(LOGS / 'source-before.json', evidence.OUTPUT_LIMIT))
        old = {item['path']: item for item in baseline['files']}
        for item in files:
            if item['path'] in expected:
                assert item['metadata']['mode'] == old[item['path']]['metadata']['mode'], 'source mode drift: ' + item['path']
    write_json(LOGS / (label + '.json'), {'commit': sha, 'tree': tree, 'files': files})


def workflow_record():
    """One public acquisition into this disposable checkout; fetched content is data only."""
    sha = os.environ['WORKFLOW_SHA']
    assert re.fullmatch('[0-9a-f]{40}', sha)
    assert os.environ['GITHUB_REPOSITORY'] == 'lmilojevicc/dotty'
    command('workflow-fetch', ['git', '--no-replace-objects',
                              '-c', 'credential.helper=', '-c', 'core.hooksPath=/dev/null',
                              '-c', 'gc.auto=0', '-c', 'maintenance.auto=false',
                              '-c', 'http.lowSpeedLimit=1', '-c', 'http.lowSpeedTime=30',
                              'fetch', '--no-auto-maintenance', '--no-tags', '--no-recurse-submodules', '--no-write-fetch-head',
                              'https://github.com/lmilojevicc/dotty.git', sha],
            {'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null',
             'GIT_TERMINAL_PROMPT': '0', 'GIT_ASKPASS': '/bin/false'})

    def obj(label, kind, oid):
        data = command(label, ['git', '--no-replace-objects', 'cat-file', kind, oid], binary=True)
        assert hashlib.sha1(kind.encode() + b' ' + str(len(data)).encode() + b'\0' + data).hexdigest() == oid
        return data

    commit = obj('workflow-commit', 'commit', sha)
    oid = commit.splitlines()[0].removeprefix(b'tree ').decode('ascii')
    tree = oid
    linkage = []
    for index, component in enumerate((b'.github', b'workflows', b'ci.yml')):
        raw = obj('workflow-tree-' + str(index), 'tree', oid)
        children = {}
        while raw:
            header, raw = raw.split(b'\0', 1)
            mode, name = header.split(b' ', 1)
            child_oid, raw = raw[:20], raw[20:]
            assert name not in children and len(child_oid) == 20
            children[name] = (mode.decode('ascii'), child_oid.hex())
        mode, child = children[component]
        assert mode == ('100644' if index == 2 else '40000'), 'workflow tree mode'
        linkage.append({'tree': oid, 'name': component.decode(), 'mode': mode, 'object': child})
        oid = child
    blob = obj('workflow-blob', 'blob', oid)
    write_json(LOGS / 'workflow-source.json', {'commit': sha, 'tree': tree, 'linkage': linkage,
                                              'blob': oid, 'sha256': hashlib.sha256(blob).hexdigest()})


def preflight():
    assert os.environ['RUNNER_ENVIRONMENT'] == 'github-hosted'
    assert platform.system() == 'Linux' and platform.machine() == 'aarch64'
    assert os.geteuid() == os.getuid() > 0
    for key, value in FIXED_ENV.items():
        assert os.environ.get(key) == value, 'incorrect acceptance environment: ' + key
    for key, name in PRIVATE.items():
        assert os.environ.get(key) == str(ROOT / name)
    assert shutil.which('dotty') is None, 'installed dotty discoverable'
    assert os.environ.get('ImageOS') and os.environ.get('ImageVersion'), 'unknown runner image'
    # Before installing tools or starting tests: actual test/source filesystems and capacity.
    capacity = []
    for index, path in enumerate((ROOT, ROOT / 'tmp', WORK)):
        filesystem = command('filesystem-' + str(index), ['stat', '-f', '-c', '%T', str(path)]).strip()
        assert filesystem in ('ext2/ext3', 'tmpfs'), 'unsupported filesystem: ' + filesystem
        fs = os.statvfs(path)
        available, inodes = fs.f_bavail * fs.f_frsize, fs.f_favail
        capacity.append({'path': str(path), 'filesystem': filesystem,
                         'available_bytes': available, 'available_inodes': inodes})
        write_json(LOGS / ('capacity-' + str(index) + '.json'), capacity[-1])
        assert available >= 8 * 1024**3 and inodes >= 100000, ('insufficient native CI headroom: ' + str(path)
            + '; available bytes=' + str(available) + ' inodes=' + str(inodes)
            + '; required bytes=' + str(8 * 1024**3) + ' inodes=100000')
    write_json(LOGS / 'capacity.json', capacity)
    # Observation only; no sudo, unshare, capability changes, or canonical UID anchor.
    command('process-status', ['cat', '/proc/self/status'])
    command('mounts', ['cat', '/proc/self/mountinfo'])
    command('reaping', ['/usr/bin/python3', '-B', '-I', '-c', REAP_PROBE])
    probe = ROOT / 'tmp/preflight-file'
    with PrivateOutput(probe, 64) as stream:
        stream.write(b'private native probe')
    st = probe.lstat()
    assert st.st_uid == os.geteuid() and stat.S_IMODE(st.st_mode) == 0o600
    for path, attribute in ((probe, 'system.posix_acl_access'),
                            (ROOT / 'tmp', 'system.posix_acl_default')):
        try:
            os.getxattr(path, attribute, follow_symlinks=False)
        except OSError as error:
            assert error.errno == errno.ENODATA, 'ACL unsupported/unknown'
        else:
            raise AssertionError('unexpected inherited ACL')
    with trusted_ancestry(ROOT / 'tmp') as anchors:
        require_bindings(anchors)
        parent = anchors[-1][2]
        assert evidence.export_metadata(os.stat('preflight-file', dir_fd=parent, follow_symlinks=False)) == evidence.export_metadata(st), 'probe drift'
        os.link('preflight-file', 'preflight-hardlink', src_dir_fd=parent, dst_dir_fd=parent, follow_symlinks=False)
        require_bindings(anchors)
        os.symlink('preflight-file', 'preflight-symlink', dir_fd=parent)
        require_bindings(anchors)
        os.mkfifo('preflight-fifo', 0o600, dir_fd=parent)
        require_bindings(anchors)
        assert os.stat('preflight-file', dir_fd=parent, follow_symlinks=False).st_nlink == 2
    write_json(LOGS / 'preflight.json', {'uid': os.geteuid(), 'private_environment': True,
                                       'acl_absence': 'ENODATA', 'reaping': 'ESRCH only',
                                       'network_isolated': False, 'cgo': '0'})


REAP_PROBE = '''import errno, os, time
r,w=os.pipe()
pid=os.fork()
if pid==0:
    os.close(r)
    child=os.fork()
    if child==0:
        os.close(w)
        time.sleep(0.2)
        os._exit(0)
    os.write(w,str(child).encode())
    os.close(w)
    os._exit(0)
os.close(w)
child=int(os.read(r,64));os.close(r)
assert os.waitpid(pid,0)==(pid,0)
for target in (pid,child):
    deadline=time.monotonic()+5
    while True:
        try: os.kill(target,0)
        except OSError as error:
            assert error.errno==errno.ESRCH
            print(str(target)+": ESRCH",flush=True)
            break
        assert time.monotonic()<deadline,"present/zombie/reused child is not absence"
        time.sleep(0.02)
'''


def diagnostic(text):
    try:
        print(text, file=sys.stderr)
    except Exception:
        print('INCOMPLETE private diagnostic; platform log: ' + text, file=sys.__stderr__)


def run():
    global RUN_DEADLINE
    RUN_DEADLINE = time.monotonic() + 75 * 60 - 8
    status = 1
    try:
        bind()
        source_record('source-before')
        workflow_record()
        source_record('source-after-workflow')
        preflight()
        command('install', ['mise', 'install'])
        mise_version = command('mise-version', ['mise', '--version'])
        assert re.match(r'^2026\.9\.1(?:[ +]|$)', mise_version)
        command('tool-inventory', ['mise', 'ls', '--json'])
        go = command('go-env', ['mise', 'exec', '--', 'go', 'env', '-json'])
        actual = json.loads(go)
        assert actual['GOVERSION'] == 'go1.26.6' and actual['CGO_ENABLED'] == '0'
        assert actual['GOOS'] == 'linux' and actual['GOARCH'] == 'arm64'
        assert actual['GOFLAGS'] == '' and actual['GOTOOLCHAIN'] == 'local'
        for lane, tool, args, expected in (
            ('yamlfmt-version', 'yamlfmt', ['-version'], '0.21.0'),
            ('golangci-version', 'golangci-lint', ['version'], '2.12.2'),
            ('goreleaser-version', 'goreleaser', ['--version'], '2.15.0'),
        ):
            output = command(lane, ['mise', 'exec', '--', tool] + args)
            assert re.search(r'(?<![0-9.])' + re.escape(expected) + r'(?=$|[\s,+)])', output), 'tool version mismatch'
        vuln_version = command('govulncheck-version', ['mise', 'exec', '--', 'govulncheck', '-version'])
        assert re.search(r'v1\.3\.0(?=$|[\s,+)])', vuln_version), 'govulncheck version mismatch'
        command('modules', ['mise', 'exec', '--', 'go', 'mod', 'download'])
        command('format', ['mise', 'run', 'fmt:check'])
        selections = [('protocol-' + name, './internal/tools/focused', '^TestFocusedProtocol' + name + '$')
                      for name in evidence.PROTOCOL_ORDER]
        selections += [('protocol-matrix', './internal/tools/focused', '^TestFocusedTaskProtocol$'),
                       ('protocol-cancellation', './internal/tools/focused', '^TestFocusedCancellation$'),
                       ('protocol-child', './internal/tools/focused', '^TestFocusedChild')]
        selections += [(lane, './internal/nativefs', '^' + test + '$/.') for lane, test in NATIVE]
        for lane, package, pattern in selections:
            text = command(lane, ['mise', 'run', 'test:focused', package, pattern])
            evidence.check_text(text, lane)
        assert os.environ['DOTTY_NATIVE_ACCEPTANCE'] == '1' and not os.environ['GOFLAGS']
        evidence.check_text(command('verify', ['mise', 'run', 'verify']), 'verify')
        for target in ('freebsd', 'windows'):
            command('compile-' + target, ['mise', 'exec', '--', 'go', 'test', '-c', '-o',
                                         str(ROOT / 'compile' / ('nativefs-' + target + '.test')),
                                         './internal/nativefs'], {'GOOS': target, 'GOARCH': 'arm64', 'CGO_ENABLED': '0'})
        command('vuln', ['mise', 'run', 'vuln'])
        source_record('source-after')
        status = 0
    except CommandFailure as error:
        status = error.status
        diagnostic(str(error))
    except Exception as error:
        diagnostic('REFUSE native CI: ' + str(error))
    finally:
        try:
            write_json(LOGS / 'main-status.json', {'exit': status, 'native_acceptance': False})
        except Exception as error:
            diagnostic('UNKNOWN main-status receipt: ' + str(error))
            status = status or 1
    return status


class CollectionDeadline(Exception):
    """Fatal deadline, deliberately not an OSError caught by per-entry retention."""


def capture_stage(label, operation):
    """No shell redirection: refusal leaves unsafe names untouched and logs missing."""
    status = 1
    try:
        bind()
        with PrivateOutput(LOGS / (label + '.stdout'), evidence.OUTPUT_LIMIT, text=True) as out, \
                PrivateOutput(LOGS / (label + '.stderr'), evidence.OUTPUT_LIMIT, text=True) as err:
            with redirect_stdout(out), redirect_stderr(err):
                try:
                    status = operation() or 0
                except CollectionDeadline:
                    raise
                except CommandFailure as error:
                    check_collection_deadline()
                    status = error.status
                    print('REFUSE ' + label + ': ' + str(error), file=sys.stderr)
                except Exception as error:
                    check_collection_deadline()
                    print('REFUSE ' + label + ': ' + str(error), file=sys.stderr)
                    status = 1
        check_collection_deadline()
        with PrivateOutput(LOGS / (label + '.exit'), 16, text=True) as stream:
            stream.write(str(status) + '\n')
    except CollectionDeadline:
        raise
    except Exception as error:
        check_collection_deadline()
        print('INCOMPLETE ' + label + ' capture/receipt; retained paths under ' + str(LOGS)
              + '; platform log only: ' + str(error), file=sys.stderr)
        status = status or 1
    return status


def upload_roles():
    """Exact filenames, roles and per-file bounds; no recursive evidence upload."""
    roles = {}
    for lane in COMMAND_LANES:
        for suffix in ('stdout', 'stderr', 'log'):
            roles[lane + '.' + suffix] = ('command stream', evidence.OUTPUT_LIMIT)
        roles[lane + '.command.json'] = ('command invocation', 16384)
        roles[lane + '.result.json'] = ('command receipt', 4096)
    for label in ('main', 'collector'):
        for suffix in ('stdout', 'stderr'):
            roles[label + '.' + suffix] = ('helper stream', evidence.OUTPUT_LIMIT)
        roles[label + '.exit'] = ('helper exit', 16)
    for name in ('binding', 'context', 'source-before', 'workflow-source', 'source-after-workflow',
                 'source-after', 'capacity', 'capacity-0', 'capacity-1', 'capacity-2', 'preflight',
                 'main-status', 'collection-checks'):
        roles[name + '.json'] = ('structured receipt', evidence.OUTPUT_LIMIT)
    roles['collection.json'] = ('collection diagnostic', evidence.EXPORT_LIMITS['wire'])
    roles['journal-inventory.jsonl'] = ('validated journal observations', evidence.EXPORT_LIMITS['wire'])
    roles['journals.tar'] = ('validated journal archive', evidence.EXPORT_LIMITS['wire'])
    return roles


UPLOAD_ENV = 'DOTTY_NATIVE_UPLOAD_PATH'


def publication_context():
    candidate, run_id, attempt = (os.environ[key] for key in ('CANDIDATE_SHA', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'))
    assert re.fullmatch('[0-9a-f]{40}', candidate), 'invalid publication candidate'
    assert re.fullmatch('[0-9]+', run_id) and re.fullmatch('[1-9][0-9]*', attempt), 'invalid publication run/attempt'
    return {'candidate': candidate, 'run': run_id, 'attempt': attempt,
            'root': str(ROOT), 'path': str(ROOT / 'upload')}


def publication_time(started, deadline):
    assert all(type(value) in (int, float) and math.isfinite(value) for value in (started, deadline)), 'invalid publication clock'
    assert deadline == started + 120 and started <= time.monotonic(), 'invalid publication clock origin'
    remaining = deadline - time.monotonic()
    assert remaining > 0, 'publication deadline expired'
    return remaining


def upload_inventory(expected_names, deadline=None):
    """Exact current output observations, including the manifest; never re-curation."""
    roles = upload_roles() | {'upload-manifest.json': ('upload manifest', evidence.EXPORT_LIMITS['wire'])}
    assert set(expected_names) <= set(roles), 'unexpected upload inventory name'
    records = {}
    def current_time():
        if deadline is not None and time.monotonic() >= deadline:
            raise CollectionDeadline('publication deadline expired')
    with trusted_ancestry(ROOT / 'upload') as anchors:
        require_bindings(anchors)
        def names():
            result = set()
            with os.scandir(anchors[-1][2]) as scan:
                for entry in scan:
                    current_time()
                    assert len(result) < len(roles), 'upload namespace budget'
                    result.add(entry.name)
            return result
        assert names() == set(expected_names), 'upload namespace drift'
        for name in sorted(expected_names):
            current_time()
            require_bindings(anchors)
            before = evidence.export_metadata(os.stat(name, dir_fd=anchors[-1][2], follow_symlinks=False))
            data = read_private(ROOT / 'upload' / name, roles[name][1])
            assert evidence.export_metadata(os.stat(name, dir_fd=anchors[-1][2], follow_symlinks=False)) == before, 'upload inventory leaf drift'
            require_bindings(anchors)
            records[name] = {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'metadata': before}
        assert names() == set(expected_names), 'upload namespace drift'
        require_bindings(anchors)
    return records


def curate_upload(deadline=None, started=None):
    """Copy only completed read observations; rejected leaves remain working-only.

    Current checks are not a permanent snapshot against later external writers.
    Missing manifest or namespace drift means incomplete curation, never acceptance.
    """
    if deadline is not None:
        publication_time(started, deadline)
    assert UPLOAD_ENV not in os.environ, 'preexisting upload publication marker'
    bind()
    roles = upload_roles()
    destination = ROOT / 'upload'
    create_private_directory('upload')  # Never adopt, truncate, or retry an old export.
    records, refusals = [], []
    expected = {}
    def within_deadline():
        if deadline is not None and time.monotonic() >= deadline:
            raise CollectionDeadline('curation deadline; upload manifest incomplete')
    def output_names(fd):
        result = set()
        with os.scandir(fd) as scan:
            for entry in scan:
                within_deadline()
                assert len(result) < len(roles) + 1, 'upload namespace budget'
                result.add(entry.name)
        return result
    with trusted_ancestry(LOGS) as inputs, trusted_ancestry(destination) as outputs:
        require_bindings(inputs)
        require_bindings(outputs)
        names = []
        with os.scandir(inputs[-1][2]) as scan:
            for entry in scan:
                within_deadline()
                assert len(names) < evidence.EXPORT_LIMITS['entries'], 'evidence namespace budget'
                names.append(entry.name)
        for name in sorted(names):
            within_deadline()
            try:
                require_bindings(inputs)
                assert name in roles, 'unexpected evidence filename'
                role, limit = roles[name]
                # No output is opened until ALL input read/name/ancestry checks pass.
                data = read_private(LOGS / name, limit)
                require_bindings(inputs)
                within_deadline()
            except (OSError, AssertionError) as error:
                refusals.append({'name': name, 'reason': str(error)})
                continue
            with PrivateOutput(destination / name, limit) as stream:
                stream.write(data)
            approved = {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
            assert {key: stream.closed_record[key] for key in approved} == approved, 'upload writer payload drift'
            expected[name] = stream.closed_record
            records.append({'name': name, 'role': role, **approved})
        require_bindings(inputs)
        with os.scandir(inputs[-1][2]) as scan:
            after = []
            for entry in scan:
                assert len(after) < evidence.EXPORT_LIMITS['entries'], 'evidence namespace budget'
                after.append(entry.name)
        if sorted(after) != sorted(names):
            refusals.append({'name': '.', 'reason': 'evidence namespace changed during curation'})
        # Independently re-observe the complete exported inventory before the manifest.
        for record in records:
            within_deadline()
            data = read_private(destination / record['name'], roles[record['name']][1])
            assert len(data) == record['bytes'] and hashlib.sha256(data).hexdigest() == record['sha256'], 'upload copy drift'
        require_bindings(outputs)
        assert output_names(outputs[-1][2]) == set(expected), 'upload namespace drift'
        within_deadline()
        expected['upload-manifest.json'] = write_json(destination / 'upload-manifest.json', {
            'files': records, 'refusals': refusals, 'complete': not refusals,
            'missing_allowed_names': sorted(set(roles) - set(names)),
            'inventory_excludes': ['upload-manifest.json'], 'native_acceptance': False,
            'boundary': 'validated observations only; later races or unavailable inputs require refusal'})
        require_bindings(outputs)
        assert output_names(outputs[-1][2]) == set(expected), 'upload namespace drift'
        assert upload_inventory(expected, deadline) == expected, 'upload writer baseline drift'
        # Readiness is separate from uploaded files and exists only after safe curation.
        # Rejected input leaves may produce a sound partial inventory, never their bytes.
        if deadline is not None:
            publication_time(started, deadline)
            write_json(ROOT / 'upload-ready.json', {
                'context': publication_context(), 'started': started, 'deadline': deadline,
                'directory': BOUND_DIRECTORIES[str(destination)], 'files': expected})
    return 1 if refusals else 0


def publish():
    """One-use publication eligibility, not acceptance or a permanent race barrier."""
    global COLLECTION_END
    bind()
    assert UPLOAD_ENV not in os.environ, 'preexisting upload publication marker'
    with trusted_ancestry(ROOT) as anchors:
        require_bindings(anchors)
        try:
            os.stat('upload-publication.json', dir_fd=anchors[-1][2], follow_symlinks=False)
        except FileNotFoundError:
            pass
        else:
            raise FileExistsError('publication already consumed; no retry')
    raw = read_private(ROOT / 'upload-ready.json', evidence.OUTPUT_LIMIT)
    ready = json.loads(raw)
    assert ready['context'] == publication_context(), 'publication candidate/run/attempt mismatch'
    remaining = publication_time(ready['started'], ready['deadline'])
    destination = str(ROOT / 'upload')
    assert destination not in BOUND_DIRECTORIES or BOUND_DIRECTORIES[destination] == ready['directory'], 'publication directory drift'
    BOUND_DIRECTORIES[destination] = ready['directory']
    def expired(signum, frame):
        raise CollectionDeadline('publication deadline expired')
    previous = signal.signal(signal.SIGALRM, expired)
    signal.setitimer(signal.ITIMER_REAL, remaining)  # Remaining ORIGINAL collector allowance only.
    prior_end, COLLECTION_END = COLLECTION_END, ready['deadline']
    try:
        assert upload_inventory(ready['files'], ready['deadline']) == ready['files'], 'publication inventory drift'
        publication_time(ready['started'], ready['deadline'])
        # Exclusive consumption precedes marker append. A failed attempt cannot be retried.
        write_json(ROOT / 'upload-publication.json', {
            'context': ready['context'], 'readiness_sha256': hashlib.sha256(raw).hexdigest(),
            'meaning': 'one-use publication attempt; success also requires the Actions step outcome'})
        publication_time(ready['started'], ready['deadline'])
        append_github_env({UPLOAD_ENV: destination}, require_empty=True)
        # Even a successfully appended marker cannot authorize a subsequently failed step.
        assert upload_inventory(ready['files'], ready['deadline']) == ready['files'], 'publication inventory drift'
        publication_time(ready['started'], ready['deadline'])
        return 0
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)
        COLLECTION_END = prior_end


def collect(capture=False):
    """120 seconds includes capture, semantics, final writes and upload curation."""
    global COLLECTION_END
    started = time.monotonic()
    end = started + 120
    prior_end, COLLECTION_END = COLLECTION_END, end
    def deadline(signum, frame):
        raise CollectionDeadline('journal collection deadline; partial evidence retained')
    previous = signal.signal(signal.SIGALRM, deadline)
    signal.alarm(120)
    try:
        if not capture:
            return collect_evidence()
        status = capture_stage('collector', collect_evidence)
        check_collection_deadline()
        try:
            curated = curate_upload(end, started)
            status = status or curated
        except CollectionDeadline:
            raise
        except Exception as error:
            check_collection_deadline()
            print('INCOMPLETE upload curation: ' + str(error), file=sys.stderr)
            status = status or 1
        return status
    finally:
        signal.alarm(0)
        signal.signal(signal.SIGALRM, previous)
        COLLECTION_END = prior_end


@contextmanager
def collection_archive(stream):
    archive = tarfile.open(fileobj=stream, mode='w', format=tarfile.PAX_FORMAT)
    try:
        yield archive
        check_collection_deadline()
        archive.close()  # Footer writes still pass through the deadline-checked writer.
    finally:
        archive.closed = True  # Exceptional unwinding/destruction must not add a footer.


def collect_evidence():
    binding = bind()
    logs, issues, entries, symlinks = [], [], [], []
    for lane in TEST_LANES:
        path = LOGS / (lane + '.log')
        try:
            logs.append((lane, read_private(path, evidence.OUTPUT_LIMIT).decode('utf-8')))
        except FileNotFoundError:
            continue
        except (OSError, UnicodeError, AssertionError) as error:
            issues.append(lane + ': ' + str(error))
    protocol_logs = [(lane, text) for lane, text in logs if lane in evidence.PROTOCOL_LANES + ['verify']]
    roots, attribution = evidence.diagnostic_root_roles(protocol_logs)
    issues.extend(attribution)
    total = 0
    exhausted = False
    hardlinks = {}
    limits = evidence.EXPORT_LIMITS

    def regular(parent, name, before):
        assert stat.S_ISREG(before.st_mode) and before.st_size <= limits['file'], 'nonregular/oversize journal'
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        try:
            assert evidence.export_metadata(os.fstat(fd)) == evidence.export_metadata(before)
            data = bytearray()
            while len(data) < before.st_size:
                check_collection_deadline()
                block = os.read(fd, min(65536, before.st_size - len(data)))
                check_collection_deadline()
                assert block, 'short journal read'
                data.extend(block)
            assert len(data) == before.st_size
            assert evidence.export_metadata(os.fstat(fd)) == evidence.export_metadata(before)
            return bytes(data)
        finally:
            os.close(fd)

    def directory(parent, name):
        before = os.stat(name, dir_fd=parent, follow_symlinks=False)
        fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        try:
            assert evidence.export_metadata(os.fstat(fd)) == evidence.export_metadata(before)
            return fd
        except BaseException:
            os.close(fd)
            raise

    def names(fd):
        result = []
        with os.scandir(fd) as scan:
            for entry in scan:
                assert len(result) < limits['entries'], 'directory entry budget'
                result.append(entry.name)
        return sorted(result)

    # Reopened components must match the ORIGINAL bind, not a new baseline.
    with trusted_ancestry(ROOT / 'tmp') as anchors:
        expected = binding['ancestry'] | {str(ROOT / name): value for name, value in binding['directories'].items()}
        def check_anchors():
            current = require_bindings(anchors)
            assert all(current[path] == value for path, value in expected.items() if path in current), 'collector original binding drift'
        check_anchors()
        tmp = anchors[-1][2]
        namespace = names(tmp)
        for name in namespace:
            if name.startswith('dotty-protocol-'):
                if not re.fullmatch('dotty-protocol-[0-9]+', name):
                    issues.append('malformed protocol root: ' + name)
                    continue
                path = str(ROOT / 'tmp' / name)
                if path not in roots:
                    roots[path] = {'case': None, 'lane': None, 'outcome': None,
                                   'removed': False, 'nested_fatal': False}
                    issues.append('undeclared root: ' + path)
        assert len(roots) <= limits['roots'], 'root budget'
        with PrivateOutput(LOGS / 'journal-inventory.jsonl', limits['wire'], text=True) as inventory, \
                PrivateOutput(LOGS / 'journals.tar', limits['wire']) as archive_stream, \
                collection_archive(archive_stream) as archive:
            wire = 0
            def emit(value):
                nonlocal wire
                text = json.dumps(value, sort_keys=True) + '\n'
                wire += len(text.encode('ascii'))
                assert wire <= limits['wire'], 'journal inventory wire budget'
                inventory.write(text)
                inventory.flush()

            nodes, directory_bindings, opened = [], [], []
            inode_names, directory_parents = {}, {}
            def inventory_node(parent, name, root, relative):
                assert len(nodes) < limits['entries'] and len(Path(relative).parts) <= limits['depth'], 'journal entry/depth budget'
                st = os.stat(name, dir_fd=parent, follow_symlinks=False)
                metadata = evidence.export_metadata(st)
                kind = {stat.S_IFREG: 'regular', stat.S_IFDIR: 'directory',
                        stat.S_IFLNK: 'symlink', stat.S_IFIFO: 'fifo'}.get(stat.S_IFMT(st.st_mode), 'unknown')
                record = {'root': root, 'path': relative, 'kind': kind, 'metadata': metadata, 'payload': None}
                # All observed names count against the bound, including policy refusals.
                nodes.append((parent, name, st, record, False))
                emit({'observed': record})
                assert st.st_uid == os.geteuid(), 'unexpected journal UID: ' + root + '/' + relative
                role = evidence.diagnostic_entry_role(roots[root]['case'], relative, kind) if relative else 'root'
                if not relative:
                    assert kind == 'directory' and stat.S_IMODE(st.st_mode) == 0o700, 'nonprivate journal root'
                assert kind != 'unknown', 'unsupported special object'
                nodes[-1] = (parent, name, st, record, True)
                if kind == 'regular':
                    inode_names.setdefault((st.st_dev, st.st_ino), []).append((parent, name, st, record))
                elif kind == 'directory' and role != 'negative-input':
                    child = directory(parent, name)
                    opened.append(child)
                    directory_parents[child] = (parent, name, metadata)
                    before_names = names(child)
                    directory_bindings.append((child, before_names, metadata))
                    for child_name in before_names:
                        child_relative = relative + '/' + child_name if relative else child_name
                        try:
                            inventory_node(child, child_name, root, child_relative)
                        except (OSError, AssertionError) as error:
                            issues.append(root + '/' + child_relative + ': ' + str(error))
                            emit({'refusal': issues[-1]})

            def revalidate_entry(parent, name, st):
                check_anchors()
                ancestor = parent
                while ancestor != tmp:
                    higher, component, metadata = directory_parents[ancestor]
                    assert evidence.export_metadata(os.fstat(ancestor)) == metadata, 'journal parent descriptor drift'
                    assert evidence.export_metadata(os.stat(component, dir_fd=higher, follow_symlinks=False)) == metadata, 'journal parent name drift'
                    ancestor = higher
                assert evidence.export_metadata(os.stat(name, dir_fd=parent, follow_symlinks=False)) == evidence.export_metadata(st), 'journal entry binding drift'

            def revalidate_payload(parent, name, st):
                revalidate_entry(parent, name, st)
                for alias_parent, alias_name, alias_st, _ in inode_names.get((st.st_dev, st.st_ino), []):
                    revalidate_entry(alias_parent, alias_name, alias_st)

            def revalidate_nodes():
                check_anchors()
                for parent, name, st, record, permitted in nodes:
                    current = os.stat(name, dir_fd=parent, follow_symlinks=False)
                    assert evidence.export_metadata(current) == record['metadata'], 'journal inventory binding drift: ' + record['root'] + '/' + record['path']
                for child, before_names, metadata in directory_bindings:
                    assert names(child) == before_names and evidence.export_metadata(os.fstat(child)) == metadata, 'journal directory drift'

            try:
                for root, role in roots.items():
                    emit({'root': root, 'role': role})
                    name = Path(root).name
                    try:
                        if name not in namespace and role['removed']:
                            continue
                        inventory_node(tmp, name, root, '')
                    except (OSError, AssertionError) as error:
                        issues.append(root + ': ' + str(error))
                        emit({'refusal': issues[-1]})
                # Complete permitted-name counts BEFORE any regular inode payload read.
                approved = set()
                for key, aliases in inode_names.items():
                    first = aliases[0][2]
                    if len(aliases) == first.st_nlink and all(evidence.export_metadata(st) == evidence.export_metadata(first) for _, _, st, _ in aliases):
                        approved.add(key)
                    else:
                        paths = [record['root'] + '/' + record['path'] for _, _, _, record in aliases]
                        issues.append('unresolved journal hardlinks: ' + repr(paths) + '; permitted names=' + str(len(aliases)) + '; nlink=' + str(first.st_nlink))
                        emit({'refusal': issues[-1]})
                revalidate_nodes()
                for parent, name, st, record, permitted in nodes:
                    if not permitted:
                        continue
                    root, relative, kind = record['root'], record['path'], record['kind']
                    member = tarfile.TarInfo('journals/' + Path(root).name + ('/' + relative if relative else ''))
                    member.mode, member.uid, member.gid = stat.S_IMODE(st.st_mode), st.st_uid, st.st_gid
                    member.mtime = st.st_mtime
                    payload = None
                    try:
                        revalidate_payload(parent, name, st)
                        if kind == 'regular':
                            key = (st.st_dev, st.st_ino)
                            if key not in approved:
                                continue  # Metadata/refusal ONLY, including inventory payload fields.
                            # The complete permitted alias set and ancestry were checked above.
                            if exhausted or st.st_size > limits['bytes'] - total:
                                exhausted = True
                                raise AssertionError('journal total byte budget')
                            assert st.st_size <= limits['file'], 'nonregular/oversize journal'
                            total += st.st_size  # Reserve before open; failed reads do not refund.
                            payload = regular(parent, name, st)
                            revalidate_payload(parent, name, st)  # Every alias and complete ancestry.
                            record['payload'] = evidence.export_encoded(payload)
                            if key in hardlinks:
                                previous, previous_metadata, previous_payload = hardlinks[key]
                                assert previous_metadata == record['metadata'] and previous_payload == payload, 'hardlink drift'
                                member.type, member.linkname = tarfile.LNKTYPE, previous
                            else:
                                hardlinks[key] = (member.name, record['metadata'], payload)
                                member.size = len(payload)
                        elif kind == 'directory':
                            member.type = tarfile.DIRTYPE
                        elif kind == 'fifo':
                            member.type = tarfile.FIFOTYPE
                        elif kind == 'symlink':
                            target = os.readlink(name, dir_fd=parent)
                            assert len(os.fsencode(target)) <= 4096, 'symlink text budget'
                            revalidate_payload(parent, name, st)  # No raced text reaches JSON or tar before this check.
                            member.type, member.linkname = tarfile.SYMTYPE, target
                            symlinks.append({'root': root, 'path': relative, 'target': target})
                            emit({'symlink': symlinks[-1]})
                        archive.addfile(member, io.BytesIO(payload) if member.isreg() else None)
                        entries.append(record)
                        emit({'captured': record})
                        assert evidence.export_metadata(os.stat(name, dir_fd=parent, follow_symlinks=False)) == record['metadata'], 'entry binding drift'
                    except (OSError, AssertionError) as error:
                        issues.append(root + '/' + relative + ': ' + str(error))
                        emit({'refusal': issues[-1]})
                revalidate_nodes()
            finally:
                for child in reversed(opened):
                    os.close(child)
            assert names(tmp) == namespace, 'temporary namespace drift'
            check_anchors()
    # Report missing individual records even when the native command itself failed.
    missing = []
    for root, role in roots.items():
        if role['removed']:
            continue
        try:
            assert role['case'] is not None, 'unknown owner; required records unknown'
            required = evidence.protocol_required_records(role)
            present = {entry['path'] for entry in entries if entry['root'] == root}
            missing.extend({'root': root, 'record': name} for name in sorted(required - present))
        except (AssertionError, TypeError) as error:
            issues.append(root + ': ' + str(error))
    # Diagnostic inventory is already retained. Strict acceptance cannot prevent its capture.
    write_json(LOGS / 'collection.json', {'issues': issues, 'roots': roots,
                                        'missing_records': missing,
                                        'missing_test_lanes': [lane for lane in TEST_LANES if lane not in dict(logs)],
                                        'entries': len(entries), 'native_acceptance': False})
    assert not missing, 'missing individual required journals'
    assert not issues, 'incomplete/ambiguous journal inventory'
    assert json.loads(read_private(LOGS / 'main-status.json', 1024))['exit'] == 0, 'main failed or incomplete'
    assert read_private(LOGS / 'main.exit', 16) == b'0\n', 'main shell failed or incomplete'
    read_private(LOGS / 'main.stdout', evidence.OUTPUT_LIMIT)
    read_private(LOGS / 'main.stderr', evidence.OUTPUT_LIMIT)
    assert [lane for lane, _ in logs] == TEST_LANES, 'missing required test lanes'
    for filename in ('binding.json', 'context.json', 'source-before.json', 'workflow-source.json',
                     'source-after-workflow.json', 'source-after.json', 'capacity.json', 'preflight.json'):
        json.loads(read_private(LOGS / filename, evidence.OUTPUT_LIMIT))
    for lane in COMMAND_LANES:
        json.loads(read_private(LOGS / (lane + '.command.json'), 16384))
        receipt = json.loads(read_private(LOGS / (lane + '.result.json'), 4096))
        assert receipt['exit'] == receipt['discarded'] == 0 and receipt['drained'] is True and receipt['complete'] is True, 'incomplete command receipt: ' + lane
        raw = read_private(LOGS / (lane + '.log'), evidence.OUTPUT_LIMIT)
        assert len(raw) == receipt['bytes'] and hashlib.sha256(raw).hexdigest() == receipt['sha256'], 'command log receipt mismatch: ' + lane
        for index, suffix in enumerate(('stdout', 'stderr')):
            raw = read_private(LOGS / (lane + '.' + suffix), evidence.OUTPUT_LIMIT)
            assert len(raw) == receipt['streams'][index], 'command stream size mismatch: ' + lane + '.' + suffix
            assert hashlib.sha256(raw).hexdigest() == receipt['stream_sha256'][suffix], 'command stream hash mismatch: ' + lane + '.' + suffix
    for lane, text in logs:
        receipt = json.loads(read_private(LOGS / (lane + '.result.json'), 4096))
        assert receipt['exit'] == receipt['discarded'] == 0 and receipt['drained']
        assert receipt['sha256'] == hashlib.sha256(text.encode()).hexdigest()
        evidence.check_text(text, lane)
    strict_roots = evidence.protocol_root_roles(protocol_logs)
    assert roots == strict_roots, 'strict/diagnostic root mismatch'
    evidence.check_export_records(strict_roots, entries)
    evidence.check_fatal_owner_witnesses(strict_roots, entries)
    write_json(LOGS / 'collection-checks.json', {'complete': True, 'native_acceptance': False,
                                              'requires': 'download and independent exact-candidate artifact audit'})
    return 0


def dispatch_cli(stage):
    try:
        result = {'prepare': prepare, 'run': lambda: capture_stage('main', run),
                  'collect': lambda: collect(capture=True), 'publish': publish}[stage]()
    except CollectionDeadline:
        return 1  # Collection has restored its clock; do not start another diagnostic.
    except Exception as error:
        print('REFUSE ' + stage + ': ' + str(error), file=sys.stderr)
        result = 1
    return result or 0


if __name__ == '__main__':
    os.umask(0o077)
    assert len(sys.argv) == 2 and sys.argv[1] in ('prepare', 'run', 'collect', 'publish')
    sys.exit(dispatch_cli(sys.argv[1]))
