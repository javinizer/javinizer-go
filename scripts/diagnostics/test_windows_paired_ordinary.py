"""Controller mocks and safe Python emitter subprocesses; no Go or product tests."""
import collections
import importlib.util
import json
import io
import os
import sys
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch, Mock

spec = importlib.util.spec_from_file_location('paired', Path(__file__).with_name('windows_paired_ordinary.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def events(names, terminal='pass'):
    return '\n'.join(json.dumps(e) for e in
        [e for name in sorted(names) for e in ({'Action': 'run', 'Test': name}, {'Action': 'pass', 'Test': name})]
        + [{'Action': terminal}])


class ParserTests(unittest.TestCase):
    def test_exact_guard(self):
        raw = events(m.GUARDS)
        audit = 'OFFLINE279_BLOCKED\n' * 26 + 'OFFLINE279_BLOCKED_HTTP_TARGET\n' * 4
        self.assertEqual(m.validate(raw, audit, 0, m.GUARDS, True, True)['validation_errors'], [])
        for badraw, badaudit, code in [(raw + '\nnot json', audit, 0), (raw, audit + 'wrong\n', 0),
                                       (raw, audit, 1), (events(set()), audit, 0)]:
            self.assertTrue(m.validate(badraw, badaudit, code, m.GUARDS, True, True)['validation_errors'])

    def test_product_false_green(self):
        good = events({m.AUTH})
        self.assertFalse(m.validate(good, '', 0, {m.AUTH}, True)['validation_errors'])
        for raw, audit, code in [(good, 'OFFLINE279_BLOCKED\n', 0), (good, '', 1),
                                  (good + '\n' + json.dumps({'Action': 'fail', 'Test': 'Earlier'}), '', 0),
                                  (good + '\n' + json.dumps({'Action': 'pass'}), '', 0),
                                  (events({m.AUTH, 'Unexpected'}), '', 0),
                                  (good + '\n' + json.dumps({'Action': 'skip', 'Test': 'Skipped'}), '', 0),
                                  (events(set()), '', 0)]:
            self.assertTrue(m.validate(raw, audit, code, {m.AUTH}, True)['validation_errors'])

    def test_full_inventory_and_only_reviewed_skips(self):
        raw = events({'Critical'}) + '\n' + '\n'.join(json.dumps(e) for e in (
            {'Action': 'run', 'Test': 'ExpectedSkip'}, {'Action': 'skip', 'Test': 'ExpectedSkip'}))
        kwargs = dict(required={'Critical'}, inventory={'Critical', 'ExpectedSkip'}, allowed_skips={'ExpectedSkip'})
        self.assertFalse(m.validate(raw, '', 0, **kwargs)['validation_errors'])
        self.assertTrue(m.validate(events({'Critical'}), '', 0, **kwargs)['validation_errors'])
        self.assertTrue(m.validate(raw, '', 0, required={'Critical'}, inventory={'Critical', 'ExpectedSkip'})['validation_errors'])
        unexpected = raw + '\n' + json.dumps({'Action': 'skip', 'Test': 'Critical/unreviewed'})
        self.assertTrue(m.validate(unexpected, '', 0, **kwargs)['validation_errors'])

    def test_schedule_is_fixed_and_bounded(self):
        self.assertEqual(len(m.SCHEDULE), 18)
        self.assertEqual([x[0] for x in m.SCHEDULE[:9]], ['original', 'cleanup', 'fixed', 'cleanup', 'fixed', 'original', 'fixed', 'original', 'cleanup'])
        for rev in m.REVISIONS:
            items = [(p, s) for r, p, s in m.SCHEDULE if r == rev]
            self.assertEqual(items.count(('server', None)), 3)
            self.assertEqual(items.count(('core', m.PREFIX)), 1)
            self.assertEqual(items.count(('core', m.CORE)), 1)
            self.assertEqual(items.count(('server', {m.AUTH})), 1)


class WorkflowScopeTests(unittest.TestCase):
    def test_expensive_matrix_is_manual_only_and_pr_job_is_lightweight(self):
        workflow = (Path(__file__).resolve().parents[2] / '.github/workflows/windows-paired-ordinary.yml').read_text(encoding='utf-8')
        jobs = workflow.split('jobs:\n', 1)[1]
        checks, compare = jobs.split('  compare:\n', 1)
        guard = "    if: github.event_name == 'workflow_dispatch'\n"
        self.assertEqual(compare.count(guard), 1)
        self.assertLess(compare.index(guard), compare.index('    runs-on:'))
        self.assertLess(compare.index(guard), compare.index('    strategy:'))
        self.assertIn("    if: github.event_name == 'pull_request'\n", checks)
        self.assertIn('    timeout-minutes: 5\n', checks)
        self.assertNotIn('strategy:', checks)
        self.assertNotIn('setup-go', checks)
        commands = [line.strip()[5:] for line in checks.splitlines() if line.strip().startswith('run: ')]
        self.assertEqual(commands, ['python -m unittest discover -s scripts/diagnostics -p test_windows_paired_ordinary.py -v'])


class EmitterTests(unittest.TestCase):
    def test_cp1252_stream_reconfigured_without_mutating_raw_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            evidence = root / 'paired-evidence'
            evidence.mkdir()
            source = evidence / '日本語-🙂.log'
            raw = '漢字 🙂 λ\n::error::synthetic message\n'.encode('utf-8') + b'\xff'
            source.write_bytes(raw)
            out_buffer, err_buffer = io.BytesIO(), io.BytesIO()
            out = io.TextIOWrapper(out_buffer, encoding='cp1252', errors='strict')
            err = io.TextIOWrapper(err_buffer, encoding='cp1252', errors='strict')
            with patch.object(m.sys, 'stdout', out), patch.object(m.sys, 'stderr', err):
                m.emit_logs(root)
                self.assertEqual(out.encoding, 'utf-8')
                self.assertEqual(err.encoding, 'utf-8')
                rendered = out_buffer.getvalue().decode('utf-8')
            self.assertIn('日本語-🙂.log', rendered)
            self.assertIn('EVIDENCE 漢字 🙂 λ', rendered)
            self.assertIn('EVIDENCE ::error::synthetic message', rendered)
            self.assertIn('EVIDENCE \ufffd', rendered)
            self.assertEqual(source.read_bytes(), raw)

    def test_actual_emit_subprocess_forced_cp1252(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            evidence = root / 'paired-evidence'
            evidence.mkdir()
            source = evidence / '証拠-🙂.txt'
            raw = '日本語 🙂 λ\n::warning::synthetic warning\r\n'.encode('utf-8')
            source.write_bytes(raw)
            env = os.environ.copy()
            env.update(PYTHONIOENCODING='cp1252:strict', PYTHONUTF8='0', PYTHONCOERCECLOCALE='0')
            script = str(Path(m.__file__).resolve())
            encoding = subprocess.run([sys.executable, '-c', 'import sys; print(sys.stdout.encoding)'], cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False)
            self.assertEqual(encoding.returncode, 0)
            self.assertEqual(encoding.stdout.strip().lower(), b'cp1252')
            result = subprocess.run([sys.executable, script, '--emit-logs'], cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False)
            self.assertEqual(result.returncode, 0, result.stderr.decode('utf-8', errors='replace'))
            rendered = result.stdout.decode('utf-8')
            self.assertIn('証拠-🙂.txt', rendered)
            self.assertIn('EVIDENCE 日本語 🙂 λ', rendered)
            self.assertIn('EVIDENCE ::warning::synthetic warning', rendered)
            self.assertTrue(all(not line.startswith('::') for line in rendered.splitlines()))
            self.assertEqual(source.read_bytes(), raw)
            self.assertEqual(result.stderr, b'')


class MetadataTests(unittest.TestCase):
    def test_valid_allowlisted_fields_and_multiple_cpus(self):
        examples = {
            'os': {'Caption': 'Windows', 'Version': '10.0', 'BuildNumber': '26100', 'OSArchitecture': '64-bit'},
            'cpu': {'Name': 'Example', 'Manufacturer': 'Example', 'NumberOfCores': 2, 'NumberOfLogicalProcessors': 4},
            'ram': {'TotalPhysicalMemory': 8589934592},
            'image': {'ImageOS': 'win25', 'ImageVersion': '20261015.1'},
        }
        for kind, value in examples.items():
            self.assertEqual(m.validate_metadata(kind, json.dumps(value)), [])
        self.assertEqual(m.validate_metadata('cpu', json.dumps([examples['cpu'], examples['cpu']])), [])

    def test_empty_null_malformed_missing_and_invalid_fields(self):
        for kind in ('os', 'cpu', 'ram', 'image'):
            for raw in ('', 'null', '{}', '[]', 'not JSON', '"text"'):
                self.assertTrue(m.validate_metadata(kind, raw), (kind, raw))
        for value in (0, -1, True, '8589934592', None):
            self.assertTrue(m.validate_metadata('ram', json.dumps({'TotalPhysicalMemory': value})))
        self.assertTrue(m.validate_metadata('image', json.dumps({'ImageOS': 'unavailable', 'ImageVersion': '20261015.1'})))
        self.assertTrue(m.validate_metadata('image', json.dumps({'ImageOS': 'win25', 'ImageVersion': ' '})))
        self.assertTrue(m.validate_metadata('cpu', json.dumps([{'Name': 'Example', 'Manufacturer': 'Example', 'NumberOfCores': 2, 'NumberOfLogicalProcessors': 4}, None])))
        errors = m.validate_metadata('os', json.dumps({'unexpected': 'do-not-echo'}))
        self.assertNotIn('do-not-echo', str(errors))

    def test_missing_metadata_makes_completed_sample_non_successful(self):
        for incomplete in ([], ['os.BuildNumber: missing or invalid']):
            with self.subTest(incomplete=incomplete), tempfile.TemporaryDirectory() as temp:
                def completed(controller):
                    controller.metadata_incomplete = incomplete
                    for record in controller.records:
                        record.update(status='finished', validation_errors=[])
                with patch.object(m.Controller, 'execute', completed), patch.object(m.Path, 'cwd', return_value=Path(temp)), patch.object(m.sys, 'platform', 'win32'), patch.object(m.sys, 'argv', ['controller']):
                    code = m.main()
                summary = json.loads((Path(temp) / 'paired-evidence/summary.json').read_text())
                self.assertEqual(code, int(bool(incomplete)))
                self.assertEqual(summary['success'], not incomplete)
                self.assertEqual(summary['metadata_incomplete'], incomplete)


class ProcessTests(unittest.TestCase):
    def test_success_preserves_stdout_stderr_and_exit(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'raw output\n')
                kwargs['stderr'].write(b'raw error\n')
                return Mock(wait=Mock(return_value=3))
            with patch.object(m.subprocess, 'Popen', side_effect=start):
                result, folder = c.run('one', ['fake'], temp)
            self.assertEqual(result['native_exit_code'], 3)
            self.assertEqual((folder / 'stdout.txt').read_text(), 'raw output\n')
            self.assertEqual((folder / 'stderr.txt').read_text(), 'raw error\n')
            self.assertFalse(c.halted)

    def test_timeout_stops_all_later_processes(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            proc = Mock(pid=1234, wait=Mock(side_effect=[subprocess.TimeoutExpired('fake', 1), 1]))
            with patch.object(m.subprocess, 'Popen', return_value=proc) as popen, patch.object(m.subprocess, 'run', return_value=Mock(returncode=0)) as cleanup:
                result, _ = c.run('timeout', ['fake'], temp)
                later, _ = c.run('later', ['fake'], temp)
            self.assertTrue(result['timed_out'])
            self.assertTrue(c.halted)
            self.assertEqual(popen.call_count, 1)
            self.assertIsNotNone(later['skipped_reason'])
            self.assertEqual(cleanup.call_args.args[0], ['taskkill', '/PID', '1234', '/T', '/F'])

    def test_cleanup_failure_still_prohibits_later_processes(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            proc = Mock(pid=1234, wait=Mock(side_effect=subprocess.TimeoutExpired('fake', 1)))
            with patch.object(m.subprocess, 'Popen', return_value=proc) as popen, patch.object(m.subprocess, 'run', side_effect=subprocess.TimeoutExpired('taskkill', 15)):
                result, _ = c.run('timeout', ['fake'], temp)
                c.run('later', ['fake'], temp)
            self.assertEqual(result['cleanup_error'], 'TimeoutExpired')
            self.assertEqual(popen.call_count, 1)

    def test_budget_and_launch_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            with patch.object(m.subprocess, 'Popen', side_effect=OSError('mock')):
                result, _ = c.run('launch', ['fake'], temp)
            self.assertEqual(result['launch_error'], 'OSError')
            c.deadline = 0
            with patch.object(m.subprocess, 'Popen') as popen:
                result, _ = c.run('expired', ['fake'], temp)
                popen.assert_not_called()
            self.assertIsNone(result['native_exit_code'])
            self.assertTrue(result['skipped_reason'])

    def test_no_overwrite(self):
        with tempfile.TemporaryDirectory() as temp:
            m.Controller(temp)
            with self.assertRaises(FileExistsError):
                m.Controller(temp)

    def test_default_gc_and_no_secret_metadata(self):
        with tempfile.TemporaryDirectory() as temp, patch.dict(m.os.environ, {'GOGC': 'secret-value', 'GOFLAGS': 'secret-value', 'GOENV': 'secret-value'}):
            c = m.Controller(temp)
            self.assertNotIn('GOGC', c.env)
            self.assertNotIn('GOFLAGS', c.env)
            self.assertEqual(c.env['GOENV'], 'off')
            self.assertNotIn('secret-value', (c.evidence / 'inherited-settings.json').read_text())


class OrchestrationTests(unittest.TestCase):
    def test_dependency_metadata_failure_retains_all_unstarted_attempts(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            called = []
            def checked(name, argv, cwd, limit=30, env=None):
                called.append((name, argv))
                if name == 'go-version': return 'go version go1.27.2 windows/amd64'
                if name == 'go-env': return json.dumps({'CGO_ENABLED': '1', 'GOAMD64': 'v1'})
                for rev, (sha, tree) in m.REVISIONS.items():
                    if name == rev + '-head': return sha
                    if name == rev + '-tree': return tree
                if name.endswith('-overlay-source'): return '10f013f25cb4aaa083de051e5315dcd927fded46'
                if name.endswith('-guard-source'): return 'b967d5d8603a8cae61282e17902d3dec76b26adc'
                if name.endswith('-make-overlay'):
                    folder = Path(argv[-1]); folder.mkdir(); (folder / 'overlay.json').write_text('{}')
                if name == 'original-package-dependencies':
                    self.assertEqual(argv, ['go', 'list', '-mod=readonly', '-deps', '-test', '-json', './internal/api/server', './internal/api/core'])
                    self.assertEqual(env['GOPROXY'], 'off')
                    self.assertEqual(env['GOVCS'], '*:off')
                    self.assertIn('-overlay=', env['GOFLAGS'])
                    raise RuntimeError('setup/identity gate failed: original-package-dependencies')
                return ''
            def metadata(name, argv, cwd, limit=30, env=None):
                self.assertTrue(name.startswith('metadata-'), 'No guard or product process may start after prerequisite failure')
                folder = c.evidence / name; folder.mkdir(); (folder / 'stdout.txt').write_text('null')
                return {'native_exit_code': 0, 'timed_out': False, 'launch_error': None}, folder
            with patch.object(c, 'checked', side_effect=checked), patch.object(c, 'run', side_effect=metadata), patch.object(m.shutil, 'which', return_value=__file__), patch.object(m, 'Controller', return_value=c), patch.object(m.sys, 'platform', 'win32'), patch.object(m.sys, 'argv', ['controller']):
                self.assertEqual(m.main(), 1)
            self.assertEqual(len(c.records), 18)
            self.assertTrue(all(r['status'] == 'not-run' and r['native_exit_code'] is None and r['started_at'] is None for r in c.records))
            self.assertFalse(any(name.endswith('-compile') for name, _ in called))
            self.assertTrue((c.evidence / 'manifest.json').is_file())
            saved = json.loads((c.evidence / 'outcomes.json').read_text())
            self.assertEqual(len(saved), 18)
            self.assertTrue(all(r['status'] == 'not-run' and 'original-package-dependencies' in r['skipped_reason'] for r in saved))
            self.assertFalse(json.loads((c.evidence / 'summary.json').read_text())['success'])

    def test_earlier_product_failure_survives_later_passes(self):
        with tempfile.TemporaryDirectory() as temp:
            c = m.Controller(temp)
            skips = json.loads(Path(m.__file__).with_name('windows_paired_skips.json').read_text())
            def checked(name, argv, cwd, limit=30, env=None):
                if name == 'go-version':
                    return 'go version go1.27.2 windows/amd64'
                if name == 'go-env':
                    return json.dumps({'CGO_ENABLED': '1', 'GOAMD64': 'v1'})
                for rev, (sha, tree) in m.REVISIONS.items():
                    if name == rev + '-head': return sha
                    if name == rev + '-tree': return tree
                if name.endswith('-overlay-source'): return '10f013f25cb4aaa083de051e5315dcd927fded46'
                if name.endswith('-guard-source'): return 'b967d5d8603a8cae61282e17902d3dec76b26adc'
                if name.endswith('-make-overlay'):
                    p = Path(argv[-1]); p.mkdir(); (p / 'overlay.json').write_text('{}')
                if name.endswith('-compile'):
                    Path(argv[argv.index('-o') + 1]).write_bytes(b'ordinary-mock-binary')
                if name.endswith('-inventory'):
                    return '\n'.join(m.PREFIX + sorted(m.CORE - set(m.PREFIX)) if '-core-' in name else [m.AUTH] + skips['server'])
                return ''
            def run(name, argv, cwd, limit=30, env=None, stdin_path=None):
                folder = c.evidence / name; folder.mkdir()
                code, raw, audit = 0, '', ''
                if name.endswith('-guard'):
                    raw = events(m.GUARDS)
                    audit = 'OFFLINE279_BLOCKED\n' * 26 + 'OFFLINE279_BLOCKED_HTTP_TARGET\n' * 4
                elif stdin_path:
                    raw = Path(stdin_path).read_text()
                elif name[:2].isdigit():
                    if name.endswith('-core') and any('TestAPIRuntime_BackgroundTaskTracking' in a for a in argv):
                        raw = '\n'.join(json.dumps(e) for n in m.PREFIX for e in ({'Action': 'run', 'Test': n}, {'Action': 'skip' if n in skips['core_prefix'] else 'pass', 'Test': n})) + '\n' + json.dumps({'Action': 'pass'})
                    else:
                        raw = events(m.CORE if name.endswith('-core') else {m.AUTH})
                        if name.endswith('-server') and not any(a.startswith('-test.run=') for a in argv):
                            raw += '\n' + '\n'.join(json.dumps(e) for n in skips['server'] for e in ({'Action': 'run', 'Test': n}, {'Action': 'skip', 'Test': n}))
                    if name.startswith('01-'): code = 1
                (folder / 'stdout.txt').write_text(raw)
                (folder / 'stderr.txt').write_text('')
                (folder / 'audit.log').write_text(audit)
                return {'native_exit_code': code, 'timed_out': False, 'launch_error': None, 'skipped_reason': None}, folder
            with patch.object(c, 'checked', side_effect=checked), patch.object(c, 'run', side_effect=run), patch.object(m.shutil, 'which', return_value=__file__):
                c.execute()
            self.assertEqual(len(c.records), 18)
            self.assertTrue(c.records[0]['validation_errors'])
            self.assertTrue(all(not r['validation_errors'] for r in c.records[1:]))
            self.assertTrue(all(r['status'] == 'finished' for r in c.records))


if __name__ == '__main__':
    unittest.main()
