package matcher

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

// Matcher identifies JAV IDs from filenames
type Matcher struct {
	config         *config.MatchingConfig
	regexPattern   *regexp.Regexp
	builtinPattern *regexp.Regexp
}

// MatchResult represents a matched file with extracted ID
type MatchResult struct {
	File        scanner.FileInfo
	ID          string // Extracted JAV ID (e.g., "IPX-535")
	PartNumber  int    // 0 = single-part, 1..N = part index
	PartSuffix  string // "-A", "-pt1", "-part2" (always with leading dash)
	IsMultiPart bool   // Whether this is a multi-part file
	MatchedBy   string // "regex" or "builtin"
}

// NewMatcher creates a new file matcher
func NewMatcher(cfg *config.MatchingConfig) (*Matcher, error) {
	m := &Matcher{
		config: cfg,
	}

	// Compile built-in pattern (covers most JAV IDs)
	// Matches:
	//   - DMM h_<digits> prefix format: h_1472smkcx003 (DMM content-ID format)
	//   - Standard JAV: ABC-123, ABC-123Z, ABC-123E, T28-123, etc.
	//   - Potential amateur: 3-6 letters + 3-4 digits (no hyphen, word boundary)
	//
	// Strategy: Be lenient in the matcher - catch potential matches generically.
	// Amateur detection happens later during DMM search via heuristics and caching.
	// False positives (like "video1080") will fail gracefully during search (no results).
	// This allows new amateur series to work automatically without code changes.
	//
	// Pattern combines formats with OR (|) operator:
	//   1. h_ prefix format: h_<digits><letters><digits> (e.g., h_1472smkcx003)
	//   2. No-hyphen format: word boundary + 3-6 letters + 3-4 digits + word boundary
	//      (prevents partial matches like "PPV1234" from "FC2PPV123456")
	//   3. Hyphen format: letters + hyphen + digits (standard JAV)
	builtinPattern := `(?i)((?:h_\d+[a-z]+\d+)|(?:\b[A-Za-z]{3,6}\d{3,4}\b)|(?:(?:[A-Za-z]+|T28)-\d+(?:[ZE])?))`
	compiled, err := regexp.Compile(builtinPattern)
	if err != nil {
		return nil, err
	}
	m.builtinPattern = compiled

	// Compile custom regex if enabled
	if cfg.RegexEnabled && cfg.RegexPattern != "" {
		customPattern, err := regexp.Compile(cfg.RegexPattern)
		if err != nil {
			return nil, err
		}
		m.regexPattern = customPattern
	}

	return m, nil
}

// Match extracts JAV IDs from a list of files
func (m *Matcher) Match(files []scanner.FileInfo) []MatchResult {
	results := make([]MatchResult, 0)

	for _, file := range files {
		if result := m.MatchFile(file); result != nil {
			results = append(results, *result)
		}
	}

	return results
}

// MatchFile attempts to extract a JAV ID from a single file
func (m *Matcher) MatchFile(file scanner.FileInfo) *MatchResult {
	// Get filename without extension
	basename := filepath.Base(file.Name)
	nameWithoutExt := strings.TrimSuffix(basename, file.Extension)

	// Try custom regex first if enabled
	if m.config.RegexEnabled && m.regexPattern != nil {
		if result := m.matchWithRegex(file, nameWithoutExt, m.regexPattern, "regex"); result != nil {
			return result
		}
	}

	// Fall back to built-in pattern
	return m.matchWithRegex(file, nameWithoutExt, m.builtinPattern, "builtin")
}

// matchWithRegex attempts to match a filename with a specific regex pattern
func (m *Matcher) matchWithRegex(file scanner.FileInfo, filename string, pattern *regexp.Regexp, matchType string) *MatchResult {
	matches := pattern.FindStringSubmatch(filename)
	if len(matches) == 0 {
		return nil
	}

	result := &MatchResult{
		File:      file,
		MatchedBy: matchType,
	}

	// First capture group is the ID
	if len(matches) > 1 {
		result.ID = strings.ToUpper(matches[1])
	}

	// Detect part suffix from the rest of the filename
	num, suffix := DetectPartSuffix(filename, result.ID)
	result.PartNumber = num
	result.PartSuffix = suffix
	result.IsMultiPart = num > 0 || suffix != ""

	return result
}

// MatchString is a helper to extract ID from a string directly
func (m *Matcher) MatchString(s string) string {
	// Try custom regex first
	if m.config.RegexEnabled && m.regexPattern != nil {
		matches := m.regexPattern.FindStringSubmatch(s)
		if len(matches) > 1 {
			return strings.ToUpper(matches[1])
		}
	}

	// Try built-in pattern
	matches := m.builtinPattern.FindStringSubmatch(s)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}

	return ""
}

// GroupByID groups match results by their ID
func GroupByID(results []MatchResult) map[string][]MatchResult {
	grouped := make(map[string][]MatchResult)

	for _, result := range results {
		grouped[result.ID] = append(grouped[result.ID], result)
	}

	return grouped
}

// FilterMultiPart filters results to only include multi-part files
func FilterMultiPart(results []MatchResult) []MatchResult {
	filtered := make([]MatchResult, 0)

	for _, result := range results {
		if result.IsMultiPart {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// FilterSinglePart filters results to only include single-part files
func FilterSinglePart(results []MatchResult) []MatchResult {
	filtered := make([]MatchResult, 0)

	for _, result := range results {
		if !result.IsMultiPart {
			filtered = append(filtered, result)
		}
	}

	return filtered
}
