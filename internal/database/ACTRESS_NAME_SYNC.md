# Confirmed offline Japanese-name batches

Migrations 26–27 and the `ActressRepository` methods provide a tested durable
storage boundary, **not recurring automatic sync**. The repository does not fetch
evidence, schedule work, authenticate users, or activate a worker. Consumers must
opt in explicitly and obtain authenticated human confirmation. All examples/tests
use disposable synthetic databases and evidence.

## Explicit source bridge and review

`r18dump.Convert` produces immutable evidence with source namespace `r18dev`; its
source IDs are not DMM IDs. An existing verified, non-quarantined, import-managed
actor can now be explicitly associated with an exact evidence `SourceKey` using
`ReviewOfflineNameSync`. No name, alias, position, URL or numeric coincidence
chooses an actor. Evidence supplies a nonempty Japanese name, not an inferred
first/last-name split or trusted alias.

The opt-in offline API/service now implements bounded selection of actors and
source records, a batch review showing both identities, source provenance and exact
before/after names, followed by one explicit confirmation of the selected batch.
See `internal/actressnamesync/README.md` and `docs/19-offline-actress-name-sync.md`.
A user-friendly UI remains a separate gate; it must not require editing per-record
configuration files. The repository review payload
includes the immutable snapshot digest, an actor-state precondition, and freshness
limit. `SubmitConfirmedOfflineNameSync` revalidates the entire batch inside its
write transaction. Calling it asserts that the authenticated API obtained
human confirmation; this repository method cannot itself authenticate a person.
Do not connect it to ordinary scraping/import or treat a preview as confirmation.
The review fingerprint is a state precondition, not a secret or signature.

Callers must not concurrently mutate selection or review slices during a call.
Each batch has 1–500 unique actors and source keys. Requests and worker IDs are
bounded opaque strings; names, source keys and digests are bounded before hashing.
A source key has one durable actor owner, even after revocation or deletion. A
new confirmation may replace only that same actor/source binding, with a larger
revision. Another actor, including a merge target, cannot silently inherit it.
Explicit source reassignment/recovery is intentionally not implemented.

## One-shot ownership and canonical mutation

Binding is not field ownership. Eligibility additionally requires existing
managed import-name provenance and no sync-name protection tombstone. User-owned,
legacy/unmanaged, revoked, unverified and quarantined actors are rejected. There
is no override/unprotect method in this slice. Only JapaneseName can change.

Canonical repository manual-edit methods revoke pending consent even for
same-value intent and explicit clears. `ProtectOfflineSyncName` expresses an
explicit same-value protection without altering an actor's import contract.
Triggers retire consent on actual identity/metadata changes, conflicting imports,
quarantine changes, deletion, primary-key moves and SQLite replacement. Ignored
inserts/updates and harmless association/no-op import writes do not revoke it.
Protection tombstones survive deletion and cannot be cleared by generic import.
Merge explicitly protects both source and target, even if target fields match.

Apply uses existing canonical alias transition/collision/quarantine, translation,
identity-snapshot credit and movie-render invalidation helpers in one transaction.
Reported credits, suppression, user overrides, thumbnail, first/last names and
inline alias metadata are not overwritten; evidence aliases never grant trusted
ownership. A failed collision/cancellation rolls back all changes and receipt.
The transaction restores only its own previously validated managed-name state;
it never promotes candidates or enables legacy/user-owned imports. Its binding
is consumed even when the name is already equal or evidence has become stale.
New evidence always requires another explicit reviewed confirmation. The actor
remains import-managed: a later permitted import can change its name and will
retire/protect any outstanding sync authority. This does not establish persistent
sync ownership over future imports. It is a user-confirmed batch update, not
permission for recurring sync.

## Durable lifecycle and recovery contract

- Submission is all-or-nothing. The same request key and exact payload returns the
  original job/receipts, including after completion or evidence expiration; reuse
  for a different payload conflicts. One active task per actor is enforced.
- Claim uses server-owned time and worker identity, a 1ms–24h lease and strictly
  increasing generation. Expired leases can be reclaimed. Apply checks the full
  task/owner/generation/expiry fence and job cancellation under write admission.
- Each apply commits canonical changes, consumed binding and receipt atomically.
  `applying` is never independently committed. A crash rolls back the transaction;
  a lost response is resolved by reading the job, not applying the receipt again.
- Cancellation prevents pending/leased application but does not undo a completed
  change. Concurrent edit/cancel/merge and apply serialize: whichever commits
  first determines the outcome. Old workers and replaced confirmations are fenced.
  Existing manual-edit methods may return SQLite BUSY instead of auto-retrying;
  callers must surface/retry that failed intent, never claim it was committed.
  Deterministic two-connection tests observe real lock contention in both orders,
  including successful explicit manual retry after a committed sync update.
- Wrong/missing snapshots return an error without consuming the lease. The caller
  must pin or reload exactly the digest recorded in the task, not the latest cache.
  Expired/future source observations become a terminal skipped receipt. A definite
  identity/binding mismatch becomes terminal invalidation, not infinite retry.
- Freshness is measured against source observation time using server-supplied
  `Now` and positive `MaxAge`; the exact age boundary is accepted. Lease expiry
  has millisecond precision. Recent timestamps do not establish source trust.
- Read receipts with `OfflineNameSyncJob`. Historical IDs, names, binding revisions,
  evidence digest, request hash and lease generation survive actor deletion.
  Migration rollback revokes outstanding work and removes dependent triggers but
  preserves audit tables/tombstones. Re-upgrade does not reactivate old consent.

## Bounded activation support

`SubmitConfirmedOfflineNameSyncWithLimits` serializes global admission with the
submission transaction, including across independent database connections. Exact
request-key retries are resolved first and remain available when the queue is
full or their evidence is unavailable. Defaults are 16 outstanding jobs and 2,000
outstanding tasks; callers may configure positive limits up to hard caps of 32
and 8,192. Pending/leased/applying tasks count; terminal receipts do not. A
replacement is conservatively counted as new work until submission commits.

`DueOfflineNameSyncJobs` returns at most 16 job metadata rows in ascending ID
order. The server carries the last ID between passes and wraps after an empty
page. Claimability and persisted retry times are filtered before the page limit,
so live leases and blocked earlier jobs do not hide later due work. A serial
worker can claim/apply one task per discovered job before its next poll.
`ListOfflineNameSyncJobs` provides newest-first metadata history (default 20,
maximum 32) using a stable created-time/ID keyset. Receipt collections remain
bounded by the 500-task batch limit. `OfflineNameSyncJobByRequestKey` resolves a
server-generated confirmation replay after restart; authentication/principal
binding remains the service's responsibility.

Migration 27 adds per-task operational diagnostics separate from immutable
receipts. Only `snapshot_missing`, `snapshot_invalid`, and `apply_failed` are
stored, never raw filesystem, database, or provider error text. Backoff starts at
five seconds, doubles, and caps at five minutes; attempt counters cap at 16.
`RecordOfflineNameSyncJobProblem` touches only currently due/claimable tasks under
write admission, returning due expired leases to pending while retaining their
generation and clearing the old owner/expiry. `RecordOfflineNameSyncLeaseProblem`
checks the full current
unexpired lease fence, then returns that task to pending without reducing its
generation. Old tokens remain unusable; the next claim must increment generation.
Neither method grants new authority or substitutes another evidence digest.

Task reads expose diagnostics only while pending/leased. Cancellation and other
terminal outcomes always take precedence over stale diagnostic history. Rolling
migration 27 back retires outstanding grants before older consumers can ignore
backoff, while preserving diagnostic/receipt history and protection tombstones.

## Remaining product gates

The disabled-by-default API/service supplies authenticated batch review and
confirmation, server clocks, bounded admission/workers, exact-digest recovery,
and observable receipts. A usable batch UI remains required before claiming a
complete user workflow. Database history currently has no pruning policy;
retention must preserve ownership/tombstone safety and active snapshot references.
No API should accept client clocks or automatically treat a cached review as human
consent. Real providers/authenticity, recurring field-level ownership policy,
source priorities, image enrichment and end-to-end UI acceptance remain separate.
HTTP-proxy origin DNS safety and Windows artifact publication remain fail-closed
under their existing gates; this offline workflow does not relax either.
