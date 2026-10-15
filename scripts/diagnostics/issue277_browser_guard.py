"""Compile-time loopback-only guard for the isolated browser fixture.
Uses the same net.Dialer.DialContext pre-DNS overlay as the native matrix.
No production repository files or installed Go files are modified.
"""
import json
from pathlib import Path
import subprocess
import sys

def prepare(destination):
    destination = Path(destination).resolve()
    destination.mkdir(parents=True, exist_ok=True)
    source = Path(subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip()) / 'src/net/dial.go'
    code = source.read_text()
    marker = 'func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {'
    if code.count(marker) != 1:
        raise RuntimeError('Go network guard insertion point changed')
    guard = '''
 if network == "tcp" || network == "tcp4" || network == "tcp6" || network == "udp" || network == "udp4" || network == "udp6" {
  host, _, _ := SplitHostPort(address)
  ip := ParseIP(host)
  if host != "localhost" && (ip == nil || !ip.IsLoopback()) { panic("ISSUE277 OFFLINE NETWORK DENIED: " + network + " " + address) }
 }
'''
    replacement = destination / 'dial.go'
    replacement.write_text(code.replace(marker, marker + guard))
    (destination / 'offline-overlay.json').write_text(json.dumps({'Replace': {str(source): str(replacement)}}))


REVISIONS = {
    'v1.5.2': '6e016085be7efded5f3d30f2932c2ad9a7cb2a36',
    'v1.6.0': '2e019b5756d1919795fa45cfb5e76c4114d34888',
    'v1.6.1': 'ad50dffc690e7ecfbd789341c580f841df1c7866',
    'main-17092b8': '17092b84850713a3bee8c32390aa56b726942b5c',
}
MODES = ('organize', 'in-place', 'in-place-norenamefolder')
SCENARIOS = ('fresh', 'foreign-video', 'equal-video', 'hardlink-video', 'resident',
             'existing-nfo', 'existing-artwork', 'existing-subtitle',
             'existing-subtitle-en', 'busy-video', 'resident-nfo',
             'resident-artwork', 'resident-subtitle', 'resident-busy')


def validate_records(records, version, revision, expected_os):
    if version not in REVISIONS or REVISIONS[version] != revision:
        raise RuntimeError('Unrecognized or mismatched pinned subject version/revision')
    if expected_os not in ('linux', 'windows'):
        raise RuntimeError('Unsupported native OS')
    scenarios = SCENARIOS + (('case-alias',) if expected_os == 'windows' else ())
    expected_keys = {(mode, move, scenario) for mode in MODES for move in (False, True) for scenario in scenarios}
    keys = [(r.get('mode'), r.get('move'), r.get('scenario')) for r in records]
    if len(keys) != len(expected_keys) or set(keys) != expected_keys:
        raise RuntimeError('Exact diagnostic case keyset missing, duplicated, or substituted')
    current = version == 'main-17092b8'
    baseline = version == 'v1.5.2'
    for r in records:
        if r.get('subject_version') != version or r.get('subject_revision') != revision:
            raise RuntimeError('Record subject does not match pinned version/revision')
        if r.get('goos') != expected_os or r.get('goarch') != 'amd64':
            raise RuntimeError('Wrong native OS/architecture; cross-compilation does not count')
        for flag in ('move', 'fenced', 'force_update', 'overwrite_existing_media',
                     'protected_bytes_preserved', 'protected_identities_preserved',
                     'source_target_same_object_before', 'original_source_present',
                     'original_source_identity_preserved', 'original_source_bytes_preserved'):
            if type(r.get(flag)) is not bool:
                raise RuntimeError('Missing or non-boolean diagnostic flag: ' + flag)
        if r['force_update'] or r['overwrite_existing_media'] or r['fenced'] == baseline:
            raise RuntimeError('Unexpected permission or publication-fence configuration')
        if not isinstance(r.get('error'), str) or not isinstance(r.get('files_sha256'), dict) or not r['files_sha256']:
            raise RuntimeError('Missing outcome or file inventory')
        for digest in r['files_sha256'].values():
            if not isinstance(digest, str) or len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
                raise RuntimeError('Invalid file hash')
        scenario = r['scenario']
        alias = scenario == 'hardlink-video' or scenario == 'case-alias' or scenario.startswith('resident')
        if r['source_target_same_object_before'] != alias:
            raise RuntimeError('Fixture source/target object identity does not match scenario')
        # Current fixed semantics must never receive a green diagnostic when
        # protected data changed. Historical unsafe observations remain visible.
        if current and (not r['protected_bytes_preserved'] or not r['protected_identities_preserved']):
            raise RuntimeError('Current subject mutated protected destination data or identity')
        if current and r['error'] and not all(r[x] for x in ('original_source_present', 'original_source_bytes_preserved', 'original_source_identity_preserved')):
            raise RuntimeError('Current failed operation did not preserve its original source')
        if r['mode'] != 'organize':
            continue
        if scenario in ('foreign-video', 'equal-video'):
            phrase = 'organization validation failed' if baseline else 'staged publication destination exists and overwrite is disabled'
            if phrase not in r['error'] or r.get('failed_step') != ('organize' if baseline else 'artifact_publication'):
                raise RuntimeError('Distinct Organize duplicate did not produce its expected refusal')
            if not all(r[x] for x in ('protected_bytes_preserved', 'protected_identities_preserved', 'original_source_present', 'original_source_bytes_preserved', 'original_source_identity_preserved')):
                raise RuntimeError('Distinct Organize duplicate damaged source or destination')
        elif scenario in ('hardlink-video', 'resident', 'case-alias'):
            if baseline or current:
                if r['error'] or not r['original_source_identity_preserved'] or not r['original_source_bytes_preserved']:
                    raise RuntimeError('Baseline/current Organize alias no-op regressed')
            elif 'staged publication destination exists and overwrite is disabled' not in r['error']:
                raise RuntimeError('Historical Organize alias diagnostic changed; investigate result')


def collect(report_path, revision):
    report_path = Path(report_path).resolve()
    report = json.loads(report_path.read_text())
    if report.get('errors'):
        raise RuntimeError('Playwright reported top-level errors')
    tests = []
    def walk(suites):
        for suite in suites:
            for spec in suite.get('specs', []):
                for test in spec.get('tests', []):
                    tests.append((spec['title'], test))
            walk(suite.get('suites', []))
    walk(report.get('suites', []))
    cases = {f'{operation}: {fixture} target' for operation in ('move', 'copy')
             for fixture in ('absent', 'different-bytes', 'same-bytes', 'hardlink')}
    expected = cases | {'authenticate disposable e2e backend'}
    if len(tests) != 9 or {name for name, _ in tests} != expected:
        raise RuntimeError(f'Expected exactly 8 named browser cases plus auth setup, got {[n for n, _ in tests]}')
    records = []
    for name, test in tests:
        results = test.get('results', [])
        if test.get('status') != 'expected' or len(results) != 1 or results[0].get('status') != 'passed':
            raise RuntimeError(f'{name}: runtime pass required; skipped, retried or failed results are not evidence')
        project = 'issue277-chromium' if name in cases else 'issue277-setup'
        if test.get('projectName') != project:
            raise RuntimeError(f'{name}: wrong runtime project')
        if name not in cases:
            continue
        artifacts = [a for a in results[0].get('attachments', []) if a['name'] == 'issue277-evidence']
        if len(artifacts) != 1:
            raise RuntimeError(f'{name}: missing selected evidence JSON')
        artifact = Path(artifacts[0]['path'])
        if not artifact.is_absolute():
            artifact = Path.cwd() / artifact
        evidence = json.loads(artifact.read_text())
        if evidence.get('revision') != revision or 'terminalJob' not in evidence:
            raise RuntimeError(f'{name}: wrong revision or missing terminal backend result')
        records.append(evidence)
    output = report_path.parent / 'validated-summary.json'
    output.write_text(json.dumps({'revision': revision, 'browser_cases_passed': 8,
        'auth_setup_passed': 1, 'cases': records}, indent=2))
    print('Verified 8 named real-browser runtime cases plus auth setup; no skips or retries.')


if __name__ == '__main__':
    if sys.argv[1] == 'prepare':
        version = subprocess.check_output(['go', 'env', 'GOVERSION'], text=True).strip()
        if version != 'go1.27.2':
            raise RuntimeError(f'Expected reviewed Go 1.27.2, got {version}')
        prepare(sys.argv[2])
    elif sys.argv[1] == 'collect':
        collect(sys.argv[2], sys.argv[3])
    else:
        raise SystemExit('Unknown command')
