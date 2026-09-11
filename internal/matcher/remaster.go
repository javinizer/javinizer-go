package matcher

import (
	"regexp"
	"strings"
)

var (
	reRemasterRemainder  = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)$`)
	contentIDShapeRegex  = regexp.MustCompile(`(?i)^((?:\d{1,5}[A-Za-z]{2,6}\d{3,5}[A-Za-z]{0,3}|[A-Za-z]{2,6}\d{4,5}[A-Za-z]{0,3}))([-_.\s].+)?$`)
	contentIDExtStripReg = regexp.MustCompile(`(?i)\.[A-Za-z0-9]{2,5}$`)
)

func remainderAfterID(name, id string) string {
	lowerName := strings.ToLower(name)
	lowerID := strings.ToLower(id)
	idx := strings.Index(lowerName, lowerID)
	if idx < 0 {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(name[idx+len(id):])
}

func remasterMarkerSpelling(remainder string) string {
	m := reRemasterRemainder.FindStringSubmatch(strings.TrimSpace(remainder))
	if len(m) != 2 {
		return ""
	}
	return strings.ToUpper(m[1])
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
	t := contentIDExtStripReg.ReplaceAllString(s, "")
	m := contentIDShapeRegex.FindStringSubmatch(t)
	if m == nil {
		return "", ""
	}
	return m[1], strings.TrimSpace(t[len(m[1]):])
}

func matchContentIDShape(s string) string {
	idText, _ := contentIDPrefixMatch(s)
	if idText == "" {
		return ""
	}
	return strings.ToUpper(idText)
}
