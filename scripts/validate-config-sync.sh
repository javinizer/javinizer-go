#!/usr/bin/env bash
# Config synchronization validator
# Ensures config.yaml.example matches DefaultConfig() values
#
# Usage: ./scripts/validate-config-sync.sh
# Exit codes: 0 = sync OK, 1 = drift detected

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Validating config synchronization...${NC}"
echo ""

# Check 1: Verify config files exist
if [[ ! -f "configs/config.yaml.example" ]]; then
    echo -e "${RED}✗ configs/config.yaml.example not found${NC}"
    exit 1
fi

if [[ ! -f "internal/config/defaults.go" ]]; then
    echo -e "${RED}✗ internal/config/defaults.go not found${NC}"
    exit 1
fi

# Check 2: Run the Go drift detection test
echo -e "${BLUE}[1/3] Running drift detection test...${NC}"
if ! go test -short -run TestDefaultConfigMatchesExample ./internal/config/... 2>&1; then
    echo -e "${RED}✗ Drift detection test failed${NC}"
    echo ""
    echo -e "${YELLOW}This means config.yaml.example and DefaultConfig() have diverged.${NC}"
    echo -e "${YELLOW}Run 'go test -v -run TestDefaultConfigMatchesExample ./internal/config/...' for details.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Drift detection test passed${NC}"
echo ""

# Check 3: Verify config.yaml.example is valid YAML
echo -e "${BLUE}[2/3] Validating config.yaml.example YAML syntax...${NC}"
if command -v python3 &> /dev/null; then
    if ! python3 -c "import yaml; yaml.safe_load(open('configs/config.yaml.example'))" 2>&1; then
        echo -e "${RED}✗ config.yaml.example is not valid YAML${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ YAML syntax OK${NC}"
else
    echo -e "${YELLOW}⚠ Skipping YAML validation (python3 not available)${NC}"
fi
echo ""

# Check 4: Verify embedded config can be loaded
echo -e "${BLUE}[3/4] Validating embedded config matches source file...${NC}"
if ! go test -short -run TestEmbeddedConfigMatchesSourceFile ./internal/config/... 2>&1; then
    echo -e "${RED}✗ Embedded config validation failed${NC}"
    echo -e "${YELLOW}The embedded config may be out of sync with config.yaml.example${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Embedded config validation passed${NC}"
echo ""

# Check 5: Detect runtime config drift in scraper constructors
echo -e "${BLUE}[4/4] Checking for hardcoded config values in scrapers...${NC}"
DRIFT_ISSUES=""

# Detect hardcoded values in ScraperSettings struct literals.
# Challenge: Go struct literals span multiple lines, so simple grep fails.
# Solution: Use awk to track "inside ScraperSettings{}" state and flag hardcoded values.

detect_hardcoded_in_struct() {
    local field="$1"
    local file="$2"
    
    awk -v field="$field" '
    BEGIN { in_struct = 0; brace_depth = 0 }
    
    # Track if we are inside ScraperSettings{ or &config.ScraperSettings{
    /ScraperSettings\s*\{/ || /&config\.ScraperSettings\s*\{/ {
        in_struct = 1
        brace_depth = 1
        struct_start_line = NR
        next
    }
    
    # Track brace depth for nested structs
    in_struct && /\{/ { brace_depth++ }
    in_struct && /\}/ { 
        brace_depth--
        if (brace_depth == 0) { in_struct = 0 }
    }
    
    # Check for hardcoded field values while inside struct
    in_struct {
        # Pattern: Field: <number> or Field: <number> * time.Duration
        # Exclude: settings.Field, resolvedField, cfg.Field (passthrough patterns)
        pattern = field ":[[:space:]]*[0-9]+"
        if ($0 ~ pattern && $0 !~ /settings\./ && $0 !~ /resolved/ && $0 !~ /cfg\./) {
            # Get filename from ARGV
            fname = FILENAME
            # Print file:line:match
            print fname ":" NR ":" $0
        }
    }
    ' "$file"
}

# Scan all non-test, non-module Go files in scraper packages
# module.go contains Defaults() which intentionally have hardcoded values
for file in $(find internal/scraper -name "*.go" ! -name "*_test.go" ! -name "module.go" 2>/dev/null); do
    for field in Timeout RetryCount RateLimit; do
        result=$(detect_hardcoded_in_struct "$field" "$file" 2>/dev/null)
        if [[ -n "$result" ]]; then
            DRIFT_ISSUES="${DRIFT_ISSUES}${result}\n"
        fi
    done
done

# Also check for time.Duration expressions like "30 * time.Second"
# Exclude module.go (contains Defaults() with intentional defaults)
TIME_DURATION_ISSUES=$(grep -rn --include="*.go" --exclude="*_test.go" --exclude="module.go" \
    -E 'Timeout:\s*[0-9]+\s*\*\s*time\.' \
    internal/scraper/ 2>/dev/null | grep -v 'settings\.Timeout' | grep -v 'resolvedTimeout' || true)

if [[ -n "$TIME_DURATION_ISSUES" ]]; then
    DRIFT_ISSUES="${DRIFT_ISSUES}Time.Duration hardcoded Timeout:\n${TIME_DURATION_ISSUES}\n"
fi

if [[ -n "$DRIFT_ISSUES" ]]; then
    echo -e "${RED}✗ Runtime config drift detected!${NC}"
    echo -e "${YELLOW}Scraper constructors have hardcoded values that may ignore user settings:${NC}"
    echo ""
    echo -e "$DRIFT_ISSUES"
    echo -e "${YELLOW}Fix: Pass settings.Timeout, settings.RetryCount, settings.RateLimit instead of hardcoding.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ No hardcoded config values detected${NC}"
echo ""

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Config synchronization validation passed!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}config.yaml.example and DefaultConfig() are synchronized.${NC}"
echo -e "${BLUE}Scraper constructors respect user config (no hardcoded drift).${NC}"

exit 0
