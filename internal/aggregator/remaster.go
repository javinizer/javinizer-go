package aggregator

import (
	"regexp"
	"strings"
)

var (
	// remasterDisplayFoldRegex folds the separated marker spellings; JavDB
	// preserves the detail page's single-letter H spelling verbatim, so the
	// lone H folds like -HD into the fused marker.
	remasterDisplayFoldRegex = regexp.MustCompile(`(?i)^(.+?)[-_.\s](HD|AI|H)$`)
	// remasterCompactDisplayFoldRegex folds separator-free spellings when the
	// marker directly follows the catalog number (RCT156HD -> RCT156H); a
	// marker glued to a series word (ABCHD) is ambiguous and stays unchanged.
	remasterCompactDisplayFoldRegex = regexp.MustCompile(`(?i)^(.+?\d)(HD|AI)$`)
)

// foldRemasterDisplayID canonicalizes scraper-returned display IDs for
// remastered releases: marker spellings fold into the canonical fused form,
// separated (RCT-156-HD, RCT-156-H) and compact (RCT156HD) alike
// (-> RCT-156H).
// Marker-free IDs are returned unchanged.
func foldRemasterDisplayID(id string) string {
	trimmed := strings.TrimSpace(id)
	if m := remasterDisplayFoldRegex.FindStringSubmatch(trimmed); m != nil {
		return foldRemasterMarkerSpelling(m[1], m[2])
	}
	if m := remasterCompactDisplayFoldRegex.FindStringSubmatch(trimmed); m != nil {
		return foldRemasterMarkerSpelling(m[1], m[2])
	}
	return id
}

// foldRemasterMarkerSpelling appends the canonical single-letter marker for
// the HD spellings (HD and the separated lone H) and keeps AI verbatim.
func foldRemasterMarkerSpelling(base, marker string) string {
	if strings.EqualFold(marker, "HD") || strings.EqualFold(marker, "H") {
		return base + "H"
	}
	return base + "AI"
}
