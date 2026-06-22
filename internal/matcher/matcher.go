package matcher

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Matcher identifies JAV IDs from filenames
type Matcher struct {
	config         *Config
	regexPattern   *regexp.Regexp
	builtinPattern *regexp.Regexp
}

// MatchResult represents a matched file with extracted ID
type MatchResult struct {
	File             models.FileMatchInfo
	ID               string // Extracted JAV ID (e.g., "IPX-535")
	PartNumber       int    // 0 = single-part, 1..N = part index
	PartSuffix       string // "-A", "-pt1", "-part2" (always with leading dash)
	IsMultiPart      bool   // Whether this is a multi-part file
	MatchedBy        string // "regex" or "builtin"
	MultipartPattern string // Pattern type: "explicit", "letter", "trailing", or "" (see PatternExplicit, PatternLetter, PatternTrailing, PatternNone)
	TrailingPrefix   string // For PatternTrailing: noise portion before the part number (e.g., "-un-javgg.net")
}

// NewMatcher creates a new file matcher
func NewMatcher(cfg *Config) (*Matcher, error) {
	m := &Matcher{
		config: cfg,
	}

	// Compile built-in pattern (covers most JAV IDs)
	// Matches:
	//   - DMM h_<digits> prefix format: h_1472smkcx003 (DMM content-ID format)
	//   - Date-based uncensored IDs: 020326_001-1PON, 020326_01-10MU, 123025-001-CARIB
	//   - Standard JAV: ABC-123, ABC-123Z, ABC-123E, T28-123, etc.
	//   - Short-prefix no-hyphen: N1234, AB567 (TokyoHot-style IDs)
	//   - Potential amateur: 3-6 letters + 3-4 digits (no hyphen, word boundary)
	//
	// Strategy: Be lenient in the matcher - catch potential matches generically.
	// Amateur detection happens later during DMM search via heuristics and caching.
	// False positives (like "video1080") will fail gracefully during search (no results).
	// This allows new amateur series to work automatically without code changes.
	//
	// Pattern combines formats with OR (|) operator:
	//   1. h_ prefix format: h_<digits><letters><digits> (e.g., h_1472smkcx003)
	//   2. Date-based uncensored: word boundary + 6 digits + separator + 2-3 digits + known source suffix
	//   3. Short-prefix no-hyphen: word boundary + 1-2 letters + 3-5 digits (e.g., N1234, AB567)
	//   4. No-hyphen format: word boundary + 3-6 letters + 3-4 digits + word boundary
	//      (prevents partial matches like "PPV1234" from "FC2PPV123456")
	//   5. Hyphen format: letters + hyphen + digits (standard JAV)
	builtinPattern := `(?i)((?:h_\d+[a-z]+\d+)|(?:\b\d{6}[-_]\d{2,3}-(?:1PON|10MU|CARIB)\b)|(?:\b[A-Za-z]{1,2}\d{3,5}\b)|(?:\b[A-Za-z]{3,6}\d{3,4}\b)|(?:(?:[A-Za-z]+|T28)-\d+(?:[ZE])?))`
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
func (m *Matcher) Match(files []models.FileMatchInfo) []MatchResult {
	results := make([]MatchResult, 0)

	for _, file := range files {
		if result := m.MatchFile(file); result != nil {
			results = append(results, *result)
		}
	}

	return results
}

// MatchFile attempts to extract a JAV ID from a single file
func (m *Matcher) MatchFile(file models.FileMatchInfo) *MatchResult {
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
func (m *Matcher) matchWithRegex(file models.FileMatchInfo, filename string, pattern *regexp.Regexp, matchType string) *MatchResult {
	matches := pattern.FindStringSubmatch(filename)
	if len(matches) == 0 {
		return nil
	}
	if len(matches) <= 1 {
		// No capture group means no usable ID for matcher output.
		return nil
	}
	id := strings.TrimSpace(matches[1])
	if id == "" {
		// Empty capture should be treated as no match to allow fallback behavior.
		return nil
	}

	result := &MatchResult{
		File:      file,
		MatchedBy: matchType,
	}

	// First capture group is the ID.
	result.ID = strings.ToUpper(id)

	// Detect part suffix from the rest of the filename
	num, suffix, patternType, trailingPrefix := DetectPartSuffix(filename, result.ID)
	result.PartNumber = num
	result.PartSuffix = suffix
	result.MultipartPattern = patternType
	result.TrailingPrefix = trailingPrefix
	// Only mark explicit patterns as multipart immediately.
	// Letter and trailing patterns need directory context validation via ValidateMultipartInDirectory().
	result.IsMultiPart = patternType == PatternExplicit

	return result
}

// MatchString is a helper to extract ID from a string directly
func (m *Matcher) MatchString(s string) string {
	// Try custom regex first
	if m.config.RegexEnabled && m.regexPattern != nil {
		matches := m.regexPattern.FindStringSubmatch(s)
		if len(matches) > 1 {
			id := strings.TrimSpace(matches[1])
			if id != "" {
				return strings.ToUpper(id)
			}
		}
	}

	// Try built-in pattern
	matches := m.builtinPattern.FindStringSubmatch(s)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}

	return ""
}

// ValidateMultipartInDirectory validates ambiguous multipart patterns
// (letter-based and trailing-number) by checking for sibling files in the same
// directory with the same ID.
//
// Validation rules:
//   - PatternLetter: 2+ letter-pattern files with the same ID in the same directory
//   - PatternTrailing: 2+ trailing-pattern files with the same ID, same directory,
//     AND same TrailingPrefix (the noise portion before the part number must match)
//   - PatternLetter and PatternTrailing do NOT cross-validate — they are separate conventions
//
// This prevents false positives for:
//   - "ABW-121-C.mp4" where -C means Chinese subtitles, not part 3
//   - "IPX-535-uncen-1.mp4" alone where -1 is not a part number
//   - "IPX-535-uncen-1.mp4" + "IPX-535-C.mp4" which are different variants, not parts
func ValidateMultipartInDirectory(results []MatchResult) []MatchResult {
	if len(results) == 0 {
		return results
	}

	// Create a copy to avoid modifying input slice
	validated := make([]MatchResult, len(results))
	copy(validated, results)

	// Group by (directory, movieID)
	type dirIDKey struct {
		dir string
		id  string
	}
	groups := make(map[dirIDKey][]int)

	for i, r := range validated {
		key := dirIDKey{dir: filepath.Dir(r.File.Path), id: r.ID}
		groups[key] = append(groups[key], i)
	}

	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}

		// Validate letter patterns: 2+ letter-pattern files with same ID = multipart
		letterIndices := []int{}
		for _, idx := range indices {
			if validated[idx].MultipartPattern == PatternLetter {
				letterIndices = append(letterIndices, idx)
			}
		}
		if len(letterIndices) >= 2 {
			for _, idx := range letterIndices {
				validated[idx].IsMultiPart = true
			}
		}

		// Validate trailing patterns: group by prefix, 2+ with same prefix = multipart
		prefixGroups := make(map[string][]int)
		for _, idx := range indices {
			if validated[idx].MultipartPattern == PatternTrailing {
				prefix := strings.ToLower(validated[idx].TrailingPrefix)
				prefixGroups[prefix] = append(prefixGroups[prefix], idx)
			}
		}
		for _, trailingIndices := range prefixGroups {
			if len(trailingIndices) >= 2 {
				for _, idx := range trailingIndices {
					validated[idx].IsMultiPart = true
				}
			}
		}
	}

	return validated
}
