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
	// The marker-base test requires a separator-free cid shape: the rental
	// strip belongs to genuine rental cid endings (…hr/…hdr/…air), and a
	// display query whose compaction would satisfy it (ABW-121-HDR) carries
	// a quality/vocabulary word, not an HD-remaster marker plus a rental 'r'.
	if r18RemasterTailRegex.FindStringSubmatch(core) != nil {
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

// displayIDsMatchByIdentity compares two display spellings by parsed identity
// tuple. Compact foldDisplay erases the separator-pinned T/T28 boundary
// (T-28123-HD and T28-123-HD both fold to t28123h), so parseable spellings
// compare series/number/suffix/marker tuples instead; unparseable spellings
// fall back to the folded comparison.
func displayIDsMatchByIdentity(a, b string) bool {
	aSeries, aNumber, aSuffix, aMarker, aOK := r18ParseRemasterTail(a)
	bSeries, bNumber, bSuffix, bMarker, bOK := r18ParseRemasterTail(b)
	if aOK && bOK {
		foldMarker := func(m string) string {
			if m == "hd" {
				return "h"
			}
			return m
		}
		return aSeries == bSeries &&
			strings.TrimLeft(aNumber, "0") == strings.TrimLeft(bNumber, "0") &&
			aSuffix == bSuffix && foldMarker(aMarker) == foldMarker(bMarker)
	}
	return foldDisplay(a) == foldDisplay(b)
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
		return rawRemasterCIDEqual(contentID, queryID)
	}
	return true
}

// cidMatchesRemasterFuzzyQuery gates null-dvd_id marker rows — both the
// Step-1 fuzzy record and the combined= acceptance in markerVariationAccept
// (the same predicate, only applied at a different point: record vs. accept)
// — and is deliberately stricter than cidMatchesRemasterQuery for H/HD
// spellings. An H/HD remaster content id keeps the display number
// (1rct00156h is RCT-156H), so the recorded row's cid core number must equal
// the query's number, padding-normalized — a stale same-series marker row for
// a different release (1rct00157h for RCT-156H) must never be recorded as the
// fuzzy fallback, or release 157's metadata would be published for release
// 156 once the content-id variations miss. AI queries keep the
// cidMatchesRemasterQuery acceptance verbatim: AI content ids diverge from
// display numbers (dv00899ai is DV-818AI), so AI cannot number-bind and
// stays number-free.
func cidMatchesRemasterFuzzyQuery(contentID, queryID, marker, series string) bool {
	if !cidMatchesRemasterQuery(contentID, queryID, marker, series) {
		return false
	}
	if marker == "ai" {
		// AI content ids diverge from display numbers (dv00899ai is
		// DV-818AI): the marker-based acceptance is already the strongest
		// available identity check.
		return true
	}
	// H/HD remaster cids keep the display number: require the row's cid core
	// number to equal the query's number, padding-normalized (1rct00156h
	// matches RCT-156H; 1rct156h and RCT-00156-HD match too).
	_, qNumber, _, _, qOK := r18ParseRemasterTail(queryID)
	_, cNumber, _, _, cOK := r18ParseRemasterTail(contentID)
	if !qOK || !cOK {
		return false
	}
	return strings.TrimLeft(qNumber, "0") == strings.TrimLeft(cNumber, "0")
}

// rawRemasterCIDEqual compares a server-stated content id against a raw
// marker-bearing query spelling: equal verbatim, or equal after padding
// normalization — the unpadded query 1rct156h and the server's padded cid
// 1rct00156h name the same product. The marker guard (series, folded
// marker, E/Z suffix) has already passed by the time this comparison runs.
func rawRemasterCIDEqual(contentID, queryID string) bool {
	if strings.EqualFold(strings.TrimSpace(contentID), strings.TrimSpace(queryID)) {
		return true
	}
	return normalizeRawCIDPadding(contentID) == normalizeRawCIDPadding(queryID)
}

// normalizeRawCIDPadding canonicalizes a raw content id by stripping
// leading zeros from its number (1rct00156h -> 1rct156h). The [hn]_ channel
// prefix, catalog digits, series, E/Z suffix and marker stay verbatim; ids
// without a cid shape are returned lowercased and compacted for comparison.
func normalizeRawCIDPadding(cid string) string {
	s := strings.ToLower(strings.TrimSpace(cid))
	prefix := ""
	core := r18CompactID(s)
	if r18PrefixedCIDRegex.MatchString(s) {
		rest := s[2:]
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		prefix, core = s[:2+i], rest[i:]
	}
	m := r18CIDAnchoredRegex.FindStringSubmatch(core)
	if m == nil {
		return core
	}
	n := strings.TrimLeft(m[3], "0")
	if n == "" {
		n = "0"
	}
	return prefix + m[1] + m[2] + n + m[4]
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

func responseContentIDMatchesVariation(body []byte, variation string) bool {
	var data contentIDLookupResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(data.ContentID), strings.TrimSpace(variation))
}

// markerVariationAccept validates a combined= response body against a
// marker-bearing query: the cid must carry the query's marker identity
// (series, folded marker, E/Z suffix; raw queries additionally require
// literal cid equality), a present dvd_id must agree on the display
// identity, and a null-dvd_id row must bind the query's core number for
// H/HD spellings. This predicate is both the Step-2 variation acceptance
// and the final guard in fetchAndParseCombined, so the normalized
// combined= fallback is covered by the same check.
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
		return displayIDsMatchByIdentity(data.DVDID, queryID)
	}
	// Null dvd_id: bind the query's core number for H/HD through the same
	// round-10 predicate that gates the Step-1 fuzzy record. Without it the
	// separately fetched combined= fallback (every resolver variation
	// missed) accepts a stale same-series marker row for a different
	// release — r18.dev's fuzzy combined= matching can answer RCT-156H with
	// 1rct00157h, publishing release 157 for 156. AI queries diverge from
	// display numbers by design (dv00899ai is DV-818AI) and stay number-free.
	return cidMatchesRemasterFuzzyQuery(data.ContentID, queryID, foldedMarker, series)
}

// guardRemasterResult applies the marker guard to a fully parsed result:
// marker-bearing queries only accept results whose content id carries the
// folded marker (verification of the number is server-owned). Raw queries
// canonicalize any display ID the server provided; a null-dvd_id response
// reaches the guard with a resolveIDs-derived display ID for H/HD cids (their
// numbers are display numbers, bound by the admitting guards), while AI cids
// — whose numbers are slot numbers, not display numbers (dv00899ai is
// DV-818-AI) — and the dump path leave the ID unset.
func guardRemasterResult(id string, res *models.ScraperResult) (*models.ScraperResult, error) {
	foldedMarker, series := classifyRemaster(id)
	if foldedMarker == "" || res == nil {
		return res, nil
	}
	if !cidMatchesRemasterQuery(res.ContentID, id, foldedMarker, series) {
		return nil, models.NewScraperNotFoundError("R18.dev", "response does not carry the requested remaster identity")
	}
	if !isRawRemasterContentIDQuery(id) {
		if res.ID != "" && !displayIDsMatchByIdentity(res.ID, id) {
			return nil, models.NewScraperNotFoundError("R18.dev", "response display ID does not match the requested remaster identity")
		}
		res.ID = canonicalRemasterDisplayID(res.ID)
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

// rawRemasterCIDShapeRegex mirrors the DMM classifier's content-id shape: a
// catalog-digit prefix, a five-digit zero-padded number (the leading zero is
// the padding evidence — display numbers never carry one), or the
// prefix-free t28 tail whose compact display spelling doubles as the raw cid
// (t28123h is T-28123H). A non-padded five-digit separator-free form
// (ABC12345H compacts to abc12345h) is a display id whose server cid is
// catalog-prefixed (1abc12345h): it must resolve as a display query, not
// demand literal cid equality.
var rawRemasterCIDShapeRegex = regexp.MustCompile(`^(?:\d+(?:t28|[a-z]+)\d+[a-z]{0,3}|(?:t28(?:0\d{4}|\d{3})|[a-z]+0\d{4})[a-z]{0,3})$`)

// rawDisplayMatchesCID reports whether a server-provided display ID agrees
// with the content id's identity (series, E/Z suffix, folded marker) and, for
// H/HD spellings, the padding-normalized number. An H/HD remaster content id
// keeps the display number (1rct00156h is RCT-156H), so a conflicting dvd_id
// (RCT-157-HD for cid 1rct00156h) is rejected instead of canonicalized and
// published under the wrong release ID — the raw-CID analog of the
// display-query guard (displayIDsMatchByIdentity) and the round-10/14 number
// binding. AI content ids diverge from display numbers by design (dv00899ai
// is DV-818AI), so AI keeps the number-free series/marker comparison;
// unverifiable or conflicting displays leave the ID unset.
func rawDisplayMatchesCID(cid, display string) bool {
	cSeries, cNumber, cEz, cMarker, cOk := r18ParseRemasterTail(cid)
	dSeries, dNumber, dEz, dMarker, dOk := r18ParseRemasterTail(display)
	if !cOk || !dOk {
		return false
	}
	if cSeries != dSeries || cEz != dEz || foldMarkerSpelling(cMarker) != foldMarkerSpelling(dMarker) {
		return false
	}
	if foldMarkerSpelling(cMarker) == "ai" {
		// AI content ids diverge from display numbers (dv00899ai is
		// DV-818AI): the series/marker identity check is already the
		// strongest available comparison, so AI stays number-free.
		return true
	}
	// H/HD remaster cids keep the display number (1rct00156h is RCT-156H):
	// require the display number to equal the cid number, padding-normalized
	// (1rct00156h matches RCT-156-HD and RCT-00156-HD, rejects RCT-157-HD).
	return strings.TrimLeft(cNumber, "0") == strings.TrimLeft(dNumber, "0")
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
