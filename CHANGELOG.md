# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.11-alpha] - 2026-04-26

### Added

- Persist file browser sort preference across navigation within session
- Use '...' truncation marker with '~' fallback for trailing dots
- Auto-switch operation mode to 'Rename file only' when destination matches source path with empty folder/subfolder format

### Fixed

- Prevent blank file on Windows when src==dst and harden preview/organizer edge cases
- Windows mixed slashes in preview and empty folder format creating unwanted subfolders

### Added

- Add client-side pagination to FileBrowser
- Add pagination controls above file list in FileBrowser

### Fixed

- Allow same-origin WebSocket and CORS when AllowedOrigins is configured
- Remove user directive from docker-compose to enable entrypoint privilege bootstrap
- Add Docker defaults for setup CIDRs and fix bind mount ownership
- Allow Docker bridge access to /auth/setup via configurable trusted CIDRs
- Resolve AP-288 404 from r18.dev by accepting blank dvd_id with validated content_id fallback
- Add TokyoHot short-prefix ID matching and search query resolution

### Changed

- Add GitHub issue templates for bug reports, feature requests, and scraper issues

## [v0.2.9-alpha] - 2026-04-22

### Fixed

- Apply environment variable overrides (DEEPL_API_KEY, OPENAI_API_KEY, etc.) before config validation in `LoadOrCreate()`, preventing "api_key is required" errors when keys are set via env vars but empty in config file
- Persist per-field scraper priorities to config file and add scrollbar to priority modal
- Clean `web/dist` before `git checkout` to remove stale build artifacts
- Update `with_embedded_web.sh` restore_placeholder to use `git checkout`
- Add `ACTRESS` singular tag and fix `ACTORNAME` to resolve from actress data instead of movie title
- Add `subfolder_path` to organize preview response and improve cross-platform compatibility
- Add cross-device move fallback to file organizer

### Changed

- Switch nightly releases to commit-hash tagging scheme
- Remove duplicated `web/placeholder` dir, use `git checkout` in Makefile restore target

## [v0.2.8-alpha] - 2026-04-19

### Added

- Windows CI test runner for path-specific bug detection
- `fsPath()` helper in `internal/history/reverter.go` that normalizes paths for afero MemMapFs (forward-slash + drive letter stripping)
- `filepath.ToSlash()` normalization on `WillMove` path comparisons in organizer strategies to prevent false positives on Windows
- `filepath.ToSlash()` normalization on `isDescendant()` path comparisons for cross-platform directory hierarchy checks
- Windows volume root detection in `cleanupEmptyDir` termination conditions
- `runtime.GOOS == "windows"` skip guards for Unix-only tests (file permissions, shell scripts, `/dev/null`, concurrent file locking)

### Fixed

- Windows path separator mismatches in organizer plan tests and batch preview tests (hardcoded `/` vs `\` from `filepath.Join`)
- Windows file handle locks preventing `t.TempDir()` cleanup — added `defer CloseLogger()` and `defer db.Close()` in test helpers
- Windows `filepath.IsAbs` returning false for `\dest\...` paths (no drive letter) in multipart test
- Windows tilde expansion failure in `validateNFOPath` — `filepath.Join("~", ...)` produces `~\...` which doesn't match the `~/` prefix check
- Windows double-slash in database error messages — normalized with `strings.ReplaceAll("//", "/")` after `filepath.ToSlash`
- Windows `WillMove` incorrectly `true` when source/target paths differ only in separator style (production bug in `organizer.go`, `strategy_organize.go`, `strategy_inplace.go`, `strategy_inplace_norenamefolder.go`)
- Windows `isDescendant()` failing to detect descendant paths due to separator mismatch
- Windows `cleanupEmptyDir` walking past drive root — added `filepath.VolumeName` root detection
- Windows concurrent config writer test timing out due to different file locking semantics
- Windows `fsPath()` drive-letter check order — check before `filepath.ToSlash()` to avoid dead code
- Windows afero MemMapFs path lookup failures in `ReadNFOSnapshot` — use `fsPath()` to strip drive letter and normalize separators
- Windows LogDir output using backslash separators — apply `filepath.ToSlash` in `overrides.go`

### Added

- `FormatActressName` shared helper in `internal/models/movie.go` deduplicating `Actress.FullName()` and `ActressInfo.FullName()` implementations
- `ScraperOption` and `ScraperChoice` types moved from `internal/api/contracts` to `internal/models/` with backward-compatible aliases, eliminating reverse dependency where all 14 scrapers imported the API layer
- `JobRepositoryInterface.Upsert()` method replacing read-then-write `FindByID` → `Create`/`Update` pattern with single atomic operation
- `UpdateRequest` struct for `POST /api/v1/batch/{id}/update` with `force_overwrite`, `preserve_nfo`, `preset`, `scalar_strategy`, `array_strategy`, `skip_nfo`, `skip_download` fields
- `skip_nfo` and `skip_download` fields on `OrganizeRequest` for `POST /api/v1/batch/{id}/organize`
- Update options UI in review page header (force overwrite, preserve NFO, skip NFO, skip download toggles) with collapsible options panel
- Organize options UI in destination settings card (skip NFO, skip download toggles)
- Older-than-days cleanup picker on Jobs page with "Clean History" and "Clean Events" buttons and client-side validation
- Version check section in ServerSettingsSection: current/latest version display, "Update available" badge, "Check for Updates" button, error state handling
- `getVersionStatus()` and `checkVersion()` API client methods
- `VersionStatusResponse` TypeScript interface with error field
- Server-side validation rejecting `force_overwrite` + `preserve_nfo` as mutually exclusive (returns 400)
- `preset` binding validation (`oneof=conservative gap-fill aggressive`)
- 1MB request body size cap on update endpoint via `io.LimitReader`

### Changed

- Javstash scraper `module.go` proxy declaration changed from value type `config.ProxyConfig` to pointer type `*config.ProxyConfig`, matching the pattern used by all other 13 scrapers
- `AggregatorOptions` and `Aggregator` struct fields changed from concrete `*database.GenreReplacementRepository`/`*database.ActressAliasRepository` to interface types `database.GenreReplacementRepositoryInterface`/`database.ActressAliasRepositoryInterface`
- `processUpdateJob` and `processUpdateMode` now accept `*UpdateOptions` parameter for configurable update behavior
- `processOrganizeJob` now accepts `skipNFO` and `skipDownload` parameters
- `updateBatchJob` handler now parses optional `UpdateRequest` body (backward compatible with empty body), validates job existence/status before body parsing, and returns 400 for malformed JSON
- `persistToDatabase` uses `Upsert` instead of `FindByID` + `Create`/`Update`, eliminating persistence race condition
- All 14 scraper module.go files updated to import `models.ScraperOption`/`models.ScraperChoice` instead of `contracts.ScraperOption`/`contracts.ScraperChoice`
- Downloader HTTP client creation deferred until actually needed — `skip_download=true` no longer triggers downloader setup, preventing proxy/registry errors on NFO-only updates
- `preserve_nfo` now uses `PreserveExisting` merge strategy (not `PreferNFO`) to prevent blanking fields when existing NFO is incomplete
- `preserve_nfo` takes final precedence after preset resolution, preventing silent override
- DisplayTitle templating runs for ALL update paths (including `force_overwrite`), not just the NFO merge block
- Preview `$effect` reactive to skip toggles via `void` references; retry path persists last update options and skip flags
- Force Overwrite and Preserve NFO made mutually exclusive in UI via reactive `$effects`
- Test databases switched from in-memory shared-cache SQLite to temp file-based SQLite with WAL mode, eliminating `database table is locked` flaky test failures under concurrency
- Cross-platform path utility (`path.ts`) with `splitPath`, `buildPathUp`, `buildBreadcrumbPath`, `isRootPath` functions for Windows/Unix path handling
- FileBrowser breadcrumb navigation, "go up" button, and path construction now use cross-platform path utility instead of Unix-only logic
- OperationRow truncated paths now show 40/60 head-tail split with click-to-expand/collapse
- 54 files changed, ~1,192 lines added, ~468 lines removed

## [v0.2.7-alpha] - 2026-04-16

### Added

- Per-IP rate limiting middleware (`internal/api/middleware/ratelimit.go`) for write endpoints using `golang.org/x/time/rate` token bucket
- Sort-by whitelist validation returning 400 Bad Request on invalid column names per endpoint
- Secure cookie flag support for reverse proxy via `X-Forwarded-Proto` header and `force_secure_cookies` config option
- Bootstrap secret env var (`JAVINIZER_SETUP_SECRET`) for `/auth/setup` endpoint protection with local-only fallback
- `DrainAndClose` HTTP utility (`internal/httpclient/drain.go`) replacing bare `resp.Body.Close()` on all error paths
- JobQueue cleanup goroutine lifecycle with `stopCleanup` channel for graceful shutdown
- BatchJob thread-safe accessors (`GetID`, `GetJobStatus`) for concurrent read access
- `ErrNotFound` sentinel error (`internal/database/errors.go`) for cache miss distinction from actual database failures
- Partial success response for NFO merge operations indicating which fields succeeded or failed
- Per-file timeout for `processUpdateJob` with configurable duration bounds (30s–600s)
- Job queue `PersistError` field for persistence failure visibility in job status
- Database save error wrapping with operation context for debugging
- `GetStatusSlim` for lightweight status polling without full movie data payloads
- `MovieRepository.Upsert` returning populated `(*Movie, error)` with associations, eliminating extra database query
- Template engine injection as shared dependency (`template.Engine`) across organizer, downloader, and job queue
- `strings.Fields` replacing character-by-character `splitActressName` concatenation
- O(1) scanner extension map lookup replacing linear scan
- `DefaultFlattenConfig` and `DefaultFlattenConfigWithRaw` helpers (`internal/scraperutil/flatten.go`) with `FlattenOverrides` struct for all 14 scrapers
- Rescrape handler decomposition into focused files: `rescrape_scrape.go`, `rescrape_update.go`, `rescrape_validate.go`, `rescrape_poster.go`
- `BatchProcessOptions` and `BatchScrapeOptions` structs replacing positional parameters
- `AggregateWithPriority` method on `AggregatorInterface` for priority-based aggregation
- `MovieRepositoryInterface.Upsert` updated to return `(*Movie, error)`
- Genre replacement CRUD API at `/api/v1/genres/replacements/` with list, create (idempotent), and delete endpoints
- `GenreReplacementsSection` Svelte component in settings page with two-column add/remove table

### Changed

- All 14 scrapers migrated to shared `DefaultFlattenConfig` or `DefaultFlattenConfigWithRaw` helpers, eliminating per-scraper `FlattenFunc` boilerplate
- Rescrape handler decomposed from 666-line monolith to 110-line main handler + 8 focused helper functions
- Batch job processing uses options structs instead of positional parameters
- Mocks regenerated with updated `AggregatorInterface` and `MovieRepositoryInterface` signatures
- 146 files changed, ~4,057 lines added, ~2,382 lines removed

### Fixed

- Unauthenticated users cannot create admin accounts via `/auth/setup` without bootstrap secret or local access
- Authenticated API endpoints reject excessive requests from a single IP with 429 response
- Invalid `sort_by` query parameters rejected with 400 instead of being passed to the database
- Session cookies have Secure flag set when request arrives via HTTPS reverse proxy
- HTTP response bodies fully drained before close on all error paths — no leaked connections
- Job cleanup goroutine stops cleanly when application shuts down
- Concurrent access to BatchJob fields passes race detector with no warnings
- Download errors for covers, posters, and trailers visible in logs and API responses
- Cache miss errors distinguishable from actual database failures in code
- Job queue persistence failures visible in job status
- Review page explicitly requests full batch data (`include_data=true`) to prevent blank editor
- Slim polling by default for `BackgroundJobIndicator` and `ProgressModal` — avoids unnecessary payload overhead
- Genre replacement POST/DELETE routes under rate-limited `writeProtected` group
- `GetStatusSlim` deep-copies `FieldSources` and `ActressSources` maps to prevent shared mutable state
- Preview effect gated during organize polling to avoid redundant `/preview` API calls

### Removed

- `normalizeJSONLDImageURL` dead code from `internal/scraper/dmm/jsonld.go`
- Ineffective first loop from `extractDescriptionNewSite` in `internal/scraper/dmm/video_dmm.go`

### Security

- Rate limiting protects write endpoints (scrape, batch, proxy, auth, genre mutations) from per-IP abuse
- Sort-by whitelist prevents SQL injection via query parameters
- Bootstrap secret prevents first-arriver takeover of `/auth/setup`
- Secure cookie flag prevents session cookie leakage over HTTP behind reverse proxy

## [v0.2.6-alpha] - 2026-04-16

### Added

- SSRF protection package (`internal/ssrf`) with `NewSSRFSafeClient()`, `WrapTransportWithSSRFCheck()`, and `CheckRedirect()` validation blocking private/loopback/link-local IPs
- Typed scraper error model (`models.ScraperError`) with categorized error kinds (network, parsing, not-found, rate-limit, auth, timeout, context-cancelled)
- Config redaction utility (`internal/config/redact.go`) for safe logging of sensitive fields (API keys, passwords, tokens)
- Panic recovery middleware for batch processing with structured error reporting
- Job queue improvements: context-aware cancellation, improved state transitions, structured error aggregation
- Batch query support for movie repository (`FindMoviesByIDs`, `FindMoviesByContentIDs`) reducing N+1 database queries
- Translation service typed errors with retry classification
- Panic recovery tests for batch execute pipeline

### Changed

- Context propagation threaded through all 14 scrapers: `.SetContext(ctx)` on every resty request in ctx-aware methods
- Context threaded through full DMM actress thumbnail chain: `parseHTML` → `extractActresses` → `extractActressFromLink` → `tryActressThumbURLs` → `extractRomajiVariantsFromActressPageCtx`
- Context threaded through JavDB `Search` retry and `ScrapeURL` paths via `fetchPageDirectCtx`
- Context threaded through DMM `FetchWithBrowser` as chromedp parent context
- Context threaded through `DownloadMediaFiles` → `DownloadAll` chain
- Aggregator simplified with typed scraper errors replacing ad-hoc error classification
- Worker pool and scraper task pipeline refactored for structured error handling
- Downloader retry logic improved with per-error-kind backoff strategies
- Temp API handlers now use `ssrf.NewSSRFSafeClient()` instead of raw `http.Client`
- Proxy test client uses `resty.NoRedirectPolicy()` to prevent open-redirect SSRF
- Removed unused context-free wrapper functions (`fetchPageDirect`, `extractRomajiVariantsFromActressPage`, etc.)
- 135 files changed, ~2,659 lines added, ~1,265 lines removed

### Fixed

- SSRF redirect bypass: proxy test and temp API handlers now validate redirect destinations against internal IPs
- Scraper context cancellation gaps: all scraper HTTP requests now respect caller context for proper timeout/cancel propagation
- DMM actress thumbnail fallback now cancellable (previously used `context.Background()` for romaji lookup and HEAD probes)
- JavDB ScrapeURL retry path now respects caller context instead of spawning untracked requests
- Batch organize goroutine immediately cancelled due to deriving context from `c.Request.Context()` instead of `context.Background()`

### Security

- SSRF hardening: `NewSSRFSafeClient()` blocks connections to loopback, private, and link-local IP ranges (prevents cloud metadata credential exfiltration via `169.254.169.254` and internal service access)
- SSRF redirect validation: `CheckRedirect()` blocks HTTP redirects to internal IP addresses
- Config redaction prevents API keys and tokens from leaking into logs

## [v0.2.5-alpha] - 2026-04-14

### Added

- Database repository layer extracted into focused repos: `movie_repo`, `actress_repo`, `actress_alias_repo`, `genre_repo`, `genre_replacement_repo`, `movie_tag_repo`, `movie_translation_repo`, `event_repo`, `batch_file_operation_repo`, `history_repo`
- Database helpers package with `InTransaction()` wrapper and common query builders
- Scraper config validation tests for all 12 configurable scrapers
- Shared scraper utility helpers (`internal/scraperutil/helpers.go`) for common extraction patterns
- Aggregator priority tests for field resolution ordering
- Organizer strategy tests for all operation modes
- NFO generator and merger unit tests
- MediaInfo extended tests: AVI/RIFF parser, MKV, MP4 with edge cases
- Worker pool error classification tests and poster cache tests
- Batch revert check tests and lifecycle extra tests

### Changed

- Monolithic `database.go` (~1,436 lines) decomposed into 10 focused repository files
- All 14 scraper `Search`/`ScrapeURL` methods refactored for consistent error handling and config-driven behavior
- Jav321 scraper restructured with improved HTML parsing reliability
- Worker pool improved with structured error wrapping
- Test coverage increased (67 files changed, ~4,829 lines added, ~2,153 lines removed)

### Fixed

- Aventertainment, DLGetchu, Jav321, JavBus, JavDB, LibreDMM, MGStage, R18Dev, TokyoHot scraper config and edge-case bugs
- DMM JSON-LD parsing and video.dmm.co.jp extraction robustness
- FC2 and Caribbeancom scraper config handling
- Worker pool error reporting for concurrent scrape failures

## [v0.2.4-alpha] - 2026-04-12

### Added

- 5-mode OperationMode enum (organize, in-place, in-place-norenamefolder, metadata-only, preview) with strategy pattern
- Auto-migration from legacy `MoveToFolder`/`RenameFolderInPlace` boolean flags to OperationMode in config
- OperationMode wired through full API stack with 4-mode frontend selector
- `LooksLikeTemplatedTitle()` with UTF-8 safe rune-based detection for double-templating prevention
- NFOTitle field to ParseResult for future NFO preservation logic
- Regression tests for double-templating and display title edge cases
- `internal/types/operation_mode.go` package with validation and parsing
- Config pipeline system for structured migration paths
- Operation mode tests across organizer, config, API, and worker packages

### Changed

- Renamed `display_name` to `display_title` across Go backend and TypeScript frontend
- DisplayTitle is now the canonical editable field with aggregator always setting it
- DisplayTitle handling simplified: always regenerate from template with fallback to Title
- Preview mode removed from frontend UI (kept in backend API)
- Strategy pattern replaces monolithic Organizer with separate strategies per operation mode
- Database migration 000003 for column rename (display_name → display_title)
- 123 files changed, ~6,665 lines added, ~988 lines removed

### Fixed

- NFO and media generation for in-place and metadata-only modes (ShouldGenerateMetadata)
- History logging for metadata-only and in-place modes
- Preview missing screenshots for metadata-only mode
- Date clearing now emits undefined instead of empty string for `*time.Time` fields
- Date formatting guards against invalid dates
- DisplayTitle not regenerated when user edits Title — now always recomputed from template

## [v0.2.3-alpha] - 2026-04-10

### Added

- DMM placeholder detection with hash-based filtering for "now_printing.jpg" screenshots
- Shared placeholder detection package (`internal/scraper/image/placeholder`) for multi-scraper reuse
- Config-driven placeholder filtering opt-in via `ScraperSettings.Extra`
- Default placeholder hashes for DMM CDN images
- Collapsible info banner in Web UI explaining screenshot filtering behavior
- Runtime config drift detection script (`scripts/validate-config-sync.sh`) with multiline struct support

### Changed

- r18dev and libredmm scrapers now use shared placeholder detection package
- Test coverage increased to 76.02% (from 75.97%)

### Fixed

- DMM scraper config drift: hardcoded Timeout=30, RetryCount=3, RateLimit=0 now correctly use settings values
- DMM scraper fallback HTTP client now preserves Proxy and DownloadProxy settings
- Placeholder detection early return bug that skipped ALL filtering when hashes empty
- Size-based placeholder detection now works independently of hash matching
- Aggregator fallback to r18dev/libredmm with unfiltered placeholders resolved

### Security

- Path validation TOCTOU vulnerability resolved
- Rate limiter cancellation under contention fixed

## [v0.2.2-alpha] - 2026-04-09

### Added

- Unified scraper scaffolding across all 14 scrapers with 86% reduction in registration boilerplate
- HTTP Client Builder pattern eliminating ~560 lines of duplicated code
- Declarative scraper registration system (reduced from 98 to 14 registration calls)

### Changed

- Consolidated scraper platform architecture for easier maintenance and extension
- Test coverage increased to 75.97% (from 67.4%)

## [v0.2.1-alpha] - 2026-04-09

### Added

- JavStash scraper for Stash-Box GraphQL API integration
- Clear All Jobs button with confirmation dialog on jobs page
- Status filter and visual grouping on jobs page
- Log rotation and improved logging configuration
- DirectURLScraper interface for all scrapers supporting direct URL scraping

### Changed

- Refactored rate limiting to shared thread-safe package for consistent throttling
- Reorganized Browser Settings UI section with subsections for clarity

### Fixed

- Security vulnerabilities from code review (rate limiter rollback bug, path validation TOCTOU, scanner TOCTOU, job queue deadlock)
- Job state machine for organization retry workflow
- Temp poster cleanup moved from organization to job dismissal
- Chrome crashpad handler error in Docker container
- Log file creation and permissions issues in Docker
- Job poster persistence after rescrape
- Frontend manual search rescrape using correct movie ID
- Domain boundary checks in multiple scrapers (javbus, r18dev, javdb, caribbeancom)
- Race conditions and edge cases in rescrape functionality

## [v0.2.0-alpha] - 2026-04-05

### Added

- Multi-language template tags support (e.g., `<TITLE:EN>`, `<TITLE:JA|EN>`)
- Language-specific fields for R18.dev API (EN/JA)
- Job persistence to database for batch operations
- Auto-archive cleanup goroutine in JobQueue
- Persistent destination path in jobs
- Jobs page redesign with job cards and temp poster persistence
- OpenAI Compatible and Anthropic translation providers
- Extended model discovery for new translation providers
- Hash-based cache invalidation for translations (settings_hash)
- Remember-me sessions for authentication
- OpenAI-compatible thinking toggle for translations
- Configurable temp directory for poster files
- Scraper plugin system with unified config architecture
- Configuration migration system
- Browser automation settings UI
- Letter-pattern multipart file discovery

### Changed

- Database migrations squashed to single baseline with hash tracking
- Renamed SystemConfig fields for clarity
- Translation configuration provider value standardized to `openai-compatible`
- Frontend scraper options disabled when global switches are off

### Fixed

- R18.dev API translations populated for both EN and JA languages
- Invalid language specs handled with base field fallback
- Destination field included in GetStatus snapshot
- Svelte 5 runes mode compatibility for dynamic components
- Navigation to /jobs when all movies excluded in review
- Job card layout and poster thumbnail centering
- Preserve multipart metadata for letter-pattern files
- Multipart move conflict for letter-pattern files
- Preserve multipart metadata in rescrape endpoint
- Translation JSON array parsing with conversational text handling
- WebSocket origin validation hardened (removed wildcard support)
- Poster path generation only when DownloadPoster enabled
- Organize preview respects NFO/media download settings

## [v0.1.5-alpha] - 2026-03-30

### Fixed

- Config round-trip: YAML/JSON save/load now preserves all scraper-specific fields
- FlareSolverr block preserved across config cycles
- DeepCopy() prevents mutation leaks between DefaultConfig() and global registry
- JavLibrary FlareSolverr client only initializes when enabled; nil proxy handled safely
- Translation ordering: buildTranslations() called after field aggregation
- Registry validation: fail-fast on malformed scraper config blocks, unknown fields disallowed
- API key validation in translation config

## [v0.1.4-alpha] - 2026-03-30

### Changed

- Code reorganization: config.go split into 7 focused files (~1968 lines reorganized)
- DMM helpers extracted to dedicated utilities (-482 lines)
- Database utilities extracted (-402 lines)
- Aggregator utilities extracted (-153 lines)
- FlareSolverr config restructured from proxy to scrapers level for cleaner architecture

## [v0.1.2-alpha] - 2026-03-17

### Added

- Web UI embedded in binary for single-binary distribution
- `web` command alias for unified API/web server entrypoint

### Changed

- CI Node.js version bumped to 22 for builds

### Fixed

- Web assets bundled in CI binaries
- API and web usage clarified in documentation

## [v0.1.1-alpha] - 2026-03-17

### Added

- R18.dev language option support (en/ja)
- GHCR Docker images with version-first tags

### Changed

- Config profile inheritance for cleaner configuration
- Legacy proxy fields removed in favor of profile-based approach

## [v0.1.0-alpha] - 2026-03-16

### Added

- **Multi-source scraping**: R18.dev, DMM/Fanza, JavLibrary, JavDB, JavBus, Jav321, LibreDMM, MGStage, TokyoHot, AVEntertainment, DLGetchu, Caribbeancom, FC2 scrapers
- **Smart file organization**: Rename and organize files/folders using configurable templates
- **Dry-run preview**: Full preview before making any changes
- **NFO generation**: Kodi/Plex-compatible metadata files
- **Media downloads**: Cover, poster, fanart, trailer, and actress image downloads
- **Multiple interfaces**: CLI, interactive TUI (Bubble Tea), REST API, and web UI (SvelteKit)
- **Interactive TUI**: Browse and scrape files with real-time progress display
- **Tag system**: Per-movie custom tags stored in database
- **Genre management**: Genre replacement rules with CLI commands
- **History tracking**: File organization operation history with rollback support
- **HTTP/SOCKS5 proxy support**: For all network requests including chromedp
- **MediaInfo integration**: Video format probing with AVI/RIFF and FLV parsers, CLI fallback
- **Actress alias system**: Alternative names mapping
- **Template system**: Folder/file naming with conditional logic and multi-part support
- **Docker deployment**: Container with user/group mapping, environment variable support
- **Authentication**: Single-user auth with setup flow and secured sessions
- **API documentation**: Scalar UI and Swagger UI at /docs and /swagger
- **WebSocket progress**: Real-time progress streaming for batch operations
- **Configurable umask**: File permission control
- **Environment variables**: JAVINIZER_* overrides for config, database, logging, temp directory
- **Amateur video scraping**: DMM support for amateur content
- **Actress thumbnail extraction**: From DMM streaming pages
- **Poster quality detection**: Intelligent cropping for DMM and R18Dev
- **Chromium support**: In Docker for headless browser scraping

### Security

- Path traversal protection for API endpoints
- CORS origin validation
- Directory traversal prevention
- SQL injection prevention
- Header injection and path traversal sanitization in frontend
