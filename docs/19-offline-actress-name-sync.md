# Confirmed offline actress-name API

This backend supports a one-time, explicitly reviewed Japanese-name update from
an existing offline R18 artifact. It is disabled by default. It does not download
upstream data, infer identity from names or equal numeric IDs, start from a scrape,
refresh artifacts automatically, or provide recurring update consent. The review
UI is not included in this backend slice.

## Provision and enable

Provision an artifact with the standalone
[`javinizer actress provision-cache` command](../cmd/javinizer/commands/actress/README.md).
It converts a supplied uncompressed COPY dump without initializing the application
database. Source URL and observation time are caller assertions; a parsed file and
SHA-256 digest prove neither source authenticity nor complete coverage. Keep the
actual observation time, not the time you happened to publish the artifact.

Then configure:

```yaml
metadata:
  actress_database:
    enabled: true
    offline_name_sync:
      enabled: true
      artifact_directory: /absolute/path/to/private-evidence
      max_age_hours: 168
      max_queued_jobs: 16
      max_queued_tasks: 2000
```

The directory must already exist and satisfy the artifact store's safety checks.
Settings and the validated startup snapshot are pinned until restart. Saving
configuration or publishing a new artifact does not replace a running service's
policy or snapshot. `max_age_hours` must be 1–8760, `max_queued_jobs` 1–32, and
`max_queued_tasks` 1–8192. An enabled service also requires the actress database.

Do not delete immutable artifact objects referenced by outstanding jobs. Jobs
always use their admitted digest, never whichever artifact is newest. A missing
or corrupt historical object leaves the job blocked and cancellable, with bounded
retry diagnostics. Windows supports existing validated artifact reads but not
artifact publication. See the [service contract](../internal/actressnamesync/README.md)
for worker, protection, restart and retention details.

## Authentication and browser write safety

All routes below are under `/api/v1/actress-name-sync` and require the existing
session or API-token authentication. Use an `Authorization: Bearer ...` header,
the existing `javinizer_session` cookie, or the desktop `X-Session-ID` header.
URL-only `?session=...` authority is not accepted by this domain.

Every POST additionally requires both:

```http
Content-Type: application/json
X-Requested-With: javinizer-offline-name-sync
```

The intent header does not replace authentication or the user's confirmation.
The three write routes use the server's existing write-rate-limited group.

Browser Origins must match the actual request's scheme, host and effective port,
or an exact entry in `api.security.allowed_origins`. Wildcards, `null`, malformed
Origins and multiple Origin headers are rejected. Forwarded-host/protocol headers
do not create trust here: configure the public HTTPS origin explicitly behind a
TLS-terminating proxy. The normal CORS preflight already allows `X-Requested-With`.
Non-browser clients may omit Origin but must still send the intent header; a
cross-site fetch-metadata signal without an allowed Origin is rejected.

Authenticated domain responses use `Cache-Control: private, no-store`, including
review references. The existing cookie scheme and global auth policy are unchanged.

## Endpoints

All successful responses are direct JSON objects, not a `data` wrapper.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/status` | 200 source availability and startup policy |
| GET | `/sources?q=...&offset=0&limit=50` | 200 bounded offline source discovery |
| POST | `/reviews` | 201 server-held exact batch review |
| POST | `/jobs` | 202 confirmed job or original idempotent receipt |
| GET | `/jobs?cursor=...&limit=20` | 200 newest-first durable history |
| GET | `/jobs/:id` | 200 job status and outcome counts |
| GET | `/jobs/:id/tasks?offset=0&limit=50` | 200 bounded task receipts |
| POST | `/jobs/:id/cancel` | 200 cancellation state and preserved counts |

Only the documented query keys are accepted. Repeated or malformed query values,
noncanonical integers and invalid cursors are rejected. Job IDs have the form
`name-` followed by 64 lowercase hexadecimal characters; always copy returned IDs.

### 1. Inspect the pinned source

```sh
curl -b cookies.txt http://localhost:8765/api/v1/actress-name-sync/status
```

Status includes `enabled`, `available`, `record_count`, `recovered`,
`max_age_seconds`, `restart_required_for_config_changes`, and an optional
`artifact_digest` or safe `code` such as `starting`, `disabled` or
`snapshot_unavailable`. An optional `worker_problem_code: "queue_unavailable"`
reports failed queue discovery and clears after the next successful read. Neither
field discloses local paths, SQL, or raw errors.

```sh
curl -b cookies.txt --get \
  --data-urlencode 'q=名前' \
  --data-urlencode 'limit=50' \
  http://localhost:8765/api/v1/actress-name-sync/sources
```

Source pages contain `items`, `next_offset` and `artifact_digest`. Each item has
`source_key`, `source_id`, `japanese_name`, `aliases`, `source_url` and
`observed_at`. Treat these names and URLs as untrusted display metadata, not HTML,
proof of identity, or permission to follow a URL. Query length is at most 128 UTF-8
bytes; page limit defaults to 50 and is at most 100. Reuse `next_offset` with the
same query until it is `null`; it is a scan position, not a count of matched rows.

Name search is discovery only. Independently verify the chosen source record's
association with the intended local actress before requesting review. Neither
similar names nor equal R18/DMM numbers establishes that association.

### 2. Request and inspect a complete review

```sh
curl -b cookies.txt \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: javinizer-offline-name-sync' \
  --data '{"choices":[{"actress_id":7,"source_key":"v1:r18dev:NDI"}]}' \
  http://localhost:8765/api/v1/actress-name-sync/reviews
```

The IDs above are illustrative; use an actual source key returned by your server.
A review request contains only `choices`, each with `actress_id` and `source_key`.
There must be 1–500 choices, nonzero local IDs, source keys of at most 512 bytes,
and no repeated actor IDs or source keys. No empty selection means "all".

The response contains:

- `review_id`: random 64-character lowercase hexadecimal confirmation reference.
- `expires_at`: five-minute expiry for a review not yet confirmed.
- `artifact_digest` and `max_age_seconds`: display-only pinned evidence policy.
- `proposals`: every selected `actress_id`, its `source` discovery metadata, and
  the exact `before_name` and `after_name` proposed by the server.

Show and inspect the entire batch before confirming. A successful review is not
permission to submit automatically. The repository validates import-managed,
verified, nonquarantined actors and protects manual edits and explicit clears.
Names, fingerprints, freshness and ownership are calculated server-side.

### 3. Confirm exactly that review

```sh
curl -b cookies.txt \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: javinizer-offline-name-sync' \
  --data '{"review_id":"COPY_THE_RETURNED_REVIEW_ID","confirmed":true}' \
  http://localhost:8765/api/v1/actress-name-sync/jobs
```

The request must contain only `review_id` and literal JSON `confirmed: true`.
A missing, false, null, numeric or string confirmation is rejected. Do not send
names, timestamps, artifact digests, actor tokens, ownership flags or request keys
back as authority. Bodies above 512 KiB, extra JSON documents, unknown fields,
duplicate keys and nonexact field spellings (including Unicode case aliases) are
rejected before service admission.

A review belongs to its verified API-token ID or session username. A different
token or session principal cannot confirm it. Logout/login under the same username
preserves that session principal; switching from token to session does not.
Existing single-user administrative visibility applies to job history and cancel;
these endpoints do not introduce per-user job ACLs.

Retry the same review ID and principal after an uncertain network result. Once
committed, confirmation returns the original job, even after restart, ticket
expiry, a full queue or disabled new admission. An unconfirmed review is lost on
restart and cannot authorize a new job. Do not create a second review just because
a confirmation response was lost.

### 4. Observe or cancel

A job response contains `id`, `snapshot_digest`, `created_at`, `cancelled`,
`status` and `counts` by task outcome. It does not mean every task has already
applied. Job statuses include `pending`, `running`, `blocked`, `completed` and
`cancelled`. `completed` can include skipped or invalidated tasks; inspect counts
and task receipts rather than treating it as all-success.

Task pages contain `items` and `next_offset` (null at the end), with a default
limit of 50 and maximum of 100. Items include local/source identities,
`before_name`, `after_name`, `status`, optional `reason`, `finished_at`, and active
operational diagnostics `problem_code`, `retry_after_ms`, `problem_attempts`.
Internal actor fingerprints, request keys and lease owner/generation are omitted.

History pages contain `items` and optional `next_cursor`. The default limit is 20,
maximum 32. Use the returned cursor for the next newest-first page. Source/task
pages and history are bounded independently of total retained job history.

```sh
curl -b cookies.txt \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: javinizer-offline-name-sync' \
  --data '{"confirmed":true}' \
  http://localhost:8765/api/v1/actress-name-sync/jobs/COPY_JOB_ID/cancel
```

Cancel prevents remaining uncommitted work; it does not undo applied changes.
The returned counts and task receipts preserve partial application. Status,
history, receipts and cancellation remain available when new admission is disabled.
Manual changes and transaction-time identity/protection checks can invalidate a
previously reviewed proposal instead of applying it.

## Errors and lifecycle

Errors have stable `code` and safe `error` fields. Raw database, artifact path,
source-secret or lease diagnostics are not reflected in response error text.

| HTTP status | Meaning |
| --- | --- |
| 400 | Invalid body, selection, query, confirmation or ID |
| 401 | Missing/invalid authentication or URL-only session |
| 403 | Missing intent header, rejected Origin or wrong review principal |
| 404 | Durable job, cursor or selected record not found |
| 409 | Protected actor, stale review/evidence or conflicting state |
| 410 | Unconfirmed review is missing or expired; request a new review |
| 413 | Request body exceeds 512 KiB |
| 415 | Content-Type is not application/json |
| 429 | Write-rate limit or review/queue capacity; capacity errors include Retry-After |
| 500 | Unclassified internal failure; raw detail withheld |
| 503 | Disabled/unavailable service, cancelled request or shutdown |
| 504 | Request deadline exceeded |

Service operations are cancelled by client disconnect or server shutdown and are
joined before the database closes. Parsing an unfinished request body and writing
a response to a stalled reader do not hold the database shutdown join. A transport
failure or shutdown is not proof that confirmation or an apply did not commit:
use idempotent confirmation retries and durable receipts to establish the outcome.
