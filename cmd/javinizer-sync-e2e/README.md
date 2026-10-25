# Offline name sync browser fixture

This test-only command wires production authentication, routes, runtime,
offline-name-sync service, worker and file-backed SQLite repositories. It
registers no scraper and exposes no fixture-control HTTP routes.

Build with `go build -o bin/javinizer-sync-e2e ./cmd/javinizer-sync-e2e`.
The host owns a private disposable directory (mode 0700), containing the regular
file `.javinizer-sync-e2e` with exact bytes `javinizer-sync-e2e disposable fixture\n`.
Set `JAVINIZER_SYNC_E2E_ROOT` to its absolute clean path. The command refuses
unmarked roots, symlink roots/state and incomplete initializations.

Environment options:

- `JAVINIZER_SYNC_E2E_PORT`: loopback port, default 18081.
- `JAVINIZER_SYNC_E2E_ORIGIN`: frontend origin, default `http://localhost:5181`.
- `JAVINIZER_SYNC_E2E_MODE`: ready, disabled, missing, stale or future.
- `JAVINIZER_SYNC_E2E_PAUSE_AFTER`: -1 for ungated work; 0 to pause before the
  first claim; n to pause after n real applies.

First startup converts synthetic R18 COPY bytes and publishes durable evidence
with production code. It seeds seven identities, protected names, Unicode,
ambiguous display names and pagination (105 records). A managed actor carries
historical credits, a manual display override and suppression plus a stale
translation. `fixture.json` records identities, source keys and artifact digests.
Restart retains the database, credentials, sessions and evidence timestamps;
it never reseeds. Authentication uses the disposable admin/adminpassword123
credentials, independently of inherited environment credentials.

The readiness stdout line is `javinizer-sync-e2e: ready`. Disabled mode is ready
without waiting for an artifact reader that intentionally does not run. SIGTERM
joins the real worker before closing SQLite. The host must await process exit
before restarting or deleting its own fixture root.

The worker gate writes `worker.paused` (job_id/applied JSON) before a claim and
holds no SQLite transaction or lease. The host creates `worker.release` to
release it. Remove both marker files when restarting a new gate scenario.
Cancellation of the runtime wakes the gate without mutating any receipt.

`JAVINIZER_SYNC_E2E_ROOT=... bin/javinizer-sync-e2e inspect` uses read-only/query-only
SQLite and one read transaction over a fixed SELECT allowlist, returning actors,
jobs, tasks, bindings, guards, credits, translations and aliases. It accepts no
SQL/table/path argument, runs no migrations and is not browser-accessible.

Browser tests must use actual successful API responses. Routing may delay or
fail transport, but must never fabricate successful review/confirm/history or
database state. Tests own and clean only their marked disposable directories.
