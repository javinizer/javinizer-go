package matcher

import (
	"regexp"
	"strings"
)

var (
	fusedRemasterRegex  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([a-z]{2,6})(\d{3})(hd|ai|h)(?:$|[-_.\s])`)
	reRemasterRemainder = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)(?:$|[-_.\s])`)
	contentIDShapeRegex = regexp.MustCompile(`(?i)^((?:\d{1,5}[A-Za-z]{2,6}\d{3,5}[A-Za-z]{0,3}|[A-Za-z]{2,6}\d{4,5}[A-Za-z]{0,3}))([-_.\s].+)?$`)
)

func normalizeFusedRemasterFilename(name string) string {
	m := fusedRemasterRegex.FindStringSubmatchIndex(name)
	if m == nil {
		return ""
	}
	return name[:m[4]] + "-" + name[m[4]:]
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
	m := contentIDShapeRegex.FindStringSubmatch(s)
	if m == nil {
		return "", ""
	}
	return m[1], strings.TrimSpace(s[len(m[1]):])
}

func matchContentIDShape(s string) string {
	idText, _ := contentIDPrefixMatch(s)
	if idText == "" {
		return ""
	}
	return strings.ToUpper(idText)
}
