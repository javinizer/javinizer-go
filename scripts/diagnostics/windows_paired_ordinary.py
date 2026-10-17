"""Bounded existing-product comparison. Native Windows only; no remote dispatch."""
import collections
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import time

REVISIONS = {
    'original': ('2f97d9e75dd8402d17544c5501222400bdf233df', 'e5e495d179e9bcf804ad23b285c572d17b346a09'),
    'cleanup': ('2895e16e1ab53a40cbc6c95ddd2242ff0be39554', '1b218219361eb636a2cc716a91b69edcfd08ef64'),
    'fixed': ('16043816a27007f304916739f1734dd0ba4d0ba0', 'dcf484715d742b387aa66add8929853057858d68'),
}
GUARDS = {'TestExternalBeforeDNS', 'TestDirectDialAndUDPWrites', 'TestLoopbackAllowed',
          'TestHTTPExternalTargetBlockedBeforeProxy', 'TestHTTPExternalTargetBlockedBeforeSOCKS',
          'TestInfrastructureProxyEndpointBlocked', 'TestHTTPLoopbackAllowed'}
CORE = {'TestAPIDeps_NilCoreDeps_Panics', 'TestAPIDeps_SetConfig_NilCoreDeps',
        'TestAPIRuntime_ReloadConfig_NilCoreDeps', 'TestAPIRuntime_ReloadConfig_NilCoreDepsGuard'}
AUTH = 'TestInjectSSRState_InitializedAuth'
PREFIX = json.loads(Path(__file__).with_name('windows_core_prefix.json').read_text())
SCHEDULE = [(r, 'server', None) for r in ('original', 'cleanup', 'fixed', 'cleanup', 'fixed', 'original', 'fixed', 'original', 'cleanup')]
SCHEDULE += [(r, 'core', PREFIX) for r in REVISIONS]
SCHEDULE += [(r, 'core', CORE) for r in REVISIONS]
SCHEDULE += [(r, 'server', {AUTH}) for r in REVISIONS]


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def validate_metadata(kind, raw):
    """Validate only allowlisted fields; diagnostics never echo unknown values."""
    try:
        value = json.loads(raw)
    except (ValueError, TypeError):
        return [kind + ': malformed JSON']
    fields = {
        'os': {'Caption': str, 'Version': str, 'BuildNumber': str, 'OSArchitecture': str},
        'cpu': {'Name': str, 'Manufacturer': str, 'NumberOfCores': int, 'NumberOfLogicalProcessors': int},
        'ram': {'TotalPhysicalMemory': int},
        'image': {'ImageOS': str, 'ImageVersion': str},
    }[kind]
    values = value if kind == 'cpu' and isinstance(value, list) else [value]
    if not values:
        return [kind + ': empty result']
    errors = []
    for index, entry in enumerate(values):
        label = kind + (f'[{index}]' if kind == 'cpu' else '')
        if not isinstance(entry, dict):
            errors.append(label + ': missing object')
            continue
        for key, expected in fields.items():
            item = entry.get(key)
            valid = (isinstance(item, str) and bool(item.strip()) and item.strip().lower() not in {'unavailable', 'unknown', 'null'}) if expected is str else (type(item) is int and item > 0)
            if not valid:
                errors.append(label + '.' + key + ': missing or invalid')
    return errors


def validate(raw, audit, code, required=None, exact=False, guard=False, inventory=None, allowed_skips=(), allowed_subtest_skips=(), expected_skips=None):
    errors, events = [], []
    for line in raw.splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
            if not isinstance(event, dict) or event.get('Action') not in {'start', 'run', 'pause', 'cont', 'pass', 'fail', 'skip', 'output', 'bench'}:
                raise ValueError('invalid event')
            events.append(event)
        except (ValueError, TypeError):
            errors.append('malformed JSON event')
    if code != 0:
        errors.append('nonzero or absent exit')
    terminal = [e['Action'] for e in events if not e.get('Test') and e['Action'] in ('pass', 'fail', 'skip')]
    if terminal != ['pass']:
        errors.append('missing unique terminal package pass')
    if any(e['Action'] == 'fail' for e in events):
        errors.append('failed test event')
    counts = collections.Counter(audit.splitlines())
    expected = {'OFFLINE279_BLOCKED': 26, 'OFFLINE279_BLOCKED_HTTP_TARGET': 4} if guard else {}
    if counts != expected:
        errors.append('unexpected audit markers')
    runs = collections.Counter(e['Test'] for e in events if e.get('Test') and '/' not in e['Test'] and e['Action'] == 'run')
    passes = collections.Counter(e['Test'] for e in events if e.get('Test') and '/' not in e['Test'] and e['Action'] == 'pass')
    for name in required or ():
        if runs[name] != 1 or passes[name] != 1:
            errors.append('missing or duplicate required test: ' + name)
    if exact and (set(runs) != set(required) or set(passes) != set(required)):
        errors.append('unexpected top-level tests')
    if inventory is not None:
        tops = [e for e in events if e.get('Test') and '/' not in e['Test']]
        terminals = collections.Counter(e['Test'] for e in tops if e['Action'] in {'pass', 'fail', 'skip'})
        skips = {e['Test'] for e in tops if e['Action'] == 'skip'}
        if set(runs) != set(inventory) or any(runs[n] != 1 for n in inventory):
            errors.append('full inventory run mismatch')
        if set(terminals) != set(inventory) or any(terminals[n] != 1 for n in inventory):
            errors.append('full inventory terminal mismatch')
        subtest_skips = {e['Test'] for e in events if e.get('Test') and '/' in e['Test'] and e['Action'] == 'skip'}
        if not subtest_skips <= set(allowed_subtest_skips):
            errors.append('unaudited subtest skip')
        if expected_skips is not None and skips != set(expected_skips):
            errors.append('expected platform/short skips mismatch')
        if not skips <= set(allowed_skips):
            errors.append('unaudited root skip')
    if (exact or guard) and any(e['Action'] == 'skip' for e in events):
        errors.append('unexpected skip')
    return {'validation_errors': errors, 'audit_counts': dict(counts), 'package_terminal': terminal,
            'observed_tests': [e for e in events if e.get('Test') and e['Action'] != 'output']}


class Controller:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.evidence = self.root / 'paired-evidence'
        self.evidence.mkdir(exist_ok=False)
        self.deadline = time.monotonic() + 600
        self.halted = False
        self.metadata_incomplete = []
        self.env = os.environ.copy()
        # Record presence only, never arbitrary inherited values or secrets.
        write(self.evidence / 'inherited-settings.json', {k: bool(self.env.get(k)) for k in
              ('GOGC', 'GOMEMLIMIT', 'GOMAXPROCS', 'GOEXPERIMENT', 'GODEBUG', 'GOFLAGS', 'GOENV')})
        for k in ('GOGC', 'GOMEMLIMIT', 'GOMAXPROCS', 'GOEXPERIMENT', 'GODEBUG', 'GOFLAGS',
                  'JAVINIZER_RUN_LIVE_API_TESTS', 'JAVINIZER_RUN_FLARESOLVERR_TESTS'):
            self.env.pop(k, None)
        self.env.update({'CGO_ENABLED': '1', 'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOOS': 'windows', 'GOARCH': 'amd64', 'GOAMD64': 'v1', 'CC': 'gcc', 'CXX': 'g++'})
        self.records = []
        for i, (rev, package, subset) in enumerate(SCHEDULE, 1):
            self.records.append({'attempt_id': f'{i:02d}-{rev}-{package}', 'revision_sha': REVISIONS[rev][0],
                                 'revision_tree': REVISIONS[rev][1], 'scope': 'core-prefix-59' if subset == PREFIX else ('isolated' if subset else 'full-server'),
                                 'required_tests': sorted((CORE & set(PREFIX)) if subset == PREFIX else (subset or {AUTH})),
                                 'status': 'not-run', 'skipped_reason': 'controller did not reach attempt',
                                 'native_exit_code': None, 'started_at': None, 'finished_at': None,
                                 'timed_out': False, 'launch_error': None, 'elapsed_seconds': None})
        write(self.evidence / 'manifest.json', {'revisions': REVISIONS, 'attempts': self.records,
                                               'controller_sha256': digest(__file__),
                                               'skip_manifest_sha256': digest(Path(__file__).with_name('windows_paired_skips.json')),
                                               'prefix_manifest_sha256': digest(Path(__file__).with_name('windows_core_prefix.json'))})

    def run(self, name, argv, cwd, limit=30, env=None, stdin_path=None):
        folder = self.evidence / name
        folder.mkdir(exist_ok=False)
        audit = folder / 'audit.log'
        audit.touch()
        result = {'argv': list(map(str, argv)), 'cwd': str(cwd), 'native_exit_code': None,
                  'timed_out': False, 'launch_error': None, 'skipped_reason': None,
                  'started_at': time.time(), 'stdout_path': str(folder / 'stdout.txt'),
                  'stderr_path': str(folder / 'stderr.txt'), 'audit_path': str(audit)}
        child_env = (env or self.env).copy()
        child_env['OFFLINE279_AUDIT_LOG'] = str(audit)
        started = time.monotonic()
        remaining = self.deadline - started
        import contextlib
        with (folder / 'stdout.txt').open('wb') as out, (folder / 'stderr.txt').open('wb') as err, \
                (Path(stdin_path).open('rb') if stdin_path else contextlib.nullcontext(None)) as source:
            if self.halted or remaining < 1:
                self.halted = True
                result['skipped_reason'] = 'host halted or budget exhausted'
            else:
                try:
                    proc = subprocess.Popen(argv, cwd=cwd, env=child_env, stdout=out, stderr=err, stdin=source)
                    try:
                        result['native_exit_code'] = proc.wait(timeout=min(limit, remaining))
                    except subprocess.TimeoutExpired:
                        result['timed_out'] = True
                        self.halted = True  # Never run another process/test on this host.
                        try:
                            cleanup = subprocess.run(['taskkill', '/PID', str(proc.pid), '/T', '/F'],
                                                     stdout=out, stderr=err, timeout=15, check=False)
                            result['cleanup_exit'] = cleanup.returncode
                            result['native_exit_code'] = proc.wait(timeout=10)
                        except (OSError, subprocess.TimeoutExpired) as exc:
                            result['cleanup_error'] = type(exc).__name__
                except OSError as exc:
                    result['launch_error'] = type(exc).__name__
        result.update(finished_at=time.time(), elapsed_seconds=time.monotonic() - started)
        write(folder / 'result.json', result)
        return result, folder

    def checked(self, name, argv, cwd, limit=30, env=None):
        result, folder = self.run(name, argv, cwd, limit, env)
        if result['native_exit_code'] != 0 or result['timed_out'] or result['skipped_reason'] or result['launch_error'] or (folder / 'audit.log').stat().st_size:
            raise RuntimeError('setup/identity gate failed: ' + name)
        return (folder / 'stdout.txt').read_text(encoding='utf-8', errors='replace').strip()

    def execute(self):
        image = {k: os.environ.get(k, 'unavailable') for k in ('ImageOS', 'ImageVersion')}
        write(self.evidence / 'image.json', image)
        self.metadata_incomplete.extend(validate_metadata('image', json.dumps(image)))
        for name, query in {
            'os': 'Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,OSArchitecture',
            'cpu': 'Get-CimInstance Win32_Processor | Select-Object Name,Manufacturer,NumberOfCores,NumberOfLogicalProcessors',
            'ram': 'Get-CimInstance Win32_ComputerSystem | Select-Object TotalPhysicalMemory',
        }.items():
            result, folder = self.run('metadata-' + name, ['pwsh', '-NoProfile', '-Command', query + ' | ConvertTo-Json'], self.root)
            if result['native_exit_code'] != 0 or result['timed_out'] or result['launch_error']:
                self.metadata_incomplete.append(name + ': command failed')
            self.metadata_incomplete.extend(validate_metadata(name, (folder / 'stdout.txt').read_text(encoding='utf-8', errors='replace')))
        write(self.evidence / 'metadata-status.json', {'unavailable': self.metadata_incomplete})
        version = self.checked('go-version', ['go', 'version'], self.root)
        if version != 'go version go1.27.2 windows/amd64':
            raise RuntimeError('unexpected native toolchain')
        info = json.loads(self.checked('go-env', ['go', 'env', '-json', 'GOVERSION', 'GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED'], self.root))
        if info.get('CGO_ENABLED') != '1' or info.get('GOAMD64') != 'v1':
            raise RuntimeError('unexpected build configuration')
        self.checked('gcc-version', ['gcc', '--version'], self.root)
        self.checked('gcc-target', ['gcc', '-dumpmachine'], self.root)
        write(self.evidence / 'tool-binary-hashes.json', {name: digest(shutil.which(name)) for name in ('go', 'gcc')})
        for rev, (sha, tree) in REVISIONS.items():
            cwd = self.root / rev
            for suffix, args, expected in (
                ('head', ['rev-parse', 'HEAD'], sha), ('tree', ['rev-parse', 'HEAD^{tree}'], tree),
                ('status', ['status', '--porcelain'], ''),
                ('overlay-source', ['rev-parse', 'HEAD:scripts/build_offline_test_overlay.py'], '10f013f25cb4aaa083de051e5315dcd927fded46'),
                ('guard-source', ['rev-parse', 'HEAD:test/offlineguard'], 'b967d5d8603a8cae61282e17902d3dec76b26adc')):
                if self.checked(rev + '-' + suffix, ['git'] + args, cwd) != expected:
                    raise RuntimeError('source identity mismatch: ' + rev + '-' + suffix)
            self.checked(rev + '-download', ['go', 'mod', 'download'], cwd, 300)
        self.deadline = time.monotonic() + 28 * 60
        self.env.update({'GOPROXY': 'off', 'GONOPROXY': 'none', 'GOSUMDB': 'off', 'GOVCS': '*:off', 'GODEBUG': 'netdns=go'})
        environments, binaries, inventories = {}, {}, {}
        skip_manifest = json.loads(Path(__file__).with_name('windows_paired_skips.json').read_text())
        for rev in REVISIONS:
            cwd = self.root / rev
            overlay = self.evidence / (rev + '-overlay')
            self.checked(rev + '-make-overlay', ['python', 'scripts/build_offline_test_overlay.py', '--output', str(overlay)], cwd)
            env = self.env.copy()
            env['GOFLAGS'] = "'-overlay=" + (overlay / 'overlay.json').as_posix() + "'"
            environments[rev] = env
            write(self.evidence / (rev + '-overlay-hashes.json'), {str(p.relative_to(overlay)): digest(p) for p in sorted(overlay.rglob('*')) if p.is_file()})
            self.checked(rev + '-package-dependencies', ['go', 'list', '-mod=readonly', '-deps', '-test', '-json', './internal/api/server', './internal/api/core'], cwd, env=env)
            result, folder = self.run(rev + '-guard', ['go', 'test', '-short', '-count=1', '-timeout=120s', '-json', './...'], cwd / 'test/offlineguard', 150, env)
            validation = validate((folder / 'stdout.txt').read_text(encoding='utf-8', errors='replace'), (folder / 'audit.log').read_text(), result['native_exit_code'], GUARDS, True, True)
            write(folder / 'validation.json', validation)
            if validation['validation_errors'] or result['timed_out']:
                raise RuntimeError('guard gate failed: ' + rev)
        # Both safety gates must pass before any product execution.
        for rev in REVISIONS:
            for package in ('server', 'core'):
                binary = self.evidence / f'{rev}-{package}.test.exe'
                self.checked(f'{rev}-{package}-compile', ['go', 'test', '-c', '-o', str(binary), './internal/api/' + package], self.root / rev, 240, environments[rev])
                self.checked(f'{rev}-{package}-buildinfo', ['go', 'version', '-m', str(binary)], self.root / rev, env=environments[rev])
                binaries[rev, package] = (binary, digest(binary))
                listing = self.checked(f'{rev}-{package}-inventory', [str(binary), '-test.list=.'], self.root / rev / 'internal/api' / package, env=environments[rev])
                names = [line for line in listing.splitlines() if line.startswith('Test')]
                if not names or len(names) != len(set(names)):
                    raise RuntimeError('invalid product inventory')
                if package == 'core' and names[:len(PREFIX)] != PREFIX:
                    raise RuntimeError('core prefix inventory/order differs from observed history: ' + rev)
                inventories[rev, package] = names
                write(self.evidence / f'{rev}-{package}-inventory.json', names)
        for rev in REVISIONS:
            if self.checked(rev + '-postbuild-source-status', ['git', 'status', '--porcelain', '--untracked-files=no'], self.root / rev, env=environments[rev]):
                raise RuntimeError('tracked source changed during dependency setup/build: ' + rev)
        write(self.evidence / 'binary-hashes.json', {f'{r}-{p}': h for (r, p), (_, h) in binaries.items()})
        for record, (rev, package, subset) in zip(self.records, SCHEDULE):
            binary, expected_hash = binaries[rev, package]
            if digest(binary) != expected_hash:
                raise RuntimeError('binary changed')
            is_prefix = subset == PREFIX
            isolated = bool(subset) and not is_prefix
            argv = [str(binary), '-test.short', '-test.count=1', '-test.timeout=' + ('60s' if isolated else '180s'), '-test.v=test2json']
            if subset:
                argv.append('-test.run=^(' + '|'.join(sorted(subset)) + ')$')
            result, folder = self.run(record['attempt_id'], argv, self.root / rev / 'internal/api' / package, 90 if isolated else 210, environments[rev])
            raw_product = (folder / 'stdout.txt').read_text(encoding='utf-8', errors='replace') + (folder / 'stderr.txt').read_text(encoding='utf-8', errors='replace')
            result['go_test_timeout'] = 'panic: test timed out after ' in raw_product
            if result['go_test_timeout']:
                self.halted = True
            record['logged_recovery_occurrences'] = {
                'nil_pointer_text': raw_product.count('runtime error: invalid memory address or nil pointer dereference'),
                'interface_conversion_text': raw_product.count('interface conversion:'),
            }  # Log text counts only; not proof of exception counts or assert.Panics behavior.
            record.update(result, binary_sha256=expected_hash, status='finished' if not result['skipped_reason'] else 'not-run')
            conversion, converted = self.run(record['attempt_id'] + '-json', ['go', 'tool', 'test2json', '-t', '-p', 'github.com/javinizer/javinizer-go/internal/api/' + package], self.root / rev, 30, environments[rev], folder / 'stdout.txt')
            record['test2json_exit_code'] = conversion['native_exit_code']
            record['test2json_result_path'] = str(converted / 'result.json')
            record.update(validate((converted / 'stdout.txt').read_text(encoding='utf-8', errors='replace'), (folder / 'audit.log').read_text(), result['native_exit_code'], (CORE & set(PREFIX)) if is_prefix else (subset or {AUTH}), isolated, inventory=PREFIX if is_prefix else (None if isolated else inventories[rev, package]), allowed_skips=skip_manifest['core_prefix'] if is_prefix else skip_manifest[package], expected_skips=skip_manifest['core_prefix'] if is_prefix else (skip_manifest['server'] if not isolated else None)))
            if is_prefix:
                observed_order = [e['Test'] for e in record['observed_tests'] if e['Action'] == 'run' and '/' not in e['Test']]
                if observed_order != PREFIX:
                    record['validation_errors'].append('core prefix execution order mismatch')
            if conversion['native_exit_code'] != 0 or conversion['timed_out'] or conversion['launch_error'] or (converted / 'audit.log').stat().st_size:
                record['validation_errors'].append('JSON conversion failed or incomplete')
            if result['timed_out'] or result['go_test_timeout']:
                record['validation_errors'].append('outer timeout; remaining host attempts prohibited')
            write(folder / 'result.json', record)
            write(self.evidence / 'outcomes.json', self.records)
        for binary, expected in binaries.values():
            if digest(binary) != expected:
                raise RuntimeError('binary changed after execution')


def emit_logs(root):
    # Windows redirected streams may default to cp1252; CI text is explicitly UTF-8.
    sys.stdout.reconfigure(encoding='utf-8', errors='backslashreplace')
    sys.stderr.reconfigure(encoding='utf-8', errors='backslashreplace')
    # Complete raw evidence is uploaded separately. Logs retain command outputs/results.
    for path in sorted((Path(root) / 'paired-evidence').rglob('*')):
        if path.is_file() and path.suffix in {'.txt', '.json', '.log'} and not any(part.endswith('-overlay') for part in path.parts):
            print('\nEVIDENCE_FILE ' + str(path.relative_to(root)), flush=True)
            for line in path.read_text(encoding='utf-8', errors='replace').splitlines():
                print('EVIDENCE ' + line, flush=True)  # Prevent workflow-command interpretation.


def main():
    if len(sys.argv) == 2 and sys.argv[1] == '--emit-logs':
        emit_logs(Path.cwd())
        return 0
    if len(sys.argv) != 1 or sys.platform != 'win32':
        raise SystemExit('Native Windows only; no arbitrary inputs accepted')
    controller = Controller(Path.cwd())
    failure = None
    try:
        controller.execute()
    except Exception as exc:
        failure = str(exc)
    finally:
        for record in controller.records:
            if record['status'] == 'not-run' and failure:
                record['skipped_reason'] = failure
        write(controller.evidence / 'outcomes.json', controller.records)
        write(controller.evidence / 'summary.json', {'error': failure, 'metadata_incomplete': controller.metadata_incomplete, 'success': not failure and not controller.metadata_incomplete and all(r['status'] == 'finished' and not r.get('validation_errors') for r in controller.records)})
    return int(bool(failure) or bool(controller.metadata_incomplete) or any(r['status'] != 'finished' or r.get('validation_errors') for r in controller.records))


if __name__ == '__main__':
    sys.exit(main())
