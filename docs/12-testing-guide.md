# Testing Guide

This guide covers testing practices, tools, and coverage requirements for the javinizer-go project.

## Table of Contents

- [Running Tests](#running-tests)
- [Coverage Requirements](#coverage-requirements)
- [Test Types](#test-types)
- [Writing Tests](#writing-tests)
- [CI/CD Integration](#cicd-integration)
- [Pre-commit Hooks](#pre-commit-hooks)
- [Troubleshooting](#troubleshooting)

## Running Tests

### Quick Start

```bash
# Run all tests
make test

# Run tests with coverage report (uses go-acc automatically via go run)
make coverage

# View coverage in browser
make coverage-html

# Check if coverage meets threshold (25%)
make coverage-check
```

### Development Tools

This project uses `go run` to execute development tools without requiring global installation. Tools are declared in `tools.go` and tracked in `go.mod`.

**Benefits:**
- ✅ No global installation needed
- ✅ Version-controlled dependencies
- ✅ Consistent across all environments
- ✅ Works in CI/CD automatically

The `make coverage` command automatically runs `go run github.com/ory/go-acc@latest` which provides better coverage aggregation across multi-package projects compared to standard `go test`.

### All Test Commands

| Command | Description | When to Use |
|---------|-------------|-------------|
| `make test` | Run all tests with verbose output | Default test command |
| `make test-short` | Run only fast tests (skips slow integration tests) | Quick validation, pre-commit |
| `make test-race` | Run race detector on concurrent packages | Before committing concurrent code changes |
| `make test-verbose` | Run tests with verbose output and no caching | Debugging test failures |
| `make bench` | Run benchmark tests | Performance testing |
| `make coverage` | Generate coverage.out file | Get coverage data |
| `make coverage-html` | Open HTML coverage report in browser | Visual coverage analysis |
| `make coverage-func` | Display function-by-function coverage breakdown | Identify specific gaps |
| `make coverage-check` | Verify coverage meets 60% threshold | Pre-push validation |
| `make ci` | Run full CI suite (vet + lint + coverage + race) | Before opening PR |

### Running Specific Package Tests

```bash
# Test a specific package
go test ./internal/worker/...

# Test with race detector
go test -race ./internal/worker/...

# Test a specific function
go test -v -run TestPoolSubmit ./internal/worker

# Test with coverage for one package
go test -coverprofile=coverage.out ./internal/matcher/...
go tool cover -html=coverage.out
```

## Coverage Requirements

### Overall Project Coverage

- **Current Baseline:** 60% (enforced in CI)
- **Short-term Goal:** 75%
- **Long-term Target:** 80%+

### Per-Package Coverage Expectations

| Package Category | Target Coverage | Rationale |
|------------------|----------------|-----------|
| **Critical packages** | 85%+ | Core business logic, data integrity |
| - `internal/worker` | 85% | Concurrent task execution, critical for reliability |
| - `internal/aggregator` | 85% | Metadata merging logic |
| - `internal/matcher` | 90% | JAV ID extraction (currently 94.6%) |
| - `internal/organizer` | 85% | File organization, data safety |
| - `internal/scanner` | 85% | File discovery |
| **Important packages** | 70%+ | User-facing features |
| - `internal/scraper/*` | 70% | External data fetching (currently 0% - needs work) |
| - `internal/nfo` | 75% | NFO generation (currently 77.6%) |
| - `internal/downloader` | 75% | Asset downloads (currently 74.2%) |
| **Supporting packages** | 50%+ | Configuration, models, utilities |
| - `internal/config` | 50% | Simple struct initialization |
| - `internal/models` | 50% | Data structures |
| - `internal/template` | 60% | Template rendering (currently 66%) |
| **Minimal coverage acceptable** | 30%+ | UI, CLI, manual testing preferred |
| - `internal/tui` | 30% | Bubble Tea UI (complex to test) |
| - `cmd/cli` | 40% | CLI commands (integration tests preferred) |
| - `internal/api` | 60% | API handlers (currently 0% - needs work) |

### Coverage Gaps to Address

**High Priority** (0% coverage, critical functionality):
1. `internal/scraper/dmm` - DMM scraper implementation
2. `internal/scraper/r18dev` - R18.dev scraper implementation
3. `internal/api` - API handlers (security concern)
4. `cmd/cli` - CLI commands

**Medium Priority** (low coverage):
5. `internal/config` (23.1%) - Configuration loading
6. `internal/database` (23.5%) - Database operations
7. `internal/mediainfo` (22.9%) - Media information parsing

## Test Types

### Unit Tests

Fast, isolated tests for individual functions/methods.

```go
func TestMatchID(t *testing.T) {
    matcher := NewMatcher(config)
    id := matcher.ExtractID("ABC-123.mp4")
    assert.Equal(t, "ABC-123", id)
}
```

**Guidelines:**
- Should run in <1 second per test
- No external dependencies (filesystem, network, database)
- Use table-driven tests for multiple scenarios
- Mark slow tests with `if testing.Short() { t.Skip() }` for use with `make test-short`

### Integration Tests

Test interactions between components or with external resources.

```go
func TestNFOGeneration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    // Test with real config file, real templates
}
```

**Guidelines:**
- Place in `*_integration_test.go` files or use build tags
- Use `testing.Short()` to allow skipping with `-short` flag
- Clean up resources (files, database entries) in test cleanup

### Race Detector Tests

Critical for concurrent code (worker pool, TUI, websockets, API).

```bash
# Run race detector on concurrent packages
make test-race

# Or manually:
go test -race ./internal/worker/...
```

**When to run:**
- Before committing changes to `internal/worker`, `internal/tui`, `internal/websocket`, `internal/api`
- When debugging concurrency issues
- In CI (runs automatically on every PR)

**Note:** Race detector tests are slower; they run in a separate CI job.

## Writing Tests

### Test File Organization

- Test files: `*_test.go` in the same package directory
- Integration tests: `*_integration_test.go` or separate `integration/` subdirectory
- Test data: `testdata/` subdirectory (convention, gitignored if needed)

### Testing Patterns

#### Table-Driven Tests

Recommended for testing multiple scenarios:

```go
func TestExtractID(t *testing.T) {
    tests := []struct {
        name     string
        filename string
        expected string
    }{
        {"Standard format", "ABC-123.mp4", "ABC-123"},
        {"With path", "/videos/ABC-123.mp4", "ABC-123"},
        {"No ID", "random.mp4", ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := ExtractID(tt.filename)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

#### Mock HTTP Clients

For scraper tests (currently missing):

```go
type mockHTTPClient struct {
    response string
    err      error
}

func (m *mockHTTPClient) Get(url string) (*http.Response, error) {
    if m.err != nil {
        return nil, m.err
    }
    return &http.Response{
        Body: io.NopCloser(strings.NewReader(m.response)),
    }, nil
}

func TestDMMScraper(t *testing.T) {
    client := &mockHTTPClient{response: `<html>...</html>`}
    scraper := NewDMMScraper(client)
    // Test scraper logic without hitting real DMM website
}
```

#### Testing Concurrent Code

Use `t.Parallel()` and proper synchronization:

```go
func TestWorkerPool(t *testing.T) {
    pool := NewPool(5)

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            task := NewMockTask(id)
            pool.Submit(task)
        }(i)
    }

    wg.Wait()
    // Verify results
}
```

### Using testify

The project uses `github.com/stretchr/testify` for assertions:

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSomething(t *testing.T) {
    result := DoSomething()

    // assert: test continues on failure
    assert.Equal(t, expected, result)
    assert.NotNil(t, result)

    // require: test stops on failure
    require.NoError(t, err)
    require.NotEmpty(t, result.ID)
}
```

## CI/CD Integration

### GitHub Actions Workflow

The project uses `.github/workflows/test.yml` with 4 parallel jobs:

1. **Unit Tests & Coverage**
   - Runs all tests
   - Generates coverage report
   - Enforces 60% minimum coverage
   - Uploads to Codecov

2. **Race Detector Tests**
   - Runs `-race` on concurrent packages
   - Catches data races in worker pool, TUI, websockets, API

3. **Linting & Code Quality**
   - `go vet`
   - `golangci-lint`
   - Code formatting check

4. **Build Verification**
   - Builds CLI binary
   - Verifies executable creation

### CI Failure Scenarios

| Failure | Cause | Fix |
|---------|-------|-----|
| Coverage check failed | Coverage below 60% | Add tests or justify lower coverage |
| Race detector failure | Data race detected | Fix concurrent access, add mutexes |
| Linting failure | Code quality issues | Run `make lint` and fix issues |
| Formatting failure | Code not formatted | Run `make fmt` |
| Build failure | Compilation errors | Fix build errors |

### Codecov Integration

Coverage reports are uploaded to Codecov on every push/PR.

**Setup:**
1. Sign up at [codecov.io](https://codecov.io)
2. Add `CODECOV_TOKEN` to GitHub repository secrets
3. View coverage reports and trends at codecov.io

**Codecov will:**
- Comment on PRs with coverage changes
- Fail PR if coverage drops significantly
- Track coverage trends over time
- Highlight uncovered lines

## Pre-commit Hooks

Install the pre-commit hook to catch issues before committing:

```bash
# One-time setup
cp scripts/pre-commit.sample .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

### What the Hook Checks

1. **Code Formatting** - Fails if code is not `gofmt`-formatted
2. **Go Vet** - Fails if `go vet` finds issues
3. **Fast Unit Tests** - Runs `go test -short` (30s timeout)
4. **Build Verification** - Ensures code compiles

### Bypassing the Hook

For emergencies only:

```bash
git commit --no-verify -m "WIP: emergency fix"
```

**Note:** CI will still enforce all checks, so this only defers validation.

## Troubleshooting

### Coverage Report Not Generated

```bash
# Ensure coverage.out exists
ls -la coverage.out

# Regenerate coverage
make coverage

# If go-acc is missing, install it
go install github.com/ory/go-acc@latest
```

### Race Detector Failures

```bash
# Run race detector locally
make test-race

# Or on specific package
go test -race -v ./internal/worker/...

# Common causes:
# - Unprotected shared variables
# - Missing mutex locks
# - Channel send/receive races
```

### Tests Timing Out

```bash
# Increase timeout
go test -timeout=5m ./...

# Or skip slow tests
go test -short ./...
```

### Coverage Check Failing Locally but Passing in CI

```bash
# Ensure you're using same coverage threshold
./scripts/check_coverage.sh 60 coverage.out

# Check if go-acc is installed
command -v go-acc

# Regenerate with go-acc
go-acc -covermode=count -coverprofile=coverage.out ./...
```

### Pre-commit Hook Not Running

```bash
# Check if hook is executable
ls -la .git/hooks/pre-commit

# Make executable
chmod +x .git/hooks/pre-commit

# Verify hook content
cat .git/hooks/pre-commit
```

## Best Practices

1. **Write tests first** for new features (TDD)
2. **Run tests locally** before pushing (`make test`, `make coverage-check`)
3. **Use table-driven tests** for multiple scenarios
4. **Test error cases** not just happy paths
5. **Keep tests fast** - unit tests should be <1s each
6. **Mark slow tests** with `testing.Short()` checks
7. **Test concurrent code** with `-race` detector
8. **Mock external dependencies** (HTTP clients, filesystems)
9. **Clean up test resources** in `defer` or `t.Cleanup()`
10. **Document complex test setups** with comments

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Testify Documentation](https://github.com/stretchr/testify)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [Table-Driven Tests](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [go-acc Coverage Tool](https://github.com/ory/go-acc)

## Contributing

When adding new features:

1. Write tests covering the new functionality
2. Ensure `make coverage-check` passes (60%+ coverage)
3. Run `make test-race` if your code involves concurrency
4. Run `make ci` to verify all CI checks pass locally
5. Include test coverage information in your PR description

**Example PR Description:**
```
## Changes
- Added new scraper for XYZ site

## Testing
- Added unit tests for scraper (85% coverage)
- Tested with mock HTTP responses
- Ran `make ci` successfully

## Coverage Impact
- Overall coverage: 62% → 64% (+2%)
- New package coverage: 85%
```
