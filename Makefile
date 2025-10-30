.PHONY: build run run-api test test-short test-race test-verbose bench clean deps install web-dev web-build web-preview web-install web-clean
.PHONY: coverage coverage-html coverage-check coverage-func ci simulate-ci
.PHONY: fmt lint vet

# Build the application (single binary)
build:
	go build -o bin/javinizer ./cmd/cli

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
