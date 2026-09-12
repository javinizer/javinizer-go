package r18dev

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

var (
	r18RemasterTailRegex = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([ez]?)(hd|ai|h)$`)
	r18CIDAnchoredRegex  = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([a-z]{0,3})$`)
	r18PrefixedCIDRegex  = regexp.MustCompile(`^[hn]_\d+(?:t28|[a-z]+)\d+[a-z]{0,3}$`)
	nonAlnumR18Regex     = regexp.MustCompile(`[^a-z0-9]+`)
)

func r18RemasterCore(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if r18PrefixedCIDRegex.MatchString(s) {
		s = s[2:]
	}
	return r18CompactID(s)
}

func r18CompactID(id string) string {
	return nonAlnumR18Regex.ReplaceAllString(strings.ToLower(strings.TrimSpace(id)), "")
}

// classifyRemaster mirrors the DMM scraper's classification for marker-bearing
// queries: the folded marker is "h" for H/HD and "ai" for AI spellings.
func classifyRemaster(id string) (foldedMarker string, series string) {
	m := r18RemasterTailRegex.FindStringSubmatch(r18RemasterCore(id))
	if m == nil {
		return "", ""
	}
	series = m[2]
	if m[5] == "ai" {
		return "ai", series
	}
	return "h", series
}

// cidMatchesMarker reports whether a content id carries the folded marker AND
// belongs to the requested series. The number is server-owned; the series is
// not — stale or unrelated marker-bearing records must not qualify.
func cidMatchesMarker(contentID, foldedMarker, series string) bool {
	if !cidCarriesMarker(contentID, foldedMarker) {
		return false
	}
	m := r18CIDAnchoredRegex.FindStringSubmatch(r18RemasterCore(contentID))
	return m != nil && m[2] == series
}

// foldDisplay normalizes a display/stored DVD ID for marker comparison:
// lowercase, alnum-only, trailing "hd" folded to "h".
func foldDisplay(s string) string {
	n := r18CompactID(s)
	if m := r18RemasterTailRegex.FindStringSubmatch(n); m != nil && m[1] == "" {
		n = m[2] + strings.TrimLeft(m[3], "0") + m[4] + m[5]
	}
	if strings.HasSuffix(n, "hd") {
		return n[:len(n)-2] + "h"
	}
	return n
}

// cidCarriesMarker reports whether a content id carries the folded marker.
func cidCarriesMarker(contentID, foldedMarker string) bool {
	c := r18CompactID(contentID)
	switch foldedMarker {
	case "ai":
		return strings.HasSuffix(c, "ai")
	case "h":
		return strings.HasSuffix(c, "h") || strings.HasSuffix(c, "hd")
	}
	return false
}

// remasterDisplaySpellings adds the hyphenated display forms r18 stores as
// dvd_id for remasters (rct-156-hd, dv-818-ai).
func remasterDisplaySpellings(id string) []string {
	m := r18RemasterTailRegex.FindStringSubmatch(r18RemasterCore(id))
	if m == nil {
		return nil
	}
	displayMarker := "hd"
	if m[5] == "ai" {
		displayMarker = "ai"
	}
	return []string{m[2] + "-" + m[3] + m[4] + "-" + displayMarker}
}

func cidMatchesRemasterQuery(contentID, queryID, marker, series string) bool {
	if !cidMatchesMarker(contentID, marker, series) {
		return false
	}
	if cidRemasterSuffix(contentID) != cidRemasterSuffix(queryID) {
		return false
	}
	if isRawRemasterContentIDQuery(queryID) {
		return strings.EqualFold(strings.TrimSpace(contentID), strings.TrimSpace(queryID))
	}
	return true
}

// cidRemasterSuffix extracts the E/Z catalog suffix from an id; empty when the
// id is not a remaster shape or carries no suffix.
func cidRemasterSuffix(id string) string {
	m := r18RemasterTailRegex.FindStringSubmatch(r18RemasterCore(id))
	if m == nil {
		return ""
	}
	return m[4]
}

func markerVariationAccept(body []byte, queryID, foldedMarker, series string) bool {
	var data contentIDLookupResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	if !cidMatchesRemasterQuery(data.ContentID, queryID, foldedMarker, series) {
		return false
	}
	if isRawRemasterContentIDQuery(queryID) {
		return true
	}
	if data.DVDID != "" {
		return foldDisplay(data.DVDID) == foldDisplay(queryID)
	}
	return true
}

// guardRemasterResult applies the marker guard to a fully parsed result:
// marker-bearing queries only accept results whose content id carries the
// folded marker (verification of the number is server-owned).
func guardRemasterResult(id string, res *models.ScraperResult) (*models.ScraperResult, error) {
	foldedMarker, series := classifyRemaster(id)
	if foldedMarker == "" || res == nil {
		return res, nil
	}
	if !cidMatchesRemasterQuery(res.ContentID, id, foldedMarker, series) {
		return nil, models.NewScraperNotFoundError("R18.dev", "response does not carry the requested remaster identity")
	}
	if !isRawRemasterContentIDQuery(id) {
		res.ID = canonicalRemasterDisplayID(id)
	} else {
		res.ID = canonicalRemasterDisplayID(res.ID)
	}
	return res, nil
}

func isRawRemasterContentIDQuery(id string) bool {
	s := strings.ToLower(strings.TrimSpace(id))
	if r18PrefixedCIDRegex.MatchString(s) {
		return true
	}
	if strings.ContainsAny(s, "-_ ") {
		return false
	}
	return rawRemasterCIDShapeRegex.MatchString(s)
}

var rawRemasterCIDShapeRegex = regexp.MustCompile(`^(?:\d+(?:t28|[a-z]+)\d+[a-z]{0,3}|(?:t28|[a-z]+)\d{5}[a-z]{0,3})$`)

func canonicalRemasterDisplayID(id string) string {
	m := r18RemasterTailRegex.FindStringSubmatch(r18RemasterCore(id))
	if m == nil {
		return strings.ToUpper(id)
	}
	marker := "H"
	if m[5] == "ai" {
		marker = "AI"
	}
	return strings.ToUpper(m[2]) + "-" + m[3] + strings.ToUpper(m[4]) + marker
}
