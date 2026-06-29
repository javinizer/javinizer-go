# Architecture Overview

Javinizer Go is a metadata scraper and file organizer for Japanese Adult Videos (JAV), written in Go. The system provides multiple user interfaces (CLI, TUI, REST API, and Web UI) and processes video files through a pipeline that extracts JAV IDs, scrapes metadata from multiple sources, aggregates results, persists to a database, and organizes files according to configurable templates.

## System Overview

At its core, Javinizer Go transforms a library of unorganized JAV video files into a structured, metadata-rich collection. The system accepts video files as input, extracts JAV identifiers from filenames, queries multiple metadata scrapers concurrently, merges results based on configurable field-level priorities, downloads associated media (covers, posters, trailers), generates NFO metadata files for media centers, and reorganizes files using template-based naming schemes.

The architecture follows a layered design with clear separation between interfaces (CLI/TUI/API), orchestration (worker pool), business logic (scraping, aggregation, organization), and persistence (database). The system supports concurrent processing of multiple files with configurable worker counts and timeouts, enabling efficient batch processing of large libraries.

## Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                          User Interfaces                             │
├───────────────┬──────────────────┬──────────────────┬───────────────┤
│      CLI      │       TUI        │    REST API      │    Web UI     │
│  (cobra cmds) │  (bubbletea TUI) │   (gin server)   │  (SvelteKit)  │
└───────┬───────┴────────┬─────────┴─────────┬────────┴───────────────┘
        │                │                   │
        └────────────────┴───────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Orchestration Layer                              │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │     Workflow seam (internal/workflow) + BatchJobInterface    │  │
│  │     - Scrape / Apply / Rescrape phases (worker.BatchJobInterface) │  │
│  │     - Bounded fan-out worker pool per phase                  │  │
│  │     - Progress reporting and error aggregation               │  │
│  └──────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Processing Pipeline                             │
├───────────┬──────────┬──────────┬────────────┬──────────┬──────────┤
│  Scanner  │ Matcher  │ Scrapers │ Aggregator │Database  │Organizer │
│ (files)   │(JAV IDs) │(metadata)│  (merge)   │(persist) │ (rename) │
└─────┬─────┴────┬─────┴────┬─────┴─────┬──────┴────┬─────┴────┬─────┘
      │          │          │           │           │          │
      └──────────┴──────────┴───────────┴───────────┴──────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Supporting Services                             │
├──────────────┬──────────────┬─────────────┬────────────┬─────────────┤
│  Downloader  │     NFO      │  Template   │ Translation│   History   │
│   (media)    │  Generator   │   Engine    │  Service   │   Tracker   │
└──────────────┴──────────────┴─────────────┴────────────┴─────────────┘
```

## Data Flow

A typical file organization operation follows this pipeline:

1. **File Discovery** - `internal/scanner` recursively scans the input directory for video files matching configured extensions and size thresholds.

2. **ID Extraction** - `internal/matcher` extracts JAV IDs from filenames using pattern matching (e.g., `IPX-123.mp4` → `IPX-123`). Also supports direct URL input for scraper-specific URLs.

3. **Metadata Scraping** - `internal/scraper` queries enabled scrapers (r18dev, dmm, javlibrary, etc.) in priority order. Each scraper returns a `ScraperResult` containing metadata fields. The system continues to the next scraper on failure, logging errors without stopping the pipeline.

4. **Result Aggregation** - `internal/aggregator` merges multiple `ScraperResult` objects into a single `Movie` model using field-level priority configuration. For each field (title, actresses, genres, etc.), the aggregator selects the first non-empty value from the priority-ordered results. Genre replacements and actress alias conversions are applied during aggregation.

5. **Translation** (optional) - `internal/translation` translates metadata fields (title, description, maker, etc.) to a target language using configured providers (DeepL, Google, OpenAI, OpenAI-compatible, Anthropic).

6. **Database Persistence** - `internal/database` stores the aggregated `Movie` to SQLite, including actresses, genres, translations, and screenshots. Historical operations are tracked for rollback capability.

7. **Media Download** - `internal/downloader` fetches cover images, posters, fanart, trailers, and actress thumbnails from scraper-provided URLs. Downloads respect proxy configurations and include retry logic for transient failures.

8. **File Organization** - `internal/organizer` renames and moves files according to template configuration (e.g., `<ID> [<MAKER>] - <TITLE> (<YEAR>)`). Supports dry-run mode for previewing changes.

9. **NFO Generation** - `internal/nfo` creates Kodi/Plex-compatible NFO metadata files with the scraped information.

10. **Progress Reporting** - Throughout the pipeline, the workflow seam and `internal/worker` phase hooks track job/phase status and broadcast updates via WebSocket to connected UI clients.

## Key Abstractions

### Scraper Interface (`internal/models/scraper.go`)

The `Scraper` interface defines the contract for all metadata sources:

```go
type Scraper interface {
    Name() string                                              // Scraper identifier (e.g., "r18dev")
    Search(ctx context.Context, id string) (*ScraperResult, error) // Scrape by JAV ID
    GetURL(ctx context.Context, id string) (string, error)     // Resolve URL for ID
    IsEnabled() bool                                           // Check if enabled in config
    Config() *models.ScraperSettings                           // Scraper-specific config
    Close() error                                              // Cleanup resources
}
```

Optional interfaces extend scraper capabilities. Consumers detect them with a
type assertion (e.g., `handler, ok := scraper.(URLHandler)`) rather than
assuming support:
- `URLHandler` - Direct URL scraping: `CanHandleURL`, `ExtractIDFromURL`, and `ScrapeURL(ctx, url)` for scraper-specific URLs
- `DownloadProxyResolver` - Resolve per-host download proxy config for media fetched from scraper-specific CDN hosts
- `ScraperQueryResolver` - Declare and normalize non-standard identifier formats a scraper can handle
- `ContentIDResolver` - Resolve a JAV ID to its DMM content-ID format (e.g., `ipx-123` → `118BDP-00118`)
- `ContentIDResolverCtx` - Context-aware variant of `ContentIDResolver` for scrapers whose lookup issues HTTP; callers assert this first and fall back to `ContentIDResolver`
- `HTMLParser` - Parse a pre-fetched `goquery.Document` into a `ScraperResult`, enabling tests with static HTML fixtures

**Location:** `internal/models/scraper.go:132-153` (core interface); optional interfaces at `:159-226`

### Aggregator Interface (`internal/aggregator/aggregator.go`)

The `AggregatorInterface` merges multiple scraper results into a unified `Movie`:

```go
type AggregatorInterface interface {
    Aggregate(results []*models.ScraperResult) (*models.Movie, *AggregateResult, error)
    AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, *AggregateResult, error)
    ReloadReplacementCaches(ctx context.Context)
}
```

The `AggregatorInterface` exposes three operations: `Aggregate` runs the default
priority merge; `AggregateWithPriority` overrides the scraper order for a single
call (used by per-job scraper filters); and `ReloadReplacementCaches` hot-reloads
the genre, word, and alias replacement maps after a mutation without rebuilding
the whole workflow factory. `Aggregate` returns a `*AggregateResult` alongside the
`*models.Movie` — it carries `FieldSources` (which scraper filled each field) and
`ResolvedPriorities` (the per-field priority lists actually used), so callers
never need to inspect hidden aggregator state. Each field uses its per-field
priority list when configured — a per-field list is **exclusive** (no global
fallback) — otherwise it falls back to the global `scrapers.priority` list,
preferring earlier scrapers for non-empty values. Genre replacement, word
replacement, actress alias resolution, and actress merging are delegated to
focused sub-processors composed in `New`.

**Location:** `internal/aggregator/aggregator.go:20-32`

### Repository Interfaces (`internal/database/interfaces.go`)

Database operations are abstracted through repository interfaces for testability:

- `MovieRepositoryInterface` - CRUD and upsert for movies (incl. translations and screenshots)
- `ActressRepositoryInterface` - Actress management and lookups
- `GenreRepositoryInterface` - Genre catalog management
- `GenreTranslationRepositoryInterface` - Per-language genre translations
- `ActressTranslationRepositoryInterface` - Per-language actress translations
- `GenreReplacementRepositoryInterface` - Genre mapping/replacement rules
- `WordReplacementRepositoryInterface` - Word mapping/replacement rules
- `HistoryRepositoryInterface` - Operation tracking and rollback
- `ActressAliasRepositoryInterface` - Actress name normalization/aliases
- `MovieTagRepositoryInterface` - Custom movie tags
- `ContentIDMappingRepositoryInterface` - Search-ID → content-ID mappings (type alias of `models.ContentIDMappingRepositoryInterface`)
- `JobRepositoryInterface` - Background job tracking
- `BatchFileOperationRepositoryInterface` - Batch file-operation records
- `ApiTokenRepositoryInterface` - API token management
- `EventRepositoryInterface` - System event log

Movie-language translations are persisted by an internal `movieTranslationRepository`
(no public interface) invoked through the movie save path, so there is no
`MovieTranslationRepositoryInterface` in this file.

**Location:** `internal/database/interfaces.go`

### Workflow Seam & BatchJobInterface (`internal/workflow`, `internal/worker`)

The orchestration layer is a unified `Workflow` abstraction. Ad-hoc task types
(`ScrapeTask`/`DownloadTask`/`OrganizeTask`/`NFOTask`) have been consolidated
onto this single seam, and the worker pool executes phase-based jobs.

`workflow.WorkflowInterface` (`internal/workflow/interfaces.go`) exposes the
seam methods callers invoke:

```go
type WorkflowInterface interface {
    Scrape(ctx context.Context, cmd scrape.ScrapeCmd, progress scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error)
    Apply(ctx context.Context, cmd ApplyCmd, progress scrape.ProgressFunc) (*ApplyResult, error)
    Preview(...) (*PreviewResult, error)
    Compare(...) (*CompareResult, error)
    ScanAndMatch(...) (*ScanAndMatchResult, error)
}
```

`worker.BatchJobInterface` (`internal/worker/batch_job_interface.go`) drives
batch jobs through three phases — **scrape**, **apply**, and **rescrape** —
with bounded fan-out concurrency per phase, progress reporting, and error
aggregation. It composes narrow sub-interfaces (`JobReader`, `MovieLookup`,
`PhaseController`, `JobCanceller`, `JobEditor`) so handlers depend on the
narrowest view they need.

At startup, `SetReconstructionDeps` re-hydrates infrastructure dependencies on
jobs loaded from the database, so post-restart apply/rescrape and movie-edit
persistence continue to work.

**Location:** `internal/workflow/interfaces.go`, `internal/worker/batch_job_interface.go`

## Directory Structure

```
javinizer-go/
├── cmd/javinizer/          # CLI entry point and command definitions
│   ├── main.go              # Bootstrap and Execute() call
│   ├── root.go              # Root cobra command
│   └── commands/            # Subcommands (sort, scrape, tui, api, etc.)
│       ├── sort/            # File organization command
│       ├── scrape/          # Manual metadata scraping
│       ├── tui/             # Terminal UI command
│       ├── update/          # Re-scrape existing files
│       └── init/            # Config initialization
│
├── internal/                # Private application code
│   ├── api/                 # REST API server (Gin framework)
│   │   ├── server/          # Gin router composition, middleware, docs/static, OpenAPI spec
│   │   ├── contracts/       # Wire-format projection layer (DTOs, movie_view)
│   │   ├── core/            # Dependency container, runtime state, path/security helpers
│   │   ├── middleware/      # Shared HTTP middleware (job-ID validation, rate limiting)
│   │   ├── apperrors/       # Typed API error mapping and HTTP error responses
│   │   ├── batch/           # Batch operations (organize, scrape, rescrape)
│   │   ├── movie/           # Movie CRUD endpoints
│   │   ├── actress/         # Actress management endpoints (incl. export/import)
│   │   ├── genre/           # Genre catalog and genre/word replacement endpoints
│   │   ├── file/            # Filesystem browsing and directory scan endpoints
│   │   ├── jobs/            # Background job, operation, and revert endpoints
│   │   ├── history/         # History and rollback endpoints
│   │   ├── events/          # System event log endpoints
│   │   ├── token/           # API token management endpoints
│   │   ├── version/         # Version and update-check endpoints
│   │   ├── realtime/        # WebSocket progress streaming
│   │   ├── auth/            # Authentication middleware
│   │   ├── system/          # Config, scraper info, proxy test, translation endpoints
│   │   ├── temp/            # Temp/cropped poster and image serving endpoints
│   │   └── testkit/         # API test helpers and mock builders
│   │
│   ├── aggregator/          # Multi-source metadata merging
│   │   └── aggregator.go    # Priority-based field selection
│   │
│   ├── database/            # SQLite persistence layer
│   │   ├── interfaces.go    # Repository interfaces
│   │   ├── db.go            # Database connection and migrations
│   │   └── [repositories]   # Movie, Actress, History, etc.
│   │
│   ├── downloader/          # Media file downloads
│   │   └── downloader.go    # Retry logic, proxy support
│   │
│   ├── matcher/             # JAV ID extraction from filenames
│   │   ├── matcher.go       # Pattern matching logic
│   │   ├── multipart.go     # Multi-part file detection
│   │   └── url_parser.go    # Direct URL handling
│   │
│   ├── models/              # Data models and interfaces
│   │   ├── scraper.go       # Scraper interface and registry
│   │   ├── movie.go         # Movie, Actress, Genre structs
│   │   └── [model files]    # History, Config, etc.
│   │
│   ├── nfo/                 # NFO metadata file generation
│   │   └── generator.go     # Kodi/Plex NFO format
│   │
│   ├── organizer/           # File renaming and moving
│   │   ├── organizer.go     # Template-based organization
│   │   └── subtitles.go     # Subtitle file handling
│   │
│   ├── scanner/             # Filesystem scanning
│   │   └── scanner.go       # Recursive directory scan
│   │
│   ├── scraper/             # Metadata scrapers
│   │   ├── registry.go      # Scraper registration
│   │   ├── dmm/             # DMM/Fanza scraper
│   │   ├── r18dev/          # R18.dev JSON API scraper
│   │   ├── javlibrary/      # JavLibrary scraper
│   │   ├── javdb/           # JavDB scraper
│   │   ├── javbus/          # JavBus scraper
│   │   ├── mgstage/         # MGS Stage scraper
│   │   ├── fc2/             # FC2 scraper
│   │   └── [more scrapers]  # Additional sources
│   │
│   ├── scraperutil/         # Scraper utilities
│   │   ├── scraper_registry.go   # Centralized scraper registry (registration catalog)
│   │   └── registration_catalog.go # Scraper configuration and initialization
│   │
│   ├── template/            # Template engine for output naming
│   │   └── engine.go        # <ID>, <TITLE>, <MAKER>, etc.
│   │
│   ├── translation/         # Metadata translation service
│   │   └── service.go       # OpenAI, DeepL, Google, OpenAI-compatible, Anthropic
│   │
│   ├── tui/                 # Terminal UI (Bubble Tea)
│   │   ├── model.go         # Application state
│   │   ├── views/           # UI components
│   │   └── interfaces.go    # Pool and progress abstractions
│   │
│   ├── worker/              # Phase-based batch job execution
│   │   ├── batch_job_interface.go # BatchJobInterface (scrape/apply/rescrape phases)
│   │   ├── scrape_phase.go  # Scrape phase
│   │   ├── apply_phase.go   # Apply (organize/NFO/download) phase
│   │   ├── rescrape_phase.go # Rescrape phase
│   │   ├── job_store.go     # In-memory job store and reconstruction
│   │   └── progress_fn.go   # Progress reporting and WebSocket broadcast
│   │
│   ├── workflow/            # Workflow seam (unified orchestration abstraction)
│   │   ├── interfaces.go    # WorkflowInterface (Scrape/Apply/Preview/Compare/ScanAndMatch)
│   │   ├── factory.go       # Dependency wiring / factory boundary
│   │   └── [orchestrators]  # scrape/apply/compare/preview/scanmatch orchestrators
│   │
│   ├── config/              # Configuration loading and validation
│   ├── httpclient/          # HTTP client factory with proxy support
│   ├── logging/             # Structured logging
│   └── testutil/            # Test helpers and builders
│
├── web/frontend/            # Web UI (SvelteKit)
│   └── src/                 # Frontend source
│       ├── routes/          # SvelteKit pages
│       ├── lib/components/  # Reusable UI components
│       └── lib/stores/      # Svelte stores (state management)
│
├── docs/                    # Documentation
├── configs/                 # Example configuration files
├── scripts/                 # Build and release scripts
└── testdata/                # Test fixtures
```

**Rationale:**

- **`cmd/` vs `internal/`** - Entry points and command wiring are public in `cmd/`, while all business logic remains in `internal/` to prevent external dependencies.
- **`internal/api/` organization** - API endpoints are grouped by domain (movie, actress, batch, history) rather than by HTTP method, making it easier to understand the capabilities of each resource.
- **`internal/scraper/` structure** - Each scraper is a subpackage with its own implementation, allowing independent testing and configuration while sharing utilities in `internal/scraperutil/`.
- **`internal/worker/` + `internal/workflow/` separation** - The `workflow` seam owns scrape/apply/rescrape orchestration; `worker` provides the phase-based `BatchJobInterface` and bounded fan-out execution. Neither knows about UI concerns, keeping them reusable and testable in isolation.
- **`web/frontend/` separation** - The SvelteKit frontend is a standalone project that communicates only via the REST API and WebSocket, enabling independent development and hot-reload during development.
