#!/usr/bin/env python3
"""SOURCE-ONLY prototype: real CLI children of a fixture-owned Windows console.

Not CI-wired. Read owned_windows_console.md before authorizing native execution.
This program never attaches/detaches a console or changes standard handles.
"""
import argparse
import ctypes
from ctypes import wintypes
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import time

TIMEOUT = 20


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def pe_subsystem(path):
    data = path.read_bytes()
    if data[:2] != b'MZ':
        raise ValueError('not a PE executable')
    offset = struct.unpack_from('<I', data, 0x3c)[0]
    if data[offset:offset + 4] != b'PE\0\0':
        raise ValueError('missing PE signature')
    if struct.unpack_from('<H', data, offset + 4)[0] != 0x8664:
        raise ValueError('expected amd64 COFF machine')
    optional = offset + 24
    if struct.unpack_from('<H', data, optional)[0] != 0x20b:
        raise ValueError('expected official amd64 PE32+ executable')
    return struct.unpack_from('<H', data, optional + 68)[0]


def kernel():
    k = ctypes.WinDLL('kernel32', use_last_error=True)
    signatures = {
        'CreateJobObjectW': ([ctypes.c_void_p, wintypes.LPCWSTR], wintypes.HANDLE),
        'SetInformationJobObject': ([wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD], wintypes.BOOL),
        'OpenProcess': ([wintypes.DWORD, wintypes.BOOL, wintypes.DWORD], wintypes.HANDLE),
        'AssignProcessToJobObject': ([wintypes.HANDLE, wintypes.HANDLE], wintypes.BOOL),
        'TerminateJobObject': ([wintypes.HANDLE, wintypes.UINT], wintypes.BOOL),
        'CloseHandle': ([wintypes.HANDLE], wintypes.BOOL),
        'GetConsoleProcessList': ([ctypes.POINTER(wintypes.DWORD), wintypes.DWORD], wintypes.DWORD),
    }
    for name, (args, result) in signatures.items():
        getattr(k, name).argtypes = args
        getattr(k, name).restype = result
    return k


def checked(value):
    if not value:
        raise ctypes.WinError(ctypes.get_last_error())
    return value


class BasicLimits(ctypes.Structure):
    _fields_ = [('process_time', ctypes.c_longlong), ('job_time', ctypes.c_longlong),
                ('flags', wintypes.DWORD), ('min_working_set', ctypes.c_size_t),
                ('max_working_set', ctypes.c_size_t), ('active_processes', wintypes.DWORD),
                ('affinity', ctypes.c_size_t), ('priority', wintypes.DWORD),
                ('scheduling', wintypes.DWORD)]


class IOCounters(ctypes.Structure):
    _fields_ = [(name, ctypes.c_ulonglong) for name in
                ('read_ops', 'write_ops', 'other_ops', 'read_bytes', 'write_bytes', 'other_bytes')]


class ExtendedLimits(ctypes.Structure):
    _fields_ = [('basic', BasicLimits), ('io', IOCounters),
                ('process_memory', ctypes.c_size_t), ('job_memory', ctypes.c_size_t),
                ('peak_process_memory', ctypes.c_size_t), ('peak_job_memory', ctypes.c_size_t)]


# Derived from version.NewCommand/root flags and pinned Cobra/pflag templates.
# Includes Cobra's UsageString newline + Println newline, then main's crash log.
INVALID_STDERR = """Usage:
  javinizer version [flags]

Flags:
  -c, --check   check for updates
  -h, --help    help for version
  -s, --short   show only the short version

Global Flags:
      --config string   config file path (default "configs/config.yaml")
  -v, --verbose         enable debug logging

unknown flag: --owned-console-probe-invalid
"""


def save_receipt(save, record):
    try:
        save()
    except Exception as error:
        record.setdefault('receipt_failures', []).append(repr(error))
        # Disk failure cannot create durable evidence; report without masking the
        # primary process failure, and prohibit a successful final admission.
        try:
            print('Cannot persist owned-process receipt: ' + repr(error), file=sys.stderr)
        except Exception:
            pass


def remember_output(record, out, err):
    if out is not None:
        record['stdout_hex'] = out.hex()
    if err is not None:
        record['stderr_hex'] = err.hex()


def cleanup_process(child, record, save):
    """Bounded best effort; retain original failure and every cleanup failure."""
    failures = record.setdefault('cleanup_failures', [])
    try:
        alive = child.poll() is None
    except Exception as error:
        failures.append('poll: ' + repr(error))
        alive = True
    if alive:
        try:
            child.kill()
            record['kill_requested'] = True
        except Exception as error:
            failures.append('kill: ' + repr(error))
    save_receipt(save, record)
    try:
        out, err = child.communicate(timeout=TIMEOUT)
        remember_output(record, out, err)
        record['exit'] = child.returncode
        record['joined'] = True
    except Exception as error:
        failures.append('bounded drain: ' + repr(error))
        if isinstance(error, subprocess.TimeoutExpired):
            partial = {}
            remember_output(partial, error.output, error.stderr)
            record.setdefault('drain_timeouts', []).append(partial)
            remember_output(record, error.output, error.stderr)
    save_receipt(save, record)


def capture(child, record, save, payload=None, timeout=TIMEOUT, abort=None, cleanup_on_error=True):
    """Persist partial bytes before trying cleanup; no implicit __exit__.wait."""
    record['pid'] = child.pid
    save_receipt(save, record)
    partial_out = partial_err = None
    deadline = time.monotonic() + timeout
    try:
        pending = payload
        while True:
            if abort is not None and abort.is_set():
                raise RuntimeError('owned capacity watcher requested termination')
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired('owned process', timeout, output=partial_out, stderr=partial_err)
            try:
                out, err = child.communicate(pending, timeout=min(remaining, 0.5) if abort is not None else remaining)
                break
            except subprocess.TimeoutExpired as progress:
                partial_out, partial_err = progress.output, progress.stderr
                pending = None
                if abort is None:
                    raise
                # Continue draining the SAME process, never rerun its command.
    except BaseException as error:
        remember_output(record, partial_out, partial_err)
        record['failure'] = repr(error)
        if isinstance(error, subprocess.TimeoutExpired):
            record['timed_out'] = True
            partial = {}
            remember_output(partial, error.output, error.stderr)
            record['first_timeout_output'] = partial
            remember_output(record, error.output, error.stderr)
        save_receipt(save, record)
        if cleanup_on_error:
            cleanup_process(child, record, save)
        raise
    remember_output(record, out, err)
    record['exit'] = child.returncode
    record['joined'] = True
    save_receipt(save, record)
    return out, err


def best_effort(action, label, record, save):
    try:
        action()
        record.setdefault('cleanup_completed', []).append(label)
    except Exception as error:
        record.setdefault('cleanup_failures', []).append(label + ': ' + repr(error))
    save_receipt(save, record)


def host():
    # No children before the supervisor's post-job-assignment handshake.
    request = json.loads(sys.stdin.buffer.readline())
    k = kernel()
    pids = (wintypes.DWORD * 4)()
    count = checked(k.GetConsoleProcessList(pids, len(pids)))
    if count != 1 or pids[0] != os.getpid():
        raise RuntimeError('host does not exclusively own its newly created console')
    root = Path(request['root']).resolve(strict=True)
    observations = []

    def save():
        (root / 'observations.json').write_text(json.dumps(observations, indent=2), encoding='utf-8')

    for subsystem in ('console', 'gui'):
        executable = root / (subsystem + '.exe')
        for gc in ('default', '1'):
            for command, expected_out, expected_err, expected_exit in request['cases']:
                label = f'{subsystem}-{gc}-{len(observations)}'
                coverage = root / label
                coverage.mkdir()
                env = os.environ.copy()
                env.pop('GOGC', None)
                if gc != 'default':
                    env['GOGC'] = gc
                env['GOCOVERDIR'] = str(coverage)
                for name in ('APPDATA', 'LOCALAPPDATA', 'HOME', 'USERPROFILE', 'XDG_CONFIG_HOME'):
                    env[name] = str(root / 'home')
                record = {'case': label, 'args': command, 'coverage_dir': str(coverage), 'gc': gc}
                observations.append(record)
                save_receipt(save, record)
                child = subprocess.Popen([str(executable), *command], cwd=root, env=env,
                                         stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                         stderr=subprocess.PIPE, close_fds=True)
                out, err = capture(child, record, save)
                if record.get('receipt_failures'):
                    raise RuntimeError('child evidence could not be persisted')
                if (out, err, child.returncode) != (expected_out.encode(), expected_err.encode(), expected_exit):
                    record['oracle_mismatch'] = True
                    save_receipt(save, record)
                    raise RuntimeError(f'{label}: exact public output/exit mismatch (see observations.json)')
    print(json.dumps({'host_pid': os.getpid(), 'console_pids_before_children': list(pids)[:count],
                      'observations': observations}), flush=True)


def assert_native_abi():
    # Check the actual native amd64 ABI, not a Linux ctypes approximation.
    if tuple(map(ctypes.sizeof, (BasicLimits, IOCounters, ExtendedLimits))) != (64, 48, 144):
        raise RuntimeError('unexpected Windows amd64 job structure ABI')
    if (BasicLimits.flags.offset, ExtendedLimits.io.offset, ExtendedLimits.process_memory.offset) != (16, 64, 112):
        raise RuntimeError('unexpected Windows amd64 job field offsets')


def supervise(args):
    manifest_path = args.manifest.resolve(strict=True)
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    if not re.fullmatch(r'[0-9a-f]{40}', args.expected_checkout) or manifest['source_commit'] != args.expected_checkout:
        raise ValueError('build source differs from externally supplied checkout')
    if manifest['build_desktop'] != '1' or manifest['tags'] != 'desktop,production':
        raise ValueError('real desktop build required for both subsystem variants')
    args.output.mkdir(parents=True, exist_ok=True)
    root = Path(tempfile.mkdtemp(prefix='owned-console-', dir=args.output.resolve()))
    (root / 'home').mkdir()
    shutil.copyfile(manifest_path, root / 'build-manifest.json')
    for name, subsystem in (('console', 3), ('gui', 2)):
        source = Path(manifest[name]['path']).resolve(strict=True)
        if digest(source) != manifest[name]['sha256'] or pe_subsystem(source) != subsystem:
            raise ValueError(f'{name}: executable hash/subsystem mismatch')
        copied = root / (name + '.exe')
        shutil.copyfile(source, copied)
        if digest(copied) != manifest[name]['sha256'] or pe_subsystem(copied) != subsystem:
            raise ValueError(f'{name}: copied executable hash/subsystem mismatch')
    version = manifest['expected_version_line']
    if not version.startswith('javinizer ') or not version.endswith('\n') or version.count('\n') != 1:
        raise ValueError('independently derived exact version line required')
    request = {'root': str(root), 'cases': [
        [['--version'], version, '', 0],
        [['version', '--owned-console-probe-invalid'], '', INVALID_STDERR, 1],
    ]}
    k = kernel()
    record = {'status': 'started', 'root': str(root)}

    def save():
        (root / 'lifecycle.json').write_text(json.dumps(record, indent=2), encoding='utf-8')

    job = None
    process = None
    child = None
    try:
        assert_native_abi()
        job = checked(k.CreateJobObjectW(None, None))
        limits = ExtendedLimits()
        limits.basic.flags = 0x2000  # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        checked(k.SetInformationJobObject(job, 9, ctypes.byref(limits), ctypes.sizeof(limits)))
        child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), '--host'],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 creationflags=subprocess.CREATE_NEW_CONSOLE, close_fds=True)
        record['pid'] = child.pid
        save_receipt(save, record)
        process = checked(k.OpenProcess(0x0100 | 0x0001, False, child.pid))
        checked(k.AssignProcessToJobObject(job, process))
        record['job_assigned_before_handshake'] = True
        save_receipt(save, record)
        capture(child, record, save, json.dumps(request).encode('utf-8'), TIMEOUT * 10)
        if child.returncode != 0:
            raise RuntimeError(f'owned host failed ({child.returncode}); evidence: {root}')
        record['status'] = 'public-output-pass-native-coverage-unverified'
        save_receipt(save, record)
        if record.get('receipt_failures'):
            raise RuntimeError('host evidence could not be persisted')
        print(f'Public output checks passed; native coverage NOT YET VERIFIED: {root}')
    except BaseException as error:
        record['primary_failure'] = repr(error)
        save_receipt(save, record)
        raise
    finally:
        primary_failure = sys.exc_info()[0] is not None
        # Termination targets only our assigned job, never a console broadcast.
        # Even a failed CloseHandle gets a preceding independent terminate attempt.
        if job is not None:
            best_effort(lambda: checked(k.TerminateJobObject(job, 124)), 'terminate owned job', record, save)
            best_effort(lambda: checked(k.CloseHandle(job)), 'close job handle', record, save)
        if child is not None:
            cleanup_process(child, record, save)
        if process is not None:
            best_effort(lambda: checked(k.CloseHandle(process)), 'close process handle', record, save)
        if (record.get('cleanup_failures') or record.get('receipt_failures')) and not primary_failure:
            raise RuntimeError(f'owned cleanup incomplete; see {root}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--expected-checkout')
    args = parser.parse_args()
    if sys.platform != 'win32' or os.environ.get('JAVINIZER_OWNED_CONSOLE_PROTOTYPE') != '1':
        raise SystemExit('requires explicit authorized native Windows prototype opt-in')
    if args.host:
        host()
    elif args.manifest and args.output and args.expected_checkout:
        supervise(args)
    else:
        parser.error('--manifest, --output and --expected-checkout required')


if __name__ == '__main__':
    main()
