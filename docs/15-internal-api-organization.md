# Internal API Organization

Conventions for keeping `internal/api` maintainable with subpackage boundaries.

## Package map

- `internal/api/contracts`: shared request/response DTOs and error payloads.
- `internal/api/core`: dependency container, runtime state, and shared security/path helpers.
- `internal/api/server`: Gin router composition, middleware, docs/static/no-route setup.
- Domain packages:
  - `actress`
  - `apperrors`
  - `auth`
  - `batch`
  - `data`
  - `events`
  - `file`
  - `genre`
  - `history`
  - `jobs`
  - `middleware`
  - `movie`
  - `realtime`
  - `system`
  - `temp`
  - `version`
- `internal/api/testkit`: shared API test utilities.

## Guardrails

- Keep route composition in `internal/api/server`; domain packages expose `RegisterRoutes(...)`.
- Keep domain-only logic inside its domain package; place cross-domain/shared logic in `core` or `contracts`.
- Avoid cross-domain imports between handler packages unless explicitly approved for shared runtime behavior.
- Keep private helpers close to call sites and split by concern before files become large.

## Size policy

- Non-test Go files in `internal/api/**` should stay under `700` lines.
- CI enforces this recursively via `scripts/check_api_file_size.sh`.
- If a file approaches the limit, split by behavior before adding new features.
