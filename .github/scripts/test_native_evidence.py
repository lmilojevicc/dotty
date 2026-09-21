"""Targeted CI parser regressions; source prepared only, not executed in this change."""
import copy
import os
import sys
import stat
import importlib.util
from pathlib import Path
import unittest

assert sys.version_info >= (3, 9), "Python 3.9+ required"
sys.dont_write_bytecode = True


# Selected pure K fixture builders; no controller import or runtime dependency.
PROTOCOL_FIXTURES = {
    "protocol_tests": {
        "TestFocusedProtocolRecordFraming": [
            "exact",
            "empty",
            "partial",
            "malformed",
            "stale",
            "wrong_phase",
            "wrong_epoch",
            "wrong_anchor",
            "wrong_action",
            "newline",
            "newlines",
            "trailing_bytes",
            "NUL_suffix",
            "NUL_prefix",
            "NUL_middle"
        ],
        "TestFocusedProtocolPublication": [
            "initial_absence_boundary",
            "initial_absence_boundary/initial_absence_is_pending",
            "initial_absence_boundary/wrapped_or_joined_absence_is_fatal",
            "initial_absence_boundary/observed_disappearance_is_fatal"
        ],
        "TestFocusedProtocolWakeClassification": [
            "benign_empty_wake",
            "old_timeout_rejected",
            "interruption",
            "TERM_interruption",
            "data",
            "EOF",
            "unexplained",
            "unobserved_interruption",
            "partial_data",
            "finished_interruption_is_observed_but_ignored",
            "actual_pipe_EOF_latches"
        ],
        "TestFocusedProtocolDescriptorClosure": [],
        "TestFocusedProtocolSolePulseWriter": [],
        "TestFocusedProtocolPulseWake": [],
        "TestFocusedProtocolNoPulseNegative": [],
        "TestFocusedProtocolPulseCancellation": [
            "before_anchor/interrupt",
            "before_anchor/terminated",
            "transition/interrupt",
            "transition/terminated"
        ],
        "TestFocusedProtocolParentAbort": [],
        "TestFocusedProtocolOwnerOnce": [
            "false",
            "true"
        ],
        "TestFocusedProtocolBoundedCapture": [
            "limit_minus_one",
            "limit",
            "limit_plus_one",
            "cumulative_exact",
            "cumulative_overflow",
            "empty_at_capacity"
        ],
        "TestFocusedProtocolCaptureEOF": [
            "registered_cleanup_retains_incomplete_capture"
        ],
        "TestFocusedProtocolEventTermination": [
            "closed_gate_EOF_before_Wait",
            "nongated_completion",
            "startup_trap_without_return",
            "open_gate_without_completion"
        ],
        "TestFocusedProtocolEventByteBudget": [
            "262143",
            "262144",
            "262145"
        ],
        "TestFocusedProtocolPendingContinuation": [
            "queued_wakes_hold",
            "queued_barrier_abort",
            "grant_during_later_pending_read",
            "interruption_skips_grant"
        ],
        "TestFocusedProtocolPulseLifecycle": [
            "sole_LF_bytes",
            "queued_bytes_are_not_grants",
            "budget",
            "deadline",
            "backpressure",
            "EPIPE_before_close_event",
            "validated_close",
            "cancel_join",
            "cancel_blocked_write_joins"
        ],
        "TestFocusedProtocolIdentityRecords": [
            "exact",
            "FIFO",
            "directory",
            "symlink",
            "replacement",
            "oversized",
            "malformed",
            "NUL",
            "trailing_newline"
        ],
        "TestFocusedProtocolOuterIdentity": [
            "startup",
            "runner",
            "descendant",
            "wrong_group",
            "publication_failure_prevents_spawn",
            "spawn-intent_failure_prevents_spawn"
        ],
        "TestFocusedProtocolAuthorizationHealth": [
            "malformed_after_cancellation",
            "overflow_after_cancellation",
            "lone_return",
            "wrong_anchor",
            "wrong_sequence",
            "wrong_action",
            "pulse_failure",
            "EOF_after_cancellation",
            "EOF_with_outstanding_entry",
            "empty_channel_loss"
        ],
        "TestFocusedProtocolNegativeRetention": [
            "record_rejection",
            "record_rejection/registered_cleanup",
            "read_failure",
            "read_failure/registered_cleanup",
            "EOF",
            "EOF/registered_cleanup",
            "abort",
            "abort/registered_cleanup",
            "positive_removal",
            "positive_removal/registered_cleanup"
        ],
        "TestFocusedProtocolFatalOwner": [],
        "TestFocusedProtocolStartFailure": [],
        "TestFocusedProtocolFatalOwnerFixture": [],
        "TestFocusedProtocolCleanupUncertainty": [
            "complete_pre-start_witness",
            "complete_pre-start_witness/partial_ACK_without_second_publication",
            "complete_pre-start_witness/short_ACK_without_second_publication",
            "complete_pre-start_witness/missing_witness",
            "complete_pre-start_witness/malformed_witness",
            "complete_pre-start_witness/partial_witness",
            "complete_pre-start_witness/NUL_witness",
            "complete_pre-start_witness/oversized_witness",
            "complete_pre-start_witness/competing_return_record",
            "complete_pre-start_witness/contradictory_return_record",
            "complete_pre-start_witness/child_start",
            "complete_pre-start_witness/spawn_intent",
            "complete_pre-start_witness/spawn_completion",
            "complete_pre-start_witness/descendant_start",
            "complete_pre-start_witness/descendant_readiness",
            "complete_pre-start_witness/FIFO_witness",
            "complete_pre-start_witness/directory_witness",
            "complete_pre-start_witness/symlink_witness"
        ],
        "TestFocusedProtocolEventChannel": [
            "complete",
            "partial",
            "NUL",
            "wrong_epoch",
            "oversized",
            "overflow",
            "duplicate",
            "fragmented_complete_event",
            "read_error_latches",
            "open_writer_is_incomplete",
            "anchor_inheritance",
            "Go_fixture_boundary_does_not_start_pulses"
        ]
    },
    "protocol_matrix": {
        "TestFocusedTaskProtocol": [
            "success",
            "exit7",
            "build_failure",
            "startup_failure",
            "signal_status",
            "before_anchor_INT",
            "before_anchor_TERM",
            "build_readiness_INT",
            "build_readiness_TERM",
            "transition_INT",
            "transition_TERM",
            "startup_INT",
            "startup_TERM",
            "partial_readiness",
            "truncated_readiness",
            "invalid_readiness",
            "truncated_readiness_INT",
            "invalid_readiness_TERM",
            "unterminated_readiness_INT",
            "unterminated_readiness_TERM",
            "ack_transition_INT",
            "ack_transition_TERM",
            "ack_write_INT",
            "ack_write_TERM",
            "ack_write_failure",
            "delivered_ack_failure",
            "invalid_ack",
            "ack_EOF",
            "partial_ack_EOF",
            "partial_ack_open_INT",
            "partial_ack_open_TERM",
            "short_invalid_ack_open_INT",
            "short_invalid_ack_open_TERM",
            "early_runner_exit",
            "partial_readiness_INT",
            "partial_readiness_TERM",
            "completion_INT",
            "completion_TERM",
            "finished_INT",
            "finished_TERM",
            "interrupted_waits_INT",
            "interrupted_waits_TERM"
        ]
    },
    "existing_cancellation_inventory": {
        "TestFocusedCancellation": [
            "runner/retained_pipe/interrupt",
            "runner/retained_pipe/terminated",
            "wrapper_runner/retained_pipe/interrupt",
            "wrapper_runner/retained_pipe/terminated",
            "wrapper_build/retained_pipe/interrupt",
            "wrapper_build/retained_pipe/terminated",
            "runner/early_close/interrupt",
            "runner/early_close/terminated",
            "wrapper_runner/early_close/interrupt",
            "wrapper_runner/early_close/terminated",
            "runner/driver_exits/interrupt",
            "runner/driver_exits/terminated",
            "wrapper_runner/driver_exits/interrupt",
            "wrapper_runner/driver_exits/terminated"
        ]
    },
    "existing_child_inventory": {
        "TestFocusedChildInvocationContext": [
            "/state/tmp/TestFocusedChildInvocationContextROOT/001/driver_with_spaces",
            "./driver_with_spaces"
        ],
        "TestFocusedChildStartErrors": [
            "existing_command_error",
            "missing",
            "not_executable"
        ],
        "TestFocusedChildSignalExit": []
    }
}


class FixtureBuilders:
    def native_log(self, aggregator, nested=False):
        names = [aggregator] + [aggregator + '/' + leaf for leaf in self.evidence.INVENTORIES[aggregator]]
        if self.evidence.NESTED[aggregator]:
            names += [aggregator + '/' + leaf for leaf in self.evidence.NESTED[aggregator]]
        lines = []
    
        def emit(name):
            lines.append('=== RUN   ' + name)
            if name.startswith('TestPrivateCreateOpenBoundary/RestrictiveUmaskDirectoryRetained/'):
                mask = name.rsplit('/', 1)[1]
                lines.extend(self.umask_block(mask, self.evidence.UMASKS[mask]).splitlines())
            if aggregator == 'TestFlockBoundary' and name.removeprefix(aggregator + '/') in self.evidence.FLOCK_CHILDREN:
                leaf = name.removeprefix(aggregator + '/')
                lines.extend(self.flock_fixture(leaf).splitlines())
                lines.extend(self.flock_block(leaf).splitlines())
            if name == 'TestCoordinatorBoundary/UserBeforeResolution':
                lines.extend(self.coordinator_fixture().splitlines())
                lines.extend(self.coordinator_block().splitlines())
            for child in names:
                parents = [p for p in names if child.startswith(p + '/')]
                if parents and max(parents, key=len) == name:
                    emit(child)
            lines.append('--- PASS: ' + name + ' (0.00s)')
            if name != aggregator:
                lines.append('executed: ' + self.evidence.PACKAGE + name)
        emit(aggregator)
        return '\n'.join(lines) + '\n'

    def umask_block(self, mask, directory):
        return '    private_umask_test.go:43: isolated umask ' + mask + ':\n        === RUN   TestPrivateUmaskProcess\n            private_umask_test.go:83: native umask ' + mask + ' file=0600 directory=' + directory + ' retained=true\n        --- PASS: TestPrivateUmaskProcess (0.00s)\n        PASS\n'

    def flock_facts(self, leaf):
        inode = 100 + list(self.evidence.FLOCK_CHILDREN).index(leaf) * 10
        return '{identity:{Device:1 Inode:' + str(inode) + ' Kind:16384 Valid:true} uid:10001 gid:10001 mode:448 nlink:2 filesystem:{state:2 model:linux-local-ext-tmpfs kind:16914836 flags:0 id:{[1 2]}} security:{state:1 mechanism:fgetxattr}}'

    def flock_fixture(self, leaf):
        return f'    flock_test.go:355: bounded native fixture os=linux arch=arm64 path={self.tmp}/synthetic-' + str(list(self.evidence.FLOCK_CHILDREN).index(leaf)) + '/001 facts=' + self.flock_facts(leaf) + '\n'

    def flock_block(self, leaf, caller='flock_process_test.go'):
        directory = self.evidence.FLOCK_CHILDREN[leaf]
        inode = 101 + list(self.evidence.FLOCK_CHILDREN).index(leaf) * 10
        kind = '16384' if directory == 'true' else '32768'
        return '    ' + caller + ':179: real process read-only flock proof:\n        === RUN   TestFlockProcess\n            flock_process_test.go:284: native child directory=' + directory + ' root-facts=' + self.flock_facts(leaf).replace('nlink:2', 'nlink:3') + ' selected={Device:1 Inode:' + str(inode) + ' Kind:' + kind + ' Valid:true}\n        --- PASS: TestFlockProcess (0.00s)\n        PASS\n'

    def coordinator_facts(self):
        return self.flock_facts('FileLockSerializesProcesses').replace('linux-local-ext-tmpfs', 'linux-tmpfs-posix-acl').replace('mechanism:fgetxattr', 'mechanism:fgetxattr:posix-access+default')

    def coordinator_fixture(self):
        return f'    coordinator_test.go:129: bounded native fixture os=linux arch=arm64 path={self.tmp}/synthetic-coordinator/001 facts=' + self.coordinator_facts() + '\n'

    def coordinator_block(self):
        root = f'{self.tmp}/synthetic-coordinator/001'
        identity = '{Device:1 Inode:100 Kind:16384 Valid:true}'
        return '    coordinator_test.go:144: real process read-only flock proof:\n        === RUN   TestCoordinatorProcess\n            coordinator_process_test.go:222: bounded coordinator child root="' + root + '" identity=' + identity + ' facts=' + self.coordinator_facts().replace('nlink:2', 'nlink:5') + ' config-selection="' + root + '/z" binding={logical:' + root + '/z physical:' + root + '/z identity:{Device:1 Inode:102 Kind:16384 Valid:true}}\n        --- PASS: TestCoordinatorProcess (0.00s)\n        PASS\n'

    def support_evidence(self):
        names = [name for (root, children) in {**self.evidence.SUPPORT, **self.evidence.FLOCK_SUPPORT, **self.evidence.COORDINATOR_SUPPORT}.items() for name in [root] + [root + '/' + child for child in children]]
        return ''.join(('=== RUN   ' + name + '\n' + ('    ' if '/' in name else '') + '--- PASS: ' + name + ' (0.00s)\n' for name in names))

    def full_evidence(self):
        full = ''.join((self.native_log(agg, True) for agg in self.evidence.INVENTORIES)) + self.support_evidence() + self.protocol_seed('verify')
        return '\n'.join((line for line in full.splitlines() if not line.startswith('executed:'))) + '\n'

    def fatal_seed(self):
        a = f'{self.tmp}/dotty-protocol-101'
        b = f'{self.tmp}/dotty-protocol-102'
        tail = '; identities are historical observations, never later signal authority\n'
        return '    task_unix_test.go:4164: private protocol evidence: ' + a + '\n    task_unix_test.go:4165: private protocol evidence: ' + b + '\n    task_unix_test.go:2337: protocol root=' + a + ' phase="" epoch= outcome=Wait result=exit status 1 cleanup-complete=true retained=true' + tail + '    task_unix_test.go:2348: preserved final stdout/stderr root=' + a + ' truncated=false:\n        --- FAIL: TestFocusedProtocolFatalOwnerFixture (0.01s)\n            task_unix_test.go:356: intentional private fatal owner unwind\n            task_unix_test.go:2337: protocol root=' + b + ' phase="reader" epoch=dotty-protocol-102 outcome=Wait result=signal: terminated cleanup-complete=true retained=true' + tail + '            task_unix_test.go:2348: preserved final stdout/stderr root=' + b + ' truncated=false:\n            task_unix_test.go:2355: retained protocol evidence ' + b + '; output-complete=true\n            task_unix_test.go:833: wrapper-trace (final bounded tail):\n                Bash synthetic-source-model\n        FAIL\n    task_unix_test.go:2355: retained protocol evidence ' + a + '; output-complete=true\n'

    def protocol_seed(self, mode):
            # Independent maintained inventory JSON is DATA, not the parser's constants.
            d=copy.deepcopy(PROTOCOL_FIXTURES)
            if mode=='verify':
                inventory={**d['protocol_tests'],**d['protocol_matrix'],**d['existing_cancellation_inventory'],**d['existing_child_inventory'],
                           'TestFocusedTaskProtocolFixture':[], 'TestFocusedCancellationProcessFixture':[]}
            elif mode=='protocol-matrix': inventory=d['protocol_matrix']
            elif mode=='protocol-cancellation':inventory=d['existing_cancellation_inventory']
            elif mode=='protocol-child':inventory=d['existing_child_inventory']
            else:
                name='TestFocusedProtocol'+mode.removeprefix('protocol-')
                inventory={name:d['protocol_tests'][name]}
            lines=[]
            for root,children in inventory.items():
                names=[root]+[root+'/'+c.replace('/state/tmp', str(self.tmp)).replace('ContextROOT/','Context321/') for c in children]
                def emit(name):
                    lines.append('=== RUN   '+name+'\n')
                    if name=='TestFocusedProtocolFatalOwner':lines.append(self.fatal_seed())
                    for child in names:
                        parents=[p for p in names if child.startswith(p+'/')]
                        if parents and max(parents,key=len)==name:emit(child)
                    lines.append(('    ' if mode=='verify' and '/' in name else '')+'--- PASS: '+name+' (0.00s)\n')
                    if mode!='verify':lines.append('executed: github.com/lmilojevicc/dotty/internal/tools/focused '+name+'\n')
                emit(root)
            return ''.join(lines)

    def export_entry(self, root, path, data=None, kind='regular', ino=2):
        types = {'regular': stat.S_IFREG, 'directory': stat.S_IFDIR, 'fifo': stat.S_IFIFO, 'symlink': stat.S_IFLNK}
        payload = self.evidence.export_encoded(data) if data is not None else None
        return {'root': root, 'path': path, 'kind': kind, 'metadata': {'dev': 1, 'ino': ino, 'mode': types[kind] | (448 if kind == 'directory' else 384), 'uid': os.geteuid(), 'gid': os.getegid(), 'nlink': 1, 'size': len(data) if data is not None else 0, 'mtime_ns': 1, 'ctime_ns': 1}, 'payload': payload}


class NativeEvidenceTests(FixtureBuilders, unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('ci_evidence_test_subject',
                                                     Path(__file__).with_name('native_evidence.py'))
        self.evidence = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.evidence)
        self.tmp = Path('/ci-private/run.1+binding/tmp')
        self.evidence.configure(self.tmp, os.geteuid())

    def test_ci_namespace_is_bound_once_without_rewriting_raw_paths(self):
        text = self.wake_log()
        roles = self.evidence.protocol_root_roles([('protocol-WakeClassification', text)], False)
        self.assertEqual(set(roles), {str(self.tmp / 'dotty-protocol-1'), str(self.tmp / 'dotty-protocol-2')})
        with self.assertRaisesRegex(AssertionError, 'already bound'):
            self.evidence.configure('/state/tmp', 10001)
        with self.assertRaisesRegex(AssertionError, 'malformed protocol root record'):
            self.evidence.protocol_root_roles([('protocol-WakeClassification', text.replace(str(self.tmp), '/state/tmp'))], False)

    def wake_log(self):
        root = 'TestFocusedProtocolWakeClassification'
        child = root + '/' + self.evidence.PROTOCOL_TESTS[root][0]
        return '\n'.join([
            '=== RUN   ' + root,
            '=== RUN   ' + child,
            '    task_unix_test.go:1: private protocol evidence: ' + str(self.tmp / 'dotty-protocol-1'),
            '--- PASS: ' + child + ' (0.00s)',
            '=== NAME  ' + root,
            '    task_unix_test.go:2: private protocol evidence: ' + str(self.tmp / 'dotty-protocol-2'),
            '--- PASS: ' + root + ' (0.00s)',
        ])

    def test_child_close_restores_parent_declaration_owner(self):
        roles = self.evidence.protocol_root_roles([('protocol-WakeClassification', self.wake_log())], False)
        self.assertEqual(roles[str(self.tmp / 'dotty-protocol-2')]['case'], 'TestFocusedProtocolWakeClassification')
        self.assertNotEqual(roles[str(self.tmp / 'dotty-protocol-1')]['case'], 'TestFocusedProtocolWakeClassification')

    def test_unfinished_log_preserves_declarations_diagnostically_only(self):
        text = self.wake_log().rsplit('\n', 1)[0]
        roots, issues = self.evidence.diagnostic_root_roles([('protocol-WakeClassification', text)])
        self.assertEqual(len(roots), 2)
        self.assertTrue(issues)
        self.assertTrue(all(role['case'] is None for role in roots.values()))
        with self.assertRaisesRegex(AssertionError, 'unfinished protocol RUN'):
            self.evidence.protocol_root_roles([('protocol-WakeClassification', text)], False)

    def test_source_declared_count_is_not_relaxed_by_ci_binding(self):
        with self.assertRaisesRegex(AssertionError, 'missing/extra source-declared fixture roots'):
            self.evidence.protocol_root_roles([('protocol-WakeClassification', self.wake_log())])
        self.assertEqual(len(self.evidence.PROTOCOL_ORDER), 24)
        self.assertEqual(len(self.evidence.PROTOCOL_MATRIX['TestFocusedTaskProtocol']), 42)
        self.assertEqual(len(self.evidence.MODES), 7)

    def child_log(self):
        inventory = self.evidence.protocol_inventory('protocol-child')
        events = []
        for root, children in inventory.items():
            events.append('=== RUN   ' + root)
            for child in children:
                child = child.replace('ContextROOT/001/', 'Context123/001/')
                name = root + '/' + child
                events.extend(['=== RUN   ' + name, '--- PASS: ' + name + ' (0.00s)',
                               'executed: github.com/lmilojevicc/dotty/internal/tools/focused ' + name])
            events.extend(['--- PASS: ' + root + ' (0.00s)',
                           'executed: github.com/lmilojevicc/dotty/internal/tools/focused ' + root])
        return '\n'.join(events)

    def test_dynamic_child_path_matches_actual_private_namespace(self):
        raw = self.child_log()
        self.evidence.check_protocol(raw, 'protocol-child')
        with self.assertRaisesRegex(AssertionError, 'inventory mismatch'):
            self.evidence.check_protocol(raw.replace(str(self.tmp), '/state/tmp'), 'protocol-child')

    def test_no_blanket_fail_or_skip_exception(self):
        self.evidence.check_protocol(self.child_log(), 'protocol-child')
        for outcome in ('FAIL', 'SKIP'):
            with self.subTest(outcome=outcome):
                with self.assertRaisesRegex(AssertionError, 'skip/failure'):
                    self.evidence.check_protocol(self.child_log() + '\n--- ' + outcome + ': Other (0.00s)', 'protocol-child')

    def fatal_witness_seed(self, qualified=True):
        root = str(self.tmp / 'dotty-protocol-102')
        epoch = root.rsplit('/', 1)[1]
        role = {'case': 'TestFocusedProtocolFatalOwner', 'lane': 'protocol-FatalOwner', 'outcome': {'phase': 'reader', 'epoch': epoch, 'wait': 'Wait result=signal: terminated', 'complete': True, 'retained': True}, 'removed': False, 'nested_fatal': True}
        prefix = "+focused: printf '%s|%s|%s|%s|%s\\n' "
    
        def enter(n):
            return prefix + 'read-enter-' + str(n) + ' reader ' + epoch + " absent 'stage=build;cancelled=0'\n" + '+focused: gate_data=\n+focused: gate_code=\n+focused: IFS=\n+focused: read -r gate_data\n'
        trace = 'Bash 5.2.37(1)-release\n' + prefix + 'boundary reader ' + epoch + ' absent ready\n' + enter(1)
        trace += prefix + 'read-return-1 reader ' + epoch + " absent 'stage=build;cancelled=0;code=0;class=wake'\n"
        if qualified:
            trace += enter(2)
        frame = ('phase=reader\n epoch=' + epoch + '\n anchor=absent\n action=stage=build;cancelled=0').encode()
        payloads = {'cleanup-complete': b'quiescent', 'fatal-unwind': b'wait-calls=1;complete=true;retained=true', 'wrapper-trace': trace.encode(), 'read-enter-2': frame}
        make = self.export_entry
        names = self.evidence.protocol_required_records(role) | set(payloads)
        entries = [make(root, '', kind='directory', ino=1)] + [make(root, name, payloads.get(name, b'synthetic required record'), ino=i) for (i, name) in enumerate(sorted(names), 10)]
        return ({root: role}, entries)

    def test_complete_configured_declaration_and_export_then_isolated_omissions(self):
        root = str(self.tmp / 'dotty-protocol-101')
        owner = 'TestFocusedProtocolStartFailure'
        declaration = '    task_unix_test.go:4164: private protocol evidence: ' + root + '\n'
        text = '=== RUN   ' + owner + '\n' + declaration + '--- PASS: ' + owner + ' (0.00s)\n'
        roots = self.evidence.protocol_root_roles([('protocol-StartFailure', text)])
        entries = [self.export_entry(root, '', kind='directory', ino=1),
                   self.export_entry(root, 'start-failure', b'start refused', ino=2)]
        self.evidence.check_export_records(roots, entries)
        with self.assertRaisesRegex(AssertionError, "missing required journals:.*start-failure"):
            self.evidence.check_export_records(roots, entries[:1])
        wrong = copy.deepcopy(entries)
        wrong[1]['metadata']['uid'] = os.geteuid() + 1
        with self.assertRaisesRegex(AssertionError, '^unexpected export UID$'):
            self.evidence.check_export_records(roots, wrong)
        with self.assertRaisesRegex(AssertionError, '^missing/extra source-declared fixture roots$'):
            self.evidence.protocol_root_roles([('protocol-StartFailure', text.replace(declaration, ''))])

    def test_combined_verify_requires_each_native_descendant_after_positive(self):
        text = self.full_evidence()
        self.evidence.check_text(text, 'verify')
        # Remove the complete RUN/PASS pair, not a mismatched half that could fail earlier.
        name = 'TestRegularObservationBoundary/' + self.evidence.NESTED['TestRegularObservationBoundary'][-1]
        missing = text.replace('=== RUN   ' + name + '\n', '').replace('--- PASS: ' + name + ' (0.00s)\n', '')
        with self.assertRaisesRegex(AssertionError, '^missing required nested case: TestRegularObservationBoundary$'):
            self.evidence.check_text(missing, 'verify')

    def check_fatal(self, roots, entries):
        self.evidence.check_export_records(roots, entries)
        self.evidence.check_fatal_owner_witnesses(roots, entries)

    def test_fatal_qualified_and_unqualified_enter2_sensitivity(self):
        for qualified in (True, False):
            with self.subTest(qualified=qualified):
                roots, entries = self.fatal_witness_seed(qualified)
                self.check_fatal(roots, entries)
                missing = [entry for entry in entries if entry['path'] != 'read-enter-2']
                bad = copy.deepcopy(entries)
                entry = next(entry for entry in bad if entry['path'] == 'read-enter-2')
                entry['payload'] = self.evidence.export_encoded(b'wrong frame')
                entry['metadata']['size'] = len(b'wrong frame')
                if qualified:
                    with self.assertRaisesRegex(AssertionError, '^fatal witness missing regular read-enter-2$'):
                        self.check_fatal(roots, missing)
                    with self.assertRaisesRegex(AssertionError, '^fatal witness read-enter-2 frame$'):
                        self.check_fatal(roots, bad)
                else:
                    self.check_fatal(roots, missing)
                    self.check_fatal(roots, bad)  # No invented universal second-read requirement.

    def test_fatal_mandatory_evidence_never_becomes_false_predicate(self):
        for qualified in (True, False):
            roots, entries = self.fatal_witness_seed(qualified)
            self.check_fatal(roots, entries)
            for name in ('cleanup-complete', 'fatal-unwind', 'read-enter-1', 'read-return-1'):
                with self.subTest(qualified=qualified, missing=name):
                    with self.assertRaisesRegex(AssertionError, 'missing required journals:.*' + name):
                        self.check_fatal(roots, [entry for entry in entries if entry['path'] != name])
            with self.assertRaisesRegex(AssertionError, '^fatal witness missing regular wrapper-trace$'):
                self.check_fatal(roots, [entry for entry in entries if entry['path'] != 'wrapper-trace'])

    def test_fatal_transcript_configured_root_complete_declarations(self):
        text = self.protocol_seed('protocol-FatalOwner')
        self.evidence.check_text(text, 'protocol-FatalOwner')
        roots = self.evidence.protocol_root_roles([('protocol-FatalOwner', text)])
        self.assertEqual(len(roots), 2)
        self.assertTrue(roots[str(self.tmp / 'dotty-protocol-102')]['nested_fatal'])
        with self.assertRaisesRegex(AssertionError, '^fatal missing/duplicate/invalid field: .*intentional private fatal owner unwind$'):
            self.evidence.check_text(text.replace('intentional private fatal owner unwind', 'altered diagnostic'), 'protocol-FatalOwner')


if __name__ == '__main__':
    unittest.main()
