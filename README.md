# Javinizer Go

A modern, high-performance Go implementation of Javinizer - a metadata scraper and file organizer for Japanese Adult Videos (JAV).

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Test & Coverage](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml/badge.svg)](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/javinizer/javinizer-go/branch/master/graph/badge.svg)](https://codecov.io/gh/javinizer/javinizer-go)

## Current Status

- **Tracked version:** `v0.1.0-alpha`
- **Release flow:** push a semver tag (`vX.Y.Z` or prerelease like `vX.Y.Z-alpha`) to build binaries, Docker images, checksums, and a GitHub release.
- **Docker image:** built as multi-arch (`linux/amd64`, `linux/arm64`) and includes frontend assets.
- **Quality gates:** CI runs tests, coverage threshold (`75%`), race tests, linting, and Docker build verification.

## Features

✅ **Multi-Source Scraping**
- R18.dev scraper (fast JSON API)
- DMM/Fanza scraper (HTML parsing)
- JavDB scraper (optional, with FlareSolverr/proxy support)
- Intelligent metadata aggregation with configurable priority
- Database caching for instant lookups

✅ **File Organization**
- Automatic JAV ID detection from filenames
- Flexible template-based folder/file naming
- Nested subfolder hierarchies (organize by year, studio, etc.)
- Move or copy files with conflict detection
- Dry-run mode for safe preview
- Force update to overwrite existing files

✅ **Metadata Management**
- Kodi/Plex-compatible NFO generation
- Actress database with Japanese name support
- Genre replacement system (database-backed)
- Multi-language support

✅ **Media Downloads**
- Cover and poster images
- Extrafanart/screenshot galleries
- Trailer videos
- Actress thumbnails
- Command-line override options

✅ **Modern Architecture**
- SQLite database for caching
- Concurrent scraping for speed
- Cross-platform single binary
- No dependencies required

✅ **Interactive TUI**
- Browse and select files visually
- Real-time progress tracking
- Concurrent processing with worker pool
- Live operation logs and statistics

✅ **API + Web Frontend**
- API server via `javinizer api`
- Frontend bundled in Docker/release builds
- Browser-based review/history/settings workflows

## Quick Start

### Installation

**Download Binary**:
```bash
# Download from releases page
# https://github.com/javinizer/javinizer-go/releases

# Or build from source
go install github.com/javinizer/javinizer-go/cmd/javinizer@latest

# Verify installed version/build
javinizer --version
javinizer version --short
```

**Initialize**:
```bash
javinizer init
```

### Basic Usage

**Interactive TUI** (Recommended):
```bash
# Launch interactive file browser
javinizer tui ~/Videos

# Use keyboard to select files, press Enter to process
# See docs/11-tui.md for complete guide
```

**Scrape metadata**:
```bash
javinizer scrape IPX-535
```

**Organize files**:
```bash
# Preview (dry-run)
javinizer sort ~/Videos --dry-run

# Actually organize
javinizer sort ~/Videos
```

**Manage genres**:
```bash
javinizer genre add "Blow" "Blowjob"
javinizer genre list
```

**Start API server**:
```bash
# Start API server (default: localhost:8080)
javinizer api

# Custom host/port
javinizer api --host 0.0.0.0 --port 9000
```

## Execute Locally (Simple)

```bash
# 1) Install deps
make deps

# 2) Build binary
make build

# 3) Initialize config/data
./bin/javinizer init

# 4) Run API + web UI (http://localhost:8080)
./bin/javinizer api
```

For local metadata processing in terminal:
```bash
./bin/javinizer sort /path/to/media --dry-run
./bin/javinizer sort /path/to/media
```

## Deploy (Simple)

### Docker Runtime

```bash
docker run --rm \
  -p 8080:8080 \
  -v $(pwd)/javinizer-data:/javinizer \
  -v /path/to/media:/media \
  ghcr.io/javinizer/javinizer-go:v0.1.0-alpha
```

Then open `http://localhost:8080`.

### Release Deployment

1. Update `internal/version/version.txt`.
2. Push a tag like `v0.1.0-alpha` (or stable `v0.1.0`).
3. GitHub Actions `CLI Binary Release` publishes binaries + Docker image + release assets.

## Documentation

Comprehensive documentation available in the `/docs` folder:

1. **[Getting Started](./docs/01-getting-started.md)** - Installation and first steps
2. **[Configuration](./docs/02-configuration.md)** - Complete configuration reference
3. **[CLI Reference](./docs/03-cli-reference.md)** - All commands and options
4. **[Template System](./docs/04-template-system.md)** - Customize naming formats
5. **[Genre Management](./docs/05-genre-management.md)** - Genre replacement guide
6. **[Database Schema](./docs/06-database-schema.md)** - Database structure and queries
7. **[API Reference](./docs/07-api-reference.md)** - REST API reference
8. **[Migration Guide](./docs/08-migration-guide.md)** - From PowerShell version
9. **[Development](./docs/09-development.md)** - Contributing guide
10. **[Troubleshooting](./docs/10-troubleshooting.md)** - Common issues and solutions
11. **[TUI Guide](./docs/11-tui.md)** - Interactive Terminal User Interface
12. **[Testing Guide](./docs/12-testing-guide.md)** - Test coverage, CI/CD, and best practices
13. **[Local CI Testing](./docs/13-local-ci-testing.md)** - Run GitHub Actions checks locally

## Project Structure

```
javinizer-go/
├── cmd/
│   └── javinizer/        # Main application (CLI + API server)
├── internal/
│   ├── aggregator/       # Metadata aggregation
│   ├── config/           # Configuration management
│   ├── database/         # Database layer (GORM)
│   ├── downloader/       # Media downloads
│   ├── history/          # Operation history tracking
│   ├── logging/          # Structured logging (logrus)
│   ├── matcher/          # File/ID matching
│   ├── models/           # Data models
│   ├── nfo/              # NFO generation
│   ├── organizer/        # File organization
│   ├── scanner/          # File scanning
│   ├── scraper/          # Scrapers (R18.dev, DMM, JavDB, ...)
│   ├── template/         # Template engine
│   ├── tui/              # Terminal User Interface (Bubble Tea)
│   └── worker/           # Worker pool and task execution
├── configs/              # Default configuration
├── data/                 # Runtime data (database)
├── docs/                 # Documentation
└── README.md             # This file
```

## Configuration

Javinizer uses YAML configuration (`configs/config.yaml`):

```yaml
scrapers:
  r18dev:
    enabled: true
  dmm:
    enabled: true

metadata:
  priority:
    title: [r18dev, dmm]
    actress: [r18dev, dmm]
    description: [dmm, r18dev]

output:
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"
  file_format: "<ID>"
  subfolder_format: []  # e.g., ["<YEAR>", "<STUDIO>"] for nested organization
  download_cover: true
  download_poster: true
  download_extrafanart: false

file_matching:
  extensions: [.mp4, .mkv, .avi, .wmv, .flv]
  exclude_patterns: ["*-trailer*", "*-sample*"]

performance:
  max_workers: 5          # Concurrent tasks for TUI
  worker_timeout: 300     # Task timeout (seconds)
  buffer_size: 100        # Progress update buffer
```

See [Configuration Guide](./docs/02-configuration.md) for all options.

## Examples

### Organize Files with Media Downloads

```bash
javinizer sort ~/Videos \
  --recursive \
  --download \
  --nfo
```

### Move Files to New Location

```bash
javinizer sort ~/Downloads \
  --dest ~/Library \
  --move \
  --dry-run  # Preview first
```

### Custom Genre Names

```bash
javinizer genre add "Creampie" "Cream Pie"
javinizer genre add "3P" "Threesome"
javinizer genre add "Beautiful Girl" "Beauty"
```

### Batch Scraping

```bash
javinizer scrape IPX-535
javinizer scrape SSIS-123
javinizer scrape ABW-001

# Now sorting uses cached metadata (instant)
javinizer sort ~/Videos
```

## Releases

Javinizer supports stable releases, prereleases, and snapshot/manual releases via GitHub Actions.

- Update `internal/version/version.txt` before cutting a stable or prerelease tag. Release CI validates that the tracked version matches the tag being published.
- **Stable release**: push tag like `v1.2.3`
- **Pre-release**: push tag like `v1.2.3-rc.1` (or `-alpha`, `-beta`)
- **Manual snapshot**: run `CLI Binary Release` workflow via `workflow_dispatch`

All release builds publish:
- Cross-platform CLI binaries (Linux/macOS/Windows)
- SHA256 checksums
- Multi-arch Docker image to GHCR (`linux/amd64`, `linux/arm64`)

Docker image tags include:
- Version tag (for example `v1.2.3` or `v1.2.3-rc.1`)
- Commit SHA tag (`sha-<shortsha>`)
- Stable releases also publish `latest`, `v<major>`, and `v<major>.<minor>`

Example GHCR image:
```bash
docker pull ghcr.io/javinizer/javinizer-go:v1.2.3
```

## Performance

Javinizer Go is significantly faster than the PowerShell version:

| Operation | PowerShell | Go | Improvement |
|-----------|-----------|-----|-------------|
| Scraping | ~5s per ID | ~1.5s per ID | 3x faster |
| File operations | Slow | Fast | 10x faster |
| Database queries | Slow (CSV) | Instant (SQLite) | 100x faster |
| Startup | ~2s (module loading) | Instant | - |

## Advantages Over PowerShell Version

- ⚡ **Much faster** - Native compilation, concurrent operations
- 🔧 **Single binary** - No dependencies, easy deployment
- 🌍 **Cross-platform** - Linux, macOS, Windows
- 💾 **Database caching** - SQLite for instant lookups
- 🎯 **Type-safe** - Compile-time error checking
- 🔄 **Modern architecture** - Clean, maintainable code

## Development Status

### Completed ✅

- Multi-source scraping (R18.dev, DMM)
- Metadata aggregation with configurable priority
- File scanning and matching (regex support)
- Template-based organization with conditional logic
- NFO generation (Kodi/Plex-compatible)
- Media downloads (cover, poster, screenshots, trailer, actress)
- Genre replacement system (database-backed)
- Database caching (SQLite with GORM)
- History tracking with CLI commands
- File logging (logrus, configurable output)
- CLI interface with verbose mode
- **Interactive TUI with concurrent processing**
- **Worker pool for parallel task execution**
- **Real-time progress tracking and statistics**
- **REST API server** (`javinizer api`)
- **Web frontend** for browsing, review, history, and settings
- Comprehensive documentation (13 guides)
- Integration and unit testing

### Planned 📋
- Additional scrapers and provider enhancements
- Batch processing improvements
- Plugin system
- Release automation refinements

## Testing

Javinizer maintains high code quality with automated testing and continuous integration.

### Quick Test Commands

```bash
# Run all tests
make test

# Run tests with coverage report
make coverage

# Run faster local coverage report (short mode)
make coverage-fast

# View coverage in browser
make coverage-html

# Check if coverage meets threshold (75%)
make coverage-check

# Run race detector on concurrent code
make test-race

# Run full CI suite locally
make ci
```

### Coverage Status

- **Current Coverage:** 75%+ threshold enforced in CI
- **Target Coverage:** 75%+
- **Critical Packages:** 85%+ (worker, aggregator, matcher, organizer)

### Development Tools

The project uses `go run` to execute development tools (like `go-acc` for strict coverage) without requiring global installation. All tool dependencies are tracked in `tools.go` and `go.mod`, ensuring consistent versions across all environments and CI/CD.

**Setup:**
```bash
# Download all dependencies (including dev tools)
make deps

# That's it! Tools work automatically via go run
make coverage       # Strict CI/release coverage (go-acc)
make coverage-fast  # Faster local coverage iteration
```

### Pre-commit Hooks

Install the pre-commit hook to catch issues before committing:

```bash
cp scripts/pre-commit.sample .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

The hook automatically checks:
- Code formatting (`gofmt`)
- Static analysis (`go vet`)
- Fast unit tests
- Build verification

For complete testing documentation, see **[Testing Guide](./docs/12-testing-guide.md)**.

## Contributing

Contributions welcome!

1. Fork the repository
2. Create your feature branch
3. Make your changes
4. **Add tests** (ensure `make coverage-check` passes)
5. Run `make ci` to verify all checks pass
6. Submit a pull request

See **[Development Guide](./docs/09-development.md)** and **[Testing Guide](./docs/12-testing-guide.md)** for details.

## Compatibility

### NFO Files

✅ Fully compatible with Kodi and Plex

### PowerShell Javinizer

✅ Can be used alongside PowerShell version
❌ Database not compatible (different systems)

## License

This project is a recreation of the original [Javinizer](https://github.com/jvlflame/Javinizer) in Go.

## Links

- **Documentation**: [docs/](./docs/01-getting-started.md)
- **Issues**: https://github.com/javinizer/javinizer-go/issues
- **Original Javinizer**: https://github.com/jvlflame/Javinizer
- **Go**: https://go.dev

## Support

- 📖 [Documentation](./docs/01-getting-started.md)
- 🐛 [Report Issues](https://github.com/javinizer/javinizer-go/issues)
- 💬 [Discussions](https://github.com/javinizer/javinizer-go/discussions)
- 🔧 [Troubleshooting Guide](./docs/10-troubleshooting.md)

---

Made with ❤️ using Go
