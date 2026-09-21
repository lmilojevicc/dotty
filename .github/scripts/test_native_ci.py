"""Prepared collector/capture regressions, UNRUN. Private test roots are retained."""
import importlib.util
from contextlib import ExitStack
import io
from types import SimpleNamespace
import errno
import hashlib
import time
import json
import os
import platform
from pathlib import Path
import signal
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

assert sys.version_info >= (3, 9), 'Python 3.9+ required'
sys.dont_write_bytecode = True


def load_source(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def linux_requirement():
    if platform.system() != 'Linux':
        return 'native CI supervisor/ACL regressions require Linux; portable parser tests remain enabled'
    if os.geteuid() == 0 or not all(hasattr(os, name) for name in ('waitid', 'WNOWAIT', 'getxattr')) or not Path('/proc/self/stat').is_file():
        return 'native CI regressions require nonroot Linux, waitid/WNOWAIT, POSIX ACL observation and /proc'
    return None


class NativeCITests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        reason = linux_requirement()
        if reason:
            if os.environ.get('DOTTY_NATIVE_ACCEPTANCE') == '1':
                raise RuntimeError('unsupported native acceptance: ' + reason)
            raise unittest.SkipTest(reason)  # Before any test setup or private-root creation.

    def setUp(self):
        # No cleanup: failures retain exactly this test-owned evidence for inspection.
        self.parent = Path(tempfile.mkdtemp(prefix='dotty-ci-collector-test-')).resolve()
        self.addCleanup(print, 'Retained CI collector test root:', self.parent)
        self.environment = mock.patch.dict(os.environ, {
            'RUNNER_TEMP': str(self.parent), 'GITHUB_RUN_ID': '1', 'GITHUB_RUN_ATTEMPT': '1',
            'GITHUB_WORKSPACE': str(self.parent / 'work'), 'CANDIDATE_SHA': '1' * 40,
        })
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.module = load_source('native_ci_test_subject', 'native_ci.py')
        self.module.evidence = load_source('native_evidence_test_subject', 'native_evidence.py')
        m = self.module
        # Public test-host ancestry is not claimed safe. Exercise actual authority
        # within this test-owned subtree, never chmod /tmp or a hosted parent.
        policy = m.ancestor_policy
        def private_policy(fd, path, operation, endpoint):
            if path.is_relative_to(self.parent):
                policy(fd, path, operation, endpoint)
        self.policy = mock.patch.object(m, 'ancestor_policy', private_policy)
        self.policy.start()
        self.addCleanup(self.policy.stop)
        self.ancestry = m.create_root()
        m.TERM_GRACE, m.KILL_GRACE, m.DRAIN_SECONDS, m.ABSENCE_SECONDS = 0.05, 0.5, 0.1, 0.5
        m.WORK.mkdir(mode=0o700)
        names = list(m.PRIVATE.values()) + ['evidence', 'compile']
        for name in names:
            m.create_private_directory(name)
        private = {key: str(m.ROOT / name) for key, name in m.PRIVATE.items()}
        private.update({'MISE_GLOBAL_CONFIG_FILE': str(m.ROOT / 'mise-config/config.toml'),
                        'MISE_SYSTEM_CONFIG_FILE': str(m.ROOT / 'mise-config/system.toml'),
                        'MISE_TRUSTED_CONFIG_PATHS': str(m.WORK / 'mise.toml')})
        isolation = mock.patch.dict(os.environ, private)
        isolation.start()
        self.addCleanup(isolation.stop)
        temp_cache = mock.patch.object(tempfile, 'tempdir', str(m.ROOT / 'tmp'))
        temp_cache.start()
        self.addCleanup(temp_cache.stop)
        m.write_json(m.ROOT / 'binding.json', {
            'root': str(m.ROOT), 'uid': os.geteuid(), 'ancestry': self.ancestry,
            'directories': {name: m.identity(m.ROOT / name) for name in ['.'] + names},
        })
        self.previous_alarm = signal.getsignal(signal.SIGALRM)
        self.addCleanup(signal.signal, signal.SIGALRM, self.previous_alarm)

    def test_reader_refuses_symlink_fifo_and_oversize_without_following(self):
        m = self.module
        target = m.ROOT / 'tmp/target'
        target.write_bytes(b'unchanged')
        link = m.ROOT / 'tmp/link'
        link.symlink_to(target)
        fifo = m.ROOT / 'tmp/fifo'
        os.mkfifo(fifo, 0o600)
        self.assertEqual(m.read_private(target, 32), b'unchanged')
        for path, bound in ((link, 32), (fifo, 32), (target, 1)):
            with self.subTest(path=path):
                with self.assertRaisesRegex(AssertionError, '^unsafe evidence file$'):
                    m.read_private(path, bound)
        self.assertEqual(target.read_bytes(), b'unchanged')

    def test_complete_nonzero_command_has_raw_streams_and_original_exit(self):
        m = self.module
        with self.assertRaises(m.CommandFailure) as raised:
            m.command('exit7', [sys.executable, '-B', '-I', '-c',
                               'import sys; print("out"); print("err",file=sys.stderr); sys.exit(7)'])
        self.assertEqual(raised.exception.status, 7)
        receipt = json.loads((m.LOGS / 'exit7.result.json').read_text())
        self.assertEqual(receipt['exit'], 7)
        self.assertTrue(receipt['drained'])
        self.assertEqual((m.LOGS / 'exit7.stdout').read_bytes(), b'out\n')
        self.assertEqual((m.LOGS / 'exit7.stderr').read_bytes(), b'err\n')

    def test_receipt_failure_does_not_replace_command_failure(self):
        m = self.module
        original = m.write_json
        def fail_receipt(path, value):
            if path.name.endswith('.result.json'):
                raise OSError('injected receipt failure')
            original(path, value)
        for status in (0, 7):
            with self.subTest(status=status), mock.patch.object(m, 'write_json', fail_receipt):
                with self.assertRaises(m.CommandFailure) as raised:
                    m.command('receipt-' + str(status), [sys.executable, '-B', '-I', '-c',
                                                        'raise SystemExit(' + str(status) + ')'])
                self.assertEqual(raised.exception.status, status or 1)

    def test_failed_partial_lane_still_archives_journals_and_special_metadata(self):
        m = self.module
        root = m.ROOT / 'tmp/dotty-protocol-1'
        root.mkdir(mode=0o700)
        (root / 'record').write_bytes(b'kept')
        os.link(root / 'record', root / 'partial')
        (root / 'directory').mkdir(mode=0o700)
        os.mkfifo(root / 'FIFO', 0o600)
        (root / 'symlink').symlink_to(str(root / 'record'))
        raw = ('=== RUN   TestFocusedProtocolPublication\n'
               '    task_unix_test.go:1: private protocol evidence: ' + str(root) + '\n')
        (m.LOGS / 'protocol-Publication.log').write_text(raw)
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        with self.assertRaisesRegex(AssertionError, 'incomplete/ambiguous'):
            m.collect()
        self.assertEqual((m.LOGS / 'protocol-Publication.log').read_text(), raw)
        with tarfile.open(m.LOGS / 'journals.tar', 'r:') as archive:
            members = {member.name: member for member in archive.getmembers()}
        prefix = 'journals/dotty-protocol-1/'
        self.assertTrue(members[prefix + 'FIFO'].isfifo())
        self.assertTrue(members[prefix + 'symlink'].issym())
        self.assertEqual(members[prefix + 'symlink'].linkname, str(root / 'record'))
        self.assertTrue(members[prefix + 'record'].islnk() or members[prefix + 'partial'].islnk())
        self.assertEqual(members['journals/dotty-protocol-1'].mode, 0o700)
        self.assertTrue(json.loads((m.LOGS / 'collection.json').read_text())['issues'])
        self.assertEqual(json.loads((m.LOGS / 'main-status.json').read_text())['exit'], 7)

    def test_reader_name_replacement_refuses_after_positive(self):
        m = self.module
        target = m.ROOT / 'tmp/target'
        target.write_bytes(b'original')
        self.assertEqual(m.read_private(target, 32), b'original')
        replacement = m.ROOT / 'tmp/replacement'
        replacement.write_bytes(b'foreign')
        original = os.open
        def replaced(name, flags, *args, **kwargs):
            fd = original(name, flags, *args, **kwargs)
            if name == 'target':
                os.rename(replacement, target)
            return fd
        with mock.patch.object(m.os, 'open', replaced):
            with self.assertRaisesRegex(AssertionError, '^evidence (descriptor|name) drift$'):
                m.read_private(target, 32)

    def source_seed(self):
        m = self.module
        (m.WORK / '.git').mkdir(mode=0o700)
        (m.WORK / 'internal').mkdir(mode=0o700)
        target = m.WORK / 'internal/example.go'
        target.write_bytes(b'package example\n')
        target.chmod(0o600)
        data = target.read_bytes()
        expected = {'internal/example.go': ('100644', hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest())}
        self.assertEqual(len(m.worktree_inventory(expected)), 2)
        return target, expected

    def test_source_extra_go_and_test_sources_are_not_hidden(self):
        target, expected = self.source_seed()
        m = self.module
        # Separate nested directories are not excluded because an ignore rule could match.
        (m.WORK / 'internal/extra_test.go').write_bytes(b'package example')
        with self.assertRaisesRegex(AssertionError, 'unexpected source file: internal/extra_test.go'):
            m.worktree_inventory(expected)

    def test_source_ignored_extra_regular_refuses_after_positive(self):
        target, expected = self.source_seed()
        m = self.module
        ignore = b'/.ignored-source.go\n'
        (m.WORK / '.gitignore').write_bytes(ignore)
        expected['.gitignore'] = ('100644', hashlib.sha1(b'blob ' + str(len(ignore)).encode() + b'\0' + ignore).hexdigest())
        m.worktree_inventory(expected)
        (m.WORK / '.ignored-source.go').write_bytes(b'package example')
        with self.assertRaisesRegex(AssertionError, 'unexpected source file: .ignored-source.go'):
            m.worktree_inventory(expected, allow_build=True)

    def test_source_tracked_blob_and_mode_mutations_refuse(self):
        target, expected = self.source_seed()
        target.chmod(0o700)
        with self.assertRaisesRegex(AssertionError, 'source executable mode mismatch: internal/example.go'):
            self.module.worktree_inventory(expected)
        target.chmod(0o600)
        target.write_bytes(b'package changed')
        with self.assertRaisesRegex(AssertionError, 'source blob mismatch: internal/example.go'):
            self.module.worktree_inventory(expected)

    def test_source_only_postbuild_owned_root_artifact_is_allowed(self):
        target, expected = self.source_seed()
        m = self.module
        artifact = m.WORK / 'dotty'
        artifact.write_bytes(b'synthetic build artifact')
        artifact.chmod(0o700)
        with self.assertRaisesRegex(AssertionError, 'unexpected source file: dotty'):
            m.worktree_inventory(expected)
        records = m.worktree_inventory(expected, allow_build=True)
        self.assertEqual(next(item for item in records if item['path'] == 'dotty')['role'], 'expected build artifact')
        os.link(artifact, m.ROOT / 'tmp/artifact-alias')
        with self.assertRaisesRegex(AssertionError, 'unsafe source file: dotty'):
            m.worktree_inventory(expected, allow_build=True)

    def test_root_unsafe_parent_refuses_without_creation(self):
        m = self.module
        parent = self.parent / 'unsafe'
        parent.mkdir(mode=0o700)
        root = parent / 'new'
        with mock.patch.object(m, 'ROOT', root):
            with mock.patch.object(m, 'ROOT', parent / 'new'), m.trusted_ancestry(parent, 'create-root') as anchors:
                self.assertTrue(m.revalidate_ancestry(anchors, 'create-root'))
            parent.chmod(0o770)  # Only the test-owned negative fixture changes mode.
            with self.assertRaisesRegex(AssertionError, 'writable ancestor: ' + str(parent)):
                m.create_root()
        self.assertFalse(root.exists())

    def test_root_ancestor_replacement_refuses_before_mkdir(self):
        m = self.module
        parent = self.parent / 'replaceable'
        parent.mkdir(mode=0o700)
        with mock.patch.object(m, 'ROOT', parent / 'new'), m.trusted_ancestry(parent, 'create-root') as anchors:
            m.revalidate_ancestry(anchors, 'create-root')
        original, calls = m.revalidate_ancestry, []
        def replaced(anchors, operation):
            calls.append(True)
            if len(calls) == 2:
                parent.rename(self.parent / 'retained-original')
                parent.mkdir(mode=0o700)
            return original(anchors, operation)
        with mock.patch.object(m, 'ROOT', parent / 'new'), mock.patch.object(m, 'revalidate_ancestry', replaced):
            with self.assertRaisesRegex(AssertionError, 'ancestor binding drift: ' + str(parent)):
                m.create_root()
        self.assertFalse((parent / 'new').exists())
        self.assertFalse((self.parent / 'retained-original/new').exists())

    def test_ancestor_acl_and_owner_unknown_refuse_after_positive(self):
        m = self.module
        fd = os.open(self.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            m.ancestor_policy(fd, self.parent, 'create-root', m.ROOT.parent)
            foreign = mock.Mock(st_mode=os.fstat(fd).st_mode, st_uid=os.geteuid() + 1)
            with mock.patch.object(m.os, 'fstat', return_value=foreign):
                with self.assertRaisesRegex(AssertionError, 'unsafe ancestor owner:'):
                    m.ancestor_policy(fd, self.parent, 'create-root', m.ROOT.parent)
            with mock.patch.object(m.os, 'getxattr', return_value=b'ACL'):
                with self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                    m.ancestor_policy(fd, self.parent, 'create-root', m.ROOT.parent)
            with mock.patch.object(m.os, 'getxattr', side_effect=OSError(errno.ENOTSUP, 'unsupported')):
                with self.assertRaisesRegex(AssertionError, 'unsupported/unknown ancestor ACL:'):
                    m.ancestor_policy(fd, self.parent, 'create-root', m.ROOT.parent)
        finally:
            os.close(fd)

    def capture_failure(self, lane, code, diagnostic, **kwargs):
        m = self.module
        started = time.monotonic()
        with self.assertRaisesRegex(m.CommandFailure, diagnostic):
            m.command(lane, [sys.executable, '-B', '-I', '-c', code], **kwargs)
        self.assertLess(time.monotonic() - started, 5)
        receipt = json.loads((m.LOGS / (lane + '.result.json')).read_text())
        self.assertFalse(receipt['complete'])
        self.assertTrue(receipt['failures'])
        return receipt

    def test_actual_capture_overflow_without_eof_is_bounded(self):
        m = self.module
        with mock.patch.object(m.evidence, 'OUTPUT_LIMIT', 64):
            receipt = self.capture_failure('overflow', 'import os,time; os.write(1,b"x"*4096); time.sleep(1)', 'output overflow')
        self.assertGreater(receipt['discarded'], 0)
        self.assertEqual((m.LOGS / 'overflow.stdout').stat().st_size, 64)

    def test_actual_capture_retained_pipe_has_drain_deadline(self):
        self.capture_failure('pipe', 'import os,time; p=os.fork(); time.sleep(1) if p==0 else None; os._exit(0)', 'stream drain deadline')

    def test_actual_command_deadline_escalates_term_ignoring_child(self):
        self.capture_failure('workflow-fetch', 'import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(1)', 'command deadline', seconds=0.1)

    def test_actual_capture_interruption_retains_incomplete_receipt(self):
        m = self.module
        selector = m.selectors.DefaultSelector
        def interrupted_selector():
            instance = selector()
            original = instance.select
            once = [True]
            def select(timeout=None):
                if once[0]:
                    once[0] = False
                    raise KeyboardInterrupt('injected interruption')
                return original(timeout)
            instance.select = select
            return instance
        with mock.patch.object(m.selectors, 'DefaultSelector', interrupted_selector):
            self.capture_failure('interrupted', 'import time; print("kept",flush=True); time.sleep(1)', 'KeyboardInterrupt')

    def test_actual_capture_store_failure_is_bounded(self):
        m = self.module
        original = m.PrivateOutput.write
        def broken(stream, data):
            if stream.path.name == 'store.log':
                raise OSError('injected store failure')
            return original(stream, data)
        with mock.patch.object(m.PrivateOutput, 'write', broken):
            self.capture_failure('store', 'import time; print("kept",flush=True); time.sleep(1)', 'injected store failure')

    def test_no_historical_group_signals_after_release_or_unknown_wait(self):
        m = self.module
        proc = mock.Mock(pid=12345, returncode=None)
        observation = mock.Mock(si_pid=12345, si_status=0, si_code=os.CLD_EXITED)
        with mock.patch.object(m.os, 'waitid', return_value=observation), \
                mock.patch.object(m.os, 'getsid', return_value=12345), \
                mock.patch.object(m.os, 'getpgid', return_value=12345), \
                mock.patch.object(m.os, 'killpg') as kill:
            m.signal_owned(proc, signal.SIGTERM)
            kill.assert_called_once_with(12345, signal.SIGTERM)
            kill.reset_mock()
            proc.returncode = 0
            with self.assertRaisesRegex(AssertionError, 'command group ownership unknown'):
                m.signal_owned(proc, signal.SIGKILL)
            kill.assert_not_called()
        with mock.patch.object(m.os, 'waitid', side_effect=ChildProcessError(errno.ECHILD, 'released')), \
                mock.patch.object(m.os, 'killpg') as kill:
            with self.assertRaises(ChildProcessError):
                m.signal_owned(proc, signal.SIGKILL)
            kill.assert_not_called()
        with mock.patch.object(m.os, 'killpg', side_effect=OSError(errno.ESRCH, 'absent')) as kill:
            self.assertTrue(m.group_absent(12345))
            kill.assert_called_once_with(12345, 0)
        with mock.patch.object(m.os, 'killpg', side_effect=OSError(errno.EPERM, 'unknown')):
            with self.assertRaisesRegex(AssertionError, 'command group absence unknown'):
                m.group_absent(12345)

    def test_actual_supervisor_last_group_signal_precedes_leader_release(self):
        m = self.module
        events = []
        killpg, waitpid = os.killpg, os.waitpid
        def kill(group, number):
            events.append(('signal', group, number))
            return killpg(group, number)
        def wait(pid, flags):
            events.append(('wait', pid, flags))
            return waitpid(pid, flags)
        with mock.patch.object(m.os, 'killpg', kill), mock.patch.object(m.os, 'waitpid', wait):
            self.assertEqual(m.command('owned-order', [sys.executable, '-B', '-I', '-c', 'print("bounded")']), 'bounded\n')
        release = next(index for index, event in enumerate(events) if event[0] == 'wait')
        mutations = [event[2] for event in events[:release] if event[0] == 'signal']
        self.assertEqual(mutations, [signal.SIGTERM, signal.SIGKILL])
        self.assertTrue(all(event[0] == 'signal' and event[2] == 0 for event in events[release + 1:]))

    def test_actual_escaped_pipe_is_not_silently_killed_or_complete(self):
        receipt = self.capture_failure('escaped',
            'import os,time; p=os.fork(); '
            '(os.setsid(),time.sleep(1),os._exit(0)) if p==0 else os._exit(0)',
            'stream drain deadline')
        self.assertIn('escaped groups are not absence-proven', receipt['process_scope'])
        self.assertFalse(receipt['complete'])

    def collector_seed(self):
        m = self.module
        root = m.ROOT / 'tmp/dotty-protocol-1'
        root.mkdir(mode=0o700)
        (root / 'start-failure').write_bytes(b'retained record')
        lane = 'protocol-StartFailure'
        m.TEST_LANES = [lane]
        m.COMMAND_LANES = [lane]
        text = ('=== RUN   TestFocusedProtocolStartFailure\n'
                '    task_unix_test.go:4164: private protocol evidence: ' + str(root) + '\n'
                '--- PASS: TestFocusedProtocolStartFailure (0.00s)\n')
        raw = text.encode()
        for suffix, data in (('log', raw), ('stdout', raw), ('stderr', b'')):
            (m.LOGS / (lane + '.' + suffix)).write_bytes(data)
        m.write_json(m.LOGS / (lane + '.command.json'), {'argv': ['synthetic']})
        m.write_json(m.LOGS / (lane + '.result.json'), {
            'exit': 0, 'discarded': 0, 'drained': True, 'complete': True,
            'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(), 'streams': [len(raw), 0],
            'stream_sha256': {'stdout': hashlib.sha256(raw).hexdigest(), 'stderr': hashlib.sha256(b'').hexdigest()}})
        for filename in ('binding.json', 'context.json', 'source-before.json', 'workflow-source.json',
                         'source-after-workflow.json', 'source-after.json', 'capacity.json', 'preflight.json'):
            m.write_json(m.LOGS / filename, {})
        m.write_json(m.LOGS / 'main-status.json', {'exit': 0})
        (m.LOGS / 'main.exit').write_bytes(b'0\n')
        (m.LOGS / 'main.stdout').write_bytes(b'')
        (m.LOGS / 'main.stderr').write_bytes(b'')
        # Isolate collector consumer wiring, not a fabricated full native run.
        # Declaration attribution, required-record/export validation remain REAL.
        parser = mock.patch.object(m.evidence, 'check_text')
        parser.start()
        self.addCleanup(parser.stop)
        return root, lane

    def assert_retained(self, member='start-failure'):
        m = self.module
        self.assertTrue((m.LOGS / 'journal-inventory.jsonl').read_text())
        with tarfile.open(m.LOGS / 'journals.tar', 'r:') as archive:
            self.assertIn('journals/dotty-protocol-1/' + member, archive.getnames())
        self.assertFalse((m.LOGS / 'collection-checks.json').exists())

    def test_collector_positive_complete_seed_reaches_all_consumers(self):
        root, lane = self.collector_seed()
        self.assertEqual(self.module.collect(), 0)
        self.module.evidence.check_text.assert_called_once()
        self.assertTrue(json.loads((self.module.LOGS / 'collection-checks.json').read_text())['complete'])

    def test_collector_missing_record_refuses_after_preservation(self):
        root, lane = self.collector_seed()
        (root / 'start-failure').rename(root / 'record')
        with self.assertRaisesRegex(AssertionError, '^missing individual required journals$'):
            self.module.collect()
        self.assert_retained('record')

    def test_collector_missing_lane_refuses_after_preservation(self):
        self.collector_seed()
        self.module.TEST_LANES.append('regular-focused')
        with self.assertRaisesRegex(AssertionError, '^missing required test lanes$'):
            self.module.collect()
        self.assert_retained()

    def test_collector_invalid_receipt_refuses_after_preservation(self):
        root, lane = self.collector_seed()
        path = self.module.LOGS / (lane + '.result.json')
        receipt = json.loads(path.read_text())
        receipt['complete'] = False
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(AssertionError, '^incomplete command receipt: ' + lane + '$'):
            self.module.collect()
        self.assert_retained()

    def test_collector_corrupted_receipt_hash_refuses_after_preservation(self):
        root, lane = self.collector_seed()
        path = self.module.LOGS / (lane + '.result.json')
        receipt = json.loads(path.read_text())
        receipt['sha256'] = '0' * 64
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(AssertionError, '^command log receipt mismatch: ' + lane + '$'):
            self.module.collect()
        self.assert_retained()

    def test_collector_external_hardlink_payload_never_exported(self):
        root, lane = self.collector_seed()
        sentinel = self.parent / 'outside-curated-sentinel'
        sentinel.write_bytes(b'PRIVATE-SENTINEL-NEVER-EXPORT-78319')
        os.link(sentinel, root / 'record')
        with self.assertRaisesRegex(AssertionError, '^incomplete/ambiguous journal inventory$'):
            self.module.collect()
        self.assert_retained()
        for name in ('journals.tar', 'journal-inventory.jsonl'):
            raw = (self.module.LOGS / name).read_bytes()
            self.assertNotIn(sentinel.read_bytes(), raw)
            self.assertNotIn(self.module.evidence.export_encoded(sentinel.read_bytes())['data'].encode(), raw)
        with tarfile.open(self.module.LOGS / 'journals.tar', 'r:') as archive:
            self.assertNotIn('journals/dotty-protocol-1/record', archive.getnames())

    def test_collector_unapproved_alias_is_not_complete_link_scope(self):
        root, lane = self.collector_seed()
        sentinel = root / 'unapproved-name'
        sentinel.write_bytes(b'UNAPPROVED-ALIAS-SENTINEL-2198')
        os.link(sentinel, root / 'record')
        with self.assertRaisesRegex(AssertionError, '^incomplete/ambiguous journal inventory$'):
            self.module.collect()
        self.assert_retained()
        self.assertNotIn(sentinel.read_bytes(), (self.module.LOGS / 'journals.tar').read_bytes())
        self.assertNotIn(self.module.evidence.export_encoded(sentinel.read_bytes())['data'], (self.module.LOGS / 'journal-inventory.jsonl').read_text())

    def test_collector_deadline_during_actual_read_retains_partial_archive(self):
        root, lane = self.collector_seed()
        m = self.module
        (root / 'record').write_bytes(b'first retained payload')
        original_add, original_read = tarfile.TarFile.addfile, os.read
        captured = []
        def add(archive, member, *args, **kwargs):
            result = original_add(archive, member, *args, **kwargs)
            captured.append(member.name)
            return result
        def read(fd, size):
            if 'journals/dotty-protocol-1/record' in captured:
                raise m.CollectionDeadline('injected actual journal read deadline')
            return original_read(fd, size)
        with mock.patch.object(tarfile.TarFile, 'addfile', add), mock.patch.object(m.os, 'read', read):
            with self.assertRaisesRegex(m.CollectionDeadline, '^injected actual journal read deadline$'):
                m.collect()
        self.assert_retained('record')

    def test_collector_deadline_still_active_during_semantic_checks(self):
        self.collector_seed()
        m = self.module
        def interrupted(*args):
            alarm.assert_called_once_with(120)  # Cancelling before validation must fail this test.
            raise m.CollectionDeadline('semantic deadline')
        with mock.patch.object(m.evidence, 'check_text', interrupted), mock.patch.object(m.signal, 'alarm') as alarm:
            with self.assertRaisesRegex(m.CollectionDeadline, '^semantic deadline$'):
                m.collect()
            self.assertEqual(alarm.call_args_list, [mock.call(120), mock.call(0)])
        self.assert_retained()
    def captured_deadline(self, stage):
        root, lane = self.collector_seed()
        m = self.module
        (root / 'record').write_bytes(b'first retained payload')
        clock, retained, written = [1000.0], {}, []
        original_read, original_write = os.read, m.PrivateOutput.write
        original_close = tarfile.TarFile.close
        inode = (root / 'start-failure').stat().st_ino
        def expire():
            self.assertFalse(retained, 'deadline injection must occur exactly once')
            retained.update({path.name: path.read_bytes() for path in m.LOGS.iterdir()})
            self.assertIn(b'first retained payload', retained['journals.tar'])
            self.assertTrue(retained['journal-inventory.jsonl'])
            clock[0] = 1120.0  # Exact original deadline, never a new allowance.
        def read(fd, size):
            result = original_read(fd, size)
            if stage == 'read' and os.fstat(fd).st_ino == inode:
                expire()
                raise m.CollectionDeadline('captured read deadline')
            return result
        def semantics(*args):
            if stage == 'semantics':
                expire()
                raise m.CollectionDeadline('captured semantic deadline')
        def write(stream, data):
            if stage == 'final-write' and stream.path.name == 'collection-checks.json':
                original_write(stream, data[:2])
                expire()
                return original_write(stream, data[2:])
            result = original_write(stream, data)
            written.append((stream.path.name, clock[0]))
            return result
        def close(archive):
            if stage == 'archive-footer' and archive.mode == 'w' and not archive.closed:
                expire()
            return original_close(archive)
        with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                mock.patch.object(m.signal, 'signal'), mock.patch.object(m.signal, 'alarm') as alarm, \
                mock.patch.object(m.os, 'read', read), \
                mock.patch.object(m.evidence, 'check_text', semantics), \
                mock.patch.object(m.PrivateOutput, 'write', write), \
                mock.patch.object(tarfile.TarFile, 'close', close), \
                mock.patch.object(m, 'curate_upload', wraps=m.curate_upload) as curate:
            with self.assertRaises(m.CollectionDeadline):
                m.collect(capture=True)  # Production route, not direct collect_evidence.
            curate.assert_not_called()
            self.assertEqual(alarm.call_args_list, [mock.call(120), mock.call(0)])
        self.assertTrue(retained, 'must actually reach the selected expiry seam')
        self.assertEqual({path.name: path.read_bytes() for path in m.LOGS.iterdir()}, retained)
        self.assertTrue(all(at < 1120 for _, at in written))
        self.assertEqual((m.LOGS / 'collector.stderr').read_bytes(), b'')
        self.assertFalse((m.LOGS / 'collector.exit').exists())
        self.assertFalse((m.ROOT / 'upload').exists())
        self.assertFalse((m.ROOT / 'upload-ready.json').exists())
        self.assertFalse((m.ROOT / 'upload-publication.json').exists())
        self.assertNotIn(m.UPLOAD_ENV, os.environ)
        self.assertIsNone(m.COLLECTION_END)
        if stage == 'final-write':
            self.assertEqual((m.LOGS / 'collection-checks.json').read_bytes(), b'{\n')
        else:
            self.assertFalse((m.LOGS / 'collection-checks.json').exists())

    def test_captured_collection_deadline_in_read_retains_without_followup_writes(self):
        self.captured_deadline('read')

    def test_captured_collection_deadline_in_semantics_retains_without_followup_writes(self):
        self.captured_deadline('semantics')

    def test_captured_collection_deadline_in_final_write_retains_incomplete_receipt(self):
        self.captured_deadline('final-write')

    def test_captured_collection_deadline_prevents_archive_footer_writes(self):
        self.captured_deadline('archive-footer')

    def test_capture_handlers_propagate_deadline_even_before_clock_observation(self):
        m = self.module
        # Distinct labels preserve all partial leaves without retries or deletion.
        for label, outer in (('inner-deadline', False), ('outer-deadline', True)):
            with self.subTest(handler=label):
                operation = mock.Mock(side_effect=m.CollectionDeadline('injected deadline'))
                original = m.PrivateOutput.close
                def close(stream):
                    if outer and stream.path.name == label + '.stderr':
                        stream.abort()
                        raise m.CollectionDeadline('injected deadline')
                    return original(stream)
                if outer:
                    operation.side_effect = None
                    operation.return_value = 0
                with mock.patch.object(m.PrivateOutput, 'close', close):
                    with self.assertRaisesRegex(m.CollectionDeadline, '^injected deadline$'):
                        m.capture_stage(label, operation)
                self.assertEqual((m.LOGS / (label + '.stderr')).read_bytes(), b'')
                self.assertFalse((m.LOGS / (label + '.exit')).exists())

    def test_dispatch_cli_collection_deadline_is_silent_after_clock_restoration(self):
        m = self.module
        deadline = m.CollectionDeadline('must not be formatted')
        with mock.patch.object(m, 'COLLECTION_END', None), \
                mock.patch.object(m, 'collect', side_effect=deadline) as collect, \
                mock.patch.object(m.CollectionDeadline, '__str__') as stringify, \
                mock.patch.object(m.CollectionDeadline, '__repr__') as represent, \
                mock.patch.object(m.CollectionDeadline, '__format__') as format_error, \
                mock.patch.object(m.sys, 'stdout') as stdout, \
                mock.patch.object(m.sys, 'stderr') as stderr, \
                mock.patch.object(m, 'write_json') as receipt, \
                mock.patch.object(m, 'curate_upload') as curate, \
                mock.patch.object(m, 'publish') as publish:
            self.assertEqual(m.dispatch_cli('collect'), 1)
            collect.assert_called_once_with(capture=True)
            self.assertIsNone(m.COLLECTION_END)
            for hook in (stringify, represent, format_error, stdout, stderr, receipt, curate, publish):
                self.assertEqual(hook.mock_calls, [])

    def test_dispatch_cli_preserves_returned_status_without_diagnostics(self):
        m = self.module
        for result, expected in ((None, 0), (0, 0), (7, 7)):
            with self.subTest(result=result), \
                    mock.patch.object(m, 'collect', return_value=result) as collect, \
                    mock.patch.object(m.sys, 'stdout') as stdout, \
                    mock.patch.object(m.sys, 'stderr') as stderr:
                self.assertEqual(m.dispatch_cli('collect'), expected)
                collect.assert_called_once_with(capture=True)
                self.assertEqual(stdout.mock_calls, [])
                self.assertEqual(stderr.mock_calls, [])

    def test_dispatch_cli_ordinary_failures_keep_refusal_and_status(self):
        m = self.module
        for error, message in ((AssertionError('ordinary failure'), 'ordinary failure'),
                               (m.CommandFailure(7, 'lane'), 'lane: exit 7')):
            with self.subTest(message=message), \
                    mock.patch.object(m, 'collect', side_effect=error) as collect, \
                    m.redirect_stdout(m.io.StringIO()) as stdout, \
                    m.redirect_stderr(m.io.StringIO()) as stderr:
                self.assertEqual(m.dispatch_cli('collect'), 1)
                collect.assert_called_once_with(capture=True)
                self.assertEqual(stdout.getvalue(), '')
                self.assertEqual(stderr.getvalue(), 'REFUSE collect: ' + message + '\n')
            if isinstance(error, m.CommandFailure):
                self.assertEqual(error.status, 7)

    def test_collect_wrapper_propagates_curation_deadline(self):
        m = self.module
        with mock.patch.object(m.signal, 'signal'), mock.patch.object(m.signal, 'alarm'), \
                mock.patch.object(m, 'capture_stage', return_value=1), \
                mock.patch.object(m, 'curate_upload', side_effect=m.CollectionDeadline('curation deadline')), \
                mock.patch.object(m.sys, 'stderr') as diagnostic:
            with self.assertRaisesRegex(m.CollectionDeadline, '^curation deadline$'):
                m.collect(capture=True)
            diagnostic.write.assert_not_called()
        self.assertFalse((m.ROOT / 'upload-ready.json').exists())

    def test_capacity_refusal_retains_measurement_before_threshold(self):
        m = self.module
        environment = dict(m.FIXED_ENV)
        environment.update({key: str(m.ROOT / name) for key, name in m.PRIVATE.items()})
        environment.update({'RUNNER_ENVIRONMENT': 'github-hosted', 'ImageOS': 'synthetic', 'ImageVersion': 'synthetic'})
        fs = mock.Mock(f_bavail=3, f_frsize=4096, f_favail=4)
        with mock.patch.dict(os.environ, environment), \
                mock.patch.object(m.platform, 'machine', return_value='aarch64'), \
                mock.patch.object(m.shutil, 'which', return_value=None), \
                mock.patch.object(m, 'command', return_value='ext2/ext3'), \
                mock.patch.object(m.os, 'statvfs', return_value=fs):
            with self.assertRaisesRegex(AssertionError, 'insufficient native CI headroom: .*; available bytes=12288 inodes=4; required bytes=8589934592 inodes=100000'):
                m.preflight()
        observed = json.loads((m.LOGS / 'capacity-0.json').read_text())
        self.assertEqual(observed['path'], str(m.ROOT))
        self.assertEqual(observed['available_bytes'], 12288)
        self.assertEqual(observed['available_inodes'], 4)

    def test_captured_child_observes_only_private_environment_paths(self):
        m = self.module
        keys = list(m.PRIVATE) + ['MISE_GLOBAL_CONFIG_FILE', 'MISE_SYSTEM_CONFIG_FILE', 'MISE_TRUSTED_CONFIG_PATHS']
        code = 'import json,os,tempfile; print(json.dumps({k:os.environ[k] for k in ' + repr(keys) + '})); print(tempfile.gettempdir())'
        lines = m.command('private-env', [sys.executable, '-B', '-I', '-c', code]).splitlines()
        actual = json.loads(lines[0])
        self.assertEqual(actual, {key: os.environ[key] for key in keys})
        for key, name in m.PRIVATE.items():
            self.assertEqual(actual[key], str(m.ROOT / name))
        self.assertEqual(lines[1], str(m.ROOT / 'tmp'))
        self.assertEqual(tempfile.gettempdir(), str(m.ROOT / 'tmp'))

    def test_diagnostic_scan_failure_does_not_bypass_owned_shutdown(self):
        m = self.module
        for index, (failure, status) in enumerate(((AssertionError('process observation budget'), 0),
                                                  (OSError(errno.EIO, 'scan error'), 7))):
            events = []
            killpg, waitpid = os.killpg, os.waitpid
            def kill(group, number):
                events.append(('signal', number))
                return killpg(group, number)
            def wait(pid, flags):
                events.append(('release', pid))
                return waitpid(pid, flags)
            lane = 'scan-failure-' + str(index)
            with self.subTest(failure=failure), mock.patch.object(m, 'session_observations', side_effect=failure), \
                    mock.patch.object(m.os, 'killpg', kill), mock.patch.object(m.os, 'waitpid', wait):
                with self.assertRaisesRegex(m.CommandFailure, 'incomplete session diagnostics') as raised:
                    m.command(lane, [sys.executable, '-B', '-I', '-c', 'raise SystemExit(' + str(status) + ')'])
            self.assertEqual(raised.exception.status, status or 1)
            receipt = json.loads((m.LOGS / (lane + '.result.json')).read_text())
            self.assertEqual(receipt['exit'], status)
            self.assertFalse(receipt['complete'])
            self.assertTrue(receipt['leader_released'])
            release = next(i for i, event in enumerate(events) if event[0] == 'release')
            self.assertEqual(events[:release], [('signal', signal.SIGTERM), ('signal', signal.SIGKILL)])
            self.assertTrue(all(event == ('signal', 0) for event in events[release + 1:]))

    def test_diagnostic_scan_error_with_live_deadline_still_kills_owned_group(self):
        m = self.module
        with mock.patch.object(m, 'session_observations', side_effect=AssertionError('process observation budget')):
            receipt = self.capture_failure('scan-live',
                'import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(1)',
                'incomplete session diagnostics', seconds=0.1)
        self.assertTrue(receipt['leader_released'])
        self.assertTrue(receipt['group_absent'])

    def test_absence_requested_sleep_is_clamped_to_remaining_deadline(self):
        m = self.module
        with mock.patch.object(m, 'group_absent', return_value=False), \
                mock.patch.object(m.time, 'monotonic', side_effect=[10.99, 10.99, 11.0]), \
                mock.patch.object(m.time, 'sleep') as sleep:
            self.assertFalse(m.observe_group_absence(12345, 11.0))
        sleep.assert_called_once()
        self.assertAlmostEqual(sleep.call_args.args[0], 0.01)

    def test_output_existing_symlink_and_hardlink_do_not_modify_targets(self):
        m = self.module
        sentinel = self.parent / 'output-sentinel'
        sentinel.write_bytes(b'OUTPUT-TARGET-UNCHANGED')
        for name, link in (('main.stdout', 'symlink'), ('collector.stdout', 'hardlink')):
            path = m.LOGS / name
            if link == 'symlink':
                path.symlink_to(sentinel)
            else:
                os.link(sentinel, path)
            operation = mock.Mock(return_value=0)
            self.assertEqual(m.capture_stage(name.split('.')[0], operation), 1)
            operation.assert_not_called()
            self.assertEqual(sentinel.read_bytes(), b'OUTPUT-TARGET-UNCHANGED')
        with self.assertRaises(FileExistsError):
            m.write_json(m.LOGS / 'collector.stdout', {'never': 'written'})
        self.assertEqual(sentinel.read_bytes(), b'OUTPUT-TARGET-UNCHANGED')

    def test_output_ancestor_swap_before_open_refuses_without_writing(self):
        m = self.module
        original = os.open
        moved = m.ROOT / 'retained-evidence'
        swapped = []
        def opened(name, flags, *args, **kwargs):
            fd = original(name, flags, *args, **kwargs)
            if name == 'evidence' and not swapped:
                swapped.append(True)
                m.LOGS.rename(moved)
                m.LOGS.mkdir(mode=0o700)
                (m.LOGS / 'main.stdout').write_bytes(b'REPLACEMENT-UNCHANGED')
            return fd
        with mock.patch.object(m.os, 'open', opened):
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                m.PrivateOutput(m.LOGS / 'main.stdout', 64)
        self.assertFalse((moved / 'main.stdout').exists())
        self.assertEqual((m.LOGS / 'main.stdout').read_bytes(), b'REPLACEMENT-UNCHANGED')

    def test_output_ancestor_swap_after_open_refuses_before_write(self):
        m = self.module
        writer = m.PrivateOutput(m.LOGS / 'bound.stdout', 64)
        moved = m.ROOT / 'retained-evidence'
        m.LOGS.rename(moved)
        m.LOGS.mkdir(mode=0o700)
        (m.LOGS / 'bound.stdout').write_bytes(b'REPLACEMENT-UNCHANGED')
        try:
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                writer.write(b'not written')
        finally:
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                writer.close()
        self.assertEqual((moved / 'bound.stdout').read_bytes(), b'')
        self.assertEqual((m.LOGS / 'bound.stdout').read_bytes(), b'REPLACEMENT-UNCHANGED')

    def env_capability(self):
        directory = self.parent / '_runner_file_commands'
        directory.mkdir(mode=0o700)
        return directory / 'set_env_01234567-89ab-cdef-0123-456789abcdef'

    def test_github_env_is_bounded_append_capability_not_arbitrary_path(self):
        m = self.module
        path = self.env_capability()
        path.write_bytes(b'EXISTING=kept\n')
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)}):
            m.append_github_env({'HOME': str(m.ROOT / 'home')})
        expected = b'EXISTING=kept\nHOME=' + str(m.ROOT / 'home').encode() + b'\n'
        self.assertEqual(path.read_bytes(), expected)
        for supplied in (m.ROOT / 'tmp/arbitrary', self.parent / path.name):
            supplied.write_bytes(b'UNCHANGED')
            with mock.patch.dict(os.environ, {'GITHUB_ENV': str(supplied)}):
                with self.assertRaisesRegex(AssertionError, 'unexpected GITHUB_ENV role path'):
                    m.append_github_env({'HOME': 'bad'})
            self.assertEqual(supplied.read_bytes(), b'UNCHANGED')
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)}):
            with self.assertRaisesRegex(AssertionError, 'environment append budget'):
                m.append_github_env({'HOME': 'x' * 65536})
        self.assertEqual(path.read_bytes(), expected)

    def test_github_env_symlink_hardlink_and_ancestor_swap_refuse(self):
        m = self.module
        path = self.env_capability()
        sentinel = self.parent / 'env-sentinel'
        sentinel.write_bytes(b'ENV-TARGET-UNCHANGED')
        path.symlink_to(sentinel)
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)}):
            with self.assertRaisesRegex(AssertionError, 'unsafe GITHUB_ENV leaf'):
                m.append_github_env({'HOME': 'bad'})
        path.rename(path.with_name('retained-symlink'))
        os.link(sentinel, path)
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)}):
            with self.assertRaisesRegex(AssertionError, 'unsafe GITHUB_ENV leaf'):
                m.append_github_env({'HOME': 'bad'})
        path.rename(path.with_name('retained-hardlink'))
        path.write_bytes(b'OWNED-UNCHANGED')
        original, swapped = os.open, []
        def opened(name, flags, *args, **kwargs):
            fd = original(name, flags, *args, **kwargs)
            if name == path.name and not swapped:
                swapped.append(True)
                path.parent.rename(self.parent / 'retained-file-commands')
                path.parent.mkdir(mode=0o700)
                path.write_bytes(b'REPLACEMENT-UNCHANGED')
            return fd
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)}), mock.patch.object(m.os, 'open', opened):
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                m.append_github_env({'HOME': 'bad'})
        self.assertEqual(sentinel.read_bytes(), b'ENV-TARGET-UNCHANGED')
        self.assertEqual(path.read_bytes(), b'REPLACEMENT-UNCHANGED')
        self.assertEqual((self.parent / 'retained-file-commands' / path.name).read_bytes(), b'OWNED-UNCHANGED')

    def assert_upload_excludes(self, payload):
        m = self.module
        upload = m.ROOT / 'upload'
        for path in upload.iterdir():
            raw = path.read_bytes()
            self.assertNotIn(payload, raw, path.name)
            self.assertNotIn(m.evidence.export_encoded(payload)['data'].encode(), raw, path.name)
        manifest = json.loads((upload / 'upload-manifest.json').read_text())
        self.assertEqual({item['name'] for item in manifest['files']} | {'upload-manifest.json'},
                         {path.name for path in upload.iterdir()})
        for item in manifest['files']:
            raw = (upload / item['name']).read_bytes()
            self.assertEqual((len(raw), hashlib.sha256(raw).hexdigest()), (item['bytes'], item['sha256']))
        return manifest

    def test_upload_external_hardlink_and_unexpected_evidence_names_are_not_payloads(self):
        m = self.module
        sentinel = self.parent / 'outside-upload-sentinel'
        secret = b'OUTSIDE-UPLOAD-SECRET-731928'
        sentinel.write_bytes(secret)
        os.link(sentinel, m.LOGS / 'protocol-StartFailure.log')
        (m.LOGS / 'unexpected-name').write_bytes(secret)
        (m.LOGS / '.hidden-raw-secret').write_bytes(secret)
        (m.LOGS / 'main.stderr').symlink_to(sentinel)
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        self.assertEqual(m.curate_upload(), 1)
        manifest = self.assert_upload_excludes(secret)
        self.assertEqual({item['name'] for item in manifest['files']}, {'main-status.json'})
        self.assertEqual({item['name'] for item in manifest['refusals']},
                         {'protocol-StartFailure.log', 'unexpected-name', '.hidden-raw-secret', 'main.stderr'})
        self.assertEqual(sentinel.read_bytes(), secret)

    def test_upload_preserves_validated_partial_artifacts_after_collection_failure(self):
        root, lane = self.collector_seed()
        (root / 'start-failure').rename(root / 'record')
        with self.assertRaisesRegex(AssertionError, 'missing individual required journals'):
            self.module.collect()
        self.assertEqual(self.module.curate_upload(), 0)
        manifest = self.assert_upload_excludes(b'NEVER-PRESENT-SENTINEL')
        names = {item['name'] for item in manifest['files']}
        self.assertTrue({'journals.tar', 'journal-inventory.jsonl', 'collection.json'} <= names)
        self.assertNotIn('collection-checks.json', names)
        with tarfile.open(self.module.ROOT / 'upload/journals.tar', 'r:') as archive:
            self.assertEqual(archive.extractfile('journals/dotty-protocol-1/record').read(), b'retained record')

    def test_upload_input_read_race_refuses_before_output_creation(self):
        m = self.module
        path = m.LOGS / 'main.stdout'
        path.write_bytes(b'original')
        inode = path.stat().st_ino
        original, changed = os.read, []
        secret = b'RACED-UPLOAD-SECRET-93821'
        def read(fd, size):
            if os.fstat(fd).st_ino == inode and not changed:
                changed.append(True)
                path.write_bytes(secret)
            return original(fd, size)
        with mock.patch.object(m.os, 'read', read):
            self.assertEqual(m.curate_upload(), 1)
        manifest = self.assert_upload_excludes(secret)
        self.assertEqual(manifest['files'], [])
        self.assertEqual(manifest['refusals'][0]['name'], 'main.stdout')
        self.assertFalse((m.ROOT / 'upload/main.stdout').exists())

    def assert_partial_excludes(self, payload):
        m = self.module
        for name in ('journals.tar', 'journal-inventory.jsonl'):
            path = m.LOGS / name
            if path.exists():
                raw = path.read_bytes()
                self.assertNotIn(payload, raw, name)
                self.assertNotIn(m.evidence.export_encoded(payload)['data'].encode(), raw, name)

    def test_collector_reopen_must_match_original_tmp_binding(self):
        root, lane = self.collector_seed()
        m = self.module
        original = m.bind
        secret = b'REPLACEMENT-BIND-SECRET-7213'
        def bind_then_replace():
            result = original()
            (m.ROOT / 'tmp').rename(m.ROOT / 'retained-tmp')
            (m.ROOT / 'tmp').mkdir(mode=0o700)
            root.mkdir(mode=0o700)
            (root / 'start-failure').write_bytes(secret)
            return result
        with mock.patch.object(m, 'bind', bind_then_replace):
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                m.collect()
        self.assert_partial_excludes(secret)
        self.assertFalse((m.LOGS / 'collection-checks.json').exists())

    def test_collector_reopen_rejects_replacement_root_not_just_tmp(self):
        root, lane = self.collector_seed()
        m = self.module
        original = m.bind
        secret = b'REPLACEMENT-ROOT-SECRET-09173'
        def bind_then_replace():
            result = original()
            m.ROOT.rename(self.parent / 'retained-run-root')
            m.ROOT.mkdir(mode=0o700)
            (m.ROOT / 'tmp').mkdir(mode=0o700)
            m.LOGS.mkdir(mode=0o700)
            root.mkdir(mode=0o700)
            (root / 'start-failure').write_bytes(secret)
            (m.LOGS / (lane + '.log')).write_bytes(secret)
            return result
        with mock.patch.object(m, 'bind', bind_then_replace):
            with self.assertRaisesRegex(AssertionError, 'binding drift'):
                m.collect()
        self.assert_partial_excludes(secret)
        self.assertFalse((m.LOGS / 'collection-checks.json').exists())

    def test_collector_regular_read_race_is_checked_before_payload_commit(self):
        root, lane = self.collector_seed()
        m = self.module
        path = root / 'start-failure'
        inode = path.stat().st_ino
        secret = b'REPLACEMENT-READ-SECRET-7182'
        original, changed = os.read, []
        def read(fd, size):
            if os.fstat(fd).st_ino == inode and not changed:
                changed.append(True)
                path.write_bytes(secret)
            return original(fd, size)
        with mock.patch.object(m.os, 'read', read):
            with self.assertRaises(AssertionError):
                m.collect()
        self.assertTrue(changed)
        self.assert_partial_excludes(secret)
        self.assertNotIn('"captured": {"kind": "regular"', (m.LOGS / 'journal-inventory.jsonl').read_text())
        with tarfile.open(m.LOGS / 'journals.tar', 'r:') as archive:
            self.assertNotIn('journals/dotty-protocol-1/start-failure', archive.getnames())

    def test_collector_parent_rename_during_read_withholds_payload(self):
        root, lane = self.collector_seed()
        m = self.module
        inode = (root / 'start-failure').stat().st_ino
        secret = b'PARENT-REPLACEMENT-SECRET-3841'
        original, changed = os.read, []
        def read(fd, size):
            if os.fstat(fd).st_ino == inode and not changed:
                changed.append(True)
                root.rename(root.with_name('retained-parent'))
                root.mkdir(mode=0o700)
                (root / 'start-failure').write_bytes(secret)
            return original(fd, size)
        with mock.patch.object(m.os, 'read', read):
            with self.assertRaises(AssertionError):
                m.collect()
        self.assertTrue(changed)
        self.assert_partial_excludes(secret)
        self.assert_partial_excludes(b'retained record')  # Original bytes also lack current ancestry.

    def test_collector_symlink_readlink_race_does_not_export_replacement_text(self):
        m = self.module
        root = m.ROOT / 'tmp/dotty-protocol-1'
        root.mkdir(mode=0o700)
        link = root / 'symlink'
        link.symlink_to('original-target')
        (m.LOGS / 'protocol-Publication.log').write_text(
            '=== RUN   TestFocusedProtocolPublication\n'
            '    task_unix_test.go:1: private protocol evidence: ' + str(root) + '\n')
        secret = 'REPLACEMENT-LINK-SECRET-92174'
        original, changed = os.readlink, []
        def readlink(name, *args, **kwargs):
            if name == 'symlink' and not changed:
                changed.append(True)
                link.rename(root / 'retained-link')
                link.symlink_to(secret)
            return original(name, *args, **kwargs)
        with mock.patch.object(m.os, 'readlink', readlink):
            with self.assertRaises(AssertionError):
                m.collect()
        self.assertTrue(changed)
        self.assert_partial_excludes(secret.encode())
        with tarfile.open(m.LOGS / 'journals.tar', 'r:') as archive:
            self.assertNotIn('journals/dotty-protocol-1/symlink', archive.getnames())

    def test_collector_aggregate_budget_reserves_before_open_and_stops_payload_reads(self):
        root, lane = self.collector_seed()
        m = self.module
        (root / 'partial').write_bytes(b'ok')
        (root / 'record').write_bytes(b'cannot-fit')
        paths = [root / name for name in ('partial', 'record', 'start-failure')]
        inodes = {path.stat().st_ino: path.name for path in paths}
        reads, opens = [], []
        original_read, original_open = os.read, os.open
        def read(fd, size):
            if os.fstat(fd).st_ino in inodes:
                reads.append((inodes[os.fstat(fd).st_ino], size))
            return original_read(fd, size)
        def opened(name, flags, *args, **kwargs):
            fd = original_open(name, flags, *args, **kwargs)
            if os.fstat(fd).st_ino in inodes:
                opens.append(inodes[os.fstat(fd).st_ino])
            return fd
        with mock.patch.dict(m.evidence.EXPORT_LIMITS, {'bytes': 3}), \
                mock.patch.object(m.os, 'read', read), mock.patch.object(m.os, 'open', opened):
            with self.assertRaisesRegex(AssertionError, 'missing individual required journals'):
                m.collect()
        self.assertEqual(reads, [('partial', 2)])
        self.assertEqual(opens, ['partial'])
        self.assert_retained('partial')
        self.assert_partial_excludes(b'cannot-fit')
        self.assert_partial_excludes(b'retained record')

    def publication_seed(self):
        m = self.module
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        started = time.monotonic()
        self.assertEqual(m.curate_upload(started + 120, started), 0)
        path = self.env_capability()
        path.write_bytes(b'')
        environment = mock.patch.dict(os.environ, {'GITHUB_ENV': str(path)})
        environment.start()
        self.addCleanup(environment.stop)
        return path, started

    def test_output_drift_during_curation_has_no_readiness_or_publication_marker(self):
        m = self.module
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        sentinel = self.parent / 'publication-outside'
        sentinel.write_bytes(b'OUTSIDE-PUBLICATION-UNCHANGED')
        original, changed = m.PrivateOutput.write, []
        def write(stream, data):
            result = original(stream, data)
            if stream.path.name == 'upload-manifest.json' and not changed:
                changed.append(True)
                (m.ROOT / 'upload/injected').symlink_to(sentinel)
            return result
        started = time.monotonic()
        with mock.patch.object(m.PrivateOutput, 'write', write), mock.patch.object(m, 'append_github_env') as append:
            with self.assertRaisesRegex(AssertionError, 'upload namespace drift'):
                m.curate_upload(started + 120, started)
            self.assertFalse((m.ROOT / 'upload-ready.json').exists())
            with self.assertRaises(FileNotFoundError):
                m.publish()
            append.assert_not_called()
        self.assertEqual(sentinel.read_bytes(), b'OUTSIDE-PUBLICATION-UNCHANGED')
        self.assertNotIn(m.UPLOAD_ENV, os.environ)

    def test_curation_baseline_records_successfully_closed_writers(self):
        m = self.module
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        original, closed = m.PrivateOutput.close, {}
        def close(stream):
            original(stream)
            if stream.path.parent == m.ROOT / 'upload':
                closed[stream.path.name] = stream.closed_record
        started = time.monotonic()
        with mock.patch.object(m.PrivateOutput, 'close', close):
            self.assertEqual(m.curate_upload(started + 120, started), 0)
        self.assertEqual(set(closed), {'main-status.json', 'upload-manifest.json'})
        ready = json.loads((m.ROOT / 'upload-ready.json').read_text())
        self.assertEqual(ready['files'], closed)
        self.assertEqual(m.upload_inventory(closed), closed)
        self.assertEqual(ready['files']['main-status.json']['sha256'],
                         hashlib.sha256((m.LOGS / 'main-status.json').read_bytes()).hexdigest())

    def curation_replacement(self, target_name, same_bytes=False):
        m = self.module
        m.write_json(m.LOGS / 'main-status.json', {'exit': 7})
        original, replaced = m.write_json, []
        original_close, closed = m.PrivateOutput.close, {}
        def close(stream):
            original_close(stream)
            if stream.path.parent == m.ROOT / 'upload':
                closed[stream.path.name] = stream.closed_record
        target = m.ROOT / 'upload' / target_name
        retained = m.ROOT / 'tmp' / ('retained-' + target_name)
        sentinel = b'UNAPPROVED-CURATION-REPLACEMENT-923417'
        def manifest_then_replace(path, value):
            result = original(path, value)
            if path.name == 'upload-manifest.json':
                self.assertEqual(set(closed), {'main-status.json', 'upload-manifest.json'})
                self.assertEqual(m.upload_inventory(closed), closed)  # Positive baseline BEFORE replacement.
                approved = target.read_bytes()
                old_inode = target.stat().st_ino
                target.rename(retained)  # Retain the writer's original inode and bytes.
                with target.open('xb') as stream:
                    stream.write(approved if same_bytes else sentinel)
                self.assertNotEqual(target.stat().st_ino, old_inode)
                replaced.append(approved)
            return result  # Never adopt the replacement as the writer's record.
        started = time.monotonic()
        with mock.patch.object(m, 'write_json', manifest_then_replace), \
                mock.patch.object(m.PrivateOutput, 'close', close), \
                mock.patch.object(m, 'append_github_env') as append:
            with self.assertRaisesRegex(AssertionError, '^upload writer baseline drift$'):
                m.curate_upload(started + 120, started)
            self.assertFalse((m.ROOT / 'upload-ready.json').exists())
            with self.assertRaises(FileNotFoundError):
                m.publish()
            append.assert_not_called()
        self.assertEqual(len(replaced), 1)
        self.assertEqual(retained.read_bytes(), replaced[0])
        self.assertEqual(target.read_bytes(), replaced[0] if same_bytes else sentinel)
        self.assertFalse((m.ROOT / 'upload-publication.json').exists())
        self.assertNotIn(m.UPLOAD_ENV, os.environ)

    def test_curation_rejects_different_payload_during_manifest_creation(self):
        self.curation_replacement('main-status.json')

    def test_curation_rejects_same_bytes_new_inode_during_manifest_creation(self):
        self.curation_replacement('main-status.json', same_bytes=True)

    def test_curation_rejects_manifest_replacement_after_writer_close(self):
        self.curation_replacement('upload-manifest.json', same_bytes=True)

    def test_publication_rechecks_output_hardlinks_before_marker(self):
        m = self.module
        env_path, started = self.publication_seed()
        target = m.ROOT / 'upload/main-status.json'
        target.rename(m.ROOT / 'tmp/retained-output')
        sentinel = self.parent / 'publication-sentinel'
        sentinel.write_bytes(b'OUTSIDE-PUBLISH-SECRET-743219')
        os.link(sentinel, target)
        with mock.patch.object(m, 'append_github_env') as append:
            with self.assertRaisesRegex(AssertionError, 'unresolved evidence ownership/hardlinks'):
                m.publish()
            append.assert_not_called()
        self.assertEqual(env_path.read_bytes(), b'')
        self.assertFalse((m.ROOT / 'upload-publication.json').exists())
        self.assertEqual(sentinel.read_bytes(), b'OUTSIDE-PUBLISH-SECRET-743219')

    def test_publication_partial_marker_write_failure_consumes_without_success(self):
        m = self.module
        env_path, started = self.publication_seed()
        inode = env_path.stat().st_ino
        original, calls = os.write, []
        def write(fd, data):
            if os.fstat(fd).st_ino == inode:
                calls.append(True)
                if len(calls) == 1:
                    return original(fd, data[:7])
                raise OSError(errno.EIO, 'injected publication append failure')
            return original(fd, data)
        with mock.patch.object(m.os, 'write', write):
            with self.assertRaisesRegex(OSError, 'injected publication append failure'):
                m.publish()
        self.assertEqual(env_path.read_bytes(), m.UPLOAD_ENV[:7].encode())
        self.assertTrue((m.ROOT / 'upload-publication.json').exists())
        with mock.patch.object(m, 'append_github_env') as append:
            with self.assertRaises(FileExistsError):
                m.publish()
            append.assert_not_called()
        # The workflow additionally requires publish-step success; this failed call cannot authorize upload.

    def test_publication_failure_after_marker_append_is_still_failure(self):
        m = self.module
        env_path, started = self.publication_seed()
        original = m.append_github_env
        def append_then_drift(values, **kwargs):
            original(values, **kwargs)
            (m.ROOT / 'upload/main-status.json').write_bytes(b'changed after marker')
        with mock.patch.object(m, 'append_github_env', append_then_drift):
            with self.assertRaisesRegex(AssertionError, 'publication inventory drift'):
                m.publish()
        self.assertIn((m.UPLOAD_ENV + '=').encode(), env_path.read_bytes())
        self.assertTrue((m.ROOT / 'upload-publication.json').exists())
        # Marker presence alone is explicitly insufficient at the upload step.

    def test_publication_rejects_preexisting_marker_and_mismatched_run_candidate(self):
        m = self.module
        env_path, started = self.publication_seed()
        for values, reason in (({m.UPLOAD_ENV: str(m.ROOT / 'upload')}, 'preexisting upload publication marker'),
                               ({m.UPLOAD_ENV: '/foreign/path'}, 'preexisting upload publication marker'),
                               ({'CANDIDATE_SHA': '2' * 40}, 'publication candidate/run/attempt mismatch'),
                               ({'GITHUB_RUN_ID': '2'}, 'publication candidate/run/attempt mismatch'),
                               ({'GITHUB_RUN_ATTEMPT': '2'}, 'publication candidate/run/attempt mismatch')):
            with self.subTest(values=values), mock.patch.dict(os.environ, values), mock.patch.object(m, 'append_github_env') as append:
                with self.assertRaisesRegex(AssertionError, reason):
                    m.publish()
                append.assert_not_called()
        self.assertEqual(env_path.read_bytes(), b'')
        self.assertFalse((m.ROOT / 'upload-publication.json').exists())
        env_path.write_bytes(b'DOTTY_NATIVE_UPLOAD_PATH=/stale/path\n')
        with self.assertRaisesRegex(AssertionError, 'preexisting publication environment data'):
            m.publish()
        self.assertEqual(env_path.read_bytes(), b'DOTTY_NATIVE_UPLOAD_PATH=/stale/path\n')

    def test_publication_rejects_expired_invalid_or_missing_original_deadline(self):
        m = self.module
        env_path, started = self.publication_seed()
        path = m.ROOT / 'upload-ready.json'
        original = json.loads(path.read_text())
        with mock.patch.object(m.time, 'monotonic', return_value=started + 121):
            with self.assertRaisesRegex(AssertionError, 'publication deadline expired'):
                m.publish()
        for deadline in (None, float('nan'), started + 121):
            state = dict(original, deadline=deadline)
            path.write_text(json.dumps(state))  # Explicit test-owned stale-state injection only.
            with self.subTest(deadline=deadline):
                with self.assertRaisesRegex(AssertionError, 'invalid publication clock'):
                    m.publish()
        state = dict(original)
        del state['started']
        path.write_text(json.dumps(state))
        with self.assertRaises(KeyError):
            m.publish()
        self.assertEqual(env_path.read_bytes(), b'')
        self.assertFalse((m.ROOT / 'upload-publication.json').exists())

    def test_safe_partial_publication_preserves_collection_failure_and_is_one_use(self):
        root, lane = self.collector_seed()
        m = self.module
        (root / 'start-failure').rename(root / 'record')
        env_path = self.env_capability()
        env_path.write_bytes(b'')
        with mock.patch.dict(os.environ, {'GITHUB_ENV': str(env_path)}):
            self.assertEqual(m.collect(capture=True), 1)
            self.assertEqual((m.LOGS / 'collector.exit').read_bytes(), b'1\n')
            self.assertEqual(m.publish(), 0)
            expected = (m.UPLOAD_ENV + '=' + str(m.ROOT / 'upload') + '\n').encode()
            self.assertEqual(env_path.read_bytes(), expected)
            with self.assertRaises(FileExistsError):
                m.publish()
            self.assertEqual(env_path.read_bytes(), expected)
        manifest = json.loads((m.ROOT / 'upload/upload-manifest.json').read_text())
        self.assertIn('journals.tar', {item['name'] for item in manifest['files']})
        self.assertNotIn('collection-checks.json', {item['name'] for item in manifest['files']})
        self.assertEqual((m.LOGS / 'collector.exit').read_bytes(), b'1\n')


class ACLDiagnosticTests(unittest.TestCase):
    """Portable fake descriptors/xattrs only; no native fixture, ACL probe, or child."""
    def setUp(self):
        self.environment = {
            'RUNNER_TEMP': '/home/runner/work/_temp', 'GITHUB_RUN_ID': '1',
            'GITHUB_RUN_ATTEMPT': '1', 'GITHUB_WORKSPACE': '/checkout',
            'GITHUB_ENV': '/home/runner/work/_temp/_runner_file_commands/set_env_12345678-1234-1234-1234-123456789abc',
        }
        with mock.patch.dict(os.environ, self.environment):
            self.module = load_source('native_ci_acl_diagnostic_subject', 'native_ci.py')
        self.descriptors, self.opens, self.stats, self.queries, self.closed = {}, [], [], [], []
        self.values, self.errors, self.drift = {}, {}, None
        self.fake = SimpleNamespace(
            environ=self.environment, O_RDONLY=os.O_RDONLY, O_DIRECTORY=os.O_DIRECTORY,
            O_NOFOLLOW=os.O_NOFOLLOW, open=self.open_fd, stat=self.stat_name,
            fstat=self.stat_fd, close=self.closed.append, getxattr=self.getxattr,
            geteuid=lambda: 1001, getegid=lambda: 1002, getgroups=lambda: [1002, 1003])

    def facts(self, path):
        # Same fake inode for each stable path; no native metadata observation.
        return SimpleNamespace(st_dev=1, st_ino=sum(map(ord, str(path))), st_mode=0o40755,
                               st_uid=0, st_gid=0, st_nlink=2, st_mtime_ns=1, st_ctime_ns=1)

    def stat_name(self, name, *, dir_fd=None, follow_symlinks):
        self.assertFalse(follow_symlinks)
        path = Path('/') if dir_fd is None else self.descriptors[dir_fd] / name
        self.stats.append(path)
        result = self.facts(path)
        if self.drift == 'name' and self.queries:
            result.st_ino += 1
        return result

    def open_fd(self, name, flags, *, dir_fd=None):
        self.assertEqual(flags, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.assertTrue((name == '/' and dir_fd is None) or
                        (dir_fd in self.descriptors and '/' not in name))
        fd = len(self.descriptors) + 10
        path = Path('/') if dir_fd is None else self.descriptors[dir_fd] / name
        self.descriptors[fd] = path
        self.opens.append(path)
        return fd

    def stat_fd(self, fd):
        result = self.facts(self.descriptors[fd])
        if self.drift in ('fd', 'ctime', 'nlink') and self.queries:
            key = {'fd': 'st_ino', 'ctime': 'st_ctime_ns', 'nlink': 'st_nlink'}[self.drift]
            setattr(result, key, getattr(result, key) + 1)
        return result

    def getxattr(self, fd, attribute):
        self.assertIsInstance(fd, int)
        self.assertIn(attribute, ('system.posix_acl_access', 'system.posix_acl_default'))
        key = (str(self.descriptors[fd]), attribute)
        self.queries.append(key)
        if key in self.errors:
            raise OSError(self.errors[key], 'untrusted error text\x1b\n')
        if key in self.values:
            return self.values[key]
        raise OSError(errno.ENODATA, 'absent')

    def diagnose(self, clock=None, **constants):
        m = self.module
        output = io.StringIO()
        with ExitStack() as stack:
            stack.enter_context(mock.patch.object(m, 'os', self.fake))
            stack.enter_context(mock.patch.object(m.sys, 'platform', 'linux'))
            stack.enter_context(mock.patch.object(m.sys, 'stdout', output))
            stack.enter_context(mock.patch.object(m.time, 'monotonic', return_value=0, side_effect=clock))
            for name in ('prepare', 'create_root', 'command', 'run', 'collect', 'publish',
                         'curate_upload', 'append_github_env', 'capture_stage'):
                forbidden = stack.enter_context(mock.patch.object(m, name, create=True,
                                                  side_effect=AssertionError('forbidden progression')))
                self.addCleanup(forbidden.assert_not_called)
            for name, value in constants.items():
                stack.enter_context(mock.patch.object(m, name, value))
            status = m.dispatch_cli('diagnose-acl')
        wire = output.getvalue()
        self.assertEqual(status, 1)
        self.assertLessEqual(len(wire), 16384)
        self.assertTrue(wire.isascii())
        self.assertEqual(wire.count('\n'), 1)
        self.assertNotIn('\x1b', wire)
        self.assertEqual(sorted(self.closed), sorted(self.descriptors))
        return json.loads(wire), wire

    def test_absent_observer_is_read_only_deduplicated_and_always_nonzero(self):
        report, _ = self.diagnose()
        expected = ['/', '/home', '/home/runner', '/home/runner/work',
                    '/home/runner/work/_temp', '/home/runner/work/_temp/_runner_file_commands']
        self.assertEqual(list(map(str, self.opens)), expected)
        self.assertEqual(report['nodes'], 6)
        self.assertEqual(report['queries'], 12)
        self.assertEqual(report['omitted_nodes'], [])
        self.assertFalse(report['native_acceptance'])
        self.assertEqual(report['process']['groups'], [1002, 1003])
        for record in report['records']:
            self.assertEqual(record['state'], 'observed')
            self.assertEqual(record['missing_attributes'], [])
            self.assertEqual(record['stat']['nlink'], 2)
            self.assertEqual(set(record['acl']), set(self.module.ACL_DIAGNOSTIC_ATTRIBUTES))
            self.assertTrue(all(item['state'] == 'absent' and item['errno'] == errno.ENODATA
                                for item in record['acl'].values()))
        self.assertTrue(all(path.name != Path(self.environment['GITHUB_ENV']).name for path in self.stats))

    def test_access_default_present_and_malformed_are_complete_uninterpreted_bytes(self):
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        self.values[('/home', access)] = bytes.fromhex('0200000001000700ffffffff')
        self.values[('/home', default)] = b'\x1bmalformed\x00'
        report, _ = self.diagnose()
        for attribute in (access, default):
            item = report['records'][1]['acl'][attribute]
            raw = self.values[('/home', attribute)]
            self.assertEqual(item['state'], 'present')
            self.assertEqual(item['interpretation'], 'unknown-uninterpreted')
            self.assertEqual(bytes.fromhex(item['hex']), raw)
            self.assertEqual(item['length'], len(raw))
            self.assertEqual(item['sha256'], hashlib.sha256(raw).hexdigest())

    def test_default_presence_after_access_absence_and_unknown_error(self):
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        self.values[('/home', default)] = b''
        self.errors[('/home/runner', access)] = errno.EACCES
        report, wire = self.diagnose()
        self.assertEqual(report['records'][1]['acl'][access]['state'], 'absent')
        self.assertEqual(report['records'][1]['acl'][default]['hex'], '')
        self.assertEqual(report['records'][2]['acl'][access]['errno'], errno.EACCES)
        self.assertEqual(report['records'][2]['acl'][access]['state'], 'unknown')
        self.assertNotIn('untrusted error text', wire)

    def test_attribute_and_complete_raw_budgets_never_emit_prefixes(self):
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        self.values[('/', access)] = b'x' * 1029
        self.values[('/', default)] = b'x' * 65537  # Impossible native result, injected at API seam.
        report, _ = self.diagnose()
        for item in report['records'][0]['acl'].values():
            self.assertEqual(item['state'], 'unknown')
            self.assertNotIn('hex', item)
        self.assertEqual(report['queries'], 2)
        self.assertEqual(report['records'][1]['acl'][access]['reason'], 'query/input-budget')

    def test_query_input_console_and_group_budgets_report_omissions(self):
        self.fake.getgroups = lambda: list(range(129))
        report, _ = self.diagnose(ACL_DIAGNOSTIC_QUERIES=1, ACL_DIAGNOSTIC_RECORD_BYTES=800)
        self.assertEqual(report['queries'], 1)
        self.assertTrue(report['omitted_nodes'])
        self.assertEqual(report['process']['groups_state'], 'unknown-budget')
        self.assertIsNone(report['process']['groups'])
        self.assertEqual({r['id'] for r in report['records']} | set(report['omitted_nodes']), set(range(6)))

    def test_aggregate_input_reserves_next_full_attribute(self):
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        self.values[('/', access)] = b'x' * 65536
        report, _ = self.diagnose(ACL_DIAGNOSTIC_INPUT_BYTES=65536)
        self.assertEqual(report['input_bytes'], 65536)
        self.assertEqual(report['queries'], 1)
        self.assertEqual(report['records'][0]['acl'][default]['reason'], 'query/input-budget')

    def test_invalid_canonical_role_and_depth_paths_do_not_open(self):
        for temp, env in (('/home//temp', self.environment['GITHUB_ENV']),
                          ('/home/../temp', self.environment['GITHUB_ENV']),
                          ('/home/temp/', self.environment['GITHUB_ENV']),
                          ('//home/temp', self.environment['GITHUB_ENV']),
                          ('relative', self.environment['GITHUB_ENV']),
                          ('/home/temp', '/outside/set_env_12345678-1234-1234-1234-123456789abc'),
                          ('/home/temp', '/home/temp/_runner_file_commands/wrong'),
                          ('/' + '/'.join(['a'] * 33), '/' + '/'.join(['a'] * 33)
                           + '/_runner_file_commands/set_env_12345678-1234-1234-1234-123456789abc')):
            with self.subTest(temp=temp, env=env):
                self.environment.update(RUNNER_TEMP=temp, GITHUB_ENV=env)
                report, _ = self.diagnose()
                self.assertEqual(report['state'], 'unknown')
                self.assertEqual(self.opens, [])

    def test_escaped_target_data_is_not_console_control(self):
        self.environment['RUNNER_TEMP'] = '/home/\x1b\n\u2603'
        self.environment['GITHUB_ENV'] = self.environment['RUNNER_TEMP'] + '/_runner_file_commands/set_env_12345678-1234-1234-1234-123456789abc'
        report, wire = self.diagnose()
        self.assertEqual(report['targets']['RUNNER_TEMP'], self.environment['RUNNER_TEMP'])
        self.assertIn('\\u001b', wire)
        self.assertIn('\\u2603', wire)

    def test_fd_name_and_metadata_drift_prevent_descendant_queries(self):
        for drift in ('fd', 'name', 'ctime', 'nlink'):
            with self.subTest(drift=drift):
                self.drift = drift
                self.queries.clear()
                self.opens.clear()
                self.descriptors.clear()
                self.closed.clear()
                report, _ = self.diagnose()
                self.assertEqual(report['records'][0]['state'], 'unknown')
                self.assertEqual(len(self.queries), 1)
                self.assertEqual(list(map(str, self.opens)), ['/'])
                self.assertTrue(all(r['state'] == 'unknown' for r in report['records']))

    def assert_pending_acl_recheck_failure(self, failure, outcome, prior=False):
        self.descriptors.clear()
        self.opens.clear()
        self.stats.clear()
        self.queries.clear()
        self.closed.clear()
        self.values.clear()
        self.errors.clear()
        self.drift = None
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        attribute = default if prior else access
        sentinel = b'failed-query-raw-sentinel\x00\x1b'
        completed = b'previously-validated-access'
        if prior:
            self.values[('/', access)] = completed
        if outcome == 'present':
            self.values[('/', attribute)] = sentinel
        elif outcome == 'error':
            self.errors[('/', attribute)] = errno.EIO
        activated = False

        def query(fd, name):
            nonlocal activated
            try:
                return self.getxattr(fd, name)
            finally:
                if name == attribute:
                    activated = True
                    self.drift = failure

        def stat_fd(fd):
            if activated and failure == 'inaccessible':
                raise OSError(errno.EACCES, 'untrusted post-query error\x1b\n')
            return self.stat_fd(fd)

        self.fake.getxattr = query
        self.fake.fstat = stat_fd
        report, wire = self.diagnose(clock=lambda: 46 if activated and failure == 'deadline' else 0)
        root = report['records'][0]
        failed = root['acl'][attribute]
        self.assertEqual(failed, {'state': 'unknown', 'reason': 'post-query inaccessible/drift/deadline',
                                  'errno': errno.EACCES if failure == 'inaccessible' else None})
        self.assertNotIn(sentinel.hex(), wire)
        self.assertNotIn(hashlib.sha256(sentinel).hexdigest(), wire)
        self.assertNotIn('failed-query-raw-sentinel', wire)
        self.assertNotIn('untrusted', wire)
        self.assertNotIn('"state":"absent"', wire)
        if prior:
            self.assertEqual(root['acl'][access], {
                'state': 'present', 'presence': 'present', 'length': len(completed),
                'sha256': hashlib.sha256(completed).hexdigest(), 'hex': completed.hex(),
                'interpretation': 'unknown-uninterpreted'})
        else:
            self.assertNotIn('"present"', wire)
            self.assertEqual(root['missing_attributes'], [default])
        self.assertEqual(report['queries'], 2 if prior else 1)
        self.assertEqual(len(self.queries), report['queries'])
        self.assertEqual(report['input_bytes'], (len(completed) if prior else 0)
                         + (len(sentinel) if outcome == 'present' else 0))
        self.assertEqual(list(map(str, self.opens)), ['/'])
        self.assertTrue(all(r['state'] == 'unknown' for r in report['records']))
        self.assertTrue(all(r['acl'] == {} and r['missing_attributes'] == [access, default]
                            for r in report['records'][1:]))
        self.assertEqual(report['final_bindings'], 'unknown-incomplete')

    def test_pending_root_acl_drift_never_serializes_unvalidated_results(self):
        access = self.module.ACL_DIAGNOSTIC_ATTRIBUTES[0]
        sentinel = b'failed-query-raw-sentinel\x00\x1b'
        self.values[('/', access)] = sentinel
        report, _ = self.diagnose()
        self.assertEqual(report['records'][0]['acl'][access]['state'], 'present')
        self.assertEqual(report['records'][0]['acl'][access]['hex'], sentinel.hex())
        for failure in ('fd', 'name', 'ctime', 'nlink'):
            for outcome in ('present', 'absent', 'error'):
                with self.subTest(failure=failure, outcome=outcome):
                    self.assert_pending_acl_recheck_failure(failure, outcome)

    def test_pending_default_acl_failures_preserve_completed_access(self):
        for failure in ('fd', 'name', 'ctime', 'nlink', 'deadline', 'inaccessible'):
            for outcome in ('present', 'absent', 'error'):
                with self.subTest(failure=failure, outcome=outcome):
                    self.assert_pending_acl_recheck_failure(failure, outcome, prior=True)

    def test_terminal_serialization_console_and_stdout_failures_never_progress(self):
        m = self.module
        expected = {'diagnostic': 'ancestor-acl', 'state': 'unknown', 'exit': 1,
                    'native_acceptance': False,
                    'missing': 'all records; observation unavailable or budget exceeded'}
        for failure in ('serialization', 'console', 'stdout', 'fallback-stdout'):
            with self.subTest(failure=failure), ExitStack() as stack:
                output = io.StringIO()
                writes = []

                def write(wire):
                    writes.append(wire)
                    if failure in ('stdout', 'fallback-stdout'):
                        output.write(wire[:7])  # Simulate a stream failure after partial output.
                        raise OSError(errno.EIO, 'untrusted stdout failure\x1b\n')
                    return output.write(wire)

                stack.enter_context(mock.patch.object(m, 'os', self.fake))
                stack.enter_context(mock.patch.object(m.sys, 'stdout', SimpleNamespace(write=write)))
                observer = stack.enter_context(mock.patch.object(m, 'observe_acl_diagnostic',
                    return_value={'diagnostic': 'ancestor-acl', 'native_acceptance': False, 'exit': 1}))
                serializer = stack.enter_context(mock.patch.object(m, 'acl_diagnostic_json',
                                                                   wraps=m.acl_diagnostic_json))
                if failure in ('serialization', 'fallback-stdout'):
                    serializer.side_effect = ValueError('untrusted serializer failure\x1b\n')
                elif failure == 'console':
                    serializer.return_value = 'x' * m.ACL_DIAGNOSTIC_CONSOLE_BYTES
                forbidden = []
                for name in ('prepare', 'create_root', 'command', 'run', 'collect', 'publish',
                             'curate_upload', 'append_github_env', 'capture_stage'):
                    forbidden.append(stack.enter_context(mock.patch.object(m, name, create=True,
                        side_effect=AssertionError('forbidden progression'))))
                forbidden.append(stack.enter_context(mock.patch('builtins.open',
                    side_effect=AssertionError('fallback file open'))))
                for name in ('open', 'write_text', 'write_bytes'):
                    forbidden.append(stack.enter_context(mock.patch.object(Path, name,
                        side_effect=AssertionError('fallback file write'))))
                status = m.dispatch_cli('diagnose-acl')
                self.assertEqual(status, 1)
                observer.assert_called_once_with()
                serializer.assert_called_once()
                for operation in forbidden:
                    operation.assert_not_called()
                self.assertEqual(len(writes), 1)
                self.assertLessEqual(len(writes[0]), m.ACL_DIAGNOSTIC_CONSOLE_BYTES)
                self.assertTrue(writes[0].isascii())
                self.assertNotIn('untrusted', writes[0])
                if failure in ('stdout', 'fallback-stdout'):
                    self.assertEqual(output.getvalue(), writes[0][:7])
                else:
                    self.assertEqual(json.loads(output.getvalue()), expected)
                    self.assertEqual(output.getvalue().count('\n'), 1)
                if failure == 'fallback-stdout':
                    self.assertEqual(json.loads(writes[0]), expected)
        self.assertEqual(self.opens, [])
        self.assertEqual(self.queries, [])

    def test_inaccessible_open_and_unavailable_api_stay_nonzero(self):
        self.fake.open = mock.Mock(side_effect=OSError(errno.EACCES, 'not readable'))
        report, _ = self.diagnose()
        self.assertEqual(report['records'][0]['errno'], errno.EACCES)
        self.assertEqual(report['queries'], 0)
        self.fake.open = self.open_fd
        self.fake.getxattr = None
        report, _ = self.diagnose()
        self.assertEqual(report['state'], 'unknown')
        self.assertEqual(self.queries, [])

    def test_read_boundary_deadline_does_not_start_metadata_reads(self):
        calls = []
        def clock():
            calls.append(True)
            return 0 if len(calls) == 1 else 46
        report, _ = self.diagnose(clock=clock)
        self.assertEqual(self.opens, [])
        self.assertEqual(self.stats, [])
        self.assertEqual(self.queries, [])
        self.assertEqual(report['final_bindings'], 'unknown-incomplete')
        self.assertTrue(all(r['missing_attributes'] == list(self.module.ACL_DIAGNOSTIC_ATTRIBUTES)
                            for r in report['records']))

    def test_console_budget_omits_whole_raw_data_with_length_and_hash_retained(self):
        access, default = self.module.ACL_DIAGNOSTIC_ATTRIBUTES
        self.values[('/', access)] = b'x' * 1028
        self.values[('/', default)] = b'y' * 1028
        report, _ = self.diagnose(ACL_DIAGNOSTIC_RECORD_BYTES=1500)
        for item in report['records'][0]['acl'].values():
            self.assertNotIn('hex', item)
            self.assertEqual(item['length'], 1028)
            self.assertEqual(item['reason'], 'console-budget')
            self.assertEqual(item['state'], 'unknown')
            self.assertIn('sha256', item)
        self.assertTrue(report['omitted_nodes'])

    def test_node_budget_and_unsupported_platform_do_not_open(self):
        report, _ = self.diagnose(ACL_DIAGNOSTIC_NODES=5)
        self.assertEqual(report['state'], 'unknown')
        self.assertEqual(self.opens, [])
        with mock.patch.object(self.module, 'os', self.fake), \
                mock.patch.object(self.module.sys, 'platform', 'darwin'):
            with self.assertRaisesRegex(ValueError, 'Linux bounded xattr API unavailable'):
                self.module.observe_acl_diagnostic()
        self.assertEqual(self.opens, [])

    def test_workflow_restores_reviewed_ordinary_and_native_route(self):
        raw = (Path(__file__).resolve().parents[2] / '.github/workflows/ci.yml').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(),
                         'b2c8be0ec4f1c4d417e1ede090f23fb636d7c50673aa6d949aadcf27932b3c18')
        source = raw.decode()
        verify, native = source.split('  verify:\n', 1)[1].split('  native-linux:\n', 1)
        self.assertNotIn('    if: ${{ false }}', verify)
        self.assertIn('      - name: Run native helper regressions\n', verify)
        self.assertNotIn('diagnose-acl', native)
        for stage in ('prepare', 'run', 'collect', 'publish'):
            self.assertIn('python3 -B -I .github/scripts/native_ci.py ' + stage + '\n', native)
        self.assertIn('    timeout-minutes: 90\n', native)
        self.assertIn('ref: ${{ github.event.pull_request.head.sha }}', native)
        self.assertIn('WORKFLOW_SHA: ${{ github.workflow_sha }}', native)
        self.assertIn('WORKFLOW_REF: ${{ github.workflow_ref }}', native)
        self.assertIn('persist-credentials: false', native)
        self.assertIn("steps.native-publication.outcome == 'success'", native)
        self.assertIn('  contents: read\n', source)
        self.assertNotIn('pull_request_target', source)
        self.assertNotIn('continue-on-error', source)


class DirectoryRoleTests(unittest.TestCase):
    """Portable policy fixtures only: no filesystem creation, native probe or child."""
    setUp = ACLDiagnosticTests.setUp
    facts = ACLDiagnosticTests.facts
    stat_name = ACLDiagnosticTests.stat_name
    open_fd = ACLDiagnosticTests.open_fd
    stat_fd = ACLDiagnosticTests.stat_fd
    getxattr = ACLDiagnosticTests.getxattr

    def chain(self, endpoint, operation):
        return self.module.trusted_ancestry(endpoint, operation)

    def test_higher_default_only_allows_default_free_creation_parent(self):
        m = self.module
        self.values[('/home', 'system.posix_acl_default')] = b'arbitrary uninterpreted template'
        with mock.patch.object(m, 'os', self.fake), self.chain(m.ROOT.parent, 'create-root') as anchors:
            self.assertIn(str(m.ROOT.parent), m.revalidate_ancestry(anchors, 'create-root'))
        self.assertIn(('/home', 'system.posix_acl_access'), self.queries)
        self.assertIn(('/home', 'system.posix_acl_default'), self.queries)

    def test_access_acl_refuses_at_every_node_and_still_queries_default(self):
        m = self.module
        endpoint = m.ROOT / 'tmp'
        for path in list(reversed(endpoint.parents)) + [endpoint]:
            with self.subTest(path=path):
                self.values = {(str(path), 'system.posix_acl_access'): b''}
                self.queries.clear()
                with mock.patch.object(m, 'os', self.fake), self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                    with self.chain(endpoint, 'collector'):
                        self.fail('access ACL admitted')
                self.assertIn((str(path), 'system.posix_acl_default'), self.queries)

    def test_both_acls_refuse_on_all_fixed_strict_roles(self):
        m = self.module
        endpoints = m.private_directories() | {m.ROOT.parent, m.ROOT.parent / '_runner_file_commands', m.WORK}
        for endpoint in endpoints:
            operation = ('create-root' if endpoint == m.ROOT.parent else
                         'github-env' if endpoint.name == '_runner_file_commands' else
                         'source-inventory' if endpoint == m.WORK else 'bind')
            for attribute in m.ACL_DIAGNOSTIC_ATTRIBUTES:
                with self.subTest(endpoint=endpoint, attribute=attribute):
                    self.values = {(str(endpoint), attribute): b'present'}
                    with mock.patch.object(m, 'os', self.fake), self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                        with self.chain(endpoint, operation):
                            self.fail('strict ACL admitted')

    def test_strict_roles_remain_strict_as_source_intermediates(self):
        m = self.module
        for protected in m.private_directories() | {m.ROOT.parent, m.ROOT.parent / '_runner_file_commands'}:
            for attribute in m.ACL_DIAGNOSTIC_ATTRIBUTES:
                with self.subTest(protected=protected, attribute=attribute):
                    self.values = {(str(protected), attribute): b'present'}
                    endpoint = protected / 'existing-checkout'
                    with mock.patch.object(m, 'WORK', endpoint), mock.patch.object(m, 'os', self.fake):
                        with self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                            with self.chain(endpoint, 'source-inventory'):
                                self.fail('intermediate demotion')

    def test_closed_roles_and_endpoint_mismatch_refuse_before_open(self):
        m = self.module
        for operation, endpoint in (('waiver', m.ROOT.parent), ('create-root', m.ROOT),
                                    ('collector', m.LOGS), ('source-inventory', m.ROOT),
                                    ('output', m.ROOT / 'tmp/negative-input'),
                                    ('github-env', m.ROOT.parent), ('bind', m.ROOT / 'foreign')):
            with self.subTest(operation=operation), mock.patch.object(m, 'os', self.fake):
                with self.assertRaisesRegex(AssertionError, 'unknown directory operation|directory endpoint mismatch'):
                    with self.chain(endpoint, operation):
                        self.fail('invalid role admitted')
        self.assertEqual(self.opens, [])

    def test_creation_parent_acl_and_unknown_child_refuse_before_mkdir(self):
        m = self.module
        self.fake.mkdir = mock.Mock()
        with mock.patch.object(m, 'os', self.fake):
            for parent, operation in ((m.ROOT.parent, m.create_root),
                                      (m.ROOT, lambda: m.create_private_directory('upload'))):
                for attribute in m.ACL_DIAGNOSTIC_ATTRIBUTES:
                    self.values = {(str(parent), attribute): b'present'}
                    with self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                        operation()
            self.values.clear()
            with self.assertRaisesRegex(AssertionError, 'unknown private directory'):
                m.create_private_directory('negative-input')
        self.fake.mkdir.assert_not_called()

    def test_chain_extension_keeps_parent_strict_and_rejects_wrong_role(self):
        m = self.module
        with mock.patch.object(m, 'os', self.fake), self.chain(m.ROOT.parent, 'create-root') as anchors:
            parent = anchors[-1][2]
            fd = self.open_fd(m.ROOT.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
            initial = {key: getattr(self.stat_fd(fd), 'st_' + key) for key in ('dev', 'ino', 'mode', 'uid', 'gid')}
            extended = anchors + [(parent, m.ROOT.name, fd, initial, m.ROOT)]
            m.revalidate_ancestry(extended, 'bind')
            with self.assertRaisesRegex(AssertionError, 'directory endpoint mismatch'):
                m.revalidate_ancestry(extended, 'create-root')
            self.values[(str(m.ROOT.parent), 'system.posix_acl_default')] = b'new template'
            with self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                m.revalidate_ancestry(extended, 'bind')

    def test_unknown_unreadable_unsupported_acl_never_means_absent(self):
        m = self.module
        for number in (errno.EACCES, errno.EIO, errno.ENOTSUP, errno.ENOENT):
            for attribute in m.ACL_DIAGNOSTIC_ATTRIBUTES:
                self.errors = {('/home', attribute): number}
                with self.subTest(errno=number, attribute=attribute), mock.patch.object(m, 'os', self.fake):
                    with self.assertRaisesRegex(AssertionError, 'unsupported/unknown ancestor ACL:'):
                        with self.chain(m.ROOT.parent, 'create-root'):
                            self.fail('uncertain ACL admitted')

    def test_symlink_owner_and_mode_refuse_without_descendant_open(self):
        m = self.module
        original = self.facts
        for field, value in (('st_mode', 0o120777), ('st_mode', 0o40777), ('st_uid', 2002)):
            def facts(path):
                result = original(path)
                if path == Path('/home'):
                    setattr(result, field, value)
                return result
            self.opens.clear()
            with self.subTest(field=field, value=value), mock.patch.object(self, 'facts', facts), mock.patch.object(m, 'os', self.fake):
                with self.assertRaisesRegex(AssertionError, 'unsafe ancestor owner|writable ancestor'):
                    with self.chain(m.ROOT.parent, 'create-root'):
                        self.fail('unsafe ancestor admitted')
            self.assertNotIn(Path('/home/runner'), self.opens)

    def test_identity_and_strict_acl_drift_refuse_on_revalidation(self):
        m = self.module
        with mock.patch.object(m, 'os', self.fake), self.chain(m.LOGS, 'export') as anchors:
            m.require_bindings(anchors, 'export')
            for drift in ('fd', 'name'):
                self.drift = drift
                with self.assertRaisesRegex(AssertionError, 'ancestor (identity|binding) drift'):
                    m.require_bindings(anchors, 'export')
            self.drift = None
            for path in (m.ROOT.parent, m.ROOT, m.LOGS):
                self.values = {(str(path), 'system.posix_acl_default'): b'drift'}
                with self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                    m.require_bindings(anchors, 'export')

    def test_output_read_and_environment_acl_refuse_before_leaf_open(self):
        m = self.module
        m.BOUND_DIRECTORIES[str(m.LOGS)] = {}
        self.fake.write = mock.Mock()
        operations = ((m.LOGS, lambda: m.PrivateOutput(m.LOGS / 'record', 64)),
                      (m.LOGS, lambda: m.read_private(m.LOGS / 'record', 64)),
                      (m.ROOT.parent / '_runner_file_commands', lambda: m.append_github_env({'HOME': 'private'})))
        for parent, operation in operations:
            self.values = {(str(parent), 'system.posix_acl_default'): b'present'}
            with mock.patch.object(m, 'os', self.fake), self.assertRaisesRegex(AssertionError, 'ancestor ACL present:'):
                operation()
        self.assertTrue(all(path.name not in ('record', Path(self.environment['GITHUB_ENV']).name) for path in self.opens))
        self.fake.write.assert_not_called()

    def test_every_ancestry_caller_has_closed_operation_and_matching_rechecks(self):
        # Source wiring complements the fake-policy matrix; it is not runtime evidence.
        import ast
        source = Path(__file__).with_name('native_ci.py').read_text()
        tree = ast.parse(source)
        expected = {'PrivateOutput': 'output', 'read_private': 'read', 'bind': 'bind',
                    'create_root': 'create-root', 'create_private_directory': 'create-private',
                    'append_github_env': 'github-env', 'worktree_inventory': 'source-inventory',
                    'preflight': 'preflight', 'upload_inventory': 'upload',
                    'curate_upload': 'export', 'publish': 'publish', 'collect_evidence': 'collector'}
        observed = {}
        for definition in tree.body:
            if not isinstance(definition, (ast.FunctionDef, ast.ClassDef)) or definition.name not in expected:
                continue
            calls = [node for node in ast.walk(definition) if isinstance(node, ast.Call)
                     and isinstance(node.func, ast.Name)
                     and node.func.id in ('trusted_ancestry', 'require_bindings', 'revalidate_ancestry')]
            roles = []
            for call in calls:
                self.assertEqual(len(call.args), 2)
                self.assertIsInstance(call.args[1], ast.Constant)
                roles.append(call.args[1].value)
            allowed = {expected[definition.name]}
            if definition.name in ('create_root', 'create_private_directory'):
                allowed.add('bind')  # Extended created chain; original parent stays strict.
            self.assertTrue(roles and set(roles) <= allowed, definition.name)
            self.assertIn(expected[definition.name], roles)
            observed[definition.name] = roles
        self.assertEqual(set(observed), set(expected))


class NativeCIPortabilityTests(unittest.TestCase):
    def test_mise_test_tasks_set_private_umask_before_unchanged_arguments(self):
        # Source-only regression: no shell execution or native capability claim.
        source = (Path(__file__).resolve().parents[2] / 'mise.toml').read_text()
        expected = {
            'test': """description = "Run Go tests"
run = '''
umask 077 || exit "$?"
case "${DOTTY_NATIVE_ACCEPTANCE:-}" in
  "") go test ./... ;;
  1) go test -v -count=1 ./... ;;
  *) echo 'DOTTY_NATIVE_ACCEPTANCE must be unset or 1' >&2; exit 2 ;;
esac
'''
""",
            '"test:focused"': """description = "Run focused Go tests by package and -run pattern"
usage = '''
arg "<package>" help="Go package path to test, e.g. ./internal/cli"
arg "<pattern>" help="Test name or regular expression for go test -run"
'''
run = '''
umask 077 || exit "$?"
exec bash ./internal/tools/focused/task.sh "${usage_package:?}" "${usage_pattern:?}"
'''
""",
        }
        for task, body in expected.items():
            with self.subTest(task=task):
                marker = '[tasks.' + task + ']\n'
                self.assertEqual(source.count(marker), 1)
                actual = source.split(marker, 1)[1].split('\n[tasks.', 1)[0]
                self.assertEqual(actual.strip(), body.strip())

    def test_unsupported_routing_skips_before_setup_only_without_acceptance(self):
        # Routing test only, not mocked Linux/ACL runtime evidence.
        with mock.patch(__name__ + '.linux_requirement', return_value='unsupported test capability'), \
                mock.patch.dict(os.environ, {'DOTTY_NATIVE_ACCEPTANCE': ''}), \
                mock.patch.object(NativeCITests, 'setUp') as setup:
            with self.assertRaisesRegex(unittest.SkipTest, 'unsupported test capability'):
                NativeCITests.setUpClass()
            setup.assert_not_called()
        with mock.patch(__name__ + '.linux_requirement', return_value='unsupported test capability'), \
                mock.patch.dict(os.environ, {'DOTTY_NATIVE_ACCEPTANCE': '1'}):
            with self.assertRaisesRegex(RuntimeError, 'unsupported native acceptance'):
                NativeCITests.setUpClass()



if __name__ == '__main__':
    unittest.main()
