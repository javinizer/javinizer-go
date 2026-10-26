# Confirmed offline Japanese-name updates

This service is a disabled-by-default, **one-shot user-confirmed batch update**,
not recurring automatic sync. It does not fetch upstream data, scrape, infer actor
identity from names, equate DMM and R18 numeric IDs, or refresh snapshots silently.
The API and catalog dialog browse bounded source pages, select exact verified
local actors and source records, review the entire proposed batch, then explicitly
confirm it. A source-name search is discovery, never identity authorization.

## Offline source provisioning

Use `javinizer actress provision-cache` with an explicitly chosen existing private
artifact directory and an uncompressed R18 COPY dump. See the command package
README for required provenance arguments and publication outcome handling. The
caller asserts where/when the supplied dump was observed; parsing and SHA-256
integrity do **not** authenticate those assertions. Never stamp old evidence with
the current time to make it fresh. Provisioning does not read or write the actor DB.
Windows supports validated reads only; artifact publication remains refused there.

Enable `metadata.actress_database.offline_name_sync` and set its absolute clean
`artifact_directory` only after provisioning. Actor database support must also be
enabled. Settings remain pinned until restart. The validated current/previous recovery
snapshot is loaded at startup. After provisioning, explicitly POST
`/actress-name-sync/sources/reload` (empty body or `{}`) to adopt the validated
current artifact without restarting. Reload takes no path, URL, clock, data or
digest input. It is authenticated, rate-limited and CSRF-protected. A failed,
cancelled or previous-manifest recovery reload preserves the usable generation
and exposes a bounded diagnostic rather than silently rolling back. An unavailable or invalid snapshot gives an explicit unavailable status,
not a live fetch or an empty successful update.

The existing private-directory, root-relative, regular-file and finite artifact
budgets apply. Do not remove objects referenced by outstanding jobs. The worker
may read an exact historical immutable digest **only from a durable admitted job**;
it never substitutes the newest artifact. A missing/corrupt object leaves work
blocked with persistent bounded backoff and cancellable diagnostics.

## Confirmation authority

Review accepts explicit actor IDs and source keys, plus an optional displayed
`artifact_digest` equality precondition. Source pagination supports the same
precondition. This digest never authorizes historical loading. Exact selected
`r18dev` source references pass through the immutable metadata snapshot provider;
no external-ID claim or name matching is enabled. The repository generates
and validates names, actor fingerprints, freshness, source ownership and manual
protection. Up to 500 choices, eight unconfirmed tickets and four concurrent review
reads are admitted per service. Tickets expire after five minutes. Source and task
pages are bounded to 100 items; durable queue limits also apply across processes.

Confirmation accepts only a random review ID and `confirmed: true`. It cannot
accept client-authored names, timestamps, digests, field ownership or actor tokens.
The ticket is bound to the existing authenticated principal: API-token ID or the
existing single-user session username. Jobs/status/cancellation use the existing
single-user administrative visibility model, not new per-user ACLs. Session
logout/re-login does not change that username principal. Token IDs are distinct.

A principal-bound hash of the random review ID is the durable idempotency key.
Successful confirmation consumes the ephemeral ticket; retry returns the original
job, including after a server restart or when new admission is disabled. A nonce
without a committed job cannot authorize a submission after restart. A different
principal cannot use it. Expiry is checked before submission; an admitted request
may finish its transaction after that check. A transport failure is not evidence
that submission failed: retry the same review ID, never manufacture a new grant.

## Worker and lifecycle

One lifecycle-managed worker initializes the source and processes one task per due
job in bounded keyset passes. It claims two-minute leases and uses the repository's
transaction fences for apply/cancel/manual-change races. Expired leases recover on
restart; persistent diagnostics delay retries from five seconds up to five minutes.
Snapshot, search index, metadata provider, startup-pinned options and source status form one immutable
generation. Reads never mix generations. Eight concurrent source reads, four
review reads, one reload candidate, one confirmation and one worker/historical
read bound generation retention; no generations are retained in a history map.
Reload adds no worker. Same-digest reload preserves pending reviews. A changed
generation invalidates all unconfirmed reviews, including reviews still being
computed. Publication and final confirmation admission serialize: an admitted
job remains pinned to its old digest; a reload published first requires a new
review and explicit confirmation. Durable receipt retries still return the
original job after reload. Existing bindings never grant recurring consent.

Shutdown cancels requests/worker and joins them before database closure. An apply
already committed remains applied. Cancellation never rolls back applied names;
it prevents remaining uncommitted work and preserves receipts. Unknown operational
errors are not reflected as raw paths/SQL in HTTP responses. The API never exposes
lease owner/generation or actor fingerprints as mutation authority.

Canonical mutation continues to require import-managed verified, nonquarantined
actors and exact unchanged confirmation state. Manual edits, explicit clears,
revocation, merge/delete and conflicting imports invalidate consent transactionally.
Aliases, credit overrides/suppression, and identity safety remain repository-owned.
UI review, live providers, recurring sync, and full issue #380 completion remain out
of scope for this backend slice.
