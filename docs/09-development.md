# Development Guide

Guide for contributing to and developing Javinizer Go.

## Project Structure

```
javinizer-go/
├── cmd/
│   └── javinizer/        # CLI + API entrypoint
├── internal/
│   ├── aggregator/       # Metadata aggregation
│   ├── api/              # API handlers
│   ├── config/           # Configuration management
│   ├── database/         # Database layer (GORM)
│   ├── downloader/       # Media downloads
│   ├── history/          # History tracking
│   ├── httpclient/       # HTTP client + FlareSolverr support
│   ├── image/            # Image processing
│   ├── imageutil/        # Image utilities
│   ├── logging/          # Logging
│   ├── matcher/          # File/ID matching
│   ├── mediainfo/        # MediaInfo extraction
│   ├── models/           # Data models
│   ├── nfo/              # NFO generation
│   ├── organizer/        # File organization
│   ├── scanner/          # File scanning
│   ├── scraper/          # Scrapers
│   ├── template/         # Template engine
│   ├── translation/      # Translation service
│   ├── tui/              # Terminal UI
│   ├── version/          # Version metadata
│   ├── websocket/        # Websocket hub
│   └── worker/           # Concurrent workers
├── web/                  # Frontend source
├── configs/              # Default configuration
├── data/                 # Runtime data
├── docs/                 # Documentation
└── scripts/              # Dev/CI helper scripts
```

## Development Setup

### Prerequisites

- Go 1.25+
- Git
- SQLite3 (for DB inspection)

### Setup

```bash
# Clone repository
git clone https://github.com/javinizer/javinizer-go.git
cd javinizer-go

# Install dependencies
go mod download

# Build
go build -o bin/javinizer ./cmd/javinizer

# Run
./bin/javinizer --help
```

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test ./... -cover

# Specific package
go test ./internal/matcher

# Verbose
go test ./... -v
```

## Adding a New Scraper

### 1. Create Scraper Package

```go
// internal/scraper/newscraper/newscraper.go
package newscraper

import (
    "github.com/javinizer/javinizer-go/internal/config"
    "github.com/javinizer/javinizer-go/internal/models"
)

type Scraper struct {
    config *config.ScraperConfig
    client *http.Client
}

func New(cfg *config.Config) *Scraper {
    return &Scraper{
        config: &cfg.Scrapers.NewScraper,
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (s *Scraper) Name() string {
    return "newscraper"
}

func (s *Scraper) IsEnabled() bool {
    return s.config.Enabled
}

func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
    // Implement scraping logic
    return &models.ScraperResult{
        ID:    id,
        Title: "...",
        // ... other fields
    }, nil
}

func (s *Scraper) GetURL(id string) string {
    return fmt.Sprintf("https://newscraper.com/movie/%s", id)
}
```

### 2. Register Scraper

```go
// cmd/javinizer/root.go
import "github.com/javinizer/javinizer-go/internal/scraper/newscraper"

registry := models.NewScraperRegistry()
registry.Register(r18dev.New(cfg))
registry.Register(dmm.New(cfg))
registry.Register(newscraper.New(cfg))  // Add here
```

## Building and Releasing

### Build for Current Platform

```bash
go build -o bin/javinizer ./cmd/javinizer
```

### Cross-Compile

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o bin/javinizer-linux-amd64 ./cmd/javinizer

# macOS
GOOS=darwin GOARCH=amd64 go build -o bin/javinizer-darwin-amd64 ./cmd/javinizer

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/javinizer-windows-amd64.exe ./cmd/javinizer
```

### Release Workflow (GitHub Actions)

Release automation is handled by `.github/workflows/cli-release.yml`.

1. Update `internal/version/version.txt` with the intended version.
2. Push a semver tag for release builds:
   - Stable: `vX.Y.Z`
   - Pre-release: `vX.Y.Z-alpha`, `vX.Y.Z-beta`, `vX.Y.Z-rc.1`, etc.
3. Workflow builds artifacts and publishes GitHub release assets.

Manual dispatch (`workflow_dispatch`) also supports snapshot/stable/prerelease runs.

### Nightly Builds

- Nightly schedule runs daily at `00:00 UTC`.
- Nightly runs are skipped when no release-impacting changes are detected in the previous 24 hours.
- Nightly publishes Docker images only (no GitHub release assets).

### Docker Tagging Rules

Published tags are determined by release type:

- Version tag: always (for example `v0.1.1`, `v0.1.1-alpha`, `v0.1.1-nightly.20260316`)
- `latest`: published for versioned release builds
- Stable-only aliases: `v<major>`, `v<major>.<minor>`
- Nightly aliases: `nightly`, `nightly-YYYYMMDD`, and `sha-<shortsha>`

### CI Quality Gates

Main CI checks include:

- Unit/integration tests
- Coverage threshold enforcement
- Race detector tests
- Linting/static analysis
- Build and Docker verification

## Contributing

### Workflow

1. Fork the repository
2. Create feature branch: `git checkout -b feature/my-feature`
3. Make changes
4. Run tests: `go test ./...`
5. Commit: `git commit -m "Add my feature"`
6. Push: `git push origin feature/my-feature`
7. Create Pull Request

## Resources

- **Go Documentation**: https://go.dev/doc/
- **GORM Documentation**: https://gorm.io/docs/
- **Cobra Documentation**: https://github.com/spf13/cobra
- **Original Javinizer**: https://github.com/jvlflame/Javinizer

---

**Next**: [Troubleshooting](./10-troubleshooting.md)
