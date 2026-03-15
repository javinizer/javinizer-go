#!/usr/bin/env bash
# simulate-ci.sh - Run the same checks as GitHub Actions locally
#
# This script simulates what runs in .github/workflows/test.yml
# Use this to catch issues before pushing to GitHub

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

echo ""
echo -e "${BOLD}========================================${NC}"
echo -e "${BOLD}  Simulating GitHub Actions CI Locally${NC}"
echo -e "${BOLD}========================================${NC}"
echo ""

# Track overall status
FAILED_JOBS=()

# Job 1: Unit Tests & Coverage
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Job 1/5: Unit Tests & Coverage${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}→ Downloading dependencies...${NC}"
if go mod download; then
    echo -e "${GREEN}✓ Dependencies downloaded${NC}"
else
    echo -e "${RED}✗ Failed to download dependencies${NC}"
    FAILED_JOBS+=("Unit Tests - Dependencies")
fi
echo ""

echo -e "${YELLOW}→ Running unit tests...${NC}"
if go test -v ./...; then
    echo -e "${GREEN}✓ Unit tests passed${NC}"
else
    echo -e "${RED}✗ Unit tests failed${NC}"
    FAILED_JOBS+=("Unit Tests - Tests")
fi
echo ""

echo -e "${YELLOW}→ Generating coverage report...${NC}"
COVERAGE_LOG=$(mktemp)
if make coverage > "$COVERAGE_LOG" 2>&1; then
    echo -e "${GREEN}✓ Coverage report generated${NC}"
else
    echo -e "${RED}✗ Failed to generate coverage${NC}"
    echo -e "${YELLOW}Coverage output:${NC}"
    tail -20 "$COVERAGE_LOG"
    FAILED_JOBS+=("Unit Tests - Coverage")
fi
rm -f "$COVERAGE_LOG"
echo ""

echo -e "${YELLOW}→ Checking coverage threshold (75%)...${NC}"
if ./scripts/check_coverage.sh 75 coverage.out; then
    echo ""
else
    FAILED_JOBS+=("Unit Tests - Coverage Threshold")
fi
echo ""

# Job 2: Race Detector Tests
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Job 2/5: Race Detector Tests${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}→ Running race detector on concurrent packages...${NC}"
if make test-race; then
    echo -e "${GREEN}✓ Race detector tests passed${NC}"
else
    echo -e "${RED}✗ Race detector found issues${NC}"
    FAILED_JOBS+=("Race Tests")
fi
echo ""

# Job 3: Linting & Code Quality
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Job 3/5: Linting & Code Quality${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}→ Running go vet...${NC}"
if make vet; then
    echo -e "${GREEN}✓ go vet passed${NC}"
else
    echo -e "${RED}✗ go vet found issues${NC}"
    FAILED_JOBS+=("Lint - go vet")
fi
echo ""

echo -e "${YELLOW}→ Running golangci-lint...${NC}"
if command -v golangci-lint >/dev/null 2>&1; then
    if make lint; then
        echo -e "${GREEN}✓ golangci-lint passed${NC}"
    else
        echo -e "${RED}✗ golangci-lint found issues${NC}"
        FAILED_JOBS+=("Lint - golangci-lint")
    fi
else
    echo -e "${YELLOW}⚠ golangci-lint not installed (skipping)${NC}"
    echo -e "${YELLOW}  Install: brew install golangci-lint${NC}"
fi
echo ""

echo -e "${YELLOW}→ Checking code formatting...${NC}"
UNFORMATTED=$(gofmt -l . 2>&1)
if [ -z "$UNFORMATTED" ]; then
    echo -e "${GREEN}✓ Code is properly formatted${NC}"
else
    echo -e "${RED}✗ Code is not formatted. Run 'make fmt'${NC}"
    echo "Unformatted files:"
    echo "$UNFORMATTED"
    FAILED_JOBS+=("Lint - Formatting")
fi
echo ""

# Job 4: Build Verification
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Job 4/5: Build Verification${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}→ Building CLI binary...${NC}"
if make build > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Build successful${NC}"

    if [ -f bin/javinizer ]; then
        echo -e "${GREEN}✓ Binary created: bin/javinizer${NC}"
    else
        echo -e "${RED}✗ Binary not found at bin/javinizer${NC}"
        FAILED_JOBS+=("Build - Binary Missing")
    fi
else
    echo -e "${RED}✗ Build failed${NC}"
    FAILED_JOBS+=("Build")
fi
echo ""

# Job 5: Docker Build Verification
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Job 5/5: Docker Build Verification${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

if command -v docker >/dev/null 2>&1 && docker version >/dev/null 2>&1; then
    DOCKER_LOG=$(mktemp)
    BUILD_DATE="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    VERSION="$(./scripts/version.sh)"

    echo -e "${YELLOW}→ Building Docker image...${NC}"
    if docker build \
        --build-arg VERSION="${VERSION}" \
        --build-arg COMMIT="local" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        -t javinizer:ci-local \
        . > "${DOCKER_LOG}" 2>&1; then
        echo -e "${GREEN}✓ Docker build passed${NC}"
    else
        echo -e "${RED}✗ Docker build failed${NC}"
        tail -20 "${DOCKER_LOG}"
        FAILED_JOBS+=("Docker Build")
    fi
    rm -f "${DOCKER_LOG}"
    echo ""

    if [ ${#FAILED_JOBS[@]} -eq 0 ]; then
        echo -e "${YELLOW}→ Verifying Docker image version output...${NC}"
        if docker run --rm --entrypoint /usr/local/bin/javinizer javinizer:ci-local version --short; then
            echo -e "${GREEN}✓ Docker image version output verified${NC}"
        else
            echo -e "${RED}✗ Docker image version verification failed${NC}"
            FAILED_JOBS+=("Docker Version Verification")
        fi
        echo ""
    fi
else
    echo -e "${YELLOW}⚠ Docker unavailable (skipping Docker build verification)${NC}"
    echo ""
fi

# Summary
echo -e "${BOLD}========================================${NC}"
echo -e "${BOLD}  CI Simulation Summary${NC}"
echo -e "${BOLD}========================================${NC}"
echo ""

if [ ${#FAILED_JOBS[@]} -eq 0 ]; then
    echo -e "${GREEN}${BOLD}✓ All checks passed!${NC}"
    echo -e "${GREEN}Your code is ready to push to GitHub${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}${BOLD}✗ ${#FAILED_JOBS[@]} check(s) failed:${NC}"
    for job in "${FAILED_JOBS[@]}"; do
        echo -e "${RED}  - ${job}${NC}"
    done
    echo ""
    echo -e "${YELLOW}Fix these issues before pushing to GitHub${NC}"
    echo ""
    exit 1
fi
