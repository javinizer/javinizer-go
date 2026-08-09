package r18devdump

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var contentIDFullRegex = regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)

// underscoreContentIDRegex recognizes PPV-style content_ids (h_086mesu00103),
// which SplitSeriesAndNumber cannot decompose because of the underscore.
var underscoreContentIDRegex = regexp.MustCompile(`^[a-z]_\d+[a-z]+\d+`)

// ContentIDCandidates constructs possible content_id formats from a dvd_id.
// For "START-575", generates: ["1start00575", "1start575"]
// For "ABF-346", generates: ["118abf00346", "118abf346", "436abf00346", "436abf346"]
// The r18.dev content_id format is: [DMM-prefix][series][zero-padded-number]
// Uses the ContentIDPrefixLookup table built from r18.dev database dumps to find
// known prefixes per series. Falls back to common prefixes if the series is unknown.
func ContentIDCandidates(id string) []string {
	// Identity candidate: the input itself in content-id form. It leads for
	// content-id-shaped input (leading numeric prefix or zero-padded 5-digit
	// number, e.g. "118ipx00535", "lulu00441") so exact content_id queries
	// honor their own row; for display-id-shaped input it is appended last so
	// canonical zero-padded variants keep priority.
	direct := strings.ToLower(normalizeDVDID(id))

	// Normalize before splitting: tolerate surrounding/internal whitespace and
	// display-id formatting so inputs like " LULU-441 " or "LULU 441" expand.
	trimmed := strings.TrimSpace(id)
	series, numStr := SplitSeriesAndNumber(trimmed)
	if series == "" || numStr == "" {
		if direct != "" {
			series, numStr = SplitSeriesAndNumber(direct)
		}
	}
	if series == "" || numStr == "" {
		if direct != "" && underscoreContentIDRegex.MatchString(direct) {
			return []string{direct}
		}
		return nil
	}

	series = strings.ToLower(series)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return nil
	}

	padded3 := fmt.Sprintf("%03d", num)
	padded5 := fmt.Sprintf("%05d", num)

	// Look up known prefixes for this series from the r18.dev database dump
	var prefixes []string
	if lookup, ok := ContentIDPrefixLookup[series]; ok {
		prefixes = lookup
	} else {
		// Fallback: try common prefixes for unknown series
		prefixes = []string{"", "1"}
	}

	var variations []string
	seen := make(map[string]bool)

	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			variations = append(variations, v)
		}
	}

	for _, prefix := range prefixes {
		// 5-digit padded (standard DMM content_id format)
		add(prefix + series + padded5)
		// 3-digit padded (used by many r18.dev content_ids)
		add(prefix + series + padded3)
	}

	if direct != "" {
		if looksLikeContentID(direct) {
			if seen[direct] {
				out := []string{direct}
				for _, v := range variations {
					if v != direct {
						out = append(out, v)
					}
				}
				return out
			}
			return append([]string{direct}, variations...)
		}
		add(direct)
	}

	return variations
}

// looksLikeContentID reports whether a normalized input already looks like a
// content_id rather than a display dvd_id: the PPV letter_digits underscore
// form (h_086mesu00103), or a leading numeric DMM prefix (118ipx00535,
// 436abf00030). The check is deliberately prefix-based: a zero-padded number
// alone (abf00030, from the display id ABF-00030) must NOT qualify, or display
// ids with padded numbers would reorder ahead of their canonical prefixed
// variants.
func looksLikeContentID(direct string) bool {
	return underscoreContentIDRegex.MatchString(direct) ||
		(direct != "" && direct[0] >= '0' && direct[0] <= '9')
}

// SplitSeriesAndNumber splits a dvd_id like "START-575" into ("START", "575")
func SplitSeriesAndNumber(id string) (string, string) {
	// Try standard format: SERIES-NUMBER
	if parts := strings.SplitN(id, "-", 2); len(parts) == 2 {
		if isAlpha(parts[0]) && isDigit(parts[1]) {
			return parts[0], parts[1]
		}
	}

	// Try already-normalized format: series575 (from normalizeID)
	lowered := strings.ToLower(id)
	if m := contentIDFullRegex.FindStringSubmatch(lowered); len(m) >= 4 {
		return m[2], m[3]
	}

	return "", ""
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

func isDigit(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
