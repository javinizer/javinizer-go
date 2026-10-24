# Actress evidence cache v1 contract

This package is an unwired, offline evidence boundary. It is not a canonical
actor catalog, identity resolver, merge authority or credit mutation layer. No
database, API, scraper, UI, runtime lifecycle or network client imports it in
this slice.

## First slice

Provide an offline builder, bounded format reader, immutable indexed snapshot
and single-writer in-memory publication. A fixture artifact exercises the
format. No live source adapter, image download, embedded production dataset,
filesystem publication or application consumer is enabled.

### Keys and evidence

A source namespace matches `[a-z][a-z0-9_-]{0,31}`. Source IDs are nonempty
opaque UTF-8 strings, at most 256 bytes, without control characters or
surrounding space. The stable key is
`v1:<source>:<unpadded-base64url(source_id)>`. It does not include names,
external-ID claims, source URLs, thumbnail URLs/bytes, timestamps, dataset
positions or mutable metadata. Missing stable source IDs are rejected;
importers must not substitute names, row numbers or content hashes for
identity.

Wire records require `source`, `source_id`, `provenance` and `metadata`;
`external_ids` is optional. Provenance requires `source_url`, `dataset_sha256`,
`parser_revision` and `observed_at`. Metadata may contain `first_name`,
`last_name`, `japanese_name`, `aliases` and `thumbnail_url`. Each external-ID
object contains `namespace` and `value`. Nulls and fields in the wrong object
are rejected.

Each record retains its source ID and provenance: HTTP(S) source URL without
user-info or fragments, lowercase 64-hex upstream dataset SHA-256, parser
revision and nonzero observed time. Query values are opaque; source adapters
must remove secrets before building. Times encode in UTC RFC3339Nano.
Provenance is an assertion supplied by the importer, not proof of source
authenticity. The snapshot's SHA-256 describes its own deterministic artifact
bytes and is distinct from each upstream dataset hash.

Names, aliases, thumbnail URL and external IDs are source claims. Namespaces
use the same canonical syntax as sources; values use the same opaque ID
validation as source IDs. Namespace/value pairs are exact, case-sensitive and
unnormalized. Aliases and external-ID claims are unordered sets: deterministic
encoding sorts them; duplicate members fail. Records sort by stable key.
Duplicate source keys fail even for identical rows. Records from different
sources are never collapsed or field-enriched together. Each record must
contain at least one metadata or external-ID claim.

External-ID lookup returns all matching records and marks ambiguity when
multiple distinct source keys claim that ID. A single matching evidence record
is still not a verified local identity. There is no name fallback or canonical
actor-ID field. Later consumers must retain current
verified/quarantine/alias/credit/override rules and use canonical transactional
mutation paths. Cache refresh cannot merge actors, promote candidates, rewrite
credits or defeat manual suppression.

### Deterministic encoding and bounds

The v1 deterministic JSON encoding is NDJSON, not a formal canonical-JSON
standard. Its first line has `format: "javinizer.actress-evidence"`,
`schema_version: 1` and a nonnegative `record_count` no greater than
MaxRecords. The builder emits one compact record per following line. The reader
accepts field ordering and JSON whitespace within each line, then normalizes
output to this deterministic encoding. Digests refer to the normalized artifact
bytes. String quoting uses Go JSON HTML escaping; field order is fixed, sets
and records are sorted as above, and timestamps use UTC RFC3339Nano. Every line
ends in LF; no blank lines, extra records or trailing bytes are accepted. The
declared count must match. A zero-record artifact is an explicit empty
snapshot, not proof that an upstream source is complete and not authority to
prune application state. The initial format is uncompressed; gzip/ZIP is
unsupported rather than implicitly decompressed. Legacy schema-v1 aggregate
caches are not imported.

Every operation requires positive caller budgets for total encoded bytes, bytes
per record/header line, record count, each decoded string, total decoded string
bytes, aliases, external-ID claims and JSON tokens per line. JSON nesting depth
is capped at 8, counting the root as depth 0. Total string accounting includes
decoded values, not the fixed schema field names. MaxBytes includes LF
delimiters; MaxRecordBytes excludes each LF. Per-line/resource validation
occurs before typed record construction; record-count limits precede allocation
of the snapshot collection. Reject invalid UTF-8, unpaired Unicode escapes,
duplicate/unknown fields, missing required fields, unsupported format/version,
truncation and any limit overrun. Object keys must match the defined lowercase
ASCII field names exactly. No silent replacement or case-insensitive field
aliasing changes a source ID.

Context is checked during reads and record processing. A blocking Reader.Read
and JSON codec CPU work are not preemptible; input providers must provide their
own read deadlines/cancellation. Reader ownership remains with the caller; the
library does not close it. Library calls create no goroutines or worker queues.
Callers must bound concurrent standalone builds and retained historical
snapshots.

### Immutable runtime publication

Callers must not mutate builder inputs concurrently with Build. After
construction, snapshots own their input and indexes. Returned records,
collections and bytes are copies. Lookup indexes retain every owner; previously
acquired snapshots stay valid after replacement. An empty store has no current
snapshot. Replacement is synchronous and admits one writer; concurrent writers
receive ErrBusy before reading their input, providing bounded admission without
a background queue.

The publication linearization point is one atomic pointer store after complete
validation/index construction and an immediate cancellation check. Failure or
cancellation before that point preserves the last good snapshot. After
publishing, return success; a later cancellation cannot retroactively undo
publication. The final check-to-store cancellation race cannot be eliminated by
this contract.

## Following slices and explicit gates

- Offline source builders must stream bounded inputs, retain
  source/dataset/parser provenance and reject unkeyed/conflicting evidence.
  Source failures or incomplete refreshes cannot imply deletion/pruning.
  Refresh policy and complete-source manifests must be explicit before any
  durable publication or pruning feature.

- Durable publication must use immutable content-addressed artifacts and a
  checked manifest/current pointer binding the exact stored artifact bytes and
  digest, sync complete bytes before visibility, and preserve the previous
  artifact on every failure. Use platform-verified atomic replacement; never
  remove the old destination and then rename. Test interruption and recovery on
  supported platforms before claiming crash safety. This first slice makes no
  filesystem-durability claim.

- A bundled production artifact must be generated reproducibly from reviewed
  sources with manifest/digest/size validation. A future compressed container
  needs separate compressed and expanded byte budgets. No such artifact is
  bundled here.

- Network builders require an independently tested target-safe fetch adapter.
  The current SSRF transport wrapper protects its dial endpoint, not
  destination DNS performed by an HTTP proxy. Do not activate a consumer using
  endpoint-only safety as a target guarantee, bypass configured proxies, or
  allow private hosts. Remote-DNS HTTP-proxy fetching remains disabled until
  that policy is resolved.

- Image inputs need encoded/read deadlines, imagevalidate's decoded
  dimension/pixel limits and explicit formats, plus bounded concurrent
  jobs/queue/memory budgets. Metadata URLs in this cache are untrusted
  references, not safe fetch permission or validation receipts. This slice
  fetches no images and asserts no image trust.
