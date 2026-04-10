# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
