# Durable offline evidence publication

This package stores **canonical, already validated r18 dump evidence**, without
live fetching, database/config/media changes or pruning. The explicit offline
`actress provision-cache` CLI is a publisher; runtime readers can use the same
finite limits from `internal/actresscache/offline`.
A source receipt is an assertion about one supplied file, never upstream
completeness, authenticity or identity/credit authority. Empty evidence remains
explicitly empty, not a deletion instruction.

## Files and admission

`Open` requires an existing absolute clean directory, rejects a symlink leaf,
checks its identity across opening, and pins it through `os.Root`. All store names
are generated flat names relative to this root; manifests cannot supply a path.
No permission changes or parent directory creation are performed. Windows root
identity is captured eagerly from a no-follow, attribute-only native handle, not
from path metadata whose file-ID lookup can be deferred until after substitution.
The handle closes before opening the root capability. Long drive/UNC roots use
extended-length names when needed; existing namespaces remain unchanged. On Darwin and
Linux the directory must not be group/other writable. **The caller must exclude
untrusted directory writers**, including same-user processes; this is not a
hostile shared-directory protocol. Root confinement does not forbid mount points
or hard links. A privileged process modifying the root is outside the guarantee.

Publication currently supports Darwin/Linux. Other platforms fail closed at
writer admission, before input reads or lock-file creation. Readers remain
bounded and verify the same canonical manifest/artifact contract. Windows native
publication is a later gate: existing path-oriented replacement helpers do not
by themselves prove root-bound replacement, admission and directory durability.
No Windows crash or atomic-write claim is made by this slice.

Existing `fsutil.ReplaceFile` uses path-based MoveFileEx with replacement;
`PublishNoReplace` uses MoveFileEx without replacement. `PublishStagedBound`
provides Windows handle/name identity checks and post-publish verification, but
its close-before-path-publication window has explicit refusal/retention semantics.
These are useful building blocks, not the complete root-bound manifest protocol.
The keyed lock registries serialize in-process mutexes, not independent processes.
A Windows writer additionally needs audited native nonblocking cross-process
locking, reparse/root-bound publication, handle-sharing rules, uncertain-outcome
classification and an explicit directory-flush/persistence contract. Native hosted
Windows tests must verify those together before enabling writes; cross-compiling
these read/refusal tests supplies no such proof.

An instance TryLock and nonblocking OS flock on the retained `.writer.lock`
inode admit one cooperating writer across instances/processes. Never unlink the
lock file, including on restart. The OS releases its lock when the handle/process
closes; no stale-file timeout or lock stealing exists. Close must not race store
operations. Concurrent readers may observe either complete committed generation;
old in-memory views stay valid.

Manifest reads retry only a verified regular-file inode replacement, at most
three attempts per slot (six total manifest attempts per Load). A fresh no-follow
leaf check must still show a regular, bounded file. Exhausted churn returns
ErrReadContention, not empty/corrupt state. Malformed manifests, digest failures,
unsafe entries, arbitrary I/O errors and immutable-object changes are not retried.
A descriptor pinning a valid older manifest remains usable after a later regular
publication; unrelated renames do not invalidate that committed generation.
Missing-manifest classification applies only to the initial leaf lookup, not
to disappearance after an existing leaf was observed.

Positive limits bound manifest bytes (at most 1 MiB), source-receipt bytes/rows,
artifact decoding, directory entries (at most one million) and stored bytes.
A bounded directory scan rejects foreign names and nonregular entries, counts
orphan/stage files too, and conservatively reserves peak publication occupancy.
Hard links count twice. No existing files are pruned to make room. Repeated
publication/orphans can reach a finite budget, at which point publication refuses
until a separately reviewed maintenance operation is supplied. No automatic GC.

## Exact data and manifest

The artifact is bounded, decoded, and required to equal the decoder's canonical
bytes exactly. The filename is `<lowercase-sha256>.ndjson`. Manifest digest and
size bind these exact bytes; provenance and counts must agree with the receipt.
Only r18dev evidence is accepted; all record dataset hashes equal the asserted
receipt SHA-256. Receipt rows/bytes/counts have finite bounds, but the publisher
cannot independently reconstruct source-file authenticity or completeness.

Manifests are fixed-schema canonical JSON with one LF. A decode/re-encode byte
comparison rejects extra/duplicate keys, unknown fields, whitespace/order aliases,
unsupported versions and type confusion. Scope is exactly `supplied-file-only`.
`current.json` and `previous.json` are the only recovery roots. Loading checks
canonical manifests, exact object length/hash, format limits and receipt binding.
Invalid current falls back to validated previous and reports `Recovered=true`.
If both are absent, ErrEmpty; otherwise neither-valid fails closed. Loading never
selects a newer-looking orphan, staging file, timestamp or directory order.

`LoadReferencedSnapshot(ctx, digest)` is a separate read-only recovery boundary
for a digest recorded in an **already admitted durable job**, not a lookup API
for arbitrary request strings. The caller must establish that job/reference
relationship. It validates lowercase SHA-256 before constructing the one flat
`<digest>.ndjson` name, then uses the same bounded root-confined regular-file and
identity checks, verifies the exact SHA-256 and canonical decoder bytes, and
requires r18dev-only records. It does not scan, read or infer current/previous,
select an orphan, reconstruct a source receipt, or retry a missing/changed object.
A reference can therefore remain valid after both manifest slots rotate, without
silently substituting a different generation. The caller must retain every
immutable artifact referenced by an active durable job. Missing or corrupt
references fail closed and require explicit repair; this package never prunes.
The narrow referenced reader remains available on Windows.

## Publication order and outcomes

1. Admit the writer; read/validate bounded artifact and receipt; load last good.
2. Preflight finite directory/peak-byte capacity.
3. Write an exclusive random staging file, file Sync, close, then hard-link to the
   digest name with **no replacement**. Existing objects must match exactly and
   receive a file Sync before reuse. Sync the directory after a new object link.
4. Stage+Sync+close the validated old manifest and atomically rename it onto
   previous, then Sync the directory before changing current.
5. Stage+Sync+close the new manifest and atomically rename it onto current.
6. Sync the directory. Clean up only this operation's own staging names.

There is **no remove-existing-destination fallback**. Unsupported hard links,
replacement or Sync refuse. A failed operation may leave bounded unreferenced
objects/stages, which are never guessed to be committed. Before the current
rename, last-good current remains, and previous is at worst the same valid old
manifest. After successful current rename, Result.Published is true even if
final directory Sync fails; callers must not interpret that error as nonexecution.
No automatic rollback overwrites a committed generation.

A current rename returning an error sets CommitAttempted and returns
ErrCommitUncertain: Published=false does not prove nonexecution in this case.
Load/recovery must establish the visible generation before a later action. A
regression performs a real rename then returns a simulated lost reply. Peak
capacity reserves the larger of old/new manifest sizes, including recovery data.

Cancellation is checked during input reads and immediately before each link or
manifest rename. A cancellation racing the final check/rename can still commit;
after successful current rename, cancellation does not undo it. Blocked I/O and
filesystem calls are not preemptible. Each operation is bounded, but callers must
also bound concurrent readers and retained views.

Atomic visibility, successful file Sync calls and successful directory Sync are
separate facts. Result exposes those facts. Local tests verify real calls and
failure ordering; they do **not** simulate power loss or prove filesystem/device
persistence. POSIX same-directory rename supplies the supported atomic-visibility
primitive. No universal crash guarantee is claimed for SMB/NFS/FUSE, device write
caches, unusual mounts, or unverified platforms. Required sync failures remain
errors, rather than being silently treated as durable success.

## Validation seams

Private per-instance operation hooks default to the same native Root/File calls;
there are no mutable global filesystem hooks or public alternate backends. Tests
inject narrow syscall failures where forcing real disk/kernel failure is unsafe,
and use actual closed handles and root/file substitutions for identity guards.
Coverage profiles are ordinary Go-generated profiles. Complete local statement
coverage does not establish exhaustive logical branch coverage, power-loss
persistence, hostile shared-directory safety or native Windows write behavior.
