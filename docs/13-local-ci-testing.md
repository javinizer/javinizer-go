# Local CI Testing Guide

This guide explains how to run GitHub Actions CI checks locally before pushing code to GitHub.

## Quick Start

### Best Option: Simulate Full CI Pipeline

```bash
make simulate-ci
```

This runs a **7-job subset** of the GitHub Actions pipeline locally with colored output (`scripts/simulate-ci.sh` prints `Job 1/7` … `Job 7/7`):
- ✅ Unit Tests & Coverage
- ✅ Race Detector Tests
- ✅ Linting & Code Quality
- ✅ Vulnerability Scan
- ✅ Frontend Tests
- ✅ Build Verification
- ✅ Docker Build Verification

> **Note:** the real `.github/workflows/test.yml` defines **9 jobs** — the simulation omits `test-windows` (needs a Windows runner) and `fullstack-e2e` (needs Playwright browsers + a live Go/Vite stack). See [CI/CD in GitHub](#cicd-in-github) below for the full list.

**Example output:**
```
========================================
  Simulating GitHub Actions CI Locally
========================================

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Job 1/7: Unit Tests & Coverage
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

→ Running unit tests...
✓ Unit tests passed

→ Generating coverage report...
✓ Coverage report generated

→ Checking Codecov-compatible coverage threshold (75%)...
✓ Coverage check PASSED
  78.4% >= 75%

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Job 2/7: Race Detector Tests
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

→ Running race detector on concurrent packages...
✓ Race detector tests passed

...

========================================
  CI Simulation Summary
========================================

✓ All checks passed!
Your code is ready to push to GitHub
```

## All Testing Options

### 1. Full CI Simulation (Recommended)

```bash
# Run all CI checks with pretty output
make simulate-ci

# Same as above (runs scripts/simulate-ci.sh)
./scripts/simulate-ci.sh
```

**What it checks** (one block per simulated job):
- Dependencies download + unit tests + coverage report (75% Codecov-compatible line threshold via `./scripts/check_coverage.sh`)
- Race detector on concurrent packages (`internal/worker`, `internal/tui`, `internal/websocket`, `internal/api`)
- `go vet` static analysis + `golangci-lint` (if installed) + `gofmt -l .` formatting check
- `govulncheck` vulnerability scan (uses `go run golang.org/x/vuln/cmd/govulncheck@latest` if the binary isn't installed)
- Frontend tests (`npm ci` + Vitest in `web/frontend/`) — skipped if Node.js is unavailable
- Build success and `bin/javinizer` binary creation
- Docker image build + `javinizer version --short` metadata verification

**When to use:**
- Before pushing to GitHub
- Before opening a pull request
- After making significant changes
- To verify everything works together

### 2. Quick CI Check

```bash
# Run core CI checks (no fancy output)
make ci
```

**What it runs** — the `ci` target in the `Makefile` declares **8 prerequisites**:

```makefile
ci: vet lint vuln coverage-check test-race config-drift check-import-guard check-mocks
```

| # | Target | What it enforces |
|---|--------|------------------|
| 1 | `vet` | `go vet ./...` static analysis |
| 2 | `lint` | `golangci-lint run` |
| 3 | `vuln` | `govulncheck ./...` (via `go run golang.org/x/vuln/cmd/govulncheck@latest`) |
| 4 | `coverage-check` | `make coverage` + `./scripts/check_coverage.sh 75 coverage.out` (75% line coverage) |
| 5 | `test-race` | race detector on `internal/worker`, `internal/tui`, `internal/websocket`, `internal/api` |
| 6 | `config-drift` | `./scripts/validate-config-sync.sh` — defaults stay in sync with `configs/config.yaml.example` |
| 7 | `check-import-guard` | `./scripts/check_import_guard.sh` — `internal/models` must not import `internal/config` (ADR-0020) |
| 8 | `check-mocks` | regenerates mockery mocks; fails if `internal/mocks/` is out of date |

> **Frontend:** `make ci` does **not** run frontend tests. Use `make ci-full` (= `ci` + `web-test`) to add the Vitest suite. The `fullstack-e2e` job is not part of either target — run it explicitly with `make test-e2e-fullstack`.

**When to use:**
- Quick verification during development
- After fixing specific issues
- When you don't need detailed output

### 3. Individual Commands

Run specific checks independently:

```bash
# Unit tests only
make test

# Fast tests (skips slow integration tests)
make test-short

# Race detector
make test-race

# Code formatting
make fmt

# Static analysis
make vet

# Linting
make lint

# Coverage
make coverage
make coverage-pkg      # Per-package breakdown
make coverage-html      # View in browser
make coverage-check     # Check threshold
```

**When to use:**
- Targeted testing during development
- Fixing specific issues (e.g., just formatting)
- When you need speed over completeness

### 4. Using `act` (Advanced)

Run the **actual** `.github/workflows/test.yml` file using Docker:

```bash
# Install act
brew install act

# Run all workflows
act

# Run specific workflow
act -W .github/workflows/test.yml

# Run a specific job — valid job ids in test.yml:
#   test, race-tests, lint, vuln, test-windows,
#   frontend-tests, build, docker-build, fullstack-e2e
act -j test
act -j vuln
act -j fullstack-e2e

# Dry run (see what would happen without running)
act -n

# List available jobs
act -l
```

**Output example:**
```
[Test & Coverage/test] 🚀  Start image=catthehacker/ubuntu:act-latest
[Test & Coverage/test]   🐳  docker pull image=catthehacker/ubuntu:act-latest
[Test & Coverage/test]   🐳  docker create image=catthehacker/ubuntu:act-latest
[Test & Coverage/test]   🐳  docker run image=catthehacker/ubuntu:act-latest
```

> **Note:** `test.yml` detects `act` via the `ACT` env var and skips the Codecov upload and coverage-report artifact steps (it prints the coverage summary locally instead). The `test` job still runs fully — only the upload steps are gated.

**Pros:**
- Most accurate (runs actual workflow file)
- Catches workflow syntax errors
- Tests GitHub Actions-specific features

**Cons:**
- Requires Docker (slow to start)
- Large Docker images (~2GB)
- Some GitHub features not supported (secrets, etc.)
- Slower than other methods

**When to use:**
- Testing workflow file changes
- Debugging complex GitHub Actions features
- Validating custom actions
- When you need exact CI environment

## Comparison Table

| Method | Speed | Accuracy | Output | Best For |
|--------|-------|----------|--------|----------|
| `make simulate-ci` | ⭐⭐⭐ Medium | ⭐⭐⭐⭐ High | ⭐⭐⭐⭐⭐ Beautiful | Pre-push checks |
| `make ci` | ⭐⭐⭐⭐ Fast | ⭐⭐⭐⭐ High | ⭐⭐ Basic | Quick verification |
| Individual commands | ⭐⭐⭐⭐⭐ Fastest | ⭐⭐⭐ Medium | ⭐⭐ Basic | Targeted fixes |
| `act` | ⭐ Very Slow | ⭐⭐⭐⭐⭐ Exact | ⭐⭐⭐ Detailed | Workflow testing |

## Typical Development Workflow

### During Development

```bash
# Fast feedback loop
make test-short
```

### Before Committing

The pre-commit hook (`scripts/pre-commit.sample`) runs **8 checks** and blocks forbidden paths (e.g. local-only planning dirs) from being committed:

| # | Check | Command |
|---|-------|---------|
| 1 | Go formatting | `gofmt -l .` |
| 2 | golangci-lint | `$HOME/go/bin/golangci-lint run ./...` (≥ v2.4.0; skipped if not installed) |
| 3 | go vet | `go vet ./...` |
| 4 | Fast unit tests | `go test -short -timeout=60s ./...` |
| 5 | Build verification | `go build -o /tmp/javinizer-test ./cmd/javinizer` |
| 6 | Swagger docs | `make swagger` then `git diff --quiet -- docs/swagger/` (regen must be committed) |
| 7 | Frontend formatting | `npx prettier --check` on staged `web/frontend/**` files (skipped without `node_modules`) |
| 8 | Frontend types | `npx svelte-check --threshold error` when `web/frontend/src/` changes |

Checks 7–8 only run when staged frontend files are present; checks 2, 6, 7, and 8 degrade gracefully (skip with a warning) if their tooling isn't installed.

To bypass (use sparingly):
```bash
git commit --no-verify -m "WIP"
```

### Before Pushing

```bash
# Full CI simulation
make simulate-ci
```

If any checks fail, the script will tell you exactly what to fix:
```
✗ 2 check(s) failed:
  - Lint - Formatting
  - Unit Tests - Coverage Threshold

Fix these issues before pushing to GitHub
```

### After Fixing Issues

```bash
# Run just the failing check
make fmt                # If formatting failed
make coverage-check     # If coverage failed

# Then re-run full simulation
make simulate-ci
```

## Troubleshooting

### `golangci-lint` Not Installed

If you see:
```
⚠ golangci-lint not installed (skipping)
  Install: brew install golangci-lint
```

Install it:
```bash
# macOS
brew install golangci-lint

# Linux
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Or using go install
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Tests Timing Out

`simulate-ci.sh` runs `go test -v ./...` with Go's default 10-minute per-package timeout (the pre-commit hook uses `-timeout=60s`). To run a faster or longer-budget subset:
```bash
# Just race tests (can be slow)
make test-race

# All tests with an explicit longer timeout
go test -timeout=15m ./...

# A single package, quickly
go test -short -timeout=60s ./internal/matcher/...
```

### Coverage Check Failing

If coverage is below 75%, you have options:
```bash
# View coverage to see what needs tests
make coverage-html

# Check per-package breakdown
make coverage-pkg

# Run with lower threshold temporarily
./scripts/check_coverage.sh 20 coverage.out

# Or update the threshold (keep both files in sync):
#   Makefile:                  ./scripts/check_coverage.sh 75 coverage.out
#   .github/workflows/test.yml: ./scripts/check_coverage.sh 75 coverage.out
```

### Race Detector Failures

Race conditions found:
```bash
# Run race detector with verbose output
go test -race -v ./internal/worker/...

# Common issues:
# - Unprotected shared variables
# - Missing mutex locks
# - Channel send/receive races
```

### Build Failures

```bash
# Clean build cache
go clean -cache

# Remove old binaries
make clean

# Try build again
make build
```

### `act` Docker Issues

```bash
# Pull latest runner image
docker pull catthehacker/ubuntu:act-latest

# Clear Docker cache if issues persist
docker system prune -a
```

## CI/CD in GitHub

When you push to GitHub, the workflow runs automatically:

1. **Triggered on:**
   - Push to `master`, `main`, or `develop` branches
   - Pull requests to these branches

2. **Jobs run in parallel** (9 jobs in `.github/workflows/test.yml`):
   - `test` — Unit Tests & Coverage (uploads to Codecov; `timeout-minutes: 20`)
   - `race-tests` — Race Detector Tests (`timeout-minutes: 30`)
   - `lint` — Linting & Code Quality: `go vet`, `internal/api` 700-line size guardrail, `golangci-lint` v2.9.0, `gofmt` check (`timeout-minutes: 15`)
   - `vuln` — Vulnerability Scan via `govulncheck@v1.5.0` (`timeout-minutes: 10`)
   - `test-windows` — Unit Tests (Windows), `go test -short ./...` on `windows-latest` (`timeout-minutes: 25`)
   - `frontend-tests` — Frontend Tests, Node 22 + Vitest (`timeout-minutes: 15`)
   - `build` — Build Verification: `make build`, Swagger regen + drift check, mockery `check-mocks`, binary + embedded-web-UI smoke test (`timeout-minutes: 15`)
   - `docker-build` — Docker Build Verification + image label/version metadata check (`timeout-minutes: 30`)
   - `fullstack-e2e` — Fullstack E2E Tests: `make test-e2e-fullstack` (Playwright: browser → SvelteKit → Go API → worker → `:memory:` SQLite). No `if:` gate, so it runs on every push/PR (`timeout-minutes: 30`)

3. **Results:**
   - Green checkmark ✅ = All passed
   - Red X ❌ = Something failed
   - Yellow dot 🟡 = In progress

4. **Coverage tracking:**
   - Automatically uploaded to Codecov
   - PR comments show coverage changes
   - Badge updates in README

## Best Practices

### Always Before Pushing

```bash
# Run full simulation
make simulate-ci

# If it passes, you're good to push
git push
```

### Quick Checks During Development

```bash
# After changing tests
make test

# After changing concurrent code
make test-race

# After refactoring
make coverage-html  # See what you broke
```

### Pre-commit Hook

Install it once:
```bash
cp scripts/pre-commit.sample .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

Now every commit automatically runs the 8 pre-commit checks (formatting, golangci-lint, go vet, fast tests, build, Swagger drift, frontend formatting, frontend types) and guards against committing forbidden paths. See [Before Committing](#before-committing) for the full table.

### Continuous Improvement

As coverage increases, raise the threshold:
```bash
# Edit Makefile
coverage-check: coverage
	@./scripts/check_coverage.sh 80 coverage.out  # Was 75

# Edit .github/workflows/test.yml
- name: Check coverage threshold
  run: ./scripts/check_coverage.sh 80 coverage.out  # Was 75
```

## Summary

| Stage | Command | Purpose |
|-------|---------|---------|
| During development | `make test-short` | Fast feedback |
| Before commit | Automatic (pre-commit hook) | Catch obvious issues |
| Before push | `make simulate-ci` | Full CI check |
| On GitHub | Automatic (GitHub Actions) | Official CI |

**Golden Rule:** If `make simulate-ci` passes locally, GitHub CI will almost certainly pass too.

> **Caveat:** `simulate-ci` covers 7 of the 9 workflow jobs — it skips `test-windows` (Windows runner) and `fullstack-e2e` (Playwright). If your change touches Windows path handling or the fullstack browser→API stack, run `act -j test-windows` / `make test-e2e-fullstack` (or push and watch CI) to cover the gap.
