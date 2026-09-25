package dmm

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
	nonAlnumRegex           = regexp.MustCompile(`[^a-z0-9]+`)
	underscorePrefixRegex   = regexp.MustCompile(`^[hn]_`)
)

// cachedRemasterIdentityMatches reports whether a cached content-id mapping
// may stand in for the query without re-running resolution. Markerless
// queries accept any mapping; raw content-id queries require the verbatim
// cid. Display marker queries bind series, folded marker and E/Z suffix —
// and, for H/HD mappings, the padding-normalized number: an H/HD remaster
// cid keeps the display number (1rct00156h is RCT-156H), so a stale
// same-series mapping for a different release (1rct00157h under RCT-156H)
// must not short-circuit the verified resolver and publish the wrong movie.
// AI mappings stay number-free: AI cids diverge from display numbers
// (dv00899ai is DV-818AI), so the marker-based identity is the strongest
// check available.
func cachedRemasterIdentityMatches(id, cid, marker, series, suffix string, raw bool) bool {
	if marker == "" {
		return true
	}
	if raw {
		return strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(cid))
	}
	clean := cleanPrefixRegex.ReplaceAllString(strings.ToLower(cid), "$1")
	cachedMarker, cachedSuffix, ok := parseAnchoredMarkerCID(clean)
	if !ok || !anchoredSeriesMatches(cid, series) || cachedMarker != marker || cachedSuffix != suffix {
		return false
	}
	if marker == "ai" {
		// AI content ids diverge from display numbers (dv00899ai is
		// DV-818AI): the marker-based acceptance is already the strongest
		// available identity check.
		return true
	}
	qNumber, qOK := remasterTailNumber(id)
	cNumber, cOK := remasterTailNumber(cid)
	if !qOK || !cOK {
		// No parseable number on either side: nothing to bind beyond the
		// marker-based acceptance already applied.
		return true
	}
	// H/HD remaster cids keep the display number: require the cached cid's
	// padding-normalized number to equal the query's display number
	// (1rct00156h matches RCT-156H and RCT-00156-HD; 1rct00157h does not).
	return trimDisplayZeros(qNumber) == trimDisplayZeros(cNumber)
}

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
// decisive: a prefix-free three-digit tail reads as series t with the
// five-digit number 28123 (t28123h is T-28123H), while catalog-prefixed or
// longer tails stay series t28 (9t28123h is T28-123H).
const t28Series = "t28"

func anchoredSeriesMatches(rawCID, wantSeries string) bool {
	norm := strings.ToLower(strings.ReplaceAll(rawCID, "-", ""))
	norm = underscorePrefixRegex.ReplaceAllString(norm, "")
	m := anchoredMarkerCIDReg.FindStringSubmatch(norm)
	if m == nil {
		return false
	}
	prefix, cidSeries, number := m[1], m[2], m[3]
	switch wantSeries {
	case "t":
		return cidSeries == "t" || (prefix == "" && cidSeries == t28Series && len(number) == 3)
	case t28Series:
		return cidSeries == t28Series && (prefix != "" || len(number) != 3)
	default:
		return cidSeries == wantSeries
	}
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
	// A prefix-free compact t28 tail with a three-digit number reads as the
	// T-series release T-28123H — but only when the input was genuinely
	// separator-free: separator-bearing display forms pin the series boundary
	// (T28-123-HD stays T28-123), and underscore cids pass a prefix-stripped
	// compact whose maker digits still own the catalog prefix.
	if lower == compact && m[1] == "" && series == t28Series && len(number) == 3 {
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

// normalizeCIDPadding canonicalizes a marker-bearing content id by
// stripping leading zeros from its number: the unpadded query spelling
// 1rct156h and the server's padded cid 1rct00156h reduce to the same
// identity. The [hn]_<digits> channel prefix, catalog digits, series, E/Z
// suffix and marker stay verbatim; ids without a marker-bearing cid shape
// (including markerless base cids) are returned unchanged so the
// normalization never rewrites non-marker identities.
func normalizeCIDPadding(cid string) string {
	s := strings.ToLower(strings.TrimSpace(cid))
	prefix := ""
	core := s
	if underscoreCIDShapeRegex.MatchString(s) {
		// [hn]_<digits> channel prefix: keep it verbatim, normalize the rest.
		rest := s[2:]
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		prefix, core = s[:2+i], rest[i:]
	}
	m := anchoredMarkerCIDReg.FindStringSubmatch(core)
	if m == nil {
		return s
	}
	n := strings.TrimLeft(m[3], "0")
	if n == "" {
		n = "0"
	}
	return prefix + m[1] + m[2] + n + m[4]
}

// bindResolvedCID reports whether a URL-extracted cid may stand in for the
// resolved cid on marker paths: exact equality, or equality up to DMM
// catalog-digit prefix stripping with the marker suffix intact.
func bindResolvedCID(urlCID, resolved string, exact bool) bool {
	if exact {
		a := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(urlCID)))
		b := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(resolved)))
		// Raw queries may arrive unpadded (1rct156h) while the server states
		// the padded cid (1rct00156h); the zero-trimmed identity is the same
		// product, so compare padding-normalized spellings.
		return a == b || normalizeCIDPadding(a) == normalizeCIDPadding(b)
	}
	a := stripRentalSuffixMarkerAware(compactQueryID(urlCID))
	b := compactQueryID(resolved)
	if a == b {
		return true
	}
	ac := cleanPrefixRegex.ReplaceAllString(a, "$1")
	bc := cleanPrefixRegex.ReplaceAllString(b, "$1")
	return ac != "" && ac == bc
}

// remasterSearchSpellings adds marker-aware display forms to the search query
// set (e.g. rct-156-hd, rct156hd for an H-marker query).
func remasterSearchSpellings(id string) []string {
	series, number, suffix, spelling, ok := parseRemasterTail(strings.ToLower(strings.TrimSpace(id)), compactQueryID(id))
	if !ok {
		return nil
	}
	padded := number
	if len(padded) < 5 {
		padded = strings.Repeat("0", 5-len(padded)) + padded
	}
	paddedSuffix := padded + suffix
	displayMarker := "hd"
	if spelling == "ai" {
		displayMarker = "ai"
	}
	short := "h"
	if spelling == "ai" {
		short = "ai"
	}
	return []string{
		series + "-" + number + suffix + "-" + displayMarker,
		series + number + suffix + displayMarker,
		series + paddedSuffix + displayMarker,
		series + "-" + paddedSuffix + "-" + displayMarker,
		series + number + suffix + short,
		series + paddedSuffix + short,
	}
}

// parseAnchoredMarkerCID parses a prefix-cleaned content id into series,
// folded trailing marker and optional E/Z catalog suffix. Returns ok=false for
// malformed ids or unrecognized suffix letters.
func parseAnchoredMarkerCID(clean string) (folded string, suffix string, ok bool) {
	m := anchoredMarkerCIDReg.FindStringSubmatch(clean)
	if m == nil {
		return "", "", false
	}
	tail := m[4]
	switch {
	case strings.HasSuffix(tail, "ai"):
		suffix, folded = strings.TrimSuffix(tail, "ai"), "ai"
	case strings.HasSuffix(tail, "hd"):
		suffix, folded = strings.TrimSuffix(tail, "hd"), "h"
	case strings.HasSuffix(tail, "h"):
		suffix, folded = strings.TrimSuffix(tail, "h"), "h"
	default:
		return "", "", false
	}
	if suffix != "" && suffix != "e" && suffix != "z" {
		return "", "", false
	}
	return folded, suffix, true
}

// extractRemasterContentIDCandidates scans a DMM search document for anchors
// whose content id shares the query's series and folded marker (number-free:
// the server owns the number). The persisted form is the verbatim
// rental-normalized URL cid; candidates remain distinct when their normalized
// content IDs differ.
type remasterCandidate struct {
	contentID string
	cleanID   string
	urls      []string
	length    int
}

func extractRemasterContentIDCandidates(doc *goquery.Document, wantSeries, wantFoldedMarker, wantSuffix string) []remasterCandidate {
	var out []remasterCandidate
	if doc == nil {
		return out
	}
	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}
		var rawCID string
		if strings.Contains(href, "cid=") {
			if mm := dmmCIDRegex.FindStringSubmatch(href); len(mm) > 1 {
				rawCID = mm[1]
			}
		} else if strings.Contains(href, "video.dmm.co.jp") && strings.Contains(href, "id=") {
			if mm := dmmIDRegex.FindStringSubmatch(href); len(mm) > 1 {
				rawCID = mm[1]
			}
		}
		if rawCID == "" {
			return
		}
		rawCID = stripRentalSuffixMarkerAware(rawCID)
		norm := strings.ToLower(strings.ReplaceAll(rawCID, "-", ""))
		clean := cleanPrefixRegex.ReplaceAllString(norm, "$1")
		folded, suffix, ok := parseAnchoredMarkerCID(clean)
		if !ok || !anchoredSeriesMatches(norm, wantSeries) || folded != wantFoldedMarker || suffix != wantSuffix {
			return
		}
		fullURL := ""
		if strings.HasPrefix(href, "/") {
			fullURL = "https://www.dmm.co.jp" + href
		} else if strings.HasPrefix(href, "http") {
			fullURL = href
		}
		out = append(out, remasterCandidate{contentID: norm, cleanID: clean, urls: []string{fullURL}, length: len(clean)})
	})
	return out
}

// resolveRemasterContentID resolves a marker-bearing query via DMM search:
// accumulate candidates across ALL query variations, dedupe repeated URLs
// for the same normalized content ID, and verify candidates via product-page
// display IDs.
func (s *scraper) resolveRemasterContentID(ctx context.Context, id, normalizedID, foldedMarker, series, catalogSuffix string) (string, error) {
	queries := uniqueNonEmptyStrings(append(buildResolveContentIDSearchQueries(id, normalizeContentID(id)), remasterSearchSpellings(id)...))

	type aggCand struct {
		contentID string
		urls      []string
	}
	order := []string{}
	byContentID := map[string]*aggCand{}

	for _, query := range queries {
		searchURLFormatted := fmt.Sprintf(searchURL, query)
		logging.Debugf("DMM: remaster resolution query variation: %s", query)

		if err := s.rateLimiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("DMM: rate limit wait failed: %w", err)
		}
		resp, err := s.client.R().SetContext(ctx).Get(searchURLFormatted)
		if err != nil {
			return "", fmt.Errorf("DMM search unavailable (possible geo-restriction or network error): %w", err)
		}
		if resp.StatusCode() == 403 || resp.StatusCode() == 451 {
			return "", models.NewScraperStatusError("DMM", resp.StatusCode(), fmt.Sprintf("DMM access blocked (status %d, likely geo-restriction)", resp.StatusCode()))
		}
		if resp.StatusCode() != 200 {
			return "", models.NewScraperStatusError("DMM", resp.StatusCode(), fmt.Sprintf("DMM search returned status code %d", resp.StatusCode()))
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
		var pageCands []remasterCandidate
		if err == nil {
			pageCands = extractRemasterContentIDCandidates(doc, series, foldedMarker, catalogSuffix)
		}
		for _, c := range pageCands {
			if existing, ok := byContentID[c.contentID]; ok {
				for _, u := range c.urls {
					if !slices.Contains(existing.urls, u) {
						existing.urls = append(existing.urls, u)
					}
				}
				continue
			}
			byContentID[c.contentID] = &aggCand{contentID: c.contentID, urls: append([]string{}, c.urls...)}
			order = append(order, c.contentID)
		}
	}

	if len(order) == 0 {
		return "", models.NewScraperNotFoundError("DMM", "no matching marker content-id found in DMM search results")
	}

	// Every search-derived candidate — singleton or ambiguous — is verified
	// against product-page display IDs: admission is number-free by design, so
	// only the page's display ID proves the requested release.
	target := id
	var verified []string
	for _, contentID := range order {
		c := byContentID[contentID]
		status, err := s.verifyCandidateDisplayID(ctx, target, c.urls)
		if err != nil {
			return "", fmt.Errorf("DMM: remaster display verification for %s: %w", id, err)
		}
		if status == displayVerified {
			verified = append(verified, contentID)
		}
	}
	if len(verified) == 1 {
		resolved := byContentID[verified[0]].contentID
		s.cacheContentID(ctx, normalizedID, resolved)
		return resolved, nil
	}
	return "", models.NewScraperNotFoundError("DMM", fmt.Sprintf("remaster resolution for %s could not verify a unique candidate (%d verified of %d distinct)", id, len(verified), len(order)))
}

func (s *scraper) cacheContentID(ctx context.Context, searchID, contentID string) {
	mapping := &models.ContentIDMapping{SearchID: searchID, ContentID: contentID, Source: s.Name()}
	if err := s.contentIDRepo.Create(ctx, mapping); err != nil {
		logging.Debugf("DMM: Failed to cache content-id mapping for %s: %v", searchID, err)
	}
}

func (s *scraper) fetchBrowserPage(ctx context.Context, url string) (string, error) {
	if s.browserFetch != nil {
		return s.browserFetch(ctx, url)
	}
	return fetchWithBrowser(ctx, url, s.browserConfig.Timeout, s.proxyProfile, s.getEnvLookup(), s.getFs())
}

type displayStatus int

const (
	displayVerified displayStatus = iota
	displayRejected
	displayUnverifiable
)

// verifyCandidateDisplayID fetches every eligible product page for a candidate
// and combines the parseable display identities: zero parseable => unverified,
// multiple distinct => unverifiable, exactly one => verified iff its pinned
// identity equals the query's pinned identity.
func (s *scraper) verifyCandidateDisplayID(ctx context.Context, id string, urls []string) (displayStatus, error) {
	identities := map[string]struct{}{}
	for _, u := range urls {
		if u == "" || (!strings.HasPrefix(u, "https://www.dmm.co.jp/") && !strings.HasPrefix(u, "https://video.dmm.co.jp/")) {
			continue
		}
		if err := s.rateLimiter.Wait(ctx); err != nil {
			return displayUnverifiable, err
		}
		var body string
		if strings.HasPrefix(u, "https://video.dmm.co.jp/") && s.useBrowser {
			var err error
			body, err = s.fetchBrowserPage(ctx, u)
			if err != nil {
				if ctx.Err() != nil {
					return displayUnverifiable, ctx.Err()
				}
				continue
			}
		} else {
			resp, err := s.client.R().SetContext(ctx).Get(u)
			if err != nil {
				if ctx.Err() != nil {
					return displayUnverifiable, ctx.Err()
				}
				continue
			}
			if resp.StatusCode() != 200 {
				continue
			}
			body = resp.String()
		}
		if doc, err := goquery.NewDocumentFromReader(strings.NewReader(body)); err == nil {
			if display := extractDisplayID(doc); display != "" {
				// Equivalent spellings (RCT-156-HD, RCT156HD) parse to the same
				// pinned identity; counting raw strings would reject an otherwise
				// uniquely correct candidate as unverifiable.
				if ds, dd, dsuf, dm, ok := displayIdentityTuple(display); ok {
					identities[ds+"\x00"+dd+"\x00"+dsuf+"\x00"+dm] = struct{}{}
				}
			}
		}
	}
	if len(identities) == 1 {
		ts, td, tsuf, tm, tok := displayIdentityTuple(id)
		if tok {
			if _, ok := identities[ts+"\x00"+td+"\x00"+tsuf+"\x00"+tm]; ok {
				return displayVerified, nil
			}
		}
		return displayRejected, nil
	}
	return displayUnverifiable, nil
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

// pageDisplayIdentityForCID resolves what the page's 品番 proves for a
// marker-bearing cid. A row matching the cid's series, folded marker and E/Z
// catalog suffix proves the page's canonical display id — for non-AI markers
// only when its padding-normalized number equals the cid's (H/HD remaster
// cids keep the display number). A row that passes those gates but numbers
// another release conflicts: DMM followed a redirect or served a different
// product for the cid (cid=1rct00156h under RCT-157-HD), so the page cannot
// publish the queried release's identity at all. Everything else — absent,
// markerless, unparseable or foreign rows — proves nothing either way and is
// ignored. AI numbers diverge from the cid by design, so AI rows never
// conflict on number and keep the page-outranks-cid rule.
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
	// applies.
	pSeries, number, ez, marker, ok := displayIdentityTuple(display)
	if !ok || pSeries != series || ez != catalogSuffix || marker != foldedMarker {
		return "", false
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
