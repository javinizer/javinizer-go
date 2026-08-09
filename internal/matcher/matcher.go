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
	strippedSuffix   string // E/Z catalog suffix stripped from the ID at match time; restored by ValidateMultipartInDirectory if the part does not confirm
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
	m.builtinPattern = regexp.MustCompile(builtinPattern)

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

	// The built-in ID pattern optionally consumes a trailing E or Z as a catalog
	// suffix (e.g. IPX-535Z). When that letter immediately precedes a digit-first
	// quality tag (the letter+trailing part form, e.g. "SVFLA-001e-4k" where 'e' is
	// part E, not a catalog suffix), strip it from the ID so the part letter is
	// detected. A bare E/Z without a trailing tag is a legitimate catalog suffix
	// (e.g. IPX-535Z) and is preserved. Only the built-in pattern applies this
	// heuristic; a custom regex that explicitly captures an E/Z-suffixed ID is
	// authoritative and must not be overridden.
	if matchType == "builtin" {
		n := len(result.ID)
		if n > 1 && (result.ID[n-1] == 'E' || result.ID[n-1] == 'Z') {
			baseID := result.ID[:n-1]
			num, suffix, patternType, trailingPrefix := DetectPartSuffix(filename, baseID)
			if patternType == PatternLetter && trailingPrefix != "" {
				result.strippedSuffix = string(result.ID[n-1])
				result.ID = baseID
				result.PartNumber = num
				result.PartSuffix = suffix
				result.MultipartPattern = patternType
				result.TrailingPrefix = trailingPrefix
				result.IsMultiPart = false
				return result
			}
		}
	}

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
		id := strings.ToUpper(matches[1])
		// Apply the same E/Z catalog-suffix stripping as matchWithRegex so MatchString
		// stays consistent with MatchFile for downstream re-match callers (e.g. the
		// scrape phase re-deriving a movie ID from a filename). A bare E/Z without a
		// trailing digit-first quality tag is a legitimate catalog suffix and preserved.
		if n := len(id); n > 1 && (id[n-1] == 'E' || id[n-1] == 'Z') {
			baseID := id[:n-1]
			_, _, patternType, trailingPrefix := DetectPartSuffix(s, baseID)
			if patternType == PatternLetter && trailingPrefix != "" {
				return baseID
			}
		}
		return id
	}

	return ""
}

// ValidateMultipartInDirectory validates ambiguous multipart patterns
// (letter-based and trailing-number) by checking for sibling files in the same
// directory with the same ID.
//
// Validation rules:
//   - PatternLetter (bare, e.g. "ABW-121-C"): 2+ bare-letter files with the same ID in
//     the same directory, all with distinct part numbers (no duplicate encodes).
//   - PatternLetter (tagged, e.g. "a-4k"): 2+ tagged-letter files with the same ID, all
//     with distinct part numbers; the trailing content must be a digit-first resolution
//     tag (e.g. -4k, -1080p) and is irrelevant to part identity (the letter is the part).
//   - Bare and tagged letter conventions do NOT cross-validate for sibling confirmation,
//     but a confirmed set's part numbers block the other convention to avoid colliding
//     -pt1/-pt2 targets.
//   - PatternTrailing: 2+ trailing-pattern files with the same ID, same directory,
//     AND same TrailingPrefix.
//   - Any letter file not confirmed has PartNumber/PartSuffix cleared.
//   - Letter part numbers must not collide with explicit/trailing-confirmed parts.
//
// This prevents false positives for:
//   - "ABW-121-C.mp4" where -C means Chinese subtitles, not part 3
//   - "a-4k.mp4" + "a-4k.mkv" (duplicate encodes of the same part)
//   - "a-4k" + "a-1080p" (same part, different quality)
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

		// Validate trailing patterns first: group by prefix, 2+ with the same prefix
		// confirm as multipart. Doing this before the letter check lets the letter
		// overlap guard see already-confirmed trailing part numbers.
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

		// Validate letter patterns. The part identity is the LETTER (A/B/C...), not any
		// trailing quality tag: two files are the same part iff they share the same letter;
		// different letters are different parts. Bare letters (no trailing content, e.g.
		// "ABW-121-C") and tagged letters (trailing content, e.g. "a-4k") are separate
		// conventions and do NOT cross-validate for sibling confirmation — a bare subtitle
		// marker like -C must not be pulled into multipart by a tagged sibling. Within a
		// convention, confirm the set as multipart only when every letter occurs exactly once
		// (no duplicate encodes, e.g. a-4k.mp4 + a-4k.mkv or A/A/B) AND no part number
		// collides with a part already confirmed by explicit or trailing patterns or by the
		// OTHER letter convention (both would render the same -pt1/-pt2 targets). Trailing
		// content (e.g. -4k vs -1080p) is irrelevant to part identity, so a-4k + b-1080p
		// confirm as parts 1 and 2. Files not confirmed have their letter-part fields cleared
		// so a lone unconfirmed file does not render part metadata.
		bareLetterIndices := []int{}
		taggedLetterIndices := []int{}
		for _, idx := range indices {
			if validated[idx].MultipartPattern == PatternLetter {
				if validated[idx].TrailingPrefix == "" {
					bareLetterIndices = append(bareLetterIndices, idx)
				} else {
					taggedLetterIndices = append(taggedLetterIndices, idx)
				}
			}
		}
		confirmedLetterParts := make(map[int]struct{})
		confirmLetterSet := func(set []int) {
			if len(set) < 2 {
				return
			}
			// A stripped E/Z catalog suffix is a genuine part letter only when it has a
			// non-stripped sibling to confirm with (e.g. d-4k + e-4k). When ALL candidates
			// are stripped (e.g. E-4k + Z-4k, distinct catalog IDs), none are genuine parts;
			// exclude the whole group so they restore their original catalog IDs.
			genuineCount := 0
			for _, idx := range set {
				if validated[idx].strippedSuffix == "" {
					genuineCount++
				}
			}
			if genuineCount == 0 {
				return
			}
			countByPart := make(map[int]int)
			for _, idx := range set {
				countByPart[validated[idx].PartNumber]++
			}
			for _, c := range countByPart {
				if c > 1 {
					return
				}
			}
			for _, idx := range indices {
				if validated[idx].MultipartPattern != PatternLetter && validated[idx].IsMultiPart {
					if countByPart[validated[idx].PartNumber] > 0 {
						return
					}
				}
			}
			// Reject if any part number was already confirmed by the other letter convention
			// (bare vs tagged), since both would render the same -pt1/-pt2 targets.
			for pn := range countByPart {
				if _, ok := confirmedLetterParts[pn]; ok {
					return
				}
			}
			for _, idx := range set {
				validated[idx].IsMultiPart = true
			}
			for pn := range countByPart {
				confirmedLetterParts[pn] = struct{}{}
			}
		}
		confirmLetterSet(bareLetterIndices)
		confirmLetterSet(taggedLetterIndices)

	}

	for i := range validated {
		if validated[i].MultipartPattern == PatternLetter && !validated[i].IsMultiPart {
			if validated[i].strippedSuffix != "" {
				validated[i].ID += validated[i].strippedSuffix
				validated[i].strippedSuffix = ""
			}
			validated[i].PartNumber = 0
			validated[i].PartSuffix = ""
		}
	}

	return validated
}
