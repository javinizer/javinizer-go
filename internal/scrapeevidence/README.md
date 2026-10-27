# Offline R18 source evidence in scrape review

This is a read-only presentation capability, independent of `offline_name_sync`
and canonical identity. It has no HTTP client, database repository, image fetch,
alias registration, background worker, queue, or apply capability.

An administrator enables a startup-owned artifact directory and freshness
policy. Every operation separately opts in; absence is off and does no artifact
I/O. Admission reads one validated immutable generation before fanout. A later
operation may admit B while an active operation retains A. Stored historical
reports are values, never recomputed against the latest artifact on reopening.
Validated previous-manifest recovery is explicit in the report.

Only exact server-observed `derived_actress.id` values from the local R18 dump
adapter are supported references. `7` and `007` remain distinct opaque strings.
The existing numeric DMMID projection remains unchanged but carries no lookup
authority. Live HTTP `actress.id` has no reviewed equivalence contract with the
dump namespace and yields `unsupported_identity`, not guessed matching or false
unmatched evidence. Names, source priority, JSON claims, cache-deserialized raw
results, and name-sync consent cannot establish this source authority.

At most four operations retain snapshots. The declared whole batch is capped at
128 reports, including multipart files. Oversize declarations receive a whole-
operation diagnostic before loading. Each report is capped at 64 references and
64 KiB encoded JSON, hence at most 8 MiB of generated rich reports per operation
before existing result-storage clones. Each raw result retains only 64 keys and
one overflow sentinel. The immutable snapshot provider supports bounded parallel
scrape fanout without a contention-sensitive registry TryLock. Release joins
active reads, clears the provider, and returns its admission slot; failed loads,
cancellation, and panics cannot leak an acquired slot.

Reports expose source-qualified references, outcomes, whole Japanese-name claims
and aliases, admission/observation times, safe provenance URLs, parser/dataset
provenance, and exact snapshot digest. They contain no split names, thumbnails,
external-ID mappings, canonical actress IDs, or applicable field patches. URLs
remove credentials, query strings, and fragments. Freshness is evaluated against
the admission time: stale or future records retain diagnostics and provenance
but do not expose name claims. Provider errors, cancellation, and budget failures
discard all partial observations. Mixed outcomes conservatively retain failure,
ambiguity, future, stale, or unsupported-identity diagnostics.

The caller attaches an owned report beside provenance only after a successful
movie scrape. The report must never enter Movie, credits, NFO/template inputs,
canonical persistence, or apply authority. A failed or stale rescrape cannot
replace the previously committed result/report pair.
