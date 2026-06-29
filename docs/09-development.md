# Development Guide

Guide for contributing to and developing Javinizer Go.

## Project Structure

```
javinizer-go/
├── cmd/
│   ├── coveragecheck/ # Coverage threshold checker CLI
│   ├── javinizer/     # CLI + API entrypoint
│   └── javinizer-e2e/ # End-to-end test binary
├── internal/
│   ├── aggregator/      # Metadata aggregation
│   ├── api/             # API server, handlers & domain packages
│   ├── challengedetect/ # Cloudflare challenge detection
│   ├── commandutil/     # Cobra command & dependency wiring helpers
│   ├── config/          # Configuration management
│   ├── coverage/        # Coverage report helpers
│   ├── database/        # Database layer (GORM)
│   ├── downloader/      # Media downloads
│   ├── enumutil/        # Generic string-backed enum helpers
│   ├── eventlog/        # Structured event log emission & stats
│   ├── formatter/       # Movie metadata formatting
│   ├── fsutil/          # Filesystem helpers (afero)
│   ├── history/         # History tracking
│   ├── httpclient/      # HTTP client + FlareSolverr support
│   ├── imageutil/       # Image utilities
│   ├── logging/         # Logging
│   ├── matcher/         # File/ID matching
│   ├── mediainfo/       # MediaInfo extraction
│   ├── mocks/           # Generated mockery mocks (do not edit)
│   ├── models/          # Data models
│   ├── nfo/             # NFO generation
│   ├── operationmode/   # OperationMode type (CLI/API/TUI)
│   ├── organizer/       # File organization
│   ├── panicutil/       # Shared panic recovery utilities
│   ├── poster/          # Poster image generation & management
│   ├── ratelimit/       # Rate limiting
│   ├── scanner/         # File scanning
│   ├── scrape/          # Scrape orchestration (cache, provenance, translation)
│   ├── scraper/         # Scraper implementations & registration
│   ├── scraperconfig/   # Scraper configuration types
│   ├── scraperutil/     # Scraper registry & shared utilities
│   ├── ssrf/            # SSRF protection / URL validation
│   ├── template/        # Template engine
│   ├── testutil/        # Shared test utilities
│   ├── translation/     # Translation service
│   ├── tui/             # Terminal UI
│   ├── update/          # Self-update checking & service
│   ├── version/         # Version metadata
│   ├── websocket/       # Websocket hub
│   ├── worker/          # Concurrent workers
│   └── workflow/        # Sort/apply/compare workflow orchestration
├── web/
│   ├── frontend/ # SvelteKit frontend source (npm / Vite)
│   ├── dist/     # Embedded web bundle (built by `make web-build`)
│   └── embed.go  # go:embed of dist/ into the binary
├── configs/              # Default configuration (config.yaml.example)
├── data/                 # Runtime data
├── docs/                 # Documentation
└── scripts/              # Dev/CI helper scripts
```

## Development Setup

### Prerequisites

- Go 1.26+
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

Scrapers implement the `models.Scraper` interface (`internal/models/scraper.go`)
and are registered as metadata via `scraperutil.ScraperRegistration`. The
registry itself is `scraperutil.NewScraperRegistry()` (in `internal/scraperutil`);
each scraper package exposes a `Register(reg scraperutil.ScraperRegistrar)`
function, and `internal/scraper/registration.go` wires every package into the
registry through `RegisterAll`. Per-scraper user settings live under
`scrapers.<name>` in the config and are resolved into
`cfg.Scrapers.Overrides[<name>]` (a `*models.ScraperSettings`) at startup.

### 1. Implement the Scraper interface

The interface requires six methods. Note that `Search` and `GetURL` take a
`context.Context` (for cancellation/timeout through rate limiters and HTTP
clients), `GetURL` returns an error, and `Config()`/`Close()` are required:

```go
// internal/scraper/newscraper/newscraper.go
package newscraper

import (
    "context"
    "fmt"
    "net/http"
    "time"

    "github.com/javinizer/javinizer-go/internal/models"
    "github.com/javinizer/javinizer-go/internal/scraperconfig"
)

type Scraper struct {
    settings *scraperconfig.ScraperSettings
    client   *http.Client
}

func newScraper(settings *scraperconfig.ScraperSettings) *Scraper {
    return &Scraper{
        settings: settings,
        client:   &http.Client{Timeout: 30 * time.Second},
    }
}

func (s *Scraper) Name() string                          { return "newscraper" }
func (s *Scraper) IsEnabled() bool                       { return s.settings != nil && s.settings.Enabled }
func (s *Scraper) Config() *scraperconfig.ScraperSettings { return s.settings }
func (s *Scraper) Close() error                          { return nil }

func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
    // Implement scraping logic, honoring ctx for cancellation/timeouts.
    return &models.ScraperResult{
        Source: "newscraper",
        ID:     id,
        Title:  "...",
        // ... other fields
    }, nil
}

func (s *Scraper) GetURL(ctx context.Context, id string) (string, error) {
    return fmt.Sprintf("https://newscraper.com/movie/%s", id), nil
}
```

### 2. Register the scraper

Add a `module.go` in the package that registers a `ScraperRegistration`
(constructor, defaults, priority, and optional options/validator). The
constructor receives typed `scraperutil.ScraperDeps` and returns the
`models.Scraper` instance — it is called at startup with the resolved
per-scraper settings:

```go
// internal/scraper/newscraper/module.go
package newscraper

import (
    "github.com/javinizer/javinizer-go/internal/config"
    "github.com/javinizer/javinizer-go/internal/models"
    "github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
    reg.Register(scraperutil.ScraperRegistration{
        Name:        "newscraper",
        Description: "New Scraper",
        Priority:    50,
        Defaults: models.ScraperSettings{
            Enabled:   true,
            Language:  "en",
            UserAgent: config.DefaultUserAgent,
        },
        Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
            return newScraper(&deps.Settings), nil
        },
    })
}
```

### 3. Wire it into RegisterAll

Finally, call your `Register` from `internal/scraper/registration.go`, the
single place that enumerates every shipped scraper:

```go
// internal/scraper/registration.go
func RegisterAll(reg scraperutil.ScraperRegistrar) {
    r18dev.Register(reg)
    dmm.Register(reg)
    // ...other scrapers...
    newscraper.Register(reg) // Add here
}
```

## Building and Releasing

### Build for Current Platform

```bash
# Build CLI only (no frontend)
go build -o bin/javinizer ./cmd/javinizer

# Build single binary (API + embedded Web UI) — requires the frontend bundle
make build
```

`make build` depends on `make web-build` (it embeds `web/dist`), so the
frontend must be built first. Run `make web-build` once (or let the `build`
target do it), otherwise the embedded UI falls back to placeholder assets.

### Cross-Compile

For one-off local builds you can set `GOOS`/`GOARCH` directly:

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o bin/javinizer-linux-amd64 ./cmd/javinizer

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o bin/javinizer-darwin-arm64 ./cmd/javinizer

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/javinizer-windows-amd64.exe ./cmd/javinizer
```

For release artifacts, prefer the Makefile targets, which embed the web bundle
(`CGO_ENABLED=1` with release LDFLAGS) and produce a universal macOS binary:

```bash
make build-cli-linux    # bin/javinizer-linux-amd64
make build-cli-darwin   # bin/javinizer-darwin-universal (amd64 + arm64 via lipo)
make build-cli-windows  # bin/javinizer-windows-amd64.exe
make build-cli-all      # all of the above
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

- Version tag: always (for example `v0.1.1`, `v0.1.1-alpha`, `0.0.0-nightly.<sha7>`)
- `latest`: published for versioned release builds
- Stable-only aliases: `v<major>`, `v<major>.<minor>`
- Nightly aliases: `nightly` and `nightly-<full-sha>`

### CI Quality Gates

CI is defined in `.github/workflows/test.yml` and runs 9 jobs in parallel on
every push and pull request. The local gate `make ci` mirrors the Go-side
checks: `vet`, `lint`, `vuln`, `coverage-check`, `test-race`, `config-drift`,
`check-import-guard`, and `check-mocks`. Run `make ci-full` to also run the
frontend suite (`make web-test`), or `make simulate-ci` to replay the GitHub
Actions jobs locally.

Main CI checks include:

- Unit/integration tests (Linux + Windows)
- Coverage threshold enforcement (75% line coverage)
- Race detector tests (`internal/worker`, `internal/tui`, `internal/websocket`, `internal/api`)
- Linting and static analysis (go vet, golangci-lint v2.9.0, gofmt)
- Vulnerability scanning (govulncheck)
- Frontend tests (Vitest)
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

- **golangci-lint** - Comprehensive linter suite (v2.9.0+)
  - Config: `.golangci.yml`
  - Run: `make lint` or `golangci-lint run`
  - CI: Required to pass (pinned to v2.9.0 in `.github/workflows/test.yml`)

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
   - Mockery mocks are up to date

### CI Pipeline

All pull requests trigger the following CI jobs (`.github/workflows/test.yml`),
which run in parallel:

- **Unit Tests & Coverage** (`test`) - Runs all Go tests and enforces the 75% line-coverage threshold
- **Race Detector Tests** (`race-tests`) - Runs the race detector on `internal/worker`, `internal/tui`, `internal/websocket`, and `internal/api`
- **Linting & Code Quality** (`lint`) - Runs go vet, golangci-lint (v2.9.0), gofmt check, and the `internal/api` file-size guardrail
- **Vulnerability Scan** (`vuln`) - Runs `govulncheck ./...`
- **Unit Tests (Windows)** (`test-windows`) - Runs the Go test suite on Windows
- **Frontend Tests** (`frontend-tests`) - Runs the Vitest suite (`npm run test --prefix web/frontend`)
- **Build Verification** (`build`) - Builds the CLI, generates and verifies Swagger docs, verifies mockery mocks are up to date, and verifies the embedded web UI
- **Docker Build Verification** (`docker-build`) - Builds the Docker image and verifies image metadata
- **Fullstack E2E Tests** (`fullstack-e2e`) - Runs the Playwright fullstack E2E suite (`make test-e2e-fullstack`)

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
