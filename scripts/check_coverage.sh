#!/usr/bin/env bash
# check_coverage.sh - Enforce test coverage thresholds for javinizer-go
#
# Usage: ./scripts/check_coverage.sh [min_coverage] [coverage_profile]
#   min_coverage: Minimum required coverage percentage (default: 75)
#   coverage_profile: Path to coverage profile (default: coverage.out)
#
# Exit codes:
#   0 - Coverage meets or exceeds threshold
#   1 - Coverage below threshold
#   2 - Coverage profile not found or invalid

set -euo pipefail

# Configuration
MIN_COVERAGE="${1:-75}"
COVERAGE_PROFILE="${2:-coverage.out}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if coverage profile exists
if [ ! -f "${COVERAGE_PROFILE}" ]; then
  echo -e "${RED}Error: Coverage profile '${COVERAGE_PROFILE}' not found${NC}"
  echo "Run 'make coverage' to generate the coverage profile first"
  exit 2
fi

if go run ./cmd/coveragecheck --metric line --min "${MIN_COVERAGE}" --profile "${COVERAGE_PROFILE}"; then
  echo -e "${GREEN}✓ Coverage check PASSED${NC}"
  exit 0
else
  EXIT_CODE=$?
  if [ ${EXIT_CODE} -eq 1 ]; then
    echo ""
    echo -e "${YELLOW}Action required:${NC}"
    echo "  - Add tests to increase coverage"
    echo "  - Run 'make coverage-html' to see uncovered code"
    echo "  - Target critical packages: scraper, api, cli"
  fi
  exit ${EXIT_CODE}
fi
