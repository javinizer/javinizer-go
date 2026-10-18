package dmm

import (
	"github.com/PuerkitoBio/goquery"
	"regexp"
	"strings"
)

var (
	// Unambiguous content-id shapes only: a channel prefix, a five-digit
	// zero-padded number, or the prefix-free t28 tail whose compact display
	// spelling doubles as the raw cid (t28123h is T-28123H, so the raw bypass
	// binds the server cid verbatim). The leading zero is the padding
	// evidence: display numbers never carry one, while DMM pads cid numbers
	// to five digits. A non-padded five-digit separator-free form (ABC12345H
	// compacts to abc12345h) is a display id whose server cid is
	// catalog-prefixed (1abc12345h); like the four-digit display ids
	// (ABP1234), it is ambiguous and must stay on the resolver path.
	remasterCIDShapeRegex   = regexp.MustCompile(`^(?:\d+(?:t28|[a-z]+)\d+[a-z]{0,3}|(?:t28(?:0\d{4}|\d{3})|[a-z]+0\d{4})[a-z]{0,3})$`)
	underscoreCIDShapeRegex = regexp.MustCompile(`^[hn]_\d+[a-z]+\d+(?:[ez]?(?:hd|ai|h)|[a-z]{0,2})$`)
	remasterTailRegex       = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([ez]?)(hd|ai|h)$`)
	anchoredMarkerCIDReg    = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([a-z]{1,3})$`)
	seriesSegmentRegex      = regexp.MustCompile(`^\d*(?:t28|[a-z]+)$`)
	separatorRunRegex       = regexp.MustCompile(`[-_.\s]+`)
	seriesPinnedTailRegex   = regexp.MustCompile(`^(\d+)([ez]?)(hd|ai|h)$`)
	// A base release's 品番 shape: series plus number with no remaster
	// marker (rct157, t28123), optionally carrying the E/Z catalog suffix
	// the remaster identity grammar also parses (rct157e, rct157z). The
	// round-25b markerless-conflict probe.
	markerlessTailRegex   = regexp.MustCompile(`^\d*(?:t28|[a-z]+)\d+[ez]?$`)
	nonAlnumRegex         = regexp.MustCompile(`[^a-z0-9]+`)
	underscorePrefixRegex = regexp.MustCompile(`^[hn]_`)
)

// remasterTailNumber extracts an id's display number through the same parse
// classifyRemasterQuery applies: rental-normalized and compacted, with an
// underscore channel cid pre-cleaned so the tail regex reads the series. The
// number alone carries no identity — callers compare padding-normalized
// values (trimDisplayZeros).
func remasterTailNumber(id string) (string, bool) {
	lowerRaw := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(id)))
	hasSeparator := strings.ContainsAny(lowerRaw, "-_. ")
	compact := compactQueryID(lowerRaw)
	if !hasSeparator {
		compact = stripRentalSuffixMarkerAware(compact)
	}
	if underscoreCIDShapeRegex.MatchString(lowerRaw) {
		compact = cleanPrefixRegex.ReplaceAllString(lowerRaw, "$1")
	}
	_, number, _, _, ok := parseRemasterTail(lowerRaw, compact)
	if !ok {
		return "", false
	}
	return number, true
}

// anchoredSeriesMatches reports whether the raw cid's anchored series matches
// the query's series, resolving the t28/t ambiguity. The catalog prefix is
// decisive: a prefix-free tail that decodes as the T series (t28123h is
// T-28123H, t281234h is T-281234H — see t28TailDecodesTSeries) reads as
// series t, while catalog-prefixed, zero-padded and longer tails stay series
// t28 (9t28123h and t2800123h are T28-123H).
const t28Series = "t28"

// t28TailDecodesTSeries reports whether a prefix-free compact t28 tail of
// this digit count reads as the T series rather than the T28 label:
// three-digit tails spell the five-digit T-series numbers (t28123h is
// T-28123H) and four-digit tails the six-digit ones (t281234h is T-281234H,
// the digit run the matcher's re-anchoring grammar decodes the same way),
// while zero-padded five-digit tails (t2800123h) are the T28 label's padded
// cids and longer unpadded tails are ambiguous display spellings that stay
// T28.
func t28TailDecodesTSeries(tail string) bool {
	return len(tail) == 3 || len(tail) == 4
}

func compactQueryID(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(s)
	return s
}

// separatorSeriesTail splits a separator-bearing display ID into its series
// segment and marker-bearing remainder. Separators pin the series boundary
// that compaction erases: "T-28123-HD" is series t number 28123, not the
// series t28 number 123 the compact form decodes to.
func separatorSeriesTail(lower string) (seg, rest string, ok bool) {
	if !strings.ContainsAny(lower, "-_. ") {
		return "", "", false
	}
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})
	if len(parts) < 2 || !seriesSegmentRegex.MatchString(parts[0]) {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], ""), true
}

// parseRemasterTail parses a marker-bearing display ID into series, number,
// E/Z suffix and marker spelling. Separator-bearing IDs take the segment
// boundary as the series identity before the T28-aware compact regex applies;
// separator-free forms use the compact regex directly.
func parseRemasterTail(lower, compact string) (series, number, ez, marker string, ok bool) {
	if seg, rest, okSep := separatorSeriesTail(lower); okSep {
		if m := seriesPinnedTailRegex.FindStringSubmatch(rest); m != nil {
			return seg, m[1], m[2], m[3], true
		}
	}
	m := remasterTailRegex.FindStringSubmatch(compact)
	if m == nil {
		return "", "", "", "", false
	}
	series, number = m[2], m[3]
	// A prefix-free compact t28 tail with a three- or four-digit number
	// reads as the T-series release — t28123h is T-28123H, and t281234h is
	// T-281234H, the six-digit run the matcher's re-anchoring grammar decodes
	// the same way (see t28TailDecodesTSeries) — but only when the input was
	// genuinely separator-free: separator-bearing display forms pin the series
	// boundary (T28-1234-HD stays T28-1234), and underscore cids pass a
	// prefix-stripped compact whose maker digits still own the catalog prefix.
	if lower == compact && m[1] == "" && series == t28Series && t28TailDecodesTSeries(number) {
		series, number = "t", "28"+number
	}
	return series, number, m[4], m[5], true
}

// classifyRemasterQuery reports whether an input ID carries an AI/HD remaster
// marker and whether it is content-id-shaped. The returned foldedMarker is "h"
// for both H and HD spellings and "ai" for AI; series is the letter sequence.
func classifyRemasterQuery(id string) (foldedMarker, series, catalogSuffix string, isContentID bool) {
	lowerRaw := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(id)))
	// A hyphenated/spaced input is a display ID, not a raw content ID: it must
	// keep the resolver path (catalog-prefix search, padding, server-mediated
	// number mapping). Only separator-free compact forms count as content IDs.
	hasSeparator := strings.ContainsAny(lowerRaw, "-_. ")
	compact := compactQueryID(lowerRaw)
	// The marker-aware rental strip belongs to genuine rental cid endings
	// (…hr/…hdr/…air) in separator-free cid/URL contexts. Compacting a display
	// query must not manufacture one: a trailing -HDR token is a
	// quality/vocabulary word (like HDTV/HDrip), not an HD-remaster marker
	// plus a rental 'r', so ABW-121-HDR stays a base-release query.
	if !hasSeparator {
		compact = stripRentalSuffixMarkerAware(compact)
	}
	isContentID = underscoreCIDShapeRegex.MatchString(lowerRaw) ||
		(!hasSeparator && remasterCIDShapeRegex.MatchString(compact))
	if underscoreCIDShapeRegex.MatchString(lowerRaw) {
		compact = cleanPrefixRegex.ReplaceAllString(lowerRaw, "$1")
	}
	series, _, catalogSuffix, markerSpelling, tailOK := parseRemasterTail(lowerRaw, compact)
	if !tailOK {
		return "", "", "", isContentID
	}
	switch markerSpelling {
	case "hd", "h":
		foldedMarker = "h"
	case "ai":
		foldedMarker = "ai"
	}
	return foldedMarker, series, catalogSuffix, isContentID
}

// canonicalRemasterDisplayID renders the query's display identity in canonical
// form (series-number + folded marker), e.g. "DV-818AI" -> "DV-818AI",
// "RCT-156-HD" -> "RCT-156H".
func canonicalRemasterDisplayID(id string) string {
	series, number, ez, spelling, ok := parseRemasterTail(strings.ToLower(strings.TrimSpace(id)), compactQueryID(id))
	if !ok {
		return strings.ToUpper(id)
	}
	marker := "H"
	if spelling == "ai" {
		marker = "AI"
	}
	return strings.ToUpper(series) + "-" + number + strings.ToUpper(ez) + marker
}

// t28CidDisplayID renders the display identity of a t28-series content id.
// A catalog-prefixed cid (9t28123h, h_003t28123h) is T28-123H; a bare
// three-digit tail (t28123h) reads as the T-series release T-28123H, matching
// the anchoredSeriesMatches disambiguation.
func t28CidDisplayID(cid string) string {
	norm := strings.ToLower(strings.ReplaceAll(cid, "-", ""))
	norm = underscorePrefixRegex.ReplaceAllString(norm, "")
	m := anchoredMarkerCIDReg.FindStringSubmatch(norm)
	if m == nil || m[2] != t28Series || len(m[3]) != 3 {
		return canonicalRemasterDisplayID(cleanPrefixRegex.ReplaceAllString(strings.ToLower(cid), "$1"))
	}
	if m[1] == "" {
		return canonicalRemasterDisplayID("t-28" + m[3] + m[4])
	}
	return canonicalRemasterDisplayID("t28-" + m[3] + m[4])
}

// stripRentalSuffixMarkerAware extends stripRentalSuffix: in addition to the
// digit-preceded rule it also removes a terminal 'r' when the remainder is a
// valid marker-bearing content ID (e.g. 1rct00156hr -> 1rct00156h).
func stripRentalSuffixMarkerAware(cid string) string {
	if out := stripRentalSuffix(cid); out != cid {
		return out
	}
	l := strings.ToLower(cid)
	if len(l) >= 2 && strings.HasSuffix(l, "r") {
		base := l[:len(l)-1]
		cleaned := cleanPrefixRegex.ReplaceAllString(base, "$1")
		if anchoredMarkerCIDReg.MatchString(cleaned) {
			return base
		}
	}
	return cid
}

// extractDisplayID reads the display DVD ID (品番) from a DMM product page's
// information table. The CID-like 商品番号 field is never used for verification.
// Labels may be legacy <td> cells or header <th> cells; th-labeled rows carry
// the value in the first td, td-labeled rows in the second.
func extractDisplayID(doc *goquery.Document) string {
	var out string
	doc.Find("tr").EachWithBreak(func(i int, sel *goquery.Selection) bool {
		th := strings.TrimSpace(sel.Find("th").First().Text())
		label := th
		if label == "" {
			label = strings.TrimSpace(sel.Find("td").First().Text())
		}
		if strings.Contains(label, "商品番号") || !strings.Contains(label, "品番") {
			return true
		}
		cells := sel.Find("td")
		var value string
		if th != "" {
			value = cells.First().Text()
		} else if cells.Length() >= 2 {
			value = cells.Eq(1).Text()
		}
		lower := strings.ToLower(value)
		norm := nonAlnumRegex.ReplaceAllString(lower, "")
		if norm == "" {
			return true
		}
		// Keep the separator form: the series boundary it pins is the only
		// thing distinguishing T-28123-HD from T28-123-HD once compacted.
		out = displaySeparatorForm(lower)
		return false
	})
	return out
}

// displaySeparatorForm canonicalizes a page display's separators: lowercased,
// trimmed, with separator runs collapsed to a single hyphen. The series
// boundary stays pinned (T-28123-HD and T-28123 HD both render t-28123-hd)
// while separator-free values pass through unchanged.
func displaySeparatorForm(lower string) string {
	trimmed := strings.Trim(lower, "-_. ")
	return separatorRunRegex.ReplaceAllString(trimmed, "-")
}

// trimDisplayZeros strips leading zeros the way the display identity regex
// does, so padded page spellings (0156) equal query spellings (156).
func trimDisplayZeros(number string) string {
	trimmed := strings.TrimLeft(number, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

// displayIdentityTuple derives the separator-pinned identity (series, numeric
// value, catalog suffix, folded marker) of a display string. Separator-bearing
// forms pin the series boundary (T-28123-HD is t/28123, T28-123-HD is
// t28/123); compact forms decode through the tail regex with the prefix-free
// T-series disambiguation. The compact form of T-28123-HD and T28-123-HD is
// identical, so verification compares pinned tuples, never compacted strings.
func displayIdentityTuple(display string) (series, value, suffix, marker string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(display))
	series, value, suffix, marker, ok = parseRemasterTail(lower, compactQueryID(display))
	if !ok {
		return "", "", "", "", false
	}
	if marker == "hd" {
		marker = "h"
	}
	return series, trimDisplayZeros(value), suffix, marker, true
}

// isMarkerlessDisplayID reports whether a nonempty 品番 parses as a base
// release's identity: series and number with no remaster marker (RCT-157,
// rct157, T-28123), with the E/Z catalog suffix displayIdentityTuple also
// parses allowed on the base spelling too (RCT-157E, RCT-157-Z). The tail
// regexes displayIdentityTuple applies require the h/ai/hd ending, so a
// row they reject that still pairs a series with a number names the base
// release rather than a remaster; pure digits, bare letters and
// vocabulary-bearing rows (12345, garbage, RCT-157-X) parse as neither and
// stay unparseable. Callers gate on the request being marker-bearing: on a
// base-release request a markerless row is the normal, trusted identity.
func isMarkerlessDisplayID(display string) bool {
	return markerlessTailRegex.MatchString(compactQueryID(display))
}

// pageDisplayIdentityForCID resolves what the page's 品番 proves for a
// marker-bearing cid. A row matching the cid's series, folded marker and E/Z
// catalog suffix proves the page's canonical display id — for non-AI markers
// only when its padding-normalized number equals the cid's (H/HD remaster
// cids keep the display number). A row that passes those gates but numbers
// another release conflicts: DMM followed a redirect or served a different
// product for the cid (cid=1rct00156h under RCT-157-HD), so the page cannot
// publish the queried release's identity at all. A parseable marker-bearing
// row that belongs to a foreign identity — another series, catalog suffix or
// marker line (cid=1rct00156h serving ABC-999-HD, or the AI remaster under an
// H url) — conflicts the same way: the page provably belongs to another
// product, and publishing its metadata under the cid-derived identity would
// misfile the release, so the whole page is rejected rather than merely
// distrusted. This mirrors Search's pageDisplayIdentityMatchesQuery tuple
// comparison and holds for AI cids too: AI numbers diverge from the cid by
// design, so an AI row never conflicts on number, but the series, marker and
// suffix never diverge between an AI cid and its display, so a foreign row
// on an AI url is still another product's page. A nonempty markerless 品番
// conflicts the same way: it names the base release — plain or carrying the
// E/Z catalog suffix (cid=1rct00156h serving RCT-157 or RCT-157E) — not
// the remaster the marker-bearing cid asks for, so the page is the base
// product's. Everything else — absent or unparseable rows — publishes
// nothing parseable to trust and is ignored.
func pageDisplayIdentityForCID(doc *goquery.Document, cid, series, foldedMarker, catalogSuffix string) (string, bool) {
	if doc == nil {
		return "", false
	}
	display := extractDisplayID(doc)
	if display == "" {
		return "", false
	}
	// The page value arrives separator-pinned, so its identity keeps the
	// boundary that separates T-28123H from T28-123H; a separator-free page
	// value decodes with the same prefix-free disambiguation the query parser
	// applies. Unparseable rows publish no identity to trust.
	pSeries, number, ez, marker, ok := displayIdentityTuple(display)
	if !ok {
		// A nonempty markerless 品番 — plain or E/Z-suffixed — names the base
		// release, not the remaster the marker-bearing cid asks for: DMM
		// followed a redirect or served the base product's page, so the row
		// cannot publish the remaster's identity and conflicts instead of
		// passing through like an absent one.
		if foldedMarker != "" && isMarkerlessDisplayID(display) {
			return "", true
		}
		return "", false
	}
	// A parseable marker-bearing 品番 belonging to a foreign series, catalog
	// suffix or marker line proves the page is another product's: the whole
	// page is a conflict, not merely an unusable row whose override is
	// dropped while the foreign metadata keeps the cid-derived identity.
	if pSeries != series || ez != catalogSuffix || marker != foldedMarker {
		return "", true
	}
	if marker != "ai" {
		// The cid-side analog of cachedRemasterIdentityMatches' number
		// binding: an H/HD 品番 numbering a different release than the cid
		// names the wrong product — the whole page is a conflict, not merely
		// an unusable row. A cid without a parseable number binds nothing
		// beyond the marker-based acceptance already applied.
		cidNumber, cidOK := remasterTailNumber(cid)
		if cidOK && trimDisplayZeros(cidNumber) != number {
			return "", true
		}
	}
	// Render from the verified split rather than re-parsing the compact form,
	// which cannot distinguish T-28123H from T28-123H.
	markerSpelling := "H"
	if marker == "ai" {
		markerSpelling = "AI"
	}
	return strings.ToUpper(pSeries + "-" + number + ez + markerSpelling), false
}

// pageRemasterDisplayID returns the canonical display ID the page's 品番
// proves for the cid, ignoring rows that conflict with it (see
// pageDisplayIdentityForCID): the page value outranks the CID-derived
// spelling for the releases it proves and publishes nothing for the rest.
func pageRemasterDisplayID(doc *goquery.Document, cid, series, foldedMarker, catalogSuffix string) string {
	pageID, _ := pageDisplayIdentityForCID(doc, cid, series, foldedMarker, catalogSuffix)
	return pageID
}
