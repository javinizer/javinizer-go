"""Controller-only fault tests: no Windows API, native process, Go, or real handle.

Mock objects are never passed to an OS API. These do not validate native ABI,
console behavior, build provenance, or the production CLI's output.
"""
import importlib.util
import json
from pathlib import Path
import struct
import subprocess
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('owned_console', Path(__file__).with_name('owned_windows_console.py'))
m = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(m)


def fake_pe(machine=0x8664, subsystem=3):
    data = bytearray(256)
    data[:2] = b'MZ'
    struct.pack_into('<I', data, 0x3c, 64)
    data[64:68] = b'PE\0\0'
    struct.pack_into('<H', data, 68, machine)
    struct.pack_into('<H', data, 88, 0x20b)
    struct.pack_into('<H', data, 156, subsystem)
    return data


class ControllerTests(unittest.TestCase):
    def test_pe_rejects_arm64_even_with_pe32plus(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / 'header.bin'
            path.write_bytes(fake_pe())
            self.assertEqual(m.pe_subsystem(path), 3)
            path.write_bytes(fake_pe(machine=0xaa64))
            with self.assertRaisesRegex(ValueError, 'amd64 COFF'):
                m.pe_subsystem(path)

    def test_capture_records_complete_bytes_exit_and_join(self):
        child = mock.Mock(pid=12, returncode=1)
        child.communicate.return_value = (b'', b'error\n')
        record = {}
        m.capture(child, record, lambda: None)
        self.assertEqual(record['stderr_hex'], b'error\n'.hex())
        self.assertEqual(record['exit'], 1)
        self.assertTrue(record['joined'])
        child.kill.assert_not_called()

    def test_timeout_preserved_before_kill_and_drain(self):
        failure = subprocess.TimeoutExpired('child', 20, output=b'partial-out', stderr=b'partial-err')
        child = mock.Mock(pid=13, returncode=124)
        child.poll.return_value = None
        child.communicate.side_effect = [failure, (b'partial-out-end', b'partial-err-end')]
        record, snapshots = {}, []
        with self.assertRaises(subprocess.TimeoutExpired) as raised:
            m.capture(child, record, lambda: snapshots.append(json.loads(json.dumps(record))))
        self.assertIs(raised.exception, failure)
        self.assertTrue(any(s.get('stdout_hex') == b'partial-out'.hex() and s.get('timed_out') for s in snapshots))
        self.assertEqual(record['first_timeout_output']['stdout_hex'], b'partial-out'.hex())
        self.assertEqual(record['first_timeout_output']['stderr_hex'], b'partial-err'.hex())
        self.assertEqual(record['stdout_hex'], b'partial-out-end'.hex())
        self.assertEqual(record['pid'], 13)
        self.assertTrue(record['joined'])
        child.kill.assert_called_once()

    def test_kill_and_drain_failure_do_not_replace_primary_timeout(self):
        failure = subprocess.TimeoutExpired('child', 20, output=b'first', stderr=b'err')
        child = mock.Mock(pid=14, returncode=None)
        child.poll.return_value = None
        child.kill.side_effect = OSError('kill denied')
        child.communicate.side_effect = [failure, subprocess.TimeoutExpired('drain', 20, output=b'last')]
        record = {}
        with self.assertRaises(subprocess.TimeoutExpired) as raised:
            m.capture(child, record, lambda: None)
        self.assertIs(raised.exception, failure)
        self.assertEqual(len(record['cleanup_failures']), 2)
        self.assertEqual(record['stderr_hex'], b'err'.hex())
        self.assertEqual(record['stdout_hex'], b'last'.hex())
        self.assertNotIn('joined', record)

    def test_cleanup_actions_are_independent_and_failures_retained(self):
        record, calls = {}, []
        def fail():
            calls.append('terminate')
            raise OSError('terminate denied')
        m.best_effort(fail, 'terminate', record, lambda: None)
        m.best_effort(lambda: calls.append('close'), 'close', record, lambda: None)
        self.assertEqual(calls, ['terminate', 'close'])
        self.assertEqual(record['cleanup_completed'], ['close'])
        self.assertIn('terminate denied', record['cleanup_failures'][0])

    def test_capacity_abort_polls_same_process_without_resending_input(self):
        abort = threading.Event()
        child = mock.Mock(pid=20, returncode=None)
        def progress(*args, **kwargs):
            abort.set()
            raise subprocess.TimeoutExpired('poll', 0.5, output=b'progress', stderr=b'partial')
        child.communicate.side_effect = progress
        record = {}
        with self.assertRaisesRegex(RuntimeError, 'capacity watcher'):
            m.capture(child, record, lambda: None, b'handshake', timeout=20,
                      abort=abort, cleanup_on_error=False)
        self.assertEqual(child.communicate.call_count, 1)
        self.assertEqual(child.communicate.call_args.args, (b'handshake',))
        child.kill.assert_not_called()  # Owning driver closes/terminates job first.
        self.assertEqual(record['stdout_hex'], b'progress'.hex())
        self.assertNotIn('timed_out', record)  # Poll timeout is not terminal timeout.

    def test_internal_poll_timeout_never_resends_handshake_or_restarts_child(self):
        child = mock.Mock(pid=21, returncode=0)
        child.communicate.side_effect = [subprocess.TimeoutExpired('poll', 0.5, output=b'part'), (b'complete', b'')]
        record = {}
        out, err = m.capture(child, record, lambda: None, b'handshake', timeout=20,
                             abort=threading.Event(), cleanup_on_error=False)
        self.assertEqual((out, err), (b'complete', b''))
        self.assertEqual(child.communicate.call_args_list[0].args, (b'handshake',))
        self.assertEqual(child.communicate.call_args_list[1].args, (None,))
        self.assertNotIn('timed_out', record)
        child.kill.assert_not_called()

    def test_receipt_write_failure_does_not_mask_primary_timeout(self):
        failure = subprocess.TimeoutExpired('child', 20, output=b'first')
        child = mock.Mock(pid=16, returncode=124)
        child.poll.return_value = None
        child.communicate.side_effect = [failure, (b'final', b'')]
        record = {}
        with mock.patch.object(m.sys, 'stderr'), self.assertRaises(subprocess.TimeoutExpired) as raised:
            m.capture(child, record, mock.Mock(side_effect=OSError('disk full')))
        self.assertIs(raised.exception, failure)
        self.assertTrue(record['receipt_failures'])
        child.kill.assert_called_once()
        self.assertTrue(record['joined'])

    def test_assignment_failure_never_sends_handshake_and_cleans_host(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            manifest = {'source_commit': 'a3a376b279fcb7f1b2292835038a1adf4d75851e',
                        'build_desktop': '1', 'tags': 'desktop,production',
                        'expected_version_line': 'javinizer fixture\n'}
            for name, subsystem in [('console', 3), ('gui', 2)]:
                path = root / (name + '.bin')
                path.write_bytes(fake_pe(subsystem=subsystem))
                manifest[name] = {'path': str(path), 'sha256': m.digest(path)}
            manifest_path = root / 'manifest.json'
            manifest_path.write_text(json.dumps(manifest))
            child = mock.Mock(pid=15, returncode=124)
            child.poll.return_value = None
            child.communicate.return_value = (b'host-partial', b'')
            kernel = mock.Mock()
            # Opaque controller objects, never real/fabricated OS handles.
            kernel.CreateJobObjectW.return_value = object()
            kernel.OpenProcess.return_value = object()
            original = OSError('assignment denied')
            kernel.AssignProcessToJobObject.side_effect = original
            kernel.TerminateJobObject.side_effect = OSError('termination denied')
            kernel.CloseHandle.side_effect = OSError('close denied')
            with mock.patch.object(m.subprocess, 'CREATE_NEW_CONSOLE', 0x10, create=True), \
                    mock.patch.object(m, 'kernel', return_value=kernel), \
                    mock.patch.object(m, 'assert_native_abi'), \
                    mock.patch.object(m.subprocess, 'Popen', return_value=child):
                with self.assertRaises(OSError) as raised:
                    m.supervise(SimpleNamespace(manifest=manifest_path, output=root / 'evidence', expected_checkout=manifest['source_commit']))
            self.assertIs(raised.exception, original)
            # Only bounded cleanup communicate; request payload was never sent.
            child.communicate.assert_called_once_with(timeout=m.TIMEOUT)
            child.kill.assert_called_once()
            kernel.TerminateJobObject.assert_called_once()
            self.assertEqual(kernel.CloseHandle.call_count, 2)
            receipt = json.loads(next((root / 'evidence').glob('*/lifecycle.json')).read_text())
            self.assertNotIn('job_assigned_before_handshake', receipt)
            self.assertEqual(len(receipt['cleanup_failures']), 3)
            self.assertIn('assignment denied', receipt['primary_failure'])
            self.assertEqual(receipt['stdout_hex'], b'host-partial'.hex())


if __name__ == '__main__':
    unittest.main()
