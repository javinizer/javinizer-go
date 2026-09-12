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
	// Unambiguous content-id shapes only: a channel prefix or a five-digit
	// zero-padded number. Separator-free four-digit display ids (ABP1234) are
	// ambiguous and must stay on the resolver path.
	remasterCIDShapeRegex   = regexp.MustCompile(`^(?:\d+(?:t28|[a-z]+)\d+[a-z]{0,3}|(?:t28|[a-z]+)\d{5}[a-z]{0,3})$`)
	underscoreCIDShapeRegex = regexp.MustCompile(`^[hn]_\d+[a-z]+\d+(?:[ez]?(?:hd|ai|h)|[a-z]{0,2})$`)
	remasterTailRegex       = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([ez]?)(hd|ai|h)$`)
	anchoredMarkerCIDReg    = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([a-z]{1,3})$`)
	nonAlnumRegex           = regexp.MustCompile(`[^a-z0-9]+`)
)

func cachedRemasterIdentityMatches(id, cid, marker, series, suffix string, raw bool) bool {
	if marker == "" {
		return true
	}
	if raw {
		return strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(cid))
	}
	clean := cleanPrefixRegex.ReplaceAllString(strings.ToLower(cid), "$1")
	cachedSeries, cachedMarker, cachedSuffix, ok := parseAnchoredMarkerCID(clean)
	return ok && cachedSeries == series && cachedMarker == marker && cachedSuffix == suffix
}

func compactQueryID(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(s)
	return s
}

// classifyRemasterQuery reports whether an input ID carries an AI/HD remaster
// marker and whether it is content-id-shaped. The returned foldedMarker is "h"
// for both H and HD spellings and "ai" for AI; series is the letter sequence.
func classifyRemasterQuery(id string) (foldedMarker, series, catalogSuffix string, isContentID bool) {
	lowerRaw := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(id)))
	compact := stripRentalSuffixMarkerAware(compactQueryID(lowerRaw))
	// A hyphenated/spaced input is a display ID, not a raw content ID: it must
	// keep the resolver path (catalog-prefix search, padding, server-mediated
	// number mapping). Only separator-free compact forms count as content IDs.
	hasSeparator := strings.ContainsAny(lowerRaw, "-_. ")
	isContentID = underscoreCIDShapeRegex.MatchString(lowerRaw) ||
		(!hasSeparator && remasterCIDShapeRegex.MatchString(compact))
	if underscoreCIDShapeRegex.MatchString(lowerRaw) {
		compact = cleanPrefixRegex.ReplaceAllString(lowerRaw, "$1")
	}
	m := remasterTailRegex.FindStringSubmatch(compact)
	if m == nil {
		return "", "", "", isContentID
	}
	series = m[2]
	catalogSuffix = m[4]
	switch m[5] {
	case "hd", "h":
		foldedMarker = "h"
	case "ai":
		foldedMarker = "ai"
	}
	return foldedMarker, series, catalogSuffix, isContentID
}

func foldMarkerSuffix(s string) string {
	if strings.HasSuffix(s, "hd") {
		return s[:len(s)-2] + "h"
	}
	return s
}

// canonicalRemasterDisplayID renders the query's display identity in canonical
// form (series-number + folded marker), e.g. "DV-818AI" -> "DV-818AI",
// "RCT-156-HD" -> "RCT-156H".
func canonicalRemasterDisplayID(id string) string {
	m := remasterTailRegex.FindStringSubmatch(compactQueryID(id))
	if m == nil {
		return strings.ToUpper(id)
	}
	marker := "H"
	if m[5] == "ai" {
		marker = "AI"
	}
	return strings.ToUpper(m[2]) + "-" + m[3] + strings.ToUpper(m[4]) + marker
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

// bindResolvedCID reports whether a URL-extracted cid may stand in for the
// resolved cid on marker paths: exact equality, or equality up to DMM
// catalog-digit prefix stripping with the marker suffix intact.
func bindResolvedCID(urlCID, resolved string, exact bool) bool {
	if exact {
		a := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(urlCID)))
		b := stripRentalSuffixMarkerAware(strings.ToLower(strings.TrimSpace(resolved)))
		return a == b
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
	compact := compactQueryID(id)
	m := remasterTailRegex.FindStringSubmatch(compact)
	if m == nil {
		return nil
	}
	series, number, suffix, spelling := m[2], m[3], m[4], m[5]
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
func parseAnchoredMarkerCID(clean string) (series string, folded string, suffix string, ok bool) {
	m := anchoredMarkerCIDReg.FindStringSubmatch(clean)
	if m == nil {
		return "", "", "", false
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
		return "", "", "", false
	}
	if suffix != "" && suffix != "e" && suffix != "z" {
		return "", "", "", false
	}
	return m[2], folded, suffix, true
}

// extractRemasterContentIDCandidates scans a DMM search document for anchors
// whose content id shares the query's series and folded marker (number-free:
// the server owns the number). The persisted form is the verbatim
// rental-normalized URL cid; deduplication is by prefix-cleaned identity with
// the catalog-digit-prefixed spelling preferred as representative.
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
		series, folded, suffix, ok := parseAnchoredMarkerCID(clean)
		if !ok || series != wantSeries || folded != wantFoldedMarker || suffix != wantSuffix {
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
// accumulate candidates across ALL query variations, dedupe by cleaned cid,
// accept a single distinct cid directly, and verify ambiguous cases via
// product-page display IDs.
func (s *scraper) resolveRemasterContentID(ctx context.Context, id, normalizedID, foldedMarker, series, catalogSuffix string) (string, error) {
	queries := uniqueNonEmptyStrings(append(buildResolveContentIDSearchQueries(id, normalizeContentID(id)), remasterSearchSpellings(id)...))

	type aggCand struct {
		contentID string
		cleanID   string
		urls      []string
	}
	order := []string{}
	byClean := map[string]*aggCand{}

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
			if existing, ok := byClean[c.cleanID]; ok {
				for _, u := range c.urls {
					if !slices.Contains(existing.urls, u) {
						existing.urls = append(existing.urls, u)
					}
				}
				if len(c.contentID) > len(existing.contentID) {
					existing.contentID = c.contentID
				}
				continue
			}
			byClean[c.cleanID] = &aggCand{contentID: c.contentID, cleanID: c.cleanID, urls: append([]string{}, c.urls...)}
			order = append(order, c.cleanID)
		}
	}

	if len(order) == 0 {
		return "", models.NewScraperNotFoundError("DMM", "no matching marker content-id found in DMM search results")
	}

	// Every search-derived candidate — singleton or ambiguous — is verified
	// against product-page display IDs: admission is number-free by design, so
	// only the page's display ID proves the requested release.
	target := foldMarkerSuffix(compactQueryID(id))
	var verified []string
	for _, clean := range order {
		c := byClean[clean]
		status, err := s.verifyCandidateDisplayID(ctx, target, c.urls)
		if err != nil {
			return "", fmt.Errorf("DMM: remaster display verification for %s: %w", id, err)
		}
		if status == displayVerified {
			verified = append(verified, clean)
		}
	}
	if len(verified) == 1 {
		resolved := byClean[verified[0]].contentID
		s.cacheContentID(ctx, normalizedID, resolved)
		return resolved, nil
	}
	return "", models.NewScraperNotFoundError("DMM", fmt.Sprintf("remaster resolution for %s could not verify a unique candidate (%d verified of %d distinct)", id, len(verified), len(order)))
}

func (s *scraper) cacheContentID(ctx context.Context, searchID, contentID string) {
	mapping := &models.ContentIDMapping{SearchID: searchID, ContentID: contentID, Source: "dmm"}
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

var displayIdentityRegex = regexp.MustCompile(`^((?:t28|[a-z]+))0*(\d+)([ez]?)(hd|ai|h)$`)

// parseDisplayIdentity splits a folded display identity into series, numeric
// value, optional E/Z catalog suffix and folded marker so padded spellings
// (rct00156h) match what product pages show (rct156h).
func parseDisplayIdentity(s string) (string, string, string, string, bool) {
	m := displayIdentityRegex.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", "", false
	}
	marker := m[4]
	if marker == "hd" {
		marker = "h"
	}
	return m[1], m[2], m[3], marker, true
}

// verifyCandidateDisplayID fetches every eligible product page for a candidate
// and combines the parseable display identities: zero parseable => unverified,
// multiple distinct => unverifiable, exactly one => verified iff it equals the
// folded target identity.
func (s *scraper) verifyCandidateDisplayID(ctx context.Context, foldedTarget string, urls []string) (displayStatus, error) {
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
				identities[display] = struct{}{}
			}
		}
	}
	if len(identities) == 1 {
		ts, td, tsuf, tm, tok := parseDisplayIdentity(foldedTarget)
		for k := range identities {
			ds, dd, dsuf, dm, dok := parseDisplayIdentity(k)
			if tok && dok && ds == ts && dd == td && dsuf == tsuf && dm == tm {
				return displayVerified, nil
			}
		}
		return displayRejected, nil
	}
	return displayUnverifiable, nil
}

// extractDisplayID reads the display DVD ID (品番) from a DMM product page's
// information table. The CID-like 商品番号 field is never used for verification.
func extractDisplayID(doc *goquery.Document) string {
	var out string
	doc.Find("tr").EachWithBreak(func(i int, sel *goquery.Selection) bool {
		label := strings.TrimSpace(sel.Find("td").First().Text())
		if strings.Contains(label, "商品番号") || !strings.Contains(label, "品番") {
			return true
		}
		cells := sel.Find("td")
		if cells.Length() < 2 {
			return true
		}
		value := strings.TrimSpace(cells.Eq(1).Text())
		norm := nonAlnumRegex.ReplaceAllString(strings.ToLower(value), "")
		if norm == "" {
			return true
		}
		out = foldMarkerSuffix(norm)
		return false
	})
	return out
}
