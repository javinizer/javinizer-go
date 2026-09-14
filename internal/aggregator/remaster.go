package aggregator

import (
	"regexp"
	"strings"
)

var remasterDisplayFoldRegex = regexp.MustCompile(`(?i)^(.+?)[-_.\s](HD|AI)$`)

// foldRemasterDisplayID canonicalizes scraper-returned display IDs for
// remastered releases: separated marker spellings fold into the canonical
// fused form (RCT-156-HD -> RCT-156H). Marker-free IDs are returned unchanged.
func foldRemasterDisplayID(id string) string {
	m := remasterDisplayFoldRegex.FindStringSubmatch(strings.TrimSpace(id))
	if m == nil {
		return id
	}
	if strings.EqualFold(m[2], "HD") {
		return m[1] + "H"
	}
	return m[1] + "AI"
}
