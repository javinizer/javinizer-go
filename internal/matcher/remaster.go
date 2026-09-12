package matcher

import (
	"regexp"
	"strings"
)

var (
	fusedRemasterRegex     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)((?:\d{1,3}|\d{6}))([ez]?)(hd|ai|h)(?:$|[-_.\s[\]()])`)
	separatedRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)[._\s]+(\d{1,6})([ez]?)?[-._\s]?(hd|ai|h)(?:$|[-_.\s[\]()])`)
	reRemasterRemainder    = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)(?:$|[-_.\s[\]()])`)
	remasterCodecTailRegex = regexp.MustCompile(`(?i)^[-_.\s]?26[45](?:\D|$)`)
	contentIDShapeRegex    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[A-Za-z]{0,3}))([-_.\s[\]()].+)?$`)
	trailingCatalogIDRegex = regexp.MustCompile(`(?i)(?:[a-z]{2,6}-\d{3,5}\b|t28-\d{1,5}\b|[hn]_\d+[a-z]+\d+|\b[a-z]+\d{4,5}[a-z]{0,3}\b)`)
	resolutionTokenRegex   = regexp.MustCompile(`(?i)^\d{3,4}x\d{3,4}$`)
	framerateTokenRegex    = regexp.MustCompile(`(?i)^\d{3,4}[pi](?:\d{2,3})?$`)
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
	if trailingCatalogIDRegex.MatchString(name[m[1]:]) {
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
	if remasterCodecTailRegex.MatchString(remainder[m[3]:]) {
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
	idText = s[m[2]:m[3]]
	if isResolutionToken(idText) {
		return "", ""
	}
	return idText, strings.TrimSpace(s[m[3]:])
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
