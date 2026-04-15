# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
