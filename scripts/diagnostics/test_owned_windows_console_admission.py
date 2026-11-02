"""Pure admission tests; synthetic evidence never stands for native execution."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock
from types import SimpleNamespace
import threading
import run_owned_windows_console_experiment as m


class AdmissionTests(unittest.TestCase):
    def test_source_binding_uses_git_blobs_and_rejects_dirty_or_wrong_producer(self):
        # Real local Git checkout rules; this is not native Windows execution.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'web/frontend').mkdir(parents=True)
            evidence = root / 'evidence'
            evidence.mkdir()
            def git(*command):
                return m.subprocess.check_output(['git', *command], cwd=root,
                                                  env=m.clean_git_env(), stderr=m.subprocess.PIPE)
            git('init')
            git('config', 'core.autocrlf', 'true')
            git('config', 'core.safecrlf', 'false')
            lock = root / 'web/frontend/package-lock.json'
            lock.write_bytes(b'line\n')
            (root / 'LICENSE').write_bytes(b'license\n')
            git('add', 'LICENSE', 'web/frontend/package-lock.json')
            git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-m', 'owned fixture')
            checkout = git('rev-parse', 'HEAD').decode().strip()
            proof = {'package_lock_sha256': m.source_byte_summary(b'line\n')['sha256'],
                     'repository_license_sha256': m.digest(root / 'LICENSE')}
            for working in (b'line\n', b'line\r\n'):
                lock.write_bytes(working)
                m.verify_source_metadata(root, proof, checkout, evidence)
                report = json.loads((evidence / 'asset-source-byte-diagnostics.json').read_text(encoding='utf-8'))
                self.assertTrue(report['tracked_checkout_clean'])
                self.assertEqual(report['files']['web/frontend/package-lock.json']['working_tree'], m.source_byte_summary(working))
            for flags in (('assume-unchanged',), ('skip-worktree',), ('assume-unchanged', 'skip-worktree')):
                for relative in ('LICENSE', 'web/frontend/package-lock.json'):
                    target = root / relative
                    original = target.read_bytes()
                    for flag in flags:
                        git('update-index', '--' + flag, relative)
                    target.write_bytes(b'hidden substantive change\n')
                    with self.subTest(flags=flags, path=relative), self.assertRaisesRegex(ValueError, 'tracked index flags'):
                        m.verify_source_metadata(root, proof, checkout, evidence)
                    rejected = json.loads((evidence / 'asset-source-byte-diagnostics.json').read_text(encoding='utf-8'))
                    self.assertNotIn('tracked_checkout_clean', rejected)
                    summary = json.loads((evidence / 'asset-source-index-flags-summary.json').read_text(encoding='utf-8'))
                    self.assertEqual(len(summary['hidden_entries_hex']), 1)
                    self.assertEqual(rejected['files'][relative]['working_tree'], m.source_byte_summary(target.read_bytes()))
                    # The verifier must reject, never mutate the caller's index.
                    status = git('ls-files', '-v', '--', relative)
                    self.assertEqual(status[:1], b's' if len(flags) == 2 else (b'h' if flags[0] == 'assume-unchanged' else b'S'))
                    for flag in flags:
                        git('update-index', '--no-' + flag, relative)
                    target.write_bytes(original)
            lock.write_bytes(b'arbitrary modification\r\n')
            with self.assertRaisesRegex(RuntimeError, 'asset-source-tracked-clean exited 1'):
                m.verify_source_metadata(root, proof, checkout, evidence)
            report = json.loads((evidence / 'asset-source-byte-diagnostics.json').read_text(encoding='utf-8'))
            self.assertNotIn('tracked_checkout_clean', report)
            self.assertEqual(report['files']['web/frontend/package-lock.json']['working_tree'], m.source_byte_summary(lock.read_bytes()))
            lock.write_bytes(b'line\r\n')
            wrong = dict(proof, package_lock_sha256='0' * 64)
            with self.assertRaisesRegex(ValueError, 'differs from tested checkout Git blobs'):
                m.verify_source_metadata(root, wrong, checkout, evidence)
            with mock.patch.object(m, 'run', side_effect=RuntimeError('diagnostic unavailable')):
                with self.assertRaisesRegex(RuntimeError, 'diagnostic unavailable'):
                    m.verify_source_metadata(root, proof, checkout, evidence)
            failed = json.loads((evidence / 'asset-source-byte-diagnostics.json').read_text(encoding='utf-8'))
            self.assertEqual(failed['files']['web/frontend/package-lock.json']['working_tree'], m.source_byte_summary(lock.read_bytes()))
            self.assertIn('diagnostic unavailable', failed['diagnostic_failure'])

    def test_trimpath_metadata_and_recorded_build_provenance(self):
        import struct
        with tempfile.TemporaryDirectory() as temporary:
            executable = Path(temporary) / 'console.exe'
            data = bytearray(200)
            data[:2] = b'MZ'
            struct.pack_into('<I', data, 0x3c, 64)
            data[64:68] = b'PE\0\0'
            struct.pack_into('<H', data, 68, 0x8664)
            struct.pack_into('<H', data, 88, 0x20b)
            struct.pack_into('<H', data, 156, 3)
            executable.write_bytes(data)
            flags = '-w -s -X github.com/javinizer/javinizer-go/internal/desktop.BuildDesktop=1'
            command = ['go', 'build', '-trimpath', '-tags', 'desktop,production', '-cover',
                       '-covermode=atomic', '-coverpkg=' + m.PACKAGE, '-ldflags', flags,
                       '-o', str(executable), './cmd/javinizer']
            settings = {'-buildmode': 'exe', '-compiler': 'gc', '-cover': 'true',
                        '-tags': 'desktop,production', '-trimpath': 'true',
                        'CGO_ENABLED': '1', 'GOOS': 'windows', 'GOARCH': 'amd64'}
            metadata = str(executable) + ': go1.27.3\n\tpath\t' + m.PACKAGE + '\n'
            metadata += ''.join('\tbuild\t' + key + '=' + value + '\n' for key, value in settings.items())
            self.assertNotIn('BuildDesktop', metadata)
            m.verify_compiled_build(executable, 'console', command, metadata, 'go1.27.3', flags)
            for wrong in (metadata.replace('go1.27.3', 'go1.27.2'),
                          metadata.replace('\tpath\t' + m.PACKAGE, '\tpath\twrong/package'),
                          metadata.replace('GOARCH=amd64', 'GOARCH=arm64'),
                          metadata.replace('-cover=true', '-cover=false'),
                          metadata + '\tbuild\tGOOS=windows\n'):
                with self.subTest(wrong=wrong), self.assertRaises(ValueError):
                    m.verify_compiled_build(executable, 'console', command, wrong, 'go1.27.3', flags)
            wrong_command = list(command)
            wrong_command[wrong_command.index('-ldflags') + 1] = flags.replace('BuildDesktop=1', 'BuildDesktop=0')
            with self.assertRaisesRegex(ValueError, 'recorded build command'):
                m.verify_compiled_build(executable, 'console', wrong_command, metadata, 'go1.27.3', flags)
            with self.assertRaisesRegex(ValueError, 'recorded build command'):
                m.verify_compiled_build(executable, 'console', wrong_command, metadata, 'go1.27.3', wrong_command[9])
            with self.assertRaisesRegex(ValueError, 'PE subsystem'):
                m.verify_compiled_build(executable, 'gui', command, metadata, 'go1.27.3', flags)
            struct.pack_into('<H', data, 156, 2)
            executable.write_bytes(data)
            gui_flags = flags + ' -H windowsgui'
            gui_command = list(command)
            gui_command[gui_command.index('-ldflags') + 1] = gui_flags
            m.verify_compiled_build(executable, 'gui', gui_command, metadata, 'go1.27.3', gui_flags)

    def test_tool_job_assignment_failure_keeps_pid_primary_and_all_cleanup(self):
        kernel = mock.Mock()
        kernel.CreateJobObjectW.return_value = object()
        kernel.OpenProcess.return_value = object()
        primary = OSError('tool assignment denied')
        kernel.AssignProcessToJobObject.side_effect = primary
        kernel.TerminateJobObject.side_effect = OSError('terminate denied')
        kernel.CloseHandle.side_effect = OSError('close denied')
        child = mock.Mock(pid=701, returncode=None)
        child.poll.return_value = None
        child.kill.side_effect = OSError('kill denied')
        child.communicate.side_effect = OSError('drain denied')
        receipt, snapshots = {}, []
        with mock.patch.object(m.owned, 'kernel', return_value=kernel), \
                mock.patch.object(m.owned, 'assert_native_abi'), \
                mock.patch.object(m.subprocess, 'Popen', return_value=child):
            with self.assertRaises(OSError) as raised:
                m.run_windows_job(['owned-tool'], Path('.'), {}, 20, receipt,
                                  lambda: snapshots.append(json.loads(json.dumps(receipt))))
        self.assertIs(raised.exception, primary)
        self.assertEqual(receipt['pid'], 701)
        self.assertIn('tool assignment denied', receipt['primary_failure'])
        self.assertTrue(any(row.get('pid') == 701 and 'primary_failure' not in row for row in snapshots))
        child.communicate.assert_called_once_with(timeout=m.owned.TIMEOUT)
        self.assertEqual(len(receipt['cleanup_failures']), 5)
        kernel.TerminateJobObject.assert_called_once()
        self.assertEqual(kernel.CloseHandle.call_count, 2)
        child.kill.assert_called_once()
        self.assertNotIn('job_assigned_before_handshake', receipt)

    def test_continuous_capacity_floor_terminates_owned_tree_and_retains_failure(self):
        state, saved = {}, []
        terminate = mock.Mock()
        m.monitor_capacity(['build-root', 'temp-root'], threading.Event(), terminate,
                           lambda value: saved.append(copy.deepcopy(value)), state,
                           sample=lambda root: SimpleNamespace(free=m.FLOOR_BYTES - (root == 'temp-root')))
        terminate.assert_called_once()
        self.assertEqual(state['samples'], 1)
        self.assertIn('96 MiB', state['failure'])
        self.assertTrue(state['termination_requested'])
        self.assertEqual(saved[0]['minimum_free_bytes'], [m.FLOOR_BYTES, m.FLOOR_BYTES - 1])

    def test_watcher_start_or_join_failure_cannot_skip_owned_cleanup(self):
        for phase in ('start', 'join'):
            with self.subTest(phase=phase):
                kernel = mock.Mock()
                kernel.CreateJobObjectW.return_value = object()
                kernel.OpenProcess.return_value = object()
                child = mock.Mock(pid=702, returncode=0)
                child.poll.return_value = 0
                child.communicate.return_value = (b'', b'')
                watcher = mock.Mock()
                watcher.is_alive.return_value = False
                primary = RuntimeError('watcher ' + phase + ' failed')
                getattr(watcher, phase).side_effect = primary
                receipt = {}
                with mock.patch.object(m.owned, 'kernel', return_value=kernel), \
                        mock.patch.object(m.owned, 'assert_native_abi'), \
                        mock.patch.object(m.subprocess, 'Popen', return_value=child), \
                        mock.patch.object(m.threading, 'Thread', return_value=watcher):
                    with self.assertRaises(RuntimeError) as raised:
                        m.run_windows_job(['owned-tool'], Path('.'), {}, 20, receipt, lambda: None)
                if phase == 'start':
                    self.assertIs(raised.exception, primary)
                    child.communicate.assert_called_once_with(timeout=m.owned.TIMEOUT)
                else:
                    self.assertIn('cleanup', str(raised.exception))
                    self.assertTrue(any('watcher join' in failure for failure in receipt['cleanup_failures']))
                kernel.TerminateJobObject.assert_called_once()
                self.assertEqual(kernel.CloseHandle.call_count, 2)
                self.assertTrue(receipt['joined'])

    def test_build_environment_cannot_inherit_source_or_compiler_overrides(self):
        inherited = {'GOENV': 'hostile.env', 'GOFLAGS': '-overlay=hostile.json',
                     'GOEXPERIMENT': 'unreviewed', 'CC': 'unreviewed-cc',
                     'CGO_CFLAGS': '-include hostile.h', 'GIT_DIR': 'wrong-repo',
                     'GIT_CONFIG_COUNT': '1', 'GIT_CONFIG_KEY_0': 'core.worktree',
                     'GIT_CONFIG_VALUE_0': 'wrong-tree', 'GOCACHE': 'owned-cache'}
        with mock.patch.dict(m.os.environ, inherited, clear=True):
            env = m.clean_build_env()
        self.assertEqual(env['GOENV'], 'off')
        self.assertEqual(env['GIT_NO_REPLACE_OBJECTS'], '1')
        self.assertEqual(env['GOWORK'], 'off')
        self.assertEqual(env['GOCACHE'], 'owned-cache')
        for key in ('GOFLAGS', 'GOEXPERIMENT', 'CC', 'CGO_CFLAGS', 'GIT_DIR', 'GIT_CONFIG_COUNT', 'GIT_CONFIG_KEY_0'):
            self.assertNotIn(key, env)

    def test_bundle_digest_and_same_run_producer_are_externally_bound(self):
        with tempfile.TemporaryDirectory() as td:
            bundle = Path(td) / 'asset.tar'
            bundle.write_bytes(b'fixture transport bytes')
            producer = {'run_id': '123', 'run_attempt': '1', 'job': 'static-e2e', 'outcome': 'success'}
            expected = m.digest(bundle)
            proof = {'producer': producer, 'bundle_sha256': expected}
            m.verify_bundle_identity(bundle, proof, expected, producer)
            for wrong in [dict(producer, run_id='124'), dict(producer, outcome='failure'), dict(producer, job='other')]:
                with self.assertRaises(ValueError):
                    m.verify_bundle_identity(bundle, proof, expected, wrong)
            bundle.write_bytes(b'tampered download')
            with self.assertRaises(ValueError):
                m.verify_bundle_identity(bundle, proof, expected, producer)

    def test_push_and_merge_provenance(self):
        a, b, c, tree = 'a' * 40, 'b' * 40, 'c' * 40, 'd' * 40
        push = {'checkout': a, 'tree': tree, 'parents': [b], 'event': 'push', 'head': a, 'base': ''}
        m.validate_provenance(push, a, 'push', a, '')
        merge = dict(push, checkout=c, parents=[b, a], event='pull_request', base=b)
        m.validate_provenance(merge, c, 'pull_request', a, b)
        for bad in [dict(merge, parents=[a, b]), dict(merge, head=b), dict(merge, tree=''), dict(merge, checkout=a)]:
            with self.assertRaises(ValueError):
                m.validate_provenance(bad, c, 'pull_request', a, b)

    def test_real_asset_shape_hash_and_identity_required(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / '_app').mkdir()
            (root / 'index.html').write_text('<html>fixture</html>')
            (root / '_app/client.js').write_text('fixture')
            info = {'checkout': 'a' * 40}
            proof = {'provenance': info, 'files': m.inventory(root)}
            m.verify_assets(root, proof, info)
            with self.assertRaises(ValueError):
                m.verify_assets(root, proof, {'checkout': 'b' * 40})
            (root / '_app/client.js').write_text('modified')
            with self.assertRaises(ValueError):
                m.verify_assets(root, proof, info)
            (root / 'index.html').write_text('Javinizer Web UI Placeholder')
            proof['files'] = m.inventory(root)
            with self.assertRaises(ValueError):
                m.verify_assets(root, proof, info)

    def profile(self, gui):
        positive = {25, 26, 29, 30, 32, 33} if gui else {25, 26, 27}
        return 'mode: atomic\n' + '\n'.join(
            f'{m.CONSOLE_SOURCE}:{line}.1,{line}.2 1 {int(line in positive)}'
            for line in (25, 26, 27, 29, 30, 32, 33))

    def test_both_actual_branch_shapes_required_separately(self):
        for gui in (False, True):
            profile = self.profile(gui)
            m.profile_counts(profile, gui)
            with self.assertRaises(ValueError):
                m.profile_counts(profile, not gui)
            rows = profile.splitlines()
            for bad in ['\n'.join(rows[:-1]), profile.replace(' 1 1', ' 1 0'),
                        profile.replace(m.PACKAGE, 'other/package'), profile + '\n' + rows[-1],
                        profile.replace('mode: atomic', 'mode: set'), profile + '\nmalformed']:
                with self.assertRaises(ValueError):
                    m.profile_counts(bad, gui)

    def records(self):
        manifest = {'expected_version_line': 'javinizer fixture\n'}
        records = []
        for subsystem in ('console', 'gui'):
            for gc in ('default', '1'):
                for command in range(2):
                    records.append({'case': f'{subsystem}-{gc}-{len(records)}', 'gc': gc,
                        'args': ['--version'] if command == 0 else ['version', '--owned-console-probe-invalid'],
                        'exit': command, 'joined': True,
                        'stdout_hex': (manifest['expected_version_line'] if command == 0 else '').encode().hex(),
                        'stderr_hex': ('' if command == 0 else m.owned.INVALID_STDERR).encode().hex()})
        return records, manifest

    def test_all_eight_exact_output_records_are_independently_rechecked(self):
        records, manifest = self.records()
        m.verify_records(records, manifest)
        for field, value in [('stderr_hex', b'unknown flag only'.hex()), ('exit', 0), ('joined', False),
                             ('gc', 'default'), ('args', ['--help']), ('timed_out', True),
                             ('cleanup_failures', ['not joined'])]:
            bad = copy.deepcopy(records)
            bad[-1][field] = value
            with self.assertRaises(ValueError):
                m.verify_records(bad, manifest)
        with self.assertRaises(ValueError):
            m.verify_records(records[:-1], manifest)


if __name__ == '__main__':
    unittest.main()
