"""Prepare offline diagnostic overlay, or validate and extract test evidence."""
import json
from pathlib import Path
import subprocess
import sys


def prepare(destination):
    destination = Path(destination).resolve()
    destination.mkdir(parents=True, exist_ok=True)
    source = Path(subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip()) / 'src/net/dial.go'
    code = source.read_text(encoding='utf-8')
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
    replacement.write_text(code.replace(marker, marker + guard), encoding='utf-8')
    (destination / 'offline-overlay.json').write_text(json.dumps({'Replace': {str(source): str(replacement)}}), encoding='utf-8')


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


def collect(log, destination, version, revision, expected_os):
    events = [json.loads(line) for line in Path(log).read_text(encoding='utf-8-sig').splitlines() if line.strip()]
    if any(e.get('Action') in ('skip', 'fail') for e in events):
        raise RuntimeError('Diagnostic skips/failures are not successful evidence')
    records = []
    # test2json splits long log lines into multiple Output events. Reassemble
    # each test stream before parsing; do not mistake partial JSON for evidence.
    streams = {}
    for e in events:
        key = (e.get('Package', ''), e.get('Test', ''))
        streams[key] = streams.get(key, '') + e.get('Output', '')
    for stream in streams.values():
        for line in stream.splitlines():
            if 'ISSUE277_RESULT ' in line:
                records.append(json.loads(line.split('ISSUE277_RESULT ', 1)[1]))
    validate_records(records, version, revision, expected_os)
    package = 'github.com/javinizer/javinizer-go/internal/workflow'
    required_passes = {'', 'TestIssue277NativeDiagnostic', 'TestIssue277NativeDiagnosticIdentitySnapshot'}
    required_passes.update(f"TestIssue277NativeDiagnostic/{r['mode']}/move={str(r['move']).lower()}/{r['scenario']}" for r in records)
    actual_passes = {e.get('Test', '') for e in events if e.get('Action') == 'pass' and e.get('Package') == package}
    if not required_passes.issubset(actual_passes):
        raise RuntimeError('Expected package, root test, identity control, or exact leaf test pass missing')
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    (destination / 'results.json').write_text(json.dumps({'version': version, 'revision': revision, 'native_os': expected_os, 'cases': records}, indent=2), encoding='utf-8')
    print(f'{version} {revision}: {len(records)} native {expected_os} cases; historical outcomes recorded, not asserted as desired behavior')
    for r in records:
        if r['error'] or not r['protected_bytes_preserved']:
            print(r['mode'], f"move={r['move']}", r['scenario'], f"preserved={r['protected_bytes_preserved']}", r['error'])


if __name__ == '__main__':
    if sys.argv[1] == 'prepare':
        prepare(sys.argv[2])
    elif sys.argv[1] == 'collect':
        collect(*sys.argv[2:])
    else:
        raise SystemExit('Unknown command')
