# Unwired metadata-only providers

This dependency slice for #380 has no application consumer, live network adapter,
movie scraper registration, configuration schema, jobs, or mutation capability.
It does not replace the separately confirmed one-shot Japanese-name API (#454),
expand its consent, or change ordinary scraping.

`SnapshotProvider` pins one immutable evidence snapshot and one source namespace.
Exact source references read a source-owned record. External-ID queries require
an explicit authoritative claim supplied by a reviewed caller; the cache retains
importer assertions and does not authenticate them. Names, matching digits,
R18 source IDs and DMM IDs never imply equivalence. The provider checks *all*
claim owners in the snapshot before source filtering, so disabled/unselected
sources cannot remove ambiguity. Ambiguous lookup copies no competing records.
A new provider must be constructed to adopt a new snapshot generation; old
readers remain valid. Nil snapshot and valid empty snapshot remain distinct.

`Registry` is separate from movie scrapers and accepts explicitly enabled
metadata capabilities. Priorities determine call/diagnostic order only; all
selected enabled providers run, and any ambiguity or multiple distinct source
keys remains ambiguous. There is no winner or merged field patch. Empty priority
lists and `__skip__` anywhere skip the request; disabled providers receive zero
calls. Unknown/duplicate entries fail before provider calls. Omitted providers
are not queried, not proof of global source completeness. Provenance, source
keys and snapshot digests remain available in ordered observations. Mixed artifact
generations fail closed even when their source keys agree. Adapters are trusted
to validate their own evidence; the registry additionally checks exact query
matches, basic provenance presence and result/digest consistency. It freezes the
request and passes independent query pointers to each adapter. Returned evidence,
including aliases and external-ID slices, is copied before validation/storage;
conflicting records for the same source key and digest fail closed. Callers must
not mutate inputs concurrently during entry, and adapters must not mutate returned
evidence concurrently while the registry takes its copy.

All registrations and priorities are bounded to 32. Positive timeout and an
explicit clock are required; zero interval means **no throttle**, false means
**disabled**, and negative intervals fail. These are constructor options, not
JSON/YAML configuration with hidden defaults. Throttling persists across calls,
uses cancellable waits and can be tested with a fake clock. One synchronous call
is admitted per registry; concurrent attempts fail with `ErrBusy`, never queue.
Provider errors, deadlines and cancellation yield no partial resolution. Providers
must cooperate with context cancellation; arbitrary blocking implementations
cannot be forcibly interrupted, and no goroutine is created to conceal that.
The built-in snapshot provider does bounded indexed reads and owns returned data.
Callers must bound registry instances, retained snapshots and concurrent callers.

Results are read-only evidence, not identity verification, local actor selection,
authorized patches, freshness judgments, pruning permission, or image-fetch safety.
No freshness or priority policy can promote ambiguous evidence. Any future
consumer must preserve canonical identity/credit/ownership safeguards and use
existing transactional mutation and explicit authorization boundaries. Network
providers, configuration presence round-trips, safe fetches, full recurring sync
and ordinary scrape enrichment remain separately gated.
