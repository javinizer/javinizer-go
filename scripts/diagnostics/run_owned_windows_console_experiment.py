#!/usr/bin/env python3
"""Future native experiment driver; not wired to CI. No production test hooks."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import ctypes
import owned_windows_console as owned

PACKAGE = 'github.com/javinizer/javinizer-go/cmd/javinizer'
CONSOLE_SOURCE = PACKAGE + '/console_windows.go'
CONSOLE_SHA = '841b0a72ad6f012b0872682a9e84b161201a6b938c4eb6c6651e306fd954a689'
SHA = re.compile(r'[0-9a-f]{40}')
DEADLINE = None
FLOOR_BYTES = 96 * 1024 * 1024


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n', encoding='utf-8')


def clean_git_env():
    env = {key: value for key, value in os.environ.items() if not key.upper().startswith('GIT_')}
    env.update(GIT_NO_REPLACE_OBJECTS='1', GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull)
    return env


def clean_build_env():
    env = clean_git_env()
    allowed_go = {'GOCACHE', 'GOMODCACHE', 'GOPATH', 'GOPROXY', 'GOSUMDB', 'GONOSUMDB', 'GOPRIVATE', 'GONOPROXY'}
    for key in list(env):
        upper = key.upper()
        if (upper.startswith('GO') and upper not in allowed_go) or upper.startswith('CGO_') or upper in {'CC', 'CXX', 'FC', 'AR', 'PKG_CONFIG'}:
            del env[key]
    env.update(GOENV='off', GOWORK='off', GOTOOLCHAIN='local', CGO_ENABLED='1', GOOS='windows', GOARCH='amd64')
    return env


def monitor_capacity(roots, stop, terminate, publish, state, sample=shutil.disk_usage, failure_signal=None):
    """Continuous owned-tool guard; never signals a user/runner console."""
    state.update(samples=0, minimum_free_bytes=[None] * len(roots), floor_bytes=FLOOR_BYTES)
    try:
        while not stop.is_set():
            free = [sample(root).free for root in roots]
            state['samples'] += 1
            state['minimum_free_bytes'] = [value if old is None else min(old, value)
                                           for old, value in zip(state['minimum_free_bytes'], free)]
            publish(state)
            if min(free) < FLOOR_BYTES:
                raise RuntimeError('continuous dual-root 96 MiB capacity floor reached')
            if stop.wait(0.5):
                break
    except Exception as error:
        state['failure'] = repr(error)
        if failure_signal is not None:
            failure_signal.set()
        try:
            terminate()
            state['termination_requested'] = True
        except Exception as cleanup_error:
            state['termination_failure'] = repr(cleanup_error)
        try:
            publish(state)
        except Exception as receipt_error:
            state['receipt_failure'] = repr(receipt_error)


def run_windows_job(command, cwd, env, timeout, receipt, save, capacity_save=lambda state: None):
    """Gate an owned tool host into a job before any compiler/tool descendants."""
    k = owned.kernel()
    owned.assert_native_abi()
    job = process = child = None
    watcher = None
    watcher_started = False
    watch_failure = threading.Event()
    watch_stop = threading.Event()
    watch_state = {}
    try:
        job = owned.checked(k.CreateJobObjectW(None, None))
        limits = owned.ExtendedLimits()
        limits.basic.flags = 0x2000
        owned.checked(k.SetInformationJobObject(job, 9, ctypes.byref(limits), ctypes.sizeof(limits)))
        child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), '--tool-host'],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 close_fds=True)
        receipt['pid'] = child.pid
        owned.save_receipt(save, receipt)
        process = owned.checked(k.OpenProcess(0x0100 | 0x0001, False, child.pid))
        owned.checked(k.AssignProcessToJobObject(job, process))
        receipt['job_assigned_before_handshake'] = True
        watcher = threading.Thread(target=monitor_capacity, daemon=True, args=(
            (Path(cwd), Path(tempfile.gettempdir())), watch_stop,
            lambda: owned.checked(k.TerminateJobObject(job, 96)), capacity_save, watch_state),
            kwargs={'failure_signal': watch_failure})
        watcher.start()
        watcher_started = True
        payload = json.dumps({'command': list(map(str, command)), 'cwd': str(cwd), 'env': env}).encode('utf-8')
        out, err = owned.capture(child, receipt, save, payload, timeout, abort=watch_failure, cleanup_on_error=False)
        return child.returncode, out, err
    except BaseException as error:
        receipt['primary_failure'] = repr(error)
        owned.save_receipt(save, receipt)
        raise
    finally:
        original = sys.exc_info()[0] is not None
        try:
            watch_stop.set()
        except Exception as error:
            receipt.setdefault('cleanup_failures', []).append('watcher stop: ' + repr(error))
        watcher_joined = True
        if watcher is not None:
            if watcher_started:
                try:
                    watcher.join(timeout=3)
                except Exception as error:
                    receipt.setdefault('cleanup_failures', []).append('watcher join: ' + repr(error))
            try:
                watcher_joined = not watcher.is_alive()
            except Exception as error:
                watcher_joined = False
                receipt.setdefault('cleanup_failures', []).append('watcher state: ' + repr(error))
            if watcher_joined:
                receipt['capacity_watch'] = watch_state
            else:
                receipt.setdefault('cleanup_failures', []).append('capacity watcher did not join; job handle retained until process exit')
        if job is not None:
            owned.best_effort(lambda: owned.checked(k.TerminateJobObject(job, 124)), 'terminate tool job', receipt, save)
            if watcher_joined:
                owned.best_effort(lambda: owned.checked(k.CloseHandle(job)), 'close tool job', receipt, save)
        if child is not None:
            owned.cleanup_process(child, receipt, save)
        if process is not None:
            owned.best_effort(lambda: owned.checked(k.CloseHandle(process)), 'close tool process', receipt, save)
        if (receipt.get('cleanup_failures') or receipt.get('receipt_failures') or receipt.get('capacity_watch', {}).get('failure')) and not original:
            raise RuntimeError('owned tool cleanup/evidence failed')


def tool_host():
    # No child exists until post-assignment stdin handshake. The outer job's
    # deadline/kill/join owns this wait and every descendant, including cgo.
    request = json.loads(sys.stdin.buffer.readline())
    child = subprocess.Popen(request['command'], cwd=request['cwd'], env=request['env'],
                             stdin=subprocess.DEVNULL, close_fds=True)
    return child.wait()


def run(command, cwd, evidence, label, env=None, timeout=900):
    """Retain first command, binary stdout/stderr, exit or timeout; never retry."""
    if DEADLINE is not None:
        timeout = min(timeout, DEADLINE - time.monotonic())
        if timeout <= 0:
            raise RuntimeError('total 25-minute native experiment deadline exceeded')
    for root in (Path(cwd), Path(tempfile.gettempdir())):
        if shutil.disk_usage(root).free < FLOOR_BYTES:
            raise RuntimeError('owned native command requires both filesystem roots above 96 MiB')
    if env is None:
        env = clean_git_env()
    receipt = {'command': list(map(str, command)), 'cwd': str(cwd), 'timeout_seconds': timeout}
    try:
        if sys.platform == 'win32':
            def save():
                write_json(evidence / (label + '.json'), receipt)
            code, out, err = run_windows_job(command, cwd, env, timeout, receipt, save,
                                            lambda state: write_json(evidence / (label + '.capacity.json'), state))
            receipt['exit'] = code
        else:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=timeout, check=False)
            receipt['exit'] = result.returncode
            out, err = result.stdout, result.stderr
    except subprocess.TimeoutExpired as error:
        receipt['timed_out'] = True
        (evidence / (label + '.stdout')).write_bytes(error.output or b'')
        (evidence / (label + '.stderr')).write_bytes(error.stderr or b'')
        write_json(evidence / (label + '.json'), receipt)
        raise
    (evidence / (label + '.stdout')).write_bytes(out)
    (evidence / (label + '.stderr')).write_bytes(err)
    write_json(evidence / (label + '.json'), receipt)
    if receipt['exit'] != 0:
        raise RuntimeError(f'{label} exited {receipt["exit"]}')
    return out


def validate_provenance(info, checkout, event, head, base):
    if any(not SHA.fullmatch(value) for value in (checkout, head, info.get('tree', ''))):
        raise ValueError('invalid checkout/head/tree identity')
    if info.get('checkout') != checkout or info.get('event') != event or info.get('head') != head or info.get('base') != base:
        raise ValueError('trusted context and recorded provenance differ')
    parents = info.get('parents')
    if not isinstance(parents, list) or any(not SHA.fullmatch(p) for p in parents):
        raise ValueError('malformed raw commit parents')
    if event == 'pull_request':
        if not SHA.fullmatch(base) or parents != [base, head]:
            raise ValueError('tested merge must contain declared base/head as ordered parents')
    elif event != 'push' or checkout != head or base:
        raise ValueError('only bound push or pull_request contexts are admitted')


def inventory(root):
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise ValueError('asset symlinks are not admitted')
        if path.is_file():
            result[path.relative_to(root).as_posix()] = digest(path)
    return result


def verify_assets(root, proof, provenance):
    if proof.get('provenance') != provenance:
        raise ValueError('asset producer used a different checkout/event identity')
    files = inventory(root)
    if files != proof.get('files') or not files:
        raise ValueError('asset manifest mismatch')
    if 'index.html' not in files or not any(p.startswith('_app/') for p in files):
        raise ValueError('real production adapter-static assets required')
    if 'Javinizer Web UI Placeholder' in (root / 'index.html').read_text(encoding='utf-8'):
        raise ValueError('placeholder assets are prohibited')
    return files


def verify_bundle_identity(bundle, proof, expected_digest, producer):
    if (not re.fullmatch(r'[0-9a-f]{64}', expected_digest or '')
            or proof.get('producer') != producer or proof.get('bundle_sha256') != expected_digest
            or digest(bundle) != expected_digest):
        raise ValueError('downloaded asset bundle differs from trusted successful same-run producer')


def source_byte_summary(data):
    """Exact byte diagnostics only; never normalize bytes for admission."""
    crlf = data.count(b'\r\n')
    return {'sha256': hashlib.sha256(data).hexdigest(), 'length': len(data),
            'lf': data.count(b'\n'), 'crlf': crlf,
            'bare_lf': data.count(b'\n') - crlf, 'lone_cr': data.count(b'\r') - crlf}


def reject_hidden_index_flags(repo, evidence, env):
    # Git diff intentionally trusts assume-unchanged/skip-worktree entries.
    # Reject those flags rather than clearing them or accepting hidden changes.
    indexed = run(['git', 'ls-files', '-v', '-z'], repo, evidence,
                  'asset-source-index-flags', env=env, timeout=30)
    entries = indexed.split(b'\0')
    if entries[-1] != b'' or any(len(entry) < 3 or entry[1:2] != b' ' for entry in entries[:-1]):
        raise ValueError('malformed tracked index flag inventory')
    hidden = [entry for entry in entries[:-1] if entry[:1].islower() or entry[:1] == b'S']
    summary = {'checked_entries': len(entries) - 1, 'hidden_entries_hex': [entry.hex() for entry in hidden]}
    write_json(evidence / 'asset-source-index-flags-summary.json', summary)
    if hidden:
        raise ValueError('tracked index flags can hide working-tree source changes')
    return summary


def verify_source_metadata(repo, proof, checkout, evidence):
    """Bind immutable producer source to Git blobs and reject dirty checkout inputs."""
    report = {'checkout': checkout, 'admission': 'exact verified-checkout Git blob SHA256 plus clean tracked checkout',
              'files': {}, 'settings_scope': 'standard system/global/local scopes observed now; diagnostic only'}
    destination = evidence / 'asset-source-byte-diagnostics.json'
    paths = [('web/frontend/package-lock.json', 'package_lock_sha256'),
             ('LICENSE', 'repository_license_sha256')]
    # Save actual bytes' summaries first, even if a later diagnostic Git command fails.
    for relative, key in paths:
        report['files'][relative] = {'expected_producer_sha256': proof[key],
                                    'working_tree': source_byte_summary((repo / relative).read_bytes())}
    write_json(destination, report)
    try:
        for index, (relative, _) in enumerate(paths):
            blob = run(['git', 'cat-file', 'blob', f'{checkout}:{relative}'], repo, evidence,
                       f'asset-source-blob-{index}', timeout=30)
            report['files'][relative]['canonical_git_blob'] = source_byte_summary(blob)
            write_json(destination, report)
        report['git_ls_files_eol'] = run(['git', 'ls-files', '--eol', '--', *[p for p, _ in paths]],
                                        repo, evidence, 'asset-source-eol', timeout=30).decode('utf-8')
        diagnostic_env = clean_git_env()
        # Read only these non-secret checkout settings, not arbitrary Git config.
        diagnostic_env.pop('GIT_CONFIG_NOSYSTEM', None)
        diagnostic_env.pop('GIT_CONFIG_GLOBAL', None)
        report['checkout_settings'] = {}
        for key in ('core.autocrlf', 'core.eol', 'core.safecrlf'):
            report['checkout_settings'][key] = run(
                ['git', 'config', '--show-origin', '--get', '--default', '<unset>', key],
                repo, evidence, 'asset-source-' + key, env=diagnostic_env, timeout=30).decode('utf-8')
            write_json(destination, report)
    except BaseException as error:
        report['diagnostic_failure'] = repr(error)
        write_json(destination, report)
        raise
    write_json(destination, report)
    for relative, key in paths:
        if proof[key] != report['files'][relative]['canonical_git_blob']['sha256']:
            raise ValueError('asset source lockfile/license differs from tested checkout Git blobs')
    report['index_flags'] = reject_hidden_index_flags(repo, evidence, diagnostic_env)
    write_json(destination, report)
    # Honor the same standard checkout conversion rules observed above. Disable
    # external diff/textconv: source changes must not be hidden by display helpers.
    run(['git', '-c', 'core.fsmonitor=false', 'diff', '--no-ext-diff', '--no-textconv',
         '--exit-code', checkout, '--'],
        repo, evidence, 'asset-source-tracked-clean', env=diagnostic_env, timeout=30)
    report['tracked_checkout_clean'] = True
    write_json(destination, report)


def verify_compiled_build(executable, name, command, module_info, goversion, expected_ldflags):
    expected_command = ['go', 'build', '-trimpath', '-tags', 'desktop,production',
                        '-cover', '-covermode=atomic', '-coverpkg=' + PACKAGE,
                        '-ldflags', expected_ldflags, '-o', str(executable), './cmd/javinizer']
    flag_tokens = expected_ldflags.split()
    desktop_flag = ('-X', 'github.com/javinizer/javinizer-go/internal/desktop.BuildDesktop=1')
    if command != expected_command or list(zip(flag_tokens, flag_tokens[1:])).count(desktop_flag) != 1:
        raise ValueError('recorded build command differs from required desktop flags')
    lines = module_info.splitlines()
    if not lines or lines[0] != str(executable) + ': ' + goversion:
        raise ValueError('compiled executable path or Go version differs')
    if lines.count('\tpath\t' + PACKAGE) != 1:
        raise ValueError('compiled import path differs')
    settings = {}
    for line in lines[1:]:
        if line.startswith('\tbuild\t'):
            key, separator, value = line[len('\tbuild\t'):].partition('=')
            if not separator or key in settings:
                raise ValueError('malformed or duplicate compiled build setting')
            settings[key] = value
    required = {'-buildmode': 'exe', '-compiler': 'gc', '-cover': 'true',
                '-tags': 'desktop,production', '-trimpath': 'true',
                'CGO_ENABLED': '1', 'GOOS': 'windows', 'GOARCH': 'amd64'}
    if any(settings.get(key) != value for key, value in required.items()):
        raise ValueError('compiled build-info differs from required supported settings')
    # Go intentionally omits -ldflags from build info under -trimpath (issue
    # 52372). The actual successful command receipt binds -X; mandatory native
    # console counters later prove desktop entry, not an invented metadata echo.
    if name not in ('console', 'gui') or owned.pe_subsystem(executable) != {'console': 3, 'gui': 2}[name]:
        raise ValueError('compiled PE subsystem differs from requested build')


def profile_counts(text, gui):
    lines = text.splitlines()
    if not lines or lines[0] != 'mode: atomic':
        raise ValueError('expected atomic child profile')
    counts = {line: None for line in (25, 26, 27, 29, 30, 32, 33)}
    seen = set()
    for row in lines[1:]:
        match = re.fullmatch(r'(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)', row)
        if not match:
            raise ValueError('malformed profile')
        filename, *nums = match.groups()
        start, col, end, endcol, statements, count = map(int, nums)
        key = (filename, start, col, end, endcol)
        if filename.rsplit('/', 1)[0] != PACKAGE or key in seen:
            raise ValueError('wrong package or duplicate profile block')
        if min(start, col, end, endcol, statements) <= 0 or (end, endcol) < (start, col):
            raise ValueError('invalid profile block')
        seen.add(key)
        if filename == CONSOLE_SOURCE:
            for line in counts:
                if start <= line <= end:
                    counts[line] = max(counts[line] or 0, count)
    required = (25, 26, 29, 30, 32, 33) if gui else (25, 26, 27)
    zero = (27,) if gui else (29, 30, 32, 33)
    if any(not counts[line] for line in required) or any(counts[line] != 0 for line in zero):
        raise ValueError('actual child does not prove the required native branch and excluded opposite branch')
    return counts


def verify_records(records, manifest):
    expected = [(s, g, c) for s in ('console', 'gui') for g in ('default', '1') for c in range(2)]
    if len(records) != len(expected):
        raise ValueError('expected exactly eight child records')
    for i, (record, (subsystem, gc, command)) in enumerate(zip(records, expected)):
        args = ['--version'] if command == 0 else ['version', '--owned-console-probe-invalid']
        out = manifest['expected_version_line'] if command == 0 else ''
        err = '' if command == 0 else owned.INVALID_STDERR
        if (record.get('case') != f'{subsystem}-{gc}-{i}' or record.get('gc') != gc
                or record.get('args') != args or record.get('exit') != command
                or record.get('stdout_hex') != out.encode('utf-8').hex()
                or record.get('stderr_hex') != err.encode('utf-8').hex()
                or not record.get('joined') or record.get('failure') or record.get('timed_out')
                or record.get('oracle_mismatch') or record.get('cleanup_failures') or record.get('receipt_failures')):
            raise ValueError('exact child output/exit/lifecycle contract not met')


def provenance(args, evidence):
    repo = args.repo.resolve(strict=True)
    actual = run(['git', 'rev-parse', 'HEAD'], repo, evidence, 'checkout').decode('ascii').strip()
    raw = run(['git', 'cat-file', '-p', actual], repo, evidence, 'raw-commit').decode('utf-8')
    headers = raw.split('\n\n', 1)[0].splitlines()
    info = {'checkout': actual, 'tree': next(s[5:] for s in headers if s.startswith('tree ')),
            'parents': [s[7:] for s in headers if s.startswith('parent ')],
            'event': args.event, 'head': args.head, 'base': args.base}
    validate_provenance(info, args.checkout, args.event, args.head, args.base)
    return info


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['assets', 'experiment', 'supervise'])
    for name in ('repo', 'output'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--assets', type=Path)
    parser.add_argument('--asset-bundle', type=Path)
    parser.add_argument('--expected-bundle-sha256')
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--run-attempt', required=True)
    parser.add_argument('--producer-outcome', required=True)
    for name in ('checkout', 'event', 'head'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--base', default='')
    parser.add_argument('--asset-proof', type=Path)
    args = parser.parse_args()
    global DEADLINE
    if args.action in ('experiment', 'supervise'):
        DEADLINE = time.monotonic() + 25 * 60
    args.repo = args.repo.resolve(strict=True)
    if args.assets is not None:
        args.assets = args.assets.resolve(strict=True)
    if not args.run_id.isdecimal() or not args.run_attempt.isdecimal() or args.producer_outcome != 'success':
        raise ValueError('trusted successful same-run static-e2e producer identity required')
    producer = {'run_id': args.run_id, 'run_attempt': args.run_attempt, 'job': 'static-e2e',
                'step': 'run-static-e2e', 'outcome': args.producer_outcome, 'command': ['make', 'test-static-e2e']}
    if args.output.resolve().is_relative_to(args.repo):
        raise ValueError('build/evidence directory must be outside the source checkout')
    if args.action == 'supervise':
        if sys.platform != 'win32' or os.environ.get('JAVINIZER_OWNED_CONSOLE_PROTOTYPE') != '1':
            raise RuntimeError('native supervisor requires explicit Windows opt-in')
        monitor = args.output.with_name(args.output.name + '-supervision')
        monitor.mkdir(parents=True, exist_ok=False)
        env = clean_git_env()
        env['JAVINIZER_CONSOLE_SUPERVISED'] = '1'
        command = [sys.executable, str(Path(__file__).resolve()), 'experiment', *sys.argv[2:]]
        # The outer owned job continuously guards ALL child Python extraction,
        # asset-copy, build and native phases, not only individual Go commands.
        run(command, monitor, monitor, 'whole-experiment', env, timeout=25 * 60)
        return
    if args.action == 'experiment' and os.environ.get('JAVINIZER_CONSOLE_SUPERVISED') != '1':
        raise RuntimeError('experiment must run under the continuously guarded supervisor')
    # Fresh evidence only; never overwrite/retry an earlier native measurement.
    args.output.mkdir(parents=True, exist_ok=False)
    evidence = args.output.resolve()
    info = provenance(args, evidence)
    if args.action == 'assets':
        if args.assets != (args.repo / 'web/frontend/build').resolve(strict=True):
            raise ValueError('assets must be the existing static-e2e production output path')
        reject_hidden_index_flags(args.repo, evidence, clean_git_env())
        paths = ['web/frontend', 'Makefile', 'scripts/with_embedded_web.sh']
        run(['git', 'diff', '--exit-code', 'HEAD', '--', *paths], args.repo, evidence, 'producer-tracked-clean')
        untracked = run(['git', 'ls-files', '--others', '--exclude-standard', '--', 'web/frontend'], args.repo, evidence, 'producer-untracked')
        if untracked.strip():
            raise ValueError('untracked frontend source cannot be attributed to HEAD')
        proof = {'provenance': info, 'files': inventory(args.assets),
                 'producer': producer,
                 'package_lock_sha256': digest(args.repo / 'web/frontend/package-lock.json'),
                 'repository_license_sha256': digest(args.repo / 'LICENSE')}
        verify_assets(args.assets, proof, info)
        bundle = evidence / 'assets.tar'
        with tarfile.open(bundle, 'w') as tar:
            for relative in proof['files']:
                tar.add(args.assets / relative, arcname=relative, recursive=False)
        proof['bundle_sha256'] = digest(bundle)
        write_json(evidence / 'assets.json', proof)
        return
    if sys.platform != 'win32' or os.environ.get('JAVINIZER_OWNED_CONSOLE_PROTOTYPE') != '1':
        raise RuntimeError('native Windows experiment requires explicit reviewed opt-in')
    if not args.asset_proof or not args.asset_bundle or not re.fullmatch(r'[0-9a-f]{64}', args.expected_bundle_sha256 or ''):
        raise ValueError('proof, bundle and trusted producer bundle digest required')
    if not hasattr(tarfile, 'data_filter'):
        raise RuntimeError('native Python must support safe tar data filtering; do not bypass it')
    asset_proof = json.loads(args.asset_proof.read_text(encoding='utf-8'))
    verify_bundle_identity(args.asset_bundle, asset_proof, args.expected_bundle_sha256, producer)
    args.assets = evidence / 'assets'
    args.assets.mkdir()
    with tarfile.open(args.asset_bundle) as bundle:
        bundle.extractall(args.assets, filter='data')
    verify_assets(args.assets, asset_proof, info)
    verify_source_metadata(args.repo, asset_proof, info['checkout'], evidence)
    write_json(evidence / 'assets.json', asset_proof)
    # Build from tracked source archive in our own fresh directory, not dirty live source.
    archive = run(['git', 'archive', '--format=tar', info['checkout']], args.repo, evidence, 'source-archive')
    archive_path = evidence / 'source.tar'
    archive_path.write_bytes(archive)
    build = evidence / 'source'
    build.mkdir()
    with tarfile.open(archive_path) as source:
        source.extractall(build, filter='data')
    if digest(build / 'cmd/javinizer/console_windows.go') != CONSOLE_SHA:
        raise ValueError('console source changed: line mapping must be independently reviewed')
    # Only replace the newly archived owned placeholder tree, never the checkout.
    shutil.rmtree(build / 'web/dist')
    shutil.copytree(args.assets, build / 'web/dist')
    verify_assets(build / 'web/dist', asset_proof, info)
    env = clean_build_env()
    effective = run(['go', 'env', '-json', 'GOENV', 'GOFLAGS', 'GOWORK', 'GOEXPERIMENT', 'CGO_ENABLED', 'GOOS', 'GOARCH', 'CC', 'CXX', 'GOROOT', 'GOTOOLCHAIN'], build, evidence, 'effective-build-env', env)
    effective = json.loads(effective.decode('utf-8'))
    if any(effective.get(key) != value for key, value in {'GOENV': '', 'GOFLAGS': '', 'GOWORK': 'off', 'GOEXPERIMENT': '', 'CGO_ENABLED': '1', 'GOOS': 'windows', 'GOARCH': 'amd64', 'GOTOOLCHAIN': 'local'}.items()):
        raise ValueError('effective build environment allows an unreviewed override')
    goversion = run(['go', 'env', 'GOVERSION'], build, evidence, 'goversion', env).decode('utf-8').strip()
    declared_go = re.search(r'^go (\S+)$', (build / 'go.mod').read_text(), re.M).group(1)
    version_pattern = re.escape('go' + declared_go) + (r'(?:\.\d+)?' if declared_go.count('.') == 1 else '')
    if not re.fullmatch(version_pattern, goversion):
        raise ValueError('actual Go toolchain differs from pinned go.mod')
    stamp = run(['git', 'show', '-s', '--format=%cI', info['checkout']], args.repo, evidence, 'build-date').decode('ascii').strip()
    run(['go', 'run', './internal/desktop/icons/genicon', '-ico', 'internal/desktop/icons/javinizer.ico',
         '-o', 'cmd/javinizer/javinizer.syso', '-arch', 'amd64'], build, evidence, 'official-icon', env)
    version = 'v0.0.0-console-probe'
    flags = f'-w -s -X github.com/javinizer/javinizer-go/internal/version.Version={version} -X github.com/javinizer/javinizer-go/internal/version.Commit={info["checkout"]} -X github.com/javinizer/javinizer-go/internal/version.BuildDate={stamp} -X github.com/javinizer/javinizer-go/internal/desktop.BuildDesktop=1'
    manifest = {'source_commit': info['checkout'], 'build_desktop': '1', 'tags': 'desktop,production',
                'provenance': info, 'instrumented': True, 'coverpkg': PACKAGE,
                'expected_version_line': f'javinizer {version} (commit: {info["checkout"]}, built: {stamp}, go: {goversion})\n',
                'effective_build_environment': effective, 'requested_goenv': env['GOENV'],
                'go_executable_sha256': digest(Path(shutil.which('go'))),
                'go_mod_sha256': digest(build / 'go.mod'), 'go_sum_sha256': digest(build / 'go.sum'),
                'repository_license_sha256': digest(build / 'LICENSE'),
                'icon_input_sha256': digest(build / 'internal/desktop/icons/javinizer.ico'),
                'icon_resource_sha256': digest(build / 'cmd/javinizer/javinizer.syso')}
    for name in ('console', 'gui'):
        executable = evidence / (name + '.exe')
        ldflags = flags + (' -H windowsgui' if name == 'gui' else '')
        command = ['go', 'build', '-trimpath', '-tags', 'desktop,production', '-cover', '-covermode=atomic',
                   '-coverpkg=' + PACKAGE, '-ldflags', ldflags, '-o', str(executable), './cmd/javinizer']
        run(command, build, evidence, 'build-' + name, env)
        module_info = run(['go', 'version', '-m', str(executable)], build, evidence, 'build-info-' + name, env).decode('utf-8')
        verify_compiled_build(executable, name, command, module_info, goversion, ldflags)
        manifest[name] = {'path': str(executable), 'sha256': digest(executable), 'command': command}
    manifest_path = evidence / 'build-manifest.json'
    write_json(manifest_path, manifest)
    host_output = evidence / 'children'
    run([sys.executable, str(Path(__file__).with_name('owned_windows_console.py')), '--manifest', str(manifest_path),
         '--output', str(host_output), '--expected-checkout', info['checkout']], build, evidence, 'native-matrix', env, timeout=300)
    owned = list(host_output.glob('owned-console-*'))
    if len(owned) != 1:
        raise ValueError('expected exactly one owned console execution')
    lifecycle = json.loads((owned[0] / 'lifecycle.json').read_text(encoding='utf-8'))
    if lifecycle.get('status') != 'public-output-pass-native-coverage-unverified' or lifecycle.get('cleanup_failures') or lifecycle.get('receipt_failures'):
        raise ValueError('native lifecycle/output failed')
    records = json.loads((owned[0] / 'observations.json').read_text(encoding='utf-8'))
    verify_records(records, manifest)
    directories, proofs = [], []
    for record in records:
        directory = Path(record['coverage_dir']).resolve(strict=True)
        if directory != (owned[0] / record['case']).resolve() or not list(directory.glob('covmeta.*')) or not list(directory.glob('covcounters.*')):
            raise ValueError('missing or incorrectly owned per-child covdata')
        profile = evidence / (record['case'] + '.out')
        run(['go', 'tool', 'covdata', 'textfmt', '-i=' + str(directory), '-o=' + str(profile)], build, evidence, 'convert-' + record['case'], env)
        counts = profile_counts(profile.read_text(encoding='utf-8'), record['case'].startswith('gui-'))
        proofs.append({'case': record['case'], 'counts': counts, 'raw_files': inventory(directory), 'profile_sha256': digest(profile)})
        directories.append(str(directory))
    merged = evidence / 'merged-covdata'
    merged.mkdir()
    run(['go', 'tool', 'covdata', 'merge', '-i=' + ','.join(directories), '-o=' + str(merged)], build, evidence, 'merge-covdata', env)
    admitted = evidence / 'coverage-owned-console-windows.out'
    run(['go', 'tool', 'covdata', 'textfmt', '-i=' + str(merged), '-o=' + str(admitted)], build, evidence, 'merged-profile', env)
    write_json(evidence / 'admission.json', {'provenance': info, 'cases': proofs,
               'profile_sha256': digest(admitted), 'qualification': 'real instrumented production CLI children; GC stress is not proof of GC timing'})


if __name__ == '__main__':
    if sys.argv[1:] == ['--tool-host']:
        raise SystemExit(tool_host())
    main()
