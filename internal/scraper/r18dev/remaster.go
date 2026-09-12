package r18dev

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

var (
	r18RemasterTailRegex     = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([ez]?)(hd|ai|h)$`)
	r18CIDAnchoredRegex      = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([a-z]{0,3})$`)
	r18PrefixedCIDRegex      = regexp.MustCompile(`^[hn]_\d+(?:t28|[a-z]+)\d+[a-z]{0,3}$`)
	r18SeriesSegmentRegex    = regexp.MustCompile(`^\d*(?:t28|[a-z]+)$`)
	r18SeriesPinnedTailRegex = regexp.MustCompile(`^(\d+)([ez]?)(hd|ai|h)$`)
	nonAlnumR18Regex         = regexp.MustCompile(`[^a-z0-9]+`)
)

func r18RemasterCore(id string) string {
	return r18CompactID(r18RemasterStem(id))
}

// r18RemasterStem lowercases, strips a rental suffix and the h_/n_ channel
// prefix, but keeps separators so the series boundary stays visible.
func r18RemasterStem(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = stripRentalSuffixMarkerAware(s)
	if r18PrefixedCIDRegex.MatchString(s) {
		s = s[2:]
	}
	return s
}

// r18SeparatorSeriesTail splits a separator-bearing display ID into its series
// segment and marker-bearing remainder, pinning the series boundary that
// compaction erases ("T-28123-HD" is series t, not series t28).
func r18SeparatorSeriesTail(lower string) (seg, rest string, ok bool) {
	if !strings.ContainsAny(lower, "-_. ") {
		return "", "", false
	}
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})
	if len(parts) < 2 || !r18SeriesSegmentRegex.MatchString(parts[0]) {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], ""), true
}

// r18ParseRemasterTail parses a marker-bearing display ID into series, number,
// E/Z suffix and marker spelling. Separator-bearing IDs take the segment
// boundary as the series identity before the T28-aware compact regex applies;
// separator-free forms use the compact regex directly.
func r18ParseRemasterTail(id string) (series, number, ez, marker string, ok bool) {
	stem := r18RemasterStem(id)
	if seg, rest, okSep := r18SeparatorSeriesTail(stem); okSep {
		if m := r18SeriesPinnedTailRegex.FindStringSubmatch(rest); m != nil {
			return seg, m[1], m[2], m[3], true
		}
	}
	m := r18RemasterTailRegex.FindStringSubmatch(r18CompactID(stem))
	if m == nil {
		return "", "", "", "", false
	}
	series, number = m[2], m[3]
	if m[1] == "" && series == t28Series && len(number) == 3 {
		series, number = "t", "28"+number
	}
	return series, number, m[4], m[5], true
}

// stripRentalSuffixMarkerAware removes a DMM rental 'r' suffix from a content
// id: a digit-preceded terminal 'r' always strips (118abp00420r ->
// 118abp00420), and a terminal 'r' whose remainder is a valid marker-bearing
// content id also strips (1rct00156hr -> 1rct00156h, dv00899air ->
// dv00899ai, h_003abc00123hdr -> h_003abc00123hd). Underscore-prefixed forms
// drop the [hn]_ prefix before the marker-base test, mirroring classification.
// r18.dev stores no rental content ids, so rental-suffixed queries must
// resolve against the stripped base identity.
func stripRentalSuffixMarkerAware(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if len(s) < 2 || !strings.HasSuffix(s, "r") {
		return s
	}
	base := s[:len(s)-1]
	if last := base[len(base)-1]; last >= '0' && last <= '9' {
		return base
	}
	core := base
	if r18PrefixedCIDRegex.MatchString(core) {
		core = core[2:]
	}
	if r18RemasterTailRegex.FindStringSubmatch(r18CompactID(core)) != nil {
		return base
	}
	return s
}

func r18CompactID(id string) string {
	return nonAlnumR18Regex.ReplaceAllString(strings.ToLower(strings.TrimSpace(id)), "")
}

// classifyRemaster mirrors the DMM scraper's classification for marker-bearing
// queries: the folded marker is "h" for H/HD and "ai" for AI spellings.
func classifyRemaster(id string) (foldedMarker string, series string) {
	series, _, _, marker, ok := r18ParseRemasterTail(id)
	if !ok {
		return "", ""
	}
	if marker == "ai" {
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
	if m == nil {
		return false
	}
	return anchoredSeriesMatches(m[1], m[2], m[3], series)
}

const t28Series = "t28"

// anchoredSeriesMatches resolves the t28/t ambiguity in the anchored cid
// split. A separator-pinned series-t query also matches a prefix-free
// three-digit t28 tail — the convention reads t28123 as series t with the
// five-digit number 28123 (T-28123H) — while catalog-prefixed or longer
// number tails stay series t28 (9t28123h is T28-123H).
func anchoredSeriesMatches(prefix, cidSeries, number, wantSeries string) bool {
	switch wantSeries {
	case "t":
		return cidSeries == "t" || (prefix == "" && cidSeries == "t28" && len(number) == 3)
	case t28Series:
		return cidSeries == t28Series && (prefix != "" || len(number) != 3)
	default:
		return cidSeries == wantSeries
	}
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
	series, number, suffix, marker, ok := r18ParseRemasterTail(id)
	if !ok {
		return nil
	}
	displayMarker := "hd"
	if marker == "ai" {
		displayMarker = "ai"
	}
	return []string{series + "-" + number + suffix + "-" + displayMarker}
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
	_, _, ez, _, ok := r18ParseRemasterTail(id)
	if !ok {
		return ""
	}
	return ez
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
// folded marker (verification of the number is server-owned). Raw queries
// canonicalize any display ID the server provided; the null-dvd_id cid echo
// never reaches this point because resolveIDs and resultFromDump leave the
// ID unset for marker-bearing content ids (r18.dev cid numbers are slot
// numbers, not display numbers — dv00899ai is DV-818-AI).
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
	} else if res.ID != "" && rawDisplayMatchesCID(res.ContentID, res.ID) {
		res.ID = canonicalRemasterDisplayID(res.ID)
	} else {
		res.ID = ""
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

// rawDisplayMatchesCID reports whether a server-provided display ID agrees
// with the content id's nonnumeric identity (series, E/Z suffix, folded
// marker). The number is server-owned and may legitimately diverge, but a
// conflicting variant spelling would silently collapse catalog variants, so
// unverifiable or conflicting displays leave the ID unset.
func rawDisplayMatchesCID(cid, display string) bool {
	cSeries, _, cEz, cMarker, cOk := r18ParseRemasterTail(cid)
	dSeries, _, dEz, dMarker, dOk := r18ParseRemasterTail(display)
	if !cOk || !dOk {
		return false
	}
	return cSeries == dSeries && cEz == dEz && foldMarkerSpelling(cMarker) == foldMarkerSpelling(dMarker)
}

func foldMarkerSpelling(marker string) string {
	if marker == "hd" {
		return "h"
	}
	return marker
}

// canonicalRemasterDisplayID renders the query's display identity in canonical
// form (series-number + folded marker), e.g. "DV-818AI" -> "DV-818AI",
// "RCT-156-HD" -> "RCT-156H". Separator-bearing IDs use the segment boundary
// as the series identity, so "T-28123-HD" canonicalizes to "T-28123H", not the
// unrelated series-t28 spelling the compact form decodes to.
func canonicalRemasterDisplayID(id string) string {
	series, number, ez, marker, ok := r18ParseRemasterTail(id)
	if !ok {
		return strings.ToUpper(id)
	}
	display := "H"
	if marker == "ai" {
		display = "AI"
	}
	return strings.ToUpper(series) + "-" + number + strings.ToUpper(ez) + display
}
