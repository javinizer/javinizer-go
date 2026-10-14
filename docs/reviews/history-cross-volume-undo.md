# Cross-volume history Undo: bounded candidate

## Status and compatibility

This repairs a preexisting limitation, not a 1.6.0 regression: ordinary primary
Undo used a plain rename in both v1.5.2 and main 8557971. EXDEV left the library
primary in place and the original source absent; independent sidecar restores
could already have happened. The primary function was byte-identical between
those revisions (SHA256 89a4cff33a2ccb2d0f6616ec1914d99fa40093fe270fcff39a7236bf964da9f6).

This candidate is **unpublished and unapproved**. It only enters the new lane
for ordinary move Undo after a real cross-device rename error, or an existing
PrimaryUndo journal. Same-volume rename, copy, update, and in-place modes retain
their existing paths. No schema migration is required; the journal is optional
inside generated_files. Legacy ambiguous rows are not adopted from equal bytes.

Windows EXDEV remains unsupported and fails closed before copy admission and on
every retry. A same-volume Windows rename does not call the admission gate.
Full FILE_ID_INFO identities and no-replace publication helpers have focused
Windows tests, but these do not establish Windows namespace durability or
handle-bound readonly deletion. The Windows tests need native execution; a
cross-compile or Linux success is not a Windows safety result.

The original reviewed snapshot was based on main 8557971. This composed
candidate incorporates approved snapshot 4f88fc44 and current-main/PR291
mode-preservation equivalent 5f876f96, preserving both PrimaryUndo and
failed-publication proofs including IdentityMode. A resumed primary now runs
the publication precheck before any primary source consumption, while retaining
the original later pre/post checks. Contradictory resident-primary and PrimaryUndo
claims are refused, including fresh journal CAS and retention. The DirectoryMove
compensation branch is not included. This exact composition requires independent
review; earlier source review is not composition approval.

## Recovery ownership

The source handle is opened without following a final symlink. Native object
identity is separate from SHA256 content proof; equal bytes never establish
ownership. The attempt token, source, destination, and stage path are committed
before stage creation. A freshly created stage identity is admitted before
streaming. A create-before-admission interruption leaves an unowned object and
requires manual recovery; its filename never grants ownership.

A retry of `copying` rotates to a new exclusive stage generation by fresh-row
CAS. Every superseded admitted generation remains in the journal. No retry
opens an old generation writable. A producer closes its writable handle before
sealing, so a delayed producer cannot modify a subsequently published inode.
A losing seal CAS neither publishes nor regresses another process's Reverted
status. The regression suite includes two real OS processes and real SQLite.

The sealed digest and exact native identity precede no-replace publication.
The publication helper cannot silently restage to another inode. The installed
object is re-proven, its parent namespace synchronized, and publication proof
committed before source cleanup.

Source cleanup commits a fresh, unpredictable terminal claim before taking the
source there. All long SQL and destination verification precede the final source
hash. Native change-time witnesses detect same-size edits with restored mtime,
including destination edits while the last source hash is running. A short
identity/witness recheck precedes unlink. This is a syscall-adjacent ownership
model, not protection from arbitrary privileged mutation at the final syscall.

## Conservative interruption behavior

Any restarted `unlinking` claim requires manual recovery, even if both endpoints
are now absent. A delayed process may still act on its committed claim; another
process must not replace that claim's endpoints. The original journal remains
unchanged. Superseded-stage cleanup uses the same rule. Automatic recovery of
these cases would need additional fenced cleanup-claim history or a separately
reviewed cross-process ownership scheme.

The errors identify the original claim source and terminal paths. Operators
must retain the journal and all named objects, inspect the restored destination
and recorded claims, and preserve uncertain/foreign occupants before any manual
repair. There is no safe general instruction to delete an object merely because
its bytes match. After a confirmed manual repair, history reconciliation needs
an explicit operator workflow; this patch does not invent one.

| Last persisted state / cut | Bounded behavior |
| --- | --- |
| No admitted attempt | Source remains; retry ordinary Undo |
| Planned; no stage created | Retry fresh creation |
| Stage created; its identity not committed | Retain source and stage; manual |
| Copying | Rotate fresh generation; retain superseded ownership |
| Sealed / publication acknowledgement lost | Re-prove exact sealed identity; never adopt equal bytes |
| Published; no cleanup claim committed | Reverify destination and source before a fresh claim |
| Unlinking, before/after take or removal | Retain exact claim and history; manual |
| Primary removed; superseded stages remain | Clean only admitted generations; interrupted claims remain manual |
| Done; final status write lost | Resume from destination proof before the missing-anchor guard |

## Persistence and retention

PrimaryUndo journal writes opt into synchronous FULL (or preserve stronger
EXTRA) on the exact pinned SQLite connection before BEGIN IMMEDIATE. The value
is read back. The previous connection-local setting is restored on every exit,
including cancellation and failed commit; an uncertain restoration discards the
connection instead of returning it to the pool. This lane requires SQLite WAL.
There is no global default change. Each merge re-reads the current ledger and
status and honors the existing job/prune fence. No filesystem streaming occurs
under the SQLite writer lock.

POSIX staging flushes payload/metadata and synchronizes the entire parent chain,
including recreated ancestors. Publication is synchronized before source
consumption. These are requests to the OS and SQLite durability contracts;
FULL and fsync do not prove behavior of arbitrary storage hardware, network
filesystems, or a device that ignores flushes. Tests exercise process interruption
and injected persistence errors, not physical power loss.

Retention takes its fresh operation snapshot inside its write claim transaction.
Entire jobs with nonterminal or malformed PrimaryUndo are excluded, and their
pruning status is restored before commit. The sweep hook independently refuses
such operations. With-hook and without-hook tests verify journal survival and a
successful subsequent Undo retry; a prune-first admission test verifies refusal
before stage creation. Complete well-formed journals can age out normally.

## Required remaining gates

- Independent review of this exact frozen candidate and any later composition.
- Native Windows same-volume/non-admission tests and explicit unsupported-EXDEV
  behavior. Windows cross-volume success must not be claimed.
- Changed-line coverage gaps reported with the candidate; no 100% claim until
  actually measured across the applicable native platform gates.
- Actual machine/power-failure validation is not supplied by synthetic cut tests.
