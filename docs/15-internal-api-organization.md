# Internal API Organization

Conventions for keeping `internal/api` maintainable with subpackage boundaries.

## Package map

`internal/api` is organized into foundational packages, cross-cutting packages, and one package per HTTP domain. Foundational packages hold shared plumbing; cross-cutting packages provide error and middleware helpers used across handlers; domain packages each own a feature surface and register their own routes.

Foundational packages:

- `internal/api/contracts`: shared request/response DTOs and error payloads (e.g. `ErrorResponse`, `RevertFileError`), plus JSON/time formatting helpers used across handlers.
- `internal/api/core`: the dependency container (`APIDeps`), runtime state (`RuntimeState`, `APIRuntime`), bootstrap entry point (`BootstrapAPI`), and shared security/path helpers — path validation, token store, pagination, HTTP errors, inode and Windows path normalization, the update checker, and hot reload.
- `internal/api/server`: Gin router composition. `NewServer(rt)` builds the engine and wires CORS, documentation routes, the `/api/v1` route groups, the embedded static web UI, and the no-route fallback.
- `internal/api/testkit`: shared API test utilities — `MockScraperWithResults`, `NewMockMovieRepo`/`NewMockActressRepo`, `CreateTestDeps`, `GetTestRuntime`/`SetTestRuntime`, `InitTestWebSocket`, and `CleanupServerHub`.

Cross-cutting packages (no routes; consumed by handlers across domains):

- `internal/api/apperrors`: API error types (`PathError`, `errorCode` constants) and the `WriteAPIError` response writer.
- `internal/api/middleware`: shared Gin middleware — `ValidateJobID` (path-traversal guard for `:id` job params) and `RateLimitMiddleware` backed by `IPRateLimiter`.

Domain packages (each owns a feature surface and is wired from `internal/api/server`):

- `actress` — actress catalog: list, search, CRUD, merge preview/execute, import/export.
- `auth` — authentication endpoints (`/auth/status`, `/auth/setup`, `/auth/login`, `/auth/logout`); exposes `RegisterPublicRoutes`, mounted on `/api/v1` before the protected group.
- `batch` — batch scrape/organize/rescrape workflow, per-result operations (rescrape, exclude, preview, poster), cancel, update, and delete.
- `events` — event-log endpoints: list, stats, and clear (`GET`/`DELETE /events`).
- `file` — filesystem endpoints: current working directory, scan, browse, and path autocomplete.
- `genre` — genre listing plus replacement CRUD, import/export, and update.
- `history` — history log: list, stats, and delete (single entry or all).
- `jobs` — job status, operations, revert-check, and revert (job-level and per-movie).
- `movie` — movie scrape, list/get, rescrape, and NFO compare.
- `realtime` — WebSocket progress endpoint (`/ws/progress`); `RegisterRoutes` mounts at the engine root, outside `/api/v1`.
- `system` — system config (`GET`/`PUT`), scraper listing, proxy test, and translation model/DeepL usage; also exposes `RegisterCoreRoutes`, which mounts `GET /health` at the engine root.
- `temp` — temp/poster image endpoints (poster serve, temp image, stored posters).
- `token` — API token management: list, create, delete, and regenerate.
- `version` — build/version info and update check (`GET /version`, `POST /version/check`).

## Route registration

All routing is composed in `internal/api/server`. `NewServer(rt)` wires the engine in order: CORS middleware, documentation routes (`/docs/openapi.json`, `/docs`, `/swagger/*`), core routes, `/api/v1` routes, the embedded static web UI, and the no-route fallback.

- `registerCoreRoutes` mounts endpoints at the engine root, outside `/api/v1`: `system.RegisterCoreRoutes` (`GET /health`) and `realtime.RegisterRoutes` (`/ws/progress`).
- `registerAPIV1Routes` creates the `/api/v1` group, calls `auth.RegisterPublicRoutes` (public auth endpoints), then builds a `protected` subgroup guarded by `auth.RequireTokenOrSession` and a `writeProtected` subgroup additionally rate-limited via `middleware.RateLimitMiddleware`. Each domain `RegisterRoutes` call attaches its handlers under the appropriate subgroup.

## Guardrails

- Keep route composition in `internal/api/server`; domain packages expose `RegisterRoutes(protected *gin.RouterGroup, ...)` and are wired from there. Exceptions: `auth` exposes `RegisterPublicRoutes` (public endpoints mounted before the protected group); `realtime.RegisterRoutes` and `system.RegisterCoreRoutes` mount at the engine root, outside `/api/v1`; `apperrors` and `middleware` are cross-cutting packages with no routes.
- Keep domain-only logic inside its domain package; place cross-domain/shared logic in `core` or `contracts`.
- Avoid cross-domain imports between handler packages unless explicitly approved for shared runtime behavior.
- Keep private helpers close to call sites and split by concern before files become large.

## Size policy

- Non-test Go files in `internal/api/**` should stay under `700` lines.
- CI enforces this recursively via `scripts/check_api_file_size.sh`, invoked as `./scripts/check_api_file_size.sh 700 internal/api` in the "Check internal/api file size guardrail" step of the `lint` job in `.github/workflows/test.yml`. The script excludes `*_test.go` and recurses with `find`.
- If a file approaches the limit, split by behavior before adding new features.
