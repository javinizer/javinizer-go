# Isolated kernel-FUSE publication tests

This suite tests issue #252's filesystem **contract**, using synthetic local files
on a disposable Linux FUSE filesystem. It never mounts, contacts, configures or
reads a personal NAS. It is not a claim to reproduce every Unraid/shfs version,
SMB configuration or underlying disk layout.

The FUSE server is a real in-process userspace daemon provided by pinned
`github.com/hanwen/go-fuse/v2 v2.12.0`. Application calls traverse the kernel VFS
and `/dev/fuse`. The daemon forwards normal operations to a private local
backing directory and returns controlled protocol errors for selected operations.
Production syscall hooks and mock `afero.Fs` implementations are not used. The
nested module keeps FUSE dependencies out of the application module and ordinary
`go test ./...` runs.

## Requirements and execution

- Linux, an ordinary non-root user, and the reviewed Go **1.27.2** toolchain
- An **already present and accessible** `/dev/fuse`
- The distribution's existing `/usr/bin/fusermount3` helper, root-owned and not
  writable by other users
- A local temporary directory with no user data

Dependency setup, which may access public Go registries:

```sh
(cd tests/fuseintegration && go mod download)
```

Then, from the repository root, run the guarded, mandatory runtime:

```sh
python3 scripts/run_fuse_integration.py --evidence fuse-evidence
```

The runner first generates the existing source-hash-checked Go network overlay
and runs all seven native guard canaries. Dependency fetching is disabled during
runtime (`GOPROXY=off`, `GOSUMDB=off`, `GOVCS=*:off`). Inherited proxy settings
stay unchanged; the strict guard blocks external targets and configured proxy
endpoints before network activity. The FUSE suite uses only synthetic local files. No browser, remote
filesystem, NAS address, credentials, application configuration or media library
is involved. The Go guard is test-process instrumentation, not an OS-level
network sandbox; the fixture itself contains no network client.

The proposed GitHub Actions job targets a fresh `ubuntu-24.04` hosted runner.
It checks existing FUSE prerequisites and **fails rather than skips** if they
are missing or unusable. It does not run sudo, install or modify a setuid helper,
create a device, load a module, change capabilities/AppArmor, or use privileged
Docker. If a runner's existing helper cannot mount, that is an explicit blocker;
no security workaround is attempted. The runtime preserves the selected Go
binary and places `/usr/bin` before `/usr/local/bin` to avoid a shadow helper.

A precondition failure on a machine without `/dev/fuse` is expected and is not a
successful integration run. Do not infer readiness from compilation or unit
fault-injection tests alone. The composed code change and this harness must pass
the hosted job before claiming real-kernel validation.

## Mandatory evidence

Mandatory fixture and application assertions:

1. `statfs` reports `FUSE_SUPER_MAGIC`, mountinfo contains the exact owned mount,
   and its device differs from the local source device.
2. A real mounted CREATE/WRITE round-trip reaches counted daemon callbacks and
   produces identical bytes in the backing directory. Kernel passthrough and
   copy-file-range shortcuts are disabled so byte counters cannot be bypassed.
3. In every application case, native-to-FUSE `renameat2(RENAME_NOREPLACE)` actually returns `EXDEV` in the
   kernel, preserving source bytes. No EXDEV hook is substituted.
4. Teardown verifies root and unambiguous mount IDs before unmount, then unmount completion
   and original directory identities. Cleanup never
   recursively deletes a tree, lazy-unmounts or deletes through a remaining
   mount. An unexpected identity, directory or unmount failure fails the test
   and retains the fixture for diagnosis.

Dedicated protocol canaries prove:

- Capable FUSE forwards a successful real backing `RENAME_NOREPLACE`, preserves
  both files on `EEXIST`, and creates a real same-inode hard link.
- Daemon `RENAME2 = ENOSYS` becomes caller `EINVAL`; a second request exercises
  the kernel's cached refusal without another daemon callback.
- Daemon `RENAME2 = EOPNOTSUPP` remains caller `EOPNOTSUPP`.
- Daemon `LINK = EPERM` reaches the caller as `EPERM`.
- Daemon `LINK = ENOSYS` is verified against the actual kernel policy: older
  kernels return `ENOSYS` twice with two daemon calls; newer kernels return
  `EPERM` twice with only one daemon call because refusal is cached. Any other
  error/count combination fails. Kernel version and observed policy are logged.

Each unsupported application case starts a **fresh mount** so the ENOSYS cache
cannot hide an unexecuted failure path. Daemon counters must prove the initial
failure was actually returned.

## Application matrix

All seven verified `fsutil` entry points run through the real `afero.OsFs` path:

- Copy, copy with explicit mode, copy with installed identity
- Copy with digest, copy with digest and mode
- Cross-device move, cross-device move with mode

For each entry point:

- **Unsupported:** `RENAME2 ENOSYS` or `EOPNOTSUPP`, plus `LINK EPERM`, must return
  `ErrPublishNoReplaceUnsupported`; zero payload staging creates/writes; source
  bytes and the original source name survive; no destination or claim residue.
  `EINVAL`/`EXDEV` must not leak through the final typed refusal, and publication
  must not be marked completed.
- **Capable:** native rename publication and hard-link fallback both succeed,
  produce exact bytes and valid returned digest/installed identity, remove the
  source only for a successful move, and leave no staging residue.
- **Collisions:** an existing destination and a foreign writer inserted at the
  actual final rename or hard link both preserve the foreign bytes and source. A successful
  preflight does not bypass the final atomic no-replace operation.
- **Errors:** real daemon WRITE `EIO` and final RENAME/LINK `EIO` preserve source,
  publish nothing and clean owned staging/probe files.
- **Cancellation:** the caller's admission proof returning `context.Canceled`
  refuses without payload staging. These APIs have no context parameter: this
  specifically tests admission cancellation, **not mid-stream cancellation**.

The runner requires all expected top-level tests, all **82 mounted leaf cases**
and the pure mount-identity parser test, rejects
any skipped test, and requires an empty runtime network audit. It archives JSON
events, stderr, exact test requirements, audit counts, toolchain/kernel/commit
metadata and prerequisite diagnostics. A missing case is a failure.

## Sources

- [Linux FUSE rename/link implementation](https://github.com/torvalds/linux/blob/v6.18/fs/fuse/dir.c)
- [Pinned go-fuse mount/daemon implementation](https://github.com/hanwen/go-fuse/tree/v2.12.0)
- [libfuse's unprivileged mount helper](https://github.com/libfuse/libfuse#about)
- [GitHub-hosted runner model](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners)
- [Ubuntu 24.04 runner image inventory](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md)

The image inventory is not a guarantee that FUSE is usable. Prerequisite checks
and actual successful mounts, protocol canaries, application assertions and
teardown evidence are the authority for each run.
