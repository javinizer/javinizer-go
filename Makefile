.PHONY: build run run-api test test-short test-race test-verbose bench clean deps install web-dev web-build web-preview web-install web-clean
.PHONY: coverage coverage-html coverage-check coverage-func ci simulate-ci
.PHONY: fmt lint vet
.PHONY: build-cli-linux build-cli-darwin build-cli-windows build-cli-all
.PHONY: act-list act-test act-build act-lint act-docker act-cli-release act-ci act-dry act-help

# Version information (can be overridden)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Build flags
LDFLAGS := -ldflags "\
	-X github.com/javinizer/javinizer-go/internal/version.Version=$(VERSION) \
	-X github.com/javinizer/javinizer-go/internal/version.Commit=$(COMMIT) \
	-X github.com/javinizer/javinizer-go/internal/version.BuildDate=$(BUILD_DATE)"

# Optimized build flags (strip debug symbols)
LDFLAGS_RELEASE := -ldflags "\
	-w -s \
	-X github.com/javinizer/javinizer-go/internal/version.Version=$(VERSION) \
	-X github.com/javinizer/javinizer-go/internal/version.Commit=$(COMMIT) \
	-X github.com/javinizer/javinizer-go/internal/version.BuildDate=$(BUILD_DATE)"

# Build the application (single binary with version info)
build:
	@echo "Building javinizer $(VERSION) (commit: $(COMMIT))..."
	go build $(LDFLAGS) -o bin/javinizer ./cmd/cli

# Run the CLI (primary target)
run:
	go run ./cmd/cli

# Run the API server using subcommand
run-api:
	go run ./cmd/cli api

# Run tests
test:
	go test -v ./...

# Run short/fast tests (for pre-commit hooks)
test-short:
	go test -short ./...

# Run tests with race detector (critical for concurrent code)
test-race:
	@echo "Running race detector on concurrent packages..."
	go test -race -v ./internal/worker/...
	go test -race -v ./internal/tui/...
	go test -race -v ./internal/websocket/...
	go test -race -v ./internal/api/...

# Run tests with verbose output
test-verbose:
	go test -v -count=1 ./...

# Run benchmarks
bench:
	go test -bench=. -benchmem ./...

# Generate coverage report using go-acc (more reliable for multi-package projects)
# Uses go run to execute go-acc from project dependencies (no global install needed)
# Version is pinned to match go.mod for reproducible builds
coverage:
	@go run github.com/ory/go-acc@v0.2.8 --covermode count -o coverage.out ./...

# Open coverage report in browser
coverage-html: coverage
	go tool cover -html=coverage.out

# Display coverage function-by-function breakdown
coverage-func: coverage
	go tool cover -func=coverage.out

# Check if coverage meets minimum threshold (default: 25% - increase as tests are added)
coverage-check: coverage
	@./scripts/check_coverage.sh 25 coverage.out

# Run full CI test suite
ci: vet lint coverage-check test-race
	@echo "All CI checks passed!"

# Simulate GitHub Actions CI locally (with pretty output)
simulate-ci:
	@./scripts/simulate-ci.sh

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Download dependencies (includes dev tools via tools.go)
deps:
	go mod download
	go mod tidy

# Install the binary
install:
	go build -o $(GOPATH)/bin/javinizer ./cmd/cli

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run linter
lint:
	golangci-lint run

# Generate API documentation
docs:
	swag init -g cmd/cli/api.go -o api/docs

# Web frontend targets
web-dev:
	cd web/frontend && npm run dev

web-build:
	cd web/frontend && npm run build

web-preview:
	cd web/frontend && npm run preview

web-install:
	cd web/frontend && npm install

web-clean:
	rm -rf web/frontend/node_modules web/frontend/.svelte-kit web/frontend/build

# ============================================================================
# CLI Binary Build Targets (for multi-platform releases)
# ============================================================================

build-cli-linux:
	@echo "Building CLI for Linux (amd64) - $(VERSION)..."
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-linux-amd64 ./cmd/cli

build-cli-darwin:
	@echo "Building CLI for macOS - $(VERSION)..."
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-darwin-amd64 ./cmd/cli
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-darwin-arm64 ./cmd/cli
	lipo -create bin/javinizer-darwin-amd64 bin/javinizer-darwin-arm64 -output bin/javinizer-darwin-universal

build-cli-windows:
	@echo "Building CLI for Windows - $(VERSION)..."
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-windows-amd64.exe ./cmd/cli

build-cli-all: build-cli-linux build-cli-darwin build-cli-windows
	@echo "All CLI binaries built successfully!"
	@ls -lh bin/

# ============================================================================
# Local GitHub Actions Testing with act
# ============================================================================

act-list:
	@echo "Available workflows:"
	@act -l

act-test:
	@echo "Running test workflow locally..."
	@act -j test

act-build:
	@echo "Running build workflow locally..."
	@act -j build

act-lint:
	@echo "Running lint workflow locally..."
	@act -j lint

act-docker:
	@echo "Running Docker build workflow locally..."
	@act -W .github/workflows/docker-test.yml

act-cli-release:
	@echo "Testing CLI release workflow locally..."
	@act -W .github/workflows/cli-release.yml --env GITHUB_REF=refs/tags/v1.0.0-test

act-ci:
	@echo "Testing all CI workflows locally with act..."
	@act -j test -j lint -j build -j coverage

act-dry:
	@echo "Dry run - show what would execute:"
	@act -n

act-help:
	@echo "act - Local GitHub Actions Testing"
	@echo ""
	@echo "Available targets:"
	@echo "  make act-list          - List all workflows"
	@echo "  make act-test          - Run test job"
	@echo "  make act-build         - Run build job"
	@echo "  make act-lint          - Run lint job"
	@echo "  make act-docker        - Test Docker workflow"
	@echo "  make act-cli-release   - Test CLI release workflow"
	@echo "  make act-ci            - Run all CI jobs"
	@echo "  make act-dry           - Dry run (show execution plan)"
	@echo ""
	@echo "Direct act commands:"
	@echo "  act -l                 - List workflows"
	@echo "  act -j <job>           - Run specific job"
	@echo "  act -v                 - Verbose output"
	@echo "  act -n                 - Dry run"
