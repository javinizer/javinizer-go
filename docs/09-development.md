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
    config *config.ScraperSettings
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

1. Push a semver tag for release builds:
   - Stable: `vX.Y.Z`
   - Pre-release: `vX.Y.Z-alpha`, `vX.Y.Z-beta`, `vX.Y.Z-rc.1`, etc.
2. Workflow builds artifacts and publishes GitHub release assets.

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

### Internal API Structure

For `internal/api` file organization conventions and size guardrails, see:

- [Internal API Organization](./15-internal-api-organization.md)

## Code Style

### Linting and Formatting Tools

The project uses the following tools for code quality:

- **gofmt** - Standard Go formatter
  - Config: Built-in Go formatting rules
  - Run: `make fmt` or `gofmt -w .`
  - CI: Checked in `.github/workflows/test.yml`

- **go vet** - Static analysis for suspicious constructs
  - Config: Built-in Go vet rules
  - Run: `make vet` or `go vet ./...`
  - CI: Required to pass in CI pipeline

- **golangci-lint** - Comprehensive linter suite (v2.4.0+)
  - Config: `.golangci.yml`
  - Run: `make lint` or `golangci-lint run`
  - CI: Required to pass with 5m timeout

### Run Commands

```bash
# Format all code
make fmt

# Run static analysis
make vet

# Run comprehensive linting
make lint

# Run all quality checks
make ci
```

### Go Code Style Guidelines

**Imports:** Grouped with blank lines separating stdlib, external, and internal packages:
```go
import (
    "context"
    "fmt"
    
    "github.com/gin-gonic/gin"
    "gopkg.in/yaml.v3"
    
    "github.com/javinizer/javinizer-go/internal/config"
)
```

**Naming Conventions:**
- Files: `lowercase.go`, test files: `package_test.go`
- Public identifiers: `PascalCase`
- Private identifiers: `camelCase`
- Interfaces: `PascalCase` + `Interface` suffix (e.g., `MovieRepositoryInterface`)
- Constants: `PascalCase` for exported, `camelCase` for private

**Error Handling:** Always wrap errors with context:
```go
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

**Function Signatures:** Context first, options pattern for optional parameters:
```go
func ProcessFile(ctx context.Context, path string, opts *Options) error

type Options struct {
    Timeout time.Duration
    Retry   int
}
```

### CI Enforcement

All code style checks are enforced in CI:
- **Formatting check** - `gofmt -l .` must show no output
- **Vet check** - `go vet ./...` must pass
- **Lint check** - `golangci-lint run` must pass
- Pull requests will fail if any check fails

## Branch Conventions

### Main Branch

The default branch is `main` (not `master`). All pull requests should target `main`.

### Branch Naming Patterns

Use descriptive branch names with the following prefixes:

- `feat/` - New features (e.g., `feat/add-merge-ui-for-duplicate`)
- `fix/` - Bug fixes (e.g., `fix/scraper-timeout`)
- `refactor/` - Code refactoring (e.g., `refactor/cli-structure`)
- `test/` - Test improvements (e.g., `test/improve-coverage-to-75`)
- `docs/` - Documentation updates (e.g., `docs/api-reference`)

### Commit Message Format

Use conventional commits format:

```
<type>: <description>
```

Types:
- `feat:` - New feature
- `fix:` - Bug fix
- `test:` - Test additions/modifications
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `style:` - Formatting, no logic changes
- `chore:` - Maintenance tasks

With optional scope:
```
feat(scraper): add support for new site
fix(batch): resolve race condition in job processing
```

## PR Process

### Pull Request Requirements

1. **Branch naming** - Use appropriate prefix (`feat/`, `fix/`, `refactor/`, `test/`)
2. **Commit messages** - Follow conventional commits format
3. **Code quality** - All CI checks must pass:
   - Unit tests pass (`go test ./...`)
   - Coverage threshold met (75% line coverage)
   - Race detector tests pass for concurrent code
   - Linting passes (`make lint`)
   - Build succeeds (`make build`)
   - Swagger documentation is up to date

### CI Pipeline

All pull requests trigger the following CI jobs (`.github/workflows/test.yml`):

- **Unit Tests & Coverage** - Runs all tests and checks 75% coverage threshold
- **Race Detector Tests** - Tests concurrent packages with race detector
- **Linting & Code Quality** - Runs go vet, golangci-lint, and format check
- **Build Verification** - Builds binary, generates Swagger docs, and verifies embedded web UI
- **Docker Build Verification** - Builds Docker image and verifies metadata

### Pre-commit Checklist

Before submitting a PR, run locally:

```bash
# Quick checks
make test-short

# Full CI locally
make ci

# Or simulate exact GitHub Actions
make simulate-ci
```

### Pull Request Workflow

1. Fork the repository (if you don't have write access)
2. Create a feature branch with appropriate prefix
3. Make your changes following code style guidelines
4. Run tests locally: `make test`
5. Commit with conventional commit message
6. Push to your fork
7. Open a pull request against `main`
8. Wait for CI checks to pass
9. Address any review feedback

### After Merge

- PRs are squash-merged to maintain clean history
- Branch is automatically deleted after merge
- Changes will be included in the next release

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
