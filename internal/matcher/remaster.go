package matcher

import (
	"regexp"
	"strings"
)

var (
	fusedRemasterRegex      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)((?:\d{1,3}|\d{6}))([ez]?)(hd|ai|h)(?:$|[-_.\s[\]()])`)
	separatedRemasterRegex  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)[._\s]+(\d{1,6})([ez]?)?[-._\s]?(hd|ai|h)(?:$|[-_.\s[\]()])`)
	reRemasterRemainder     = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)(?:$|[-_.\s[\]()])`)
	remasterCodecTailRegex  = regexp.MustCompile(`(?i)^[-_.\s]?\d{3}(?:\D|$)`)
	contentIDShapeRegex     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[A-Za-z]{0,3}))(?:[-_.\s[\]()](.*)$|$)`)
	trailingCatalogIDRegex  = regexp.MustCompile(`(?i)(?:[a-z]{1,}-\d{1,}\b|t28-\d{1,}\b|[hn]_\d+[a-z]+\d+|\b[a-z]+\d{4,5}[a-z]{0,3}\b|\b\d+[a-z]{2,}\d+[a-z]{0,3}\b|\b(?:t28|[a-z]{1,8})[-._\s]\d{1,6}(?:[-._\s]?(?:hd|ai|h))?\b)`)
	remasterPartLabelRegex  = regexp.MustCompile(`(?i)\b(?:part|pt|disc|vol|cd)-?\d{1,2}\b`)
	resolutionTokenRegex    = regexp.MustCompile(`(?i)^\d{3,4}x\d{3,4}$`)
	resolutionTailRegex     = regexp.MustCompile(`(?i)^[-_.\s]?(?:\d{3,4}[pi]|\d{3,4}x\d{3,4})(?:\D|$)`)
	framerateTokenRegex     = regexp.MustCompile(`(?i)^\d{3,4}[pi](?:\d{2,3})?$`)
	remasterMarkerTailRegex = regexp.MustCompile(`(?i)(?:ez)?(?:hd|ai|h)$`)
	rawTokenRegex           = regexp.MustCompile(`[A-Za-z0-9]+`)
	strongRawTokenRegex     = regexp.MustCompile(`(?i)^(?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[ez]?(?:hd|ai|h))$`)
)

func builtinStartsInsideContentID(s string, pattern *regexp.Regexp) bool {
	start, end, ok := contentIDCandidate(s)
	if !ok {
		return false
	}
	match := pattern.FindStringSubmatchIndex(s)
	return len(match) > 3 && match[2] > start && match[2] < end
}

func normalizeFusedRemasterFilename(name string) string {
	m := fusedRemasterRegex.FindStringSubmatchIndex(name)
	fused := m != nil
	if m == nil {
		m = separatedRemasterRegex.FindStringSubmatchIndex(name)
	}
	if m == nil {
		return ""
	}
	// Part labels (part-2, pt 3) are not catalog ids and must not suppress
	// the fused normalization.
	remainder := remasterPartLabelRegex.ReplaceAllString(name[m[1]:], "")
	if trailingCatalogIDRegex.MatchString(remainder) {
		return normalizeFusedRemasterFilename(name[m[1]:])
	}
	// A prefix-free compact t28 tail with a three-digit number reads as the
	// T-series release T-28123H (catalog-prefixed or separator-pinned forms
	// stay T28-123).
	if fused && strings.EqualFold(name[m[2]:m[3]], "t28") && m[5]-m[4] == 3 {
		return name[:m[2]] + "t-28" + name[m[4]:]
	}
	return name[:m[3]] + "-" + name[m[4]:]
}

func remainderAfterID(name, id string) string {
	lowerName := strings.ToLower(name)
	lowerID := strings.ToLower(id)
	idx := strings.Index(lowerName, lowerID)
	if idx < 0 {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(name[idx+len(id):])
}

func splitRemasterMarker(remainder string) (string, string) {
	remainder = strings.TrimSpace(remainder)
	m := reRemasterRemainder.FindStringSubmatchIndex(remainder)
	if m == nil {
		return "", remainder
	}
	marker := strings.ToUpper(remainder[m[2]:m[3]])
	// H/HD before codec digits stays ambiguous (H.264-class), but AI is an
	// explicit release marker and must survive a following codec tag, and a
	// resolution tag (720p, 1080i) is not a codec spelling.
	if marker != "AI" && remasterCodecTailRegex.MatchString(remainder[m[3]:]) && !resolutionTailRegex.MatchString(remainder[m[3]:]) {
		return "", remainder
	}
	return marker, remainder[m[3]:]
}

func remasterMarkerSpelling(remainder string) string {
	spelling, _ := splitRemasterMarker(remainder)
	return spelling
}

func foldRemasterMarker(spelling string) string {
	if spelling == "HD" {
		return "H"
	}
	return spelling
}

// contentIDPrefixMatch extracts a content-id prefix from a stem, returning the
// captured id text and the post-id remainder (which may carry part suffixes).
// contentIDCandidate locates the raw content id the name should resolve to.
// The leftmost shape match wins unless it is a weak prefixless form without a
// marker tail (a word plus a year, e.g. birthday2024), in which case a
// numeric-prefixed or marker-bearing raw id later in the name outranks it.
func contentIDCandidate(s string) (start, end int, ok bool) {
	m := contentIDShapeRegex.FindStringSubmatchIndex(s)
	if m == nil {
		return 0, 0, false
	}
	id := s[m[2]:m[3]]
	if remasterMarkerTailRegex.MatchString(id) {
		return m[2], m[3], true
	}
	if isResolutionToken(id) {
		// The leftmost shape hit is a resolution token (1080p60, 1920x1080);
		// it never becomes the candidate, but a strong raw id later in the
		// name (e.g. 1080p60 1rct00156h.mkv) still wins, as with the weak
		// standalone-token case. Without one, there is no candidate.
		for _, loc := range rawTokenRegex.FindAllStringIndex(s, -1) {
			token := s[loc[0]:loc[1]]
			if !strongRawTokenRegex.MatchString(token) || isResolutionToken(token) {
				continue
			}
			return loc[0], loc[1], true
		}
		return 0, 0, false
	}
	for _, loc := range rawTokenRegex.FindAllStringIndex(s, -1) {
		token := s[loc[0]:loc[1]]
		if !strongRawTokenRegex.MatchString(token) || isResolutionToken(token) {
			continue
		}
		return loc[0], loc[1], true
	}
	return m[2], m[3], true
}

func contentIDPrefixMatch(s string) (idText string, remainder string) {
	start, end, ok := contentIDCandidate(s)
	if !ok {
		return "", ""
	}
	return s[start:end], strings.TrimSpace(s[end:])
}

func matchContentIDShape(s string) string {
	idText, _ := contentIDPrefixMatch(s)
	if idText == "" {
		return ""
	}
	return strings.ToUpper(idText)
}

func isResolutionToken(idText string) bool {
	return resolutionTokenRegex.MatchString(idText) || framerateTokenRegex.MatchString(idText)
}
