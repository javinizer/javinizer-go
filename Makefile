.PHONY: help build run run-api run-api-dev test test-short test-race test-verbose bench clean clean-all deps install web-dev web-build web-preview web-install web-clean
.PHONY: coverage coverage-fast coverage-html coverage-check coverage-func ci simulate-ci
.PHONY: fmt lint vet swagger docs mocks
.PHONY: build-cli-linux build-cli-darwin build-cli-windows build-cli-all
.PHONY: act-list act-test act-build act-lint act-docker act-cli-release act-ci act-dry act-help
.PHONY: docker-build docker-build-no-cache docker-run docker-stop docker-clean docker-push docker-test docker-logs docker-help
.PHONY: docker-compose-up docker-compose-down docker-compose-restart docker-compose-logs docker-compose-build

# Default target - show help
.DEFAULT_GOAL := help

# Help target - displays available targets
help:
	@echo "Javinizer Makefile - Available Targets"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build              - Build single binary (API + embedded Web UI)"
	@echo "  make run                - Run CLI directly (no build)"
	@echo "  make run-api            - Run API server directly"
	@echo "  make run-api-dev        - Run API server with hot reload (air)"
	@echo "  make install            - Install binary to GOPATH/bin"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run all tests with verbose output"
	@echo "  make test-short         - Run fast tests only (for pre-commit)"
	@echo "  make test-race          - Run race detector on concurrent packages"
	@echo "  make test-verbose       - Run tests with verbose output and count=1"
	@echo "  make bench              - Run benchmarks"
	@echo ""
	@echo "Coverage:"
	@echo "  make coverage           - Generate strict coverage report for CI/release (coverage.out)"
	@echo "  make coverage-fast      - Generate faster local coverage report (coverage.out)"
	@echo "  make coverage-html      - Open coverage report in browser"
	@echo "  make coverage-func      - Display function-by-function coverage"
	@echo "  make coverage-check     - Enforce 75%% minimum coverage threshold"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt                - Format code with go fmt"
	@echo "  make vet                - Run go vet"
	@echo "  make lint               - Run golangci-lint"
	@echo "  make swagger            - Generate Swagger API documentation"
	@echo "  make mocks              - Generate mocks from interfaces (mockery v3)"
	@echo ""
	@echo "CI/CD:"
	@echo "  make ci                 - Run full CI suite (vet + lint + coverage + race)"
	@echo "  make simulate-ci        - Simulate GitHub Actions CI locally"
	@echo ""
	@echo "Web Frontend:"
	@echo "  make web-install        - Install npm dependencies"
	@echo "  make web-dev            - Start dev server with hot reload"
	@echo "  make web-build          - Build frontend and sync to web/dist"
	@echo "  make web-preview        - Preview production build"
	@echo "  make web-clean          - Clean node_modules and build artifacts"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean              - Remove build artifacts and coverage"
	@echo "  make clean-all          - Deep clean (includes test binaries)"
	@echo ""
	@echo "Cross-Platform Builds:"
	@echo "  make build-cli-linux    - Build for Linux (amd64)"
	@echo "  make build-cli-darwin   - Build for macOS (universal binary)"
	@echo "  make build-cli-windows  - Build for Windows (amd64)"
	@echo "  make build-cli-all      - Build for all platforms"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-help        - Show Docker-specific commands"
	@echo ""
	@echo "GitHub Actions (act):"
	@echo "  make act-help           - Show act-specific commands"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  COMMIT=$(COMMIT)"
	@echo ""

# Version information (can be overridden)
VERSION_FILE := internal/version/version.txt
VERSION ?= $(shell ./scripts/version.sh)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

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

# Build the application (single binary with embedded web UI and version info)
build: web-build
	@echo "Building javinizer $(VERSION) (commit: $(COMMIT))..."
	go build $(LDFLAGS) -o bin/javinizer ./cmd/javinizer

# Run the CLI (primary target)
run:
	go run ./cmd/javinizer

# Run the API server using subcommand
run-api:
	go run ./cmd/javinizer api

# Run API with hot reload (requires air, falls back to go run air)
run-api-dev:
	@if command -v air >/dev/null 2>&1; then \
		air -c .air.toml; \
	else \
		echo "air not found in PATH, running via go run github.com/air-verse/air@latest"; \
		go run github.com/air-verse/air@latest -c .air.toml; \
	fi

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

# Generate strict coverage report using go-acc (used by CI/release)
# Uses go run to execute go-acc from project dependencies (no global install needed)
# Version is pinned to match go.mod for reproducible builds
# Excludes: mocks (generated), tui (interactive UI), docs (generated API docs), testutil (test helpers)
coverage:
	@rm -f coverage.out
	@go run github.com/ory/go-acc@v0.2.8 --covermode count --ignore mocks,tui,docs,testutil -o coverage.out ./... -- -count=1

# Generate faster local coverage report (package-level, short mode)
# This is optimized for iteration speed and is not used for CI threshold enforcement.
coverage-fast:
	@rm -f coverage.out
	@pkgs=$$(go list ./... | grep -Ev '/(mocks|tui|docs|testutil)(/|$$)'); \
	go test -short -covermode=count -coverprofile=coverage.out -count=1 $$pkgs

# Open coverage report in browser
coverage-html: coverage
	go tool cover -html=coverage.out

# Display coverage function-by-function breakdown
coverage-func: coverage
	go tool cover -func=coverage.out

# Check if coverage meets minimum threshold (75% as per CLAUDE.md)
coverage-check: coverage
	@./scripts/check_coverage.sh 75 coverage.out

# Run full CI test suite
ci: vet lint coverage-check test-race
	@echo "All CI checks passed!"

# Simulate GitHub Actions CI locally (with pretty output)
simulate-ci:
	@./scripts/simulate-ci.sh

# Clean build artifacts and coverage reports
clean:
	@echo "Cleaning build artifacts and coverage reports..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "Clean complete!"

# Deep clean - removes everything including test binaries and caches
clean-all: clean
	@echo "Deep cleaning (test binaries, caches, etc.)..."
	rm -f *.test cli javinizer api.test
	rm -f COMMIT_MESSAGE.txt zen_*.changeset
	go clean -cache -testcache -modcache
	@echo "Deep clean complete!"

# Download dependencies (includes dev tools via tools.go)
deps:
	go mod download
	go mod tidy

# Install the binary
install:
	go build $(LDFLAGS) -o $(GOPATH)/bin/javinizer ./cmd/javinizer

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run linter
lint:
	golangci-lint run

# Generate Swagger API documentation
swagger:
	swag init -g cmd/javinizer/commands/api/command.go -o docs/swagger

# Alias for backward compatibility
docs: swagger

# Generate mocks from interfaces using mockery v3
# Requires: mockery v3.5+ (install: go install github.com/vektra/mockery/v3@latest)
# Config: .mockery.yaml
# Output: internal/mocks/
mocks:
	@echo "Generating mocks with mockery..."
	@go run github.com/vektra/mockery/v3@latest --config .mockery.yaml
	@echo "Post-processing: Unifying package names to 'mocks'..."
	@for file in internal/mocks/*.go; do \
		sed -i '' 's/^package models$$/package mocks/' "$$file"; \
		sed -i '' 's/^package database$$/package mocks/' "$$file"; \
		sed -i '' 's/^package httpclient$$/package mocks/' "$$file"; \
		sed -i '' 's/^package aggregator$$/package mocks/' "$$file"; \
	done
	@echo "Mock generation complete! Generated mocks in internal/mocks/"

# Web frontend targets
web-dev:
	cd web/frontend && npm run dev -- --port 5174

web-build:
	cd web/frontend && npm run build
	rm -rf web/dist
	mkdir -p web/dist
	cp -R web/frontend/build/. web/dist/

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
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-linux-amd64 ./cmd/javinizer

build-cli-darwin:
	@echo "Building CLI for macOS - $(VERSION)..."
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-darwin-amd64 ./cmd/javinizer
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-darwin-arm64 ./cmd/javinizer
	lipo -create bin/javinizer-darwin-amd64 bin/javinizer-darwin-arm64 -output bin/javinizer-darwin-universal

build-cli-windows:
	@echo "Building CLI for Windows - $(VERSION)..."
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o bin/javinizer-windows-amd64.exe ./cmd/javinizer

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
	@act -W .github/workflows/test.yml -j docker-build

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

# ============================================================================
# Docker Build and Deployment Targets
# ============================================================================

# Docker image configuration
DOCKER_IMAGE := javinizer
DOCKER_TAG := $(VERSION)
DOCKER_REGISTRY ?= ghcr.io/javinizer
DOCKER_FULL_IMAGE := $(DOCKER_IMAGE):$(DOCKER_TAG)
DOCKER_CONTAINER_NAME := javinizer

# Build Docker image with version injection
docker-build:
	@echo "Building Docker image $(DOCKER_FULL_IMAGE)..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_FULL_IMAGE) \
		-t $(DOCKER_IMAGE):latest \
		.
	@echo "Docker image built successfully!"
	@docker images $(DOCKER_IMAGE)

# Build Docker image without cache (force clean build)
docker-build-no-cache:
	@echo "Building Docker image $(DOCKER_FULL_IMAGE) without cache..."
	docker build --no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_FULL_IMAGE) \
		-t $(DOCKER_IMAGE):latest \
		.
	@echo "Docker image built successfully!"
	@docker images $(DOCKER_IMAGE)

# Run Docker container (detached mode)
docker-run:
	@echo "Starting Docker container $(DOCKER_CONTAINER_NAME)..."
	docker run -d \
		--name $(DOCKER_CONTAINER_NAME) \
		-p 8080:8080 \
		-v $(PWD)/javinizer:/javinizer \
		-v $(PWD)/data:/data \
		$(DOCKER_FULL_IMAGE)
	@echo "Container started! Access at http://localhost:8080"
	@echo "Logs: docker logs -f $(DOCKER_CONTAINER_NAME)"

# Stop and remove Docker container
docker-stop:
	@echo "Stopping Docker container $(DOCKER_CONTAINER_NAME)..."
	-docker stop $(DOCKER_CONTAINER_NAME)
	-docker rm $(DOCKER_CONTAINER_NAME)
	@echo "Container stopped and removed"

# View container logs
docker-logs:
	docker logs -f $(DOCKER_CONTAINER_NAME)

# Validate Docker image metadata and startup
docker-test: docker-build
	@echo "Validating Docker image version output..."
	docker run --rm --entrypoint /usr/local/bin/javinizer $(DOCKER_FULL_IMAGE) version --short

# Clean Docker images and containers
docker-clean:
	@echo "Cleaning Docker images and containers..."
	-docker stop $(DOCKER_CONTAINER_NAME) 2>/dev/null || true
	-docker rm $(DOCKER_CONTAINER_NAME) 2>/dev/null || true
	-docker rmi $(DOCKER_IMAGE):latest 2>/dev/null || true
	-docker rmi $(DOCKER_FULL_IMAGE) 2>/dev/null || true
	@echo "Docker cleanup complete"

# Push Docker image to registry (requires login)
docker-push:
	@echo "Pushing Docker image to $(DOCKER_REGISTRY)..."
	docker tag $(DOCKER_FULL_IMAGE) $(DOCKER_REGISTRY)/$(DOCKER_FULL_IMAGE)
	docker tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	docker push $(DOCKER_REGISTRY)/$(DOCKER_FULL_IMAGE)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	@echo "Docker image pushed successfully!"

# ============================================================================
# Docker Compose Targets
# ============================================================================

# Start services with docker-compose
docker-compose-up:
	@echo "Starting services with docker-compose..."
	docker-compose up -d
	@echo "Services started! Access at http://localhost:8080"

# Stop services
docker-compose-down:
	@echo "Stopping docker-compose services..."
	docker-compose down

# Restart services
docker-compose-restart:
	@echo "Restarting docker-compose services..."
	docker-compose restart

# View docker-compose logs
docker-compose-logs:
	docker-compose logs -f

# Build docker-compose services
docker-compose-build:
	@echo "Building docker-compose services..."
	docker-compose build

# Help target for Docker commands
docker-help:
	@echo "Docker Build Targets:"
	@echo ""
	@echo "Build:"
	@echo "  make docker-build              - Build Docker image with version injection"
	@echo "  make docker-build-no-cache     - Build without cache (clean build)"
	@echo ""
	@echo "Run:"
	@echo "  make docker-run                - Run container (detached, port 8080)"
	@echo "  make docker-stop               - Stop and remove container"
	@echo "  make docker-logs               - View container logs"
	@echo "  make docker-test               - Validate Docker image version metadata"
	@echo ""
	@echo "Cleanup:"
	@echo "  make docker-clean              - Remove images and containers"
	@echo ""
	@echo "Registry:"
	@echo "  make docker-push               - Push image to registry"
	@echo ""
	@echo "Docker Compose:"
	@echo "  make docker-compose-up         - Start services"
	@echo "  make docker-compose-down       - Stop services"
	@echo "  make docker-compose-restart    - Restart services"
	@echo "  make docker-compose-logs       - View logs"
	@echo "  make docker-compose-build      - Build services"
	@echo ""
	@echo "Variables (can override):"
	@echo "  VERSION=$(VERSION)"
	@echo "  DOCKER_REGISTRY=$(DOCKER_REGISTRY)"
	@echo ""
	@echo "Examples:"
	@echo "  make docker-build VERSION=1.0.0"
	@echo "  make docker-push DOCKER_REGISTRY=ghcr.io/myorg"
