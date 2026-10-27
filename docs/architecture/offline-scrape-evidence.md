# Same-source R18 evidence in scrape review

This optional feature presents **R18 source evidence**, not a verified DMM
identity or a canonical-name recommendation. It is offline, read-only, and off
by default. It does not activate the independent metadata-fetch package.

## Two independent opt-ins

The administrator must provision a validated actress-cache artifact locally and
configure the startup-owned reader:

```yaml
metadata:
  offline_scrape_evidence:
    enabled: false
    artifact_directory: "" # administrator-owned, clean absolute directory
    max_age_hours: 168      # finite, 1–8760 hours
```

After enabling the capability and restarting, the user must also check **Show
R18 source evidence** for each Browse, Manual, single-rescrape or bulk-rescrape
operation. The checkbox starts unchecked, is not remembered as consent, and
resets for the next operation. The optional API flag is
`show_offline_evidence: true`; absent or false preserves the default-off behavior.
There is no client-supplied artifact path or identity claim.

This reader is independent of `metadata.actress_database.offline_name_sync`.
Name-sync consent, confirmation tickets, source-search status and public artifact
digests neither enable this reader nor grant source identity. It performs no
actor-name sync, alias registration, translation, image download or extra movie
DB write. Existing scrape persistence still runs normally.

## Supported identity boundary

Only exact opaque `derived_actress.id` strings observed by the production local
R18 dump adapter can match records from the same exported namespace. `7` and
`007` are distinct. The transient references are not JSON fields; decoding raw
scraper JSON, API requests or cached job data cannot manufacture fresh authority.
Stored historical reports remain displayable without re-granting that authority.

The live R18 HTTP API's numeric actress ID has **no verified mapping contract** to
that dump namespace. Such results explicitly report `unsupported_identity`.
Legacy `ActressInfo.DMMID`, equal numbers, actress names, external-ID metadata and
sync consent are never matching authority. Existing legacy DMMID behavior is
unchanged. A normal database-cache scrape likewise does not regain private dump
references.

## Read-only presentation and persistence

The report is an owned, deep-cloned provenance sidecar, never a field on `Movie`.
It contains the exact source reference, outcome, admitted snapshot digest,
observation time, whole Japanese name/aliases when available, and sanitized source
provenance. URLs are plain text, with credentials, query and fragment removed.
The card has no apply, save, register-alias, fetch or image controls.

Canonical names, actress associations/credits, existing UI name precedence,
folder/template output and NFO contents are unchanged. Apply never consumes the
report. Result and report publication is atomic and revision-fenced. Failed,
conflicting or cancelled rescrapes retain the previously committed report; a
successful opt-out clears it as a whole. Reopening a job uses its stored report,
not the latest artifact. Old jobs without reports retain their previous shape.

Initial opted-in reports are published only after the existing movie persistence
succeeds. The atomic rescrape path also preserves existing ordinary provenance
when no new ordinary attribution is supplied. The result-store integrity work
ensures stable running/final result identity and owned snapshots; these are the
small default-off consistency changes required to fence delayed UI responses.
Stores lacking atomic result+provenance capability cannot publish evidence.

## Admission, reload and bounds

Startup settings are pinned until restart. Every requested operation loads one
immutable artifact generation before fanout; it does not reuse the name-sync
reload authority. Publishing generation B does not change an admitted batch A.
A new operation may admit B. There is no added reload endpoint or background queue.

At most four operation snapshots are retained concurrently. An operation allows
at most 128 rich result reports (including discovered multipart files), 64 exact
actor references per report and 64 KiB encoded report data. Over-budget requests,
busy admission, disabled/unavailable artifacts, stale/future observations,
ambiguity, provider failures and unsupported identity produce explicit bounded
diagnostics. They never turn a failed movie scrape into success. Stale/future
observations and failed providers do not disclose candidate names as usable data.

Opted-in server-task admission precedes resource/DB access. Shutdown closes that
admission, cancels and joins all admitted work before dependencies close. The
synchronous API request deadline covers artifact loading and post-scrape stages.
Async batch ownership is server-linked, not tied to the initiating HTTP request.

## Validation boundary

The dedicated offline browser workflow provisions a real synthetic COPY dump,
imports it through SQLite and the production R18 adapter, and separately converts
and publishes an actress artifact. It exercises the actual UI and API for default
absence, explicit opt-in, exact `7`/`007`, read-only desktop/mobile presentation,
historical reopen, failed-rescrape preservation and successful opt-out clearing.
Go, Node and browser network guards prohibit non-loopback runtime traffic.
Inventory alone is not execution: exact passed case names, zero skips/retries,
request and source attachments, screenshots, clean head/tree and artifact hashes
are verified before the hosted proof is accepted. Native Windows runtime tests
are separate from Linux/browser proof.
