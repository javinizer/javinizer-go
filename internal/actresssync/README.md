# Selected-actor offline metadata preview

This is the first read-only connection between the offline evidence cache and
selected current catalog identities. `ActressRepository.PreviewOfflineSync`
reads actual actor rows and import-name provenance together and calls `Preview`
with one pinned immutable snapshot. It returns one outcome per requested local
ID in request order, including deleted/missing IDs. It does not expose an API,
start jobs, fetch images, modify canonical names, grant aliases, promote
candidates, or touch credits, suppression, translations or render state.

## Identity and ownership boundary

Only a positive DMM ID already present on a verified, non-quarantined,
import-managed local actor can match an exact `dmm` external-ID claim in evidence.
The database's positive-DMM uniqueness invariant is retained. Neither source IDs
nor names/aliases/URLs create a relationship. `7` and `07` remain distinct claims.
Multiple source keys claiming the same DMM ID are ambiguous even if metadata is
identical or one observation is stale. No priority or freshness filtering can
remove that ambiguity. `LookupUnique` detects it before copying candidate data,
so a repeated selected ID lookup cannot materialize every competing record.

User-owned actors, revoked import management, legacy imports without explicit
management and unknown origins are protected wholesale. Existing provenance
only covers import names, not sync-specific ownership of every field. Therefore
an `evidence_available` item is **source evidence, not an authorized field patch**.
In particular, it does not license overwriting a thumbnail, first/last-name
boundary, alias metadata, trusted alias mapping, manual override or suppressed
credit. Field-level sync ownership and write-time checks are a later requirement.

The returned target ID, DMM ID, flags and UpdatedAt describe the read. They are
not a lease/fencing token. A future writer must re-read identity, quarantine,
manual ownership and per-field protections inside the same transaction as lease
validation and canonical mutation. It must not call the user-edit
`UpdateCanonicalFields` method as an automatic sync path or pass stale preview
metadata into generic ImportUpsert. Merge/delete/cancel remain later durable-job
integration work. This slice adds no migration; current baseline ends at 25.

## Where authoritative references will come from

Existing catalog rows with positive DMM IDs already supply the local side of the
bridge. The upstream side must be an explicit DMM-ID claim emitted by a reviewed
source-specific provider from an authoritative identifier field. The tests use
synthetic explicit claims to exercise that complete read path. They do not ship
a production dataset or establish a provider's authenticity.

The current `r18dump.Convert` adapter intentionally emits only `r18dump` source
IDs, **not DMM claims**. Thus its artifacts alone cannot currently enrich these
actors, even when an ID's digits and the actor's names happen to match. The next
production integration must either implement a fixture-tested provider that
actually supplies authoritative DMM identifiers, or add durable, explicitly
verified actor/source bindings with ownership, conflict and deletion semantics.
Do not manufacture DMM equivalence from r18 IDs, source positions, or name lookup.
Actors without a positive DMM ID remain `missing_identity` until such explicit
bindings exist. There is no universal name-based fallback.

## Bounded, deterministic, offline behavior

Selection is explicit, nonempty, unique positive local IDs. A positive caller
limit is capped at 500 before SQL parameters/results are allocated; empty never
means all. Database errors, invalid input and cancellation return no partial plan.
The repository query is one joined read inside one transaction; missing
provenance fails closed to protection, and a missing provenance table is an error.
No query repairs pending quarantine state. Callers must not concurrently mutate
input slices during entry and must bound concurrent requests themselves.

The caller supplies nonzero `Now` and positive `MaxAge`. Evidence at exactly the
age limit is fresh; anything older is stale and future observations are refused.
Freshness is based on the source's asserted observation time, not artifact
publication time. No trust is added by a recent timestamp. The entire selection
uses one immutable snapshot pointer and digest, unaffected by refresh. A nil
snapshot differs from a valid empty snapshot (`no_snapshot` versus `unmatched`).
Returned evidence is an owned copy, including aliases and external IDs.

The package has no movie-scraper capability and creates no workers or queues.
Provider configuration, source priorities, `__skip__`, throttling and live
providers are deliberately not activated or claimed complete. HTTP-proxy target
DNS safety and Windows artifact writes remain gated exactly as before. The
cache's existing validation/memory budgets bound each retained evidence record;
selection limits additionally bound copied results. Ordinary scraping remains
unchanged. A full user workflow still needs safe providers/bindings, lease-fenced
canonical storage, complete API/runtime recovery/cancel, and UI activation.
