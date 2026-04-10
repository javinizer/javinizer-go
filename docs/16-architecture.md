<!-- generated-by: gsd-doc-writer -->

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
│  │           Worker Pool (concurrent task execution)            │  │
│  │           - Semaphore-based concurrency control              │  │
│  │           - Progress tracking and error aggregation          │  │
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

5. **Translation** (optional) - `internal/translation` translates metadata fields (title, description, maker, etc.) to a target language using configured providers (DeepL, Google, LibreTranslate).

6. **Database Persistence** - `internal/database` stores the aggregated `Movie` to SQLite, including actresses, genres, translations, and screenshots. Historical operations are tracked for rollback capability.

7. **Media Download** - `internal/downloader` fetches cover images, posters, fanart, trailers, and actress thumbnails from scraper-provided URLs. Downloads respect proxy configurations and include retry logic for transient failures.

8. **File Organization** - `internal/organizer` renames and moves files according to template configuration (e.g., `<ID> [<MAKER>] - <TITLE> (<YEAR>)`). Supports dry-run mode for previewing changes.

9. **NFO Generation** - `internal/nfo` creates Kodi/Plex-compatible NFO metadata files with the scraped information.

10. **Progress Reporting** - Throughout the pipeline, `internal/worker/progress` tracks task status and broadcasts updates via WebSocket to connected UI clients.

## Key Abstractions

### Scraper Interface (`internal/models/scraper.go`)

The `Scraper` interface defines the contract for all metadata sources:

```go
type Scraper interface {
    Name() string                              // Scraper identifier (e.g., "r18dev")
    Search(id string) (*ScraperResult, error) // Scrape by JAV ID
    GetURL(id string) (string, error)          // Resolve URL for ID
    IsEnabled() bool                           // Check if enabled in config
    Config() *config.ScraperSettings           // Scraper-specific config
    Close() error                              // Cleanup resources
}
```

Optional interfaces extend scraper capabilities:
- `URLHandler` - Handle direct URL scraping (extract ID from URL)
- `DirectURLScraper` - Scrape from URL instead of ID search
- `ScraperQueryResolver` - Normalize non-standard IDs
- `ContentIDResolver` - Resolve JAV ID to DMM content-ID format

**Location:** `internal/models/scraper.go:119-139`

### Aggregator Interface (`internal/aggregator/aggregator.go`)

The `AggregatorInterface` merges multiple scraper results into a unified `Movie`:

```go
type AggregatorInterface interface {
    Aggregate(results []*models.ScraperResult) (*models.Movie, error)
    GetResolvedPriorities() map[string][]string
}
```

The aggregator applies field-level priority merging, genre replacement rules, actress alias conversion, and configured translation. Each field (title, actresses, genres, etc.) uses the same global scraper priority, preferring earlier scrapers for non-empty values.

**Location:** `internal/aggregator/aggregator.go:22-28`

### Repository Interfaces (`internal/database/interfaces.go`)

Database operations are abstracted through repository interfaces for testability:

- `MovieRepositoryInterface` - CRUD operations for movies
- `ActressRepositoryInterface` - Actress database management
- `MovieTranslationRepositoryInterface` - Multi-language metadata
- `GenreReplacementRepositoryInterface` - Genre mapping rules
- `HistoryRepositoryInterface` - Operation tracking and rollback
- `ActressAliasRepositoryInterface` - Actress name normalization
- `MovieTagRepositoryInterface` - Custom movie tags
- `ContentIDMappingRepositoryInterface` - ID format mappings
- `JobRepositoryInterface` - Background job tracking

**Location:** `internal/database/interfaces.go`

### Worker Pool (`internal/worker/pool.go`)

The `Pool` manages concurrent task execution with semaphore-based concurrency control:

```go
type Pool struct {
    sem        *semaphore.Weighted  // Limits concurrent workers
    ctx        context.Context      // Cancellation support
    maxWorkers int64                // Configured worker limit
    timeout    time.Duration        // Per-task timeout
    progress   *ProgressTracker     // Status broadcasting
}
```

Tasks implement the `Task` interface with `ID()`, `Type()`, `Description()`, and `Execute(ctx)` methods. The pool handles task submission, execution, timeout enforcement, progress updates, and error aggregation.

**Location:** `internal/worker/pool.go:13-23`

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
│   │   ├── server/          # Server setup and routing
│   │   ├── batch/           # Batch operations (organize, scrape)
│   │   ├── movie/           # Movie CRUD endpoints
│   │   ├── actress/         # Actress management endpoints
│   │   ├── auth/            # Authentication middleware
│   │   ├── history/         # History and rollback endpoints
│   │   └── system/          # Config and scraper info endpoints
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
│   │   ├── registry.go      # Scraper configuration and initialization
│   │   └── priority.go      # Scraper priority resolution
│   │
│   ├── template/            # Template engine for output naming
│   │   └── engine.go        # <ID>, <TITLE>, <MAKER>, etc.
│   │
│   ├── translation/         # Metadata translation service
│   │   └── service.go       # DeepL, Google, LibreTranslate
│   │
│   ├── tui/                 # Terminal UI (Bubble Tea)
│   │   ├── model.go         # Application state
│   │   ├── views/           # UI components
│   │   └── interfaces.go    # Pool and progress abstractions
│   │
│   ├── worker/              # Concurrent task processing
│   │   ├── pool.go          # Worker pool management
│   │   ├── progress.go      # Status tracking and WebSocket broadcast
│   │   ├── tasks.go         # Task definitions (scrape, organize, download)
│   │   └── single_scrape.go # Single file scraping workflow
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
- **`internal/worker/` isolation** - The worker pool is a pure concurrency primitive with no knowledge of scraping or organization logic, making it reusable and testable in isolation.
- **`web/frontend/` separation** - The SvelteKit frontend is a standalone project that communicates only via the REST API and WebSocket, enabling independent development and hot-reload during development.
