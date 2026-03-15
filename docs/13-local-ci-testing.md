# Local CI Testing Guide

This guide explains how to run GitHub Actions CI checks locally before pushing code to GitHub.

## Quick Start

### Best Option: Simulate Full CI Pipeline

```bash
make simulate-ci
```

This runs **all 5 GitHub Actions jobs** locally with colored output:
- ✅ Unit Tests & Coverage
- ✅ Race Detector Tests
- ✅ Linting & Code Quality
- ✅ Build Verification
- ✅ Docker Build Verification

**Example output:**
```
========================================
  Simulating GitHub Actions CI Locally
========================================

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Job 1/5: Unit Tests & Coverage
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

→ Running unit tests...
✓ Unit tests passed

→ Generating coverage report...
✓ Coverage report generated

→ Checking coverage threshold (75%)...
✓ Coverage check PASSED
  78.4% >= 75%

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Job 2/5: Race Detector Tests
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

**What it checks:**
- Dependencies download
- Unit tests pass
- Coverage generation and threshold (75%)
- Race detector on concurrent packages
- go vet static analysis
- golangci-lint (if installed)
- Code formatting (gofmt)
- Build success and binary creation
- Docker image build and version metadata

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

**What it runs:**
```bash
make vet
make lint
make coverage-check
make test-race
```

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
make coverage-fast      # Faster local coverage iteration
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

# Run specific job
act -j test

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

The pre-commit hook automatically runs:
```bash
gofmt -l .           # Formatting check
go vet ./...         # Static analysis
go test -short ./... # Fast tests
go build ./cmd/javinizer   # Build verification
```

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

Increase timeout in `simulate-ci.sh` or run specific tests:
```bash
# Just race tests (can be slow)
make test-race

# All tests with longer timeout
go test -timeout=10m ./...
```

### Coverage Check Failing

If coverage is below 75%, you have options:
```bash
# View coverage to see what needs tests
make coverage-html

# Check per-package breakdown
make coverage-func

# Run with lower threshold temporarily
./scripts/check_coverage.sh 20 coverage.out

# Or update Makefile threshold
# Edit Makefile: change 25 to your desired %
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

2. **Jobs run in parallel:**
   - Unit Tests & Coverage (~2-3 min)
   - Race Detector Tests (~1-2 min)
   - Linting & Code Quality (~30 sec)
   - Build Verification (~30 sec)

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

Now every commit automatically checks:
- ✅ Formatting
- ✅ Static analysis
- ✅ Fast tests
- ✅ Build

### Continuous Improvement

As coverage increases, raise the threshold:
```bash
# Edit Makefile
coverage-check: coverage
	@./scripts/check_coverage.sh 30 coverage.out  # Was 25

# Edit .github/workflows/test.yml
- name: Check coverage threshold
  run: ./scripts/check_coverage.sh 30 coverage.out  # Was 25
```

## Summary

| Stage | Command | Purpose |
|-------|---------|---------|
| During development | `make test-short` | Fast feedback |
| Before commit | Automatic (pre-commit hook) | Catch obvious issues |
| Before push | `make simulate-ci` | Full CI check |
| On GitHub | Automatic (GitHub Actions) | Official CI |

**Golden Rule:** If `make simulate-ci` passes locally, GitHub CI will almost certainly pass too.
