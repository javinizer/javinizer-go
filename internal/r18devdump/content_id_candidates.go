package r18devdump

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var contentIDFullRegex = regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)

// zeroPaddedCIDRegex matches prefixless digital content ids with a five-digit
// zero-padded number (rct00156hd-class).
var zeroPaddedCIDRegex = regexp.MustCompile(`^(?:t28|[a-z]+)\d{5}[a-z]{0,3}$`)

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
	return contentIDCandidates(id, false)
}

func contentIDCandidates(id string, markerAware bool) []string {
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

	if markerAware {
		if m := t28RemasterBaseRegex.FindStringSubmatch(direct); m != nil {
			series, numStr = "t28", m[1]
		}
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
	if markerAware && series == "t28" {
		prefixes = []string{"9", "", "1"}
	} else if lookup, ok := ContentIDPrefixLookup[series]; ok {
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

var t28RemasterBaseRegex = regexp.MustCompile(`(?i)^t28(\d{2,5})$`)

var remasterMarkerTailRgx = regexp.MustCompile(`(?i)^(.*\d)(hd|ai|h)$`)

// ContentIDCandidatesWithMarker is ContentIDCandidates for marker-bearing
// inputs: the trailing H/HD/AI marker is split off, base candidates are built
// from the core id, and the folded marker (hd -> h) is re-appended to every
// candidate after zero-padding and DMM prefixing.
func ContentIDCandidatesWithMarker(id string) []string {
	raw := strings.ToLower(strings.TrimSpace(id))
	if parts := remasterMarkerTailRgx.FindStringSubmatch(raw); parts != nil && underscoreContentIDRegex.FindString(parts[1]) == parts[1] {
		return []string{raw}
	}
	// Normalize display separators first: advertised spellings (RCT-156-HD,
	// DV-818-AI, RCT-156 HD) place a separator between number and marker.
	compacted := strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(strings.TrimSpace(id))
	m := remasterMarkerTailRgx.FindStringSubmatch(compacted)
	if m == nil {
		return ContentIDCandidates(id)
	}
	marker := strings.ToLower(m[2])
	// Only display spellings fold HD to H; separator evidence in the original
	// input forces display semantics even when the compacted shape looks like
	// a zero-padded content id (RCT-00156-HD vs raw rct00156hd).
	hasSeparator := strings.ContainsAny(id, "-_. ")
	if marker == "hd" && (hasSeparator || (!looksLikeContentID(strings.ToLower(compacted)) && !zeroPaddedCIDRegex.MatchString(strings.ToLower(compacted)))) {
		marker = "h"
	}
	base := contentIDCandidates(m[1], true)
	out := make([]string, 0, len(base)+1)
	if !hasSeparator && zeroPaddedCIDRegex.MatchString(raw) {
		out = append(out, raw)
	}
	for _, c := range base {
		candidate := c + marker
		if len(out) == 0 || candidate != out[0] {
			out = append(out, candidate)
		}
	}
	return out
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
