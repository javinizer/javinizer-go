package matcher

import (
	"regexp"
	"strings"
)

var (
	fusedRemasterRegex     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)(\d{2,3})(hd|ai|h)(?:$|[-_.\s])`)
	separatedRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)[._\s]+(\d{2,5})[-._\s]?(hd|ai|h)(?:$|[-_.\s])`)
	reRemasterRemainder    = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)(?:$|[-_.\s])`)
	contentIDShapeRegex    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:\d+(?:t28|[A-Za-z]+)\d{3,5}[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[A-Za-z]{0,3}))([-_.\s].+)?$`)
)

func builtinStartsInsideContentID(s string, pattern *regexp.Regexp) bool {
	raw := contentIDShapeRegex.FindStringSubmatchIndex(s)
	if raw == nil {
		return false
	}
	match := pattern.FindStringSubmatchIndex(s)
	return len(match) > 3 && match[2] > raw[2] && match[2] < raw[3]
}

func normalizeFusedRemasterFilename(name string) string {
	m := fusedRemasterRegex.FindStringSubmatchIndex(name)
	if m == nil {
		m = separatedRemasterRegex.FindStringSubmatchIndex(name)
	}
	if m == nil {
		return ""
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
	return strings.ToUpper(remainder[m[2]:m[3]]), remainder[m[3]:]
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
func contentIDPrefixMatch(s string) (idText string, remainder string) {
	m := contentIDShapeRegex.FindStringSubmatchIndex(s)
	if m == nil {
		return "", ""
	}
	return s[m[2]:m[3]], strings.TrimSpace(s[m[3]:])
}

func matchContentIDShape(s string) string {
	idText, _ := contentIDPrefixMatch(s)
	if idText == "" {
		return ""
	}
	return strings.ToUpper(idText)
}
