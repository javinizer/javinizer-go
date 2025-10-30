#!/usr/bin/env bash
# check_coverage.sh - Enforce test coverage thresholds for javinizer-go
#
# Usage: ./scripts/check_coverage.sh [min_coverage] [coverage_profile]
#   min_coverage: Minimum required coverage percentage (default: 60)
#   coverage_profile: Path to coverage profile (default: coverage.out)
#
# Exit codes:
#   0 - Coverage meets or exceeds threshold
#   1 - Coverage below threshold
#   2 - Coverage profile not found or invalid

set -euo pipefail

# Configuration
MIN_COVERAGE="${1:-60}"
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

# Extract total coverage percentage
# go tool cover -func outputs lines like:
# total:                    (statements)    72.3%
TOTAL_COVERAGE=$(go tool cover -func="${COVERAGE_PROFILE}" | awk '/^total:/ {print $3}' | sed 's/%//')

if [ -z "${TOTAL_COVERAGE}" ]; then
  echo -e "${RED}Error: Could not parse coverage from ${COVERAGE_PROFILE}${NC}"
  exit 2
fi

echo "=========================================="
echo "Test Coverage Report"
echo "=========================================="
echo "Coverage Profile: ${COVERAGE_PROFILE}"
echo "Current Coverage: ${TOTAL_COVERAGE}%"
echo "Required Minimum: ${MIN_COVERAGE}%"
echo "=========================================="

# Compare coverage against threshold using awk for float comparison
RESULT=$(awk -v current="${TOTAL_COVERAGE}" -v required="${MIN_COVERAGE}" \
  'BEGIN {
    if (current + 0 < required + 0) {
      print "FAIL"
      exit 1
    } else {
      print "PASS"
      exit 0
    }
  }')

EXIT_CODE=$?

if [ ${EXIT_CODE} -eq 0 ]; then
  echo -e "${GREEN}✓ Coverage check PASSED${NC}"
  echo -e "${GREEN}  ${TOTAL_COVERAGE}% >= ${MIN_COVERAGE}%${NC}"
  exit 0
else
  echo -e "${RED}✗ Coverage check FAILED${NC}"
  echo -e "${RED}  ${TOTAL_COVERAGE}% < ${MIN_COVERAGE}%${NC}"
  echo ""
  echo -e "${YELLOW}Action required:${NC}"
  echo "  - Add tests to increase coverage"
  echo "  - Run 'make coverage-html' to see uncovered code"
  echo "  - Target critical packages: scraper, api, cli"
  exit 1
fi
