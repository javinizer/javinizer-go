package matcher

import (
	"regexp"
	"strings"
)

var (
	reRemasterRemainder  = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)$`)
	contentIDShapeRegex  = regexp.MustCompile(`(?i)^(?:\d{1,5}[A-Za-z]{2,6}\d{3,5}[A-Za-z]{0,3}|[A-Za-z]{2,6}\d{4,5}[A-Za-z]{0,3})$`)
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

func matchContentIDShape(s string) string {
	t := contentIDExtStripReg.ReplaceAllString(s, "")
	if contentIDShapeRegex.MatchString(t) {
		return strings.ToUpper(t)
	}
	return ""
}
