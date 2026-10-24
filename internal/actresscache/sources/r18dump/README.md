# Offline r18.dev COPY evidence adapter

`Convert` accepts a caller-owned **uncompressed** reader and explicit positive
input/output budgets. It has no network, file publication, CLI/runtime or database
mutation wiring. Importing the existing dump parser also imports its SQLite
package dependencies; this adapter never opens a database or invokes an importer.

The accepted subset uses LF-terminated UTF-8 lines, explicit public-schema COPY
column lists and the existing parser's `n`, `t`, `r`, `b`, `f`, `v`, and backslash
escapes. SQL NULL is exactly raw `\N`; escaped `\\N` is literal text. Unknown
escapes, NUL, CRLF, duplicate columns, malformed COPY headers, field-count
mismatches and unfinished blocks fail closed. Quoted/unquoted lowercase table
and column identifiers are accepted, including mixed quoting. Non-COPY lines
are ignored as source text and hashed; **no SQL is executed**. This is a bounded
subset reader, not a general PostgreSQL interpreter.

The reader gates every line before the existing parser receives it. Input bytes
include LF and all ignored/trailing source text; line budgets exclude LF.
`MaxRows` counts every COPY data row, including unrelated tables. The line cap is
at most 1 MiB, below the reused parser's fixed 4 MiB scanner ceiling. Input is
streamed rather than retained wholesale. Parser/scanner buffers and temporary
field arrays remain bounded by the input/line budgets. Accepted fields are also
bounded by the output format's string/record/token budgets.

Exactly one `derived_actress` block with an `id` column is required. Supported
columns are `id`, `name_romaji`, `name_kanji`, `name_kana`, `image_url`; arbitrary
order and subsets are accepted. Unknown actress columns fail for review on
schema drift. Other tables are structurally checked and discarded. An explicit
empty actress block is accepted; a missing block is an error. IDs are opaque,
case-sensitive source strings: no trimming, numeric conversion, name fallback,
DMM equivalence, or row-position fallback. NULL/invalid/duplicate IDs fail.

`name_kanji` maps to JapaneseName. Nonempty `name_romaji` and `name_kana` remain
whole-name alias claims, with exact equal values deduplicated. Neither provides
first/last-name boundaries, so those fields remain empty. Optional NULL names
are absent. Rows without usable metadata fail existing evidence validation.
`image_url` is deliberately omitted: real dumps contain bare filenames, and
this slice does not infer a host/path or import image bytes. No external-ID
claims are manufactured. Later image support requires a separately reviewed
source-specific mapping and safe fetch boundary.

Caller provenance supplies source URL, parser revision and observation time;
these are assertions, not authentication. The caller must redact URL secrets.
Observation times normalize to UTC. DatasetSHA256 must be empty on entry and is
filled with SHA-256 of **all exact accepted input bytes through successful EOF**.
Trailing bytes change the digest; any trailing read error, truncation, malformed
content, exceeded budget or cancellation returns no snapshot and a zero receipt.
The artifact digest remains separate from this upstream-input digest.

Each row is validated before retention. Duplicate keys and cumulative encoded
record bytes, header-count growth and decoded-string admission are checked
before append. The final format builder independently validates the complete
result. Retained evidence is bounded by input bytes, row/record counts and output
budgets; a single transient record serialization is bounded by its own output
budget. No workers, queues or background goroutines are created. Callers must
bound simultaneous conversions and retained snapshots.

The receipt reports supplied-file bytes/rows/hash and whether the actress block
was explicitly empty. Successful structural parsing **does not establish upstream
completeness**, authenticate a source, license pruning, or authorize identity
merges, trusted aliases, candidate promotion, credit changes or overrides. No
partial receipt or replacement is returned on failure. Existing snapshots are
immutable and remain the caller's responsibility.

Context checks occur during reads, row admission and final building. A blocked
reader or parsing CPU cannot be preempted by context; callers own reader lifetime
and deadlines. There is no disk durability claim. Live fetch remains disabled:
the existing HTTP-proxy endpoint guard does not protect destination DNS resolved
by the proxy. Durable artifacts, complete-source manifests, production bundles,
image fetching and application consumers remain later gates.
