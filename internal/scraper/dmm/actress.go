package dmm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/image/placeholder"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func (s *scraper) extractActresses(ctx context.Context, doc *goquery.Document) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	actressIndexByID := make(map[int]int)

	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		labelCell := row.Find("td").First()
		if labelCell.Length() == 0 {
			return
		}

		labelText := strings.TrimSpace(labelCell.Text())

		isActressRow := strings.Contains(labelText, "Actress") ||
			strings.Contains(labelText, "actress") ||
			strings.Contains(labelText, "出演者") ||
			strings.Contains(labelText, "演者")

		if !isActressRow {
			return
		}

		contentCell := row.Find("td").Eq(1)
		if contentCell.Length() == 0 {
			return
		}

		contentCell.Find(actressLinkSelector).Each(func(j int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(ctx, sel)
			if actress.DMMID == 0 {
				return
			}
			if actress.JapaneseName == "" && actress.FirstName != "" && actress.LastName != "" {
				actress.FirstName, actress.LastName = actress.LastName, actress.FirstName
			}

			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM: Actress extracted - Name: %s, ThumbURL: %s, ID: %d", actress.FullName(), actress.ThumbURL, actress.DMMID)
			}
		})
	})

	return actresses
}

func (s *scraper) extractActressesFromStreamingPage(ctx context.Context, doc *goquery.Document) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	actressIndexByID := make(map[int]int)

	if castSection := doc.Find(`[data-e2eid='actress-information']`).First(); castSection.Length() > 0 {
		castSection.Find(actressLinkSelector).Each(func(i int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(ctx, sel)
			if actress.DMMID == 0 {
				return
			}
			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM Streaming: Actress extracted from cast section - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})

		if len(actresses) > 0 {
			logging.Debugf("DMM Streaming: Found %d actresses in data-e2eid cast section", len(actresses))
			return actresses
		}
	}

	doc.Find("h2").Each(func(i int, heading *goquery.Selection) {
		if len(actresses) > 0 {
			return
		}
		if !strings.Contains(scraperutil.CleanString(heading.Text()), "この商品に出演しているAV女優") {
			return
		}

		container := findNearestActressContainer(heading)
		if container == nil || container.Length() == 0 {
			return
		}

		container.Find(actressLinkSelector).Each(func(j int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(ctx, sel)
			if actress.DMMID == 0 {
				return
			}
			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM Streaming: Actress extracted from heading-matched cast section - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})
	})

	if len(actresses) > 0 {
		logging.Debugf("DMM Streaming: Found %d actresses via heading-matched cast section", len(actresses))
		return actresses
	}

	metadataSelectors := []string{
		buildScopedActressSelector("table"),
		buildScopedActressSelector("dl"),
		buildScopedActressSelector(".productData"),
		buildScopedActressSelector(".cmn-detail"),
		buildScopedActressSelector(".product-info"),
	}

	for _, selector := range metadataSelectors {
		doc.Find(selector).Each(func(i int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(ctx, sel)
			if actress.DMMID > 0 {
				if !upsertActressInfo(&actresses, actressIndexByID, actress) {
					return
				}
				logging.Debugf("DMM Streaming: Actress extracted from metadata - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})

		if len(actresses) > 0 {
			logging.Debugf("DMM Streaming: Found %d actresses using selector: %s", len(actresses), selector)
			return actresses
		}
	}

	logging.Debug("DMM Streaming: No reliable cast section found; skipping global actress-link fallback")

	return actresses
}

func (s *scraper) extractActressFromLink(ctx context.Context, sel *goquery.Selection) models.ActressInfo {
	href, exists := sel.Attr("href")
	if !exists {
		return models.ActressInfo{}
	}

	actressID := extractActressID(href)
	if actressID == 0 {
		return models.ActressInfo{}
	}

	actressName := cleanActressName(sel.Text())
	if shouldSkipActressName(actressName) {
		return models.ActressInfo{}
	}

	thumbURL := extractActressThumbURL(sel)

	isJapanese := actressJapaneseCharRe.MatchString(actressName)

	actress := models.ActressInfo{
		DMMID:    actressID,
		ThumbURL: thumbURL,
	}

	if isJapanese {
		actress.JapaneseName = actressName
	} else {
		parts := strings.Fields(actressName)
		if len(parts) >= 2 {
			actress.FirstName = parts[0]
			actress.LastName = parts[1]
		} else if len(parts) == 1 {
			actress.FirstName = parts[0]
		}
	}

	if actress.ThumbURL == "" {
		actress.ThumbURL = s.tryActressThumbURLs(ctx, actress.FirstName, actress.LastName, actress.DMMID)
	}

	return actress
}

func buildScopedActressSelector(scope string) string {
	return fmt.Sprintf(
		"%s a[href*='?actress='], %s a[href*='&actress='], %s a[href*='/article=actress/id=']",
		scope, scope, scope,
	)
}

func findNearestActressContainer(sel *goquery.Selection) *goquery.Selection {
	if sel == nil {
		return nil
	}

	container := sel.Parent()
	for depth := 0; depth < 8 && container.Length() > 0; depth++ {
		if container.Find(actressLinkSelector).Length() > 0 {
			return container
		}
		container = container.Parent()
	}

	return nil
}

func extractActressID(href string) int {
	if matches := actressIDRegex.FindStringSubmatch(href); len(matches) > 1 {
		if actressID, err := strconv.Atoi(matches[1]); err == nil {
			return actressID
		}
	}
	if matches := actressArticleIDRegex.FindStringSubmatch(href); len(matches) > 1 {
		if actressID, err := strconv.Atoi(matches[1]); err == nil {
			return actressID
		}
	}
	return 0
}

func cleanActressName(name string) string {
	name = scraperutil.CleanString(name)
	name = actressParenRegex.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func shouldSkipActressName(name string) bool {
	return name == "" ||
		strings.Contains(name, "購入前") ||
		strings.Contains(name, "レビュー") ||
		strings.Contains(name, "ポイント")
}

func extractActressThumbURL(sel *goquery.Selection) string {
	if thumbURL := extractActressThumbURLWithin(sel); thumbURL != "" {
		return thumbURL
	}
	return extractActressThumbURLWithin(sel.Parent())
}

func extractActressThumbURLWithin(root *goquery.Selection) string {
	if root == nil || root.Length() == 0 {
		return ""
	}

	var thumbURL string
	root.Find("img, source").EachWithBreak(func(_ int, image *goquery.Selection) bool {
		for _, attr := range []string{"data-src", "src", "srcset"} {
			value, exists := image.Attr(attr)
			if !exists || value == "" || strings.HasPrefix(value, "data:image") {
				continue
			}
			if normalized := normalizeActressThumbURL(value); normalized != "" {
				thumbURL = normalized
				return false
			}
		}
		return true
	})
	return thumbURL
}

func normalizeActressThumbURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	rawURL = strings.ReplaceAll(rawURL, "&amp;", "&")
	if commaIdx := strings.Index(rawURL, ","); commaIdx != -1 {
		rawURL = strings.TrimSpace(rawURL[:commaIdx])
	}
	if whitespaceIdx := strings.IndexAny(rawURL, " \t\r\n"); whitespaceIdx != -1 {
		rawURL = rawURL[:whitespaceIdx]
	}

	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}
	if strings.HasPrefix(rawURL, "/") && !strings.HasPrefix(rawURL, "//") {
		rawURL = "https://video.dmm.co.jp" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !imageutil.IsDMMHost(parsed.Hostname()) {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Hostname())
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func upsertActressInfo(actresses *[]models.ActressInfo, indexByID map[int]int, actress models.ActressInfo) bool {
	if actress.DMMID == 0 {
		return false
	}

	if idx, exists := indexByID[actress.DMMID]; exists {
		existing := &(*actresses)[idx]
		if existing.ThumbURL == "" && actress.ThumbURL != "" {
			existing.ThumbURL = actress.ThumbURL
		}
		if existing.JapaneseName == "" && actress.JapaneseName != "" {
			existing.JapaneseName = actress.JapaneseName
		}
		if existing.FirstName == "" && actress.FirstName != "" {
			existing.FirstName = actress.FirstName
		}
		if existing.LastName == "" && actress.LastName != "" {
			existing.LastName = actress.LastName
		}
		return false
	}

	indexByID[actress.DMMID] = len(*actresses)
	*actresses = append(*actresses, actress)
	return true
}

func (s *scraper) tryActressThumbURLs(ctx context.Context, firstName, lastName string, dmmID int) string {
	var profileDoc *goquery.Document
	if dmmID > 0 {
		profileDoc = s.fetchActressPageDoc(ctx, dmmID)
	}
	return s.tryActressThumbURLsWithProfileDoc(ctx, firstName, lastName, dmmID, profileDoc)
}

func (s *scraper) tryActressThumbURLsWithProfileDoc(ctx context.Context, firstName, lastName string, dmmID int, profileDoc *goquery.Document) string {
	candidates := buildActressThumbCandidates(firstName, lastName, dmmID, profileDoc)
	if profileDoc != nil {
		for _, romaji := range extractRomajiVariantsFromActressDoc(profileDoc) {
			candidates = append(candidates, fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s.jpg", romaji))
		}
		candidates = dedupeActressImageCandidates(candidates)
	}
	if thumbnail := s.firstExistingActressImage(ctx, candidates); thumbnail != "" {
		return thumbnail
	}
	if dmmID > 0 {
		if candidate := s.resolveActressThumbnailFromStreamingList(ctx, dmmID); candidate != "" {
			if thumbnail := s.firstExistingActressImage(ctx, []string{candidate}); thumbnail != "" {
				return thumbnail
			}
		}
	}
	logging.Debugf("DMM: No actress thumbnail found (tried %d profile candidates)", len(candidates))
	return ""
}

var newActressProbeClient = httpclient.NewRestyClient

func (s *scraper) firstExistingActressImage(ctx context.Context, candidates []string) string {
	testClient, err := newActressProbeClient(s.proxyProfile, 5*time.Second, 0)
	if err != nil {
		// Warn (not Debug): falling back to an explicit no-proxy client can
		// expose the caller's direct IP when the configured proxy is unreachable,
		// so this must be visible at the default log level, not debug-only.
		logging.Warnf("DMM: Failed to create thumbnail probe client with scraper proxy: %v, using explicit no-proxy fallback", err)
		testClient = httpclient.NewRestyClientNoProxy(5*time.Second, 0)
	}
	testClient.SetRedirectPolicy(resty.NoRedirectPolicy())

	placeholderConfig := placeholder.ConfigFromSettings(&s.settings, placeholder.DefaultDMMPlaceholderHashes)
	for _, candidate := range candidates {
		if !actressImageExists(ctx, testClient, candidate) {
			continue
		}
		filtered, count, filterErr := placeholder.FilterURLs(ctx, testClient, []string{candidate}, placeholderConfig)
		if filterErr != nil || count > 0 || len(filtered) == 0 {
			logging.Debugf("DMM: Rejected actress placeholder thumbnail: %s", candidate)
			continue
		}
		logging.Debugf("DMM: Found actress thumbnail via fallback: %s", candidate)
		return candidate
	}
	return ""
}

func (s *scraper) resolveActressThumbnailFromStreamingList(ctx context.Context, dmmID int) string {
	listDoc, err := s.fetchActressStreamingDoc(ctx, fmt.Sprintf("https://video.dmm.co.jp/av/list/?actress=%d", dmmID))
	if err != nil {
		logging.Debugf("DMM: Actress streaming list lookup failed for ID %d: %v", dmmID, err)
		return ""
	}
	detailURL := firstActressStreamingDetailURL(listDoc)
	if detailURL == "" {
		return ""
	}
	detailDoc, err := s.fetchActressStreamingDoc(ctx, detailURL)
	if err != nil {
		logging.Debugf("DMM: First actress streaming detail lookup failed for ID %d: %v", dmmID, err)
		return ""
	}
	return extractExactActressThumbFromStreamingDoc(detailDoc, dmmID)
}

var fetchActressPageWithBrowser = fetchWithBrowser

var parseActressPageHTML = func(bodyHTML string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(bodyHTML))
}

func (s *scraper) fetchActressStreamingDoc(ctx context.Context, rawURL string) (*goquery.Document, error) {
	if s.useBrowser {
		bodyHTML, err := fetchActressPageWithBrowser(ctx, rawURL, s.browserConfig.Timeout, s.proxyProfile, s.getEnvLookup(), s.getFs())
		if err != nil {
			return nil, err
		}
		return parseActressPageHTML(bodyHTML)
	}
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := s.client.R().SetContext(ctx).Get(rawURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, models.NewScraperStatusError("DMM", resp.StatusCode(), "DMM actress streaming page lookup failed")
	}
	return parseActressPageHTML(resp.String())
}

func firstActressStreamingDetailURL(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	base, _ := url.Parse("https://video.dmm.co.jp")
	var detailURL string
	doc.Find(`a[href*='/av/content/?id=']`).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href := sel.AttrOr("href", "")
		ref, err := url.Parse(strings.TrimSpace(href))
		if err != nil {
			return true
		}
		resolved := base.ResolveReference(ref)
		id := resolved.Query().Get("id")
		if resolved.Scheme != "https" || resolved.Host != "video.dmm.co.jp" || resolved.Path != "/av/content/" || id == "" {
			return true
		}
		query := url.Values{}
		query.Set("id", id)
		resolved.RawQuery = query.Encode()
		resolved.Fragment = ""
		detailURL = resolved.String()
		return false
	})
	return detailURL
}

func extractExactActressThumbFromStreamingDoc(doc *goquery.Document, dmmID int) string {
	if doc == nil || dmmID <= 0 {
		return ""
	}
	var thumbnail string
	doc.Find(actressLinkSelector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href, _ := sel.Attr("href")
		if extractActressID(href) != dmmID {
			return true
		}
		root := sel
		for depth := 0; depth < 4 && root.Length() > 0; depth++ {
			if depth > 0 && !containsOnlyExactActressLink(root, dmmID) {
				break
			}
			if candidate := extractActressThumbURLWithin(root); candidate != "" {
				thumbnail = candidate
				return false
			}
			root = root.Parent()
		}
		return true
	})
	return thumbnail
}

func containsOnlyExactActressLink(root *goquery.Selection, dmmID int) bool {
	count := 0
	exact := true
	root.Find(actressLinkSelector).Each(func(_ int, sel *goquery.Selection) {
		count++
		href, _ := sel.Attr("href")
		if extractActressID(href) != dmmID {
			exact = false
		}
	})
	return count == 1 && exact
}

func (s *scraper) ResolveActressThumbnail(ctx context.Context, actress models.ActressInfo) string {
	if actress.DMMID > 0 {
		return s.tryActressThumbURLs(ctx, actress.FirstName, actress.LastName, actress.DMMID)
	}
	if actress.ThumbURL != "" {
		return normalizeActressThumbURL(actress.ThumbURL)
	}
	return s.tryActressThumbURLs(ctx, actress.FirstName, actress.LastName, actress.DMMID)
}

func (s *scraper) ResolveActressMetadata(ctx context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	if actress.DMMID <= 0 {
		return models.ActressInfo{}, nil
	}
	profileDoc, err := s.fetchActressMetadataDocErr(ctx, actress.DMMID)
	if err != nil {
		return models.ActressInfo{DMMID: actress.DMMID}, err
	}
	if profileDoc == nil {
		return models.ActressInfo{DMMID: actress.DMMID}, nil
	}
	metadata := extractActressProfileMetadata(profileDoc, actress.DMMID)
	if metadata.JapaneseName == "" {
		metadata.JapaneseName = actress.JapaneseName
	}
	if actress.ThumbURL == "" || models.IsKnownInvalidDMMActressThumbnail(actress.ThumbURL) {
		metadata.ThumbURL = s.tryActressThumbURLsWithProfileDoc(ctx, actress.FirstName, actress.LastName, actress.DMMID, profileDoc)
	}
	return metadata, nil
}

var _ models.ActressThumbnailResolver = (*scraper)(nil)
var _ models.ActressMetadataResolver = (*scraper)(nil)
var _ models.ActressFieldCapable = (*scraper)(nil)

// ActressFields ... DMM actress profiles carry all name variants and thumbnails.
func (s *scraper) ActressFields() []string {
	return []string{"actress", "actress_japanese_name", "actress_first_name", "actress_last_name", "actress_url"}
}

// fetchActressMetadataDocErr surfaces transient failures so the resolver
// contract can distinguish "no better data" from "lookup failed".
func (s *scraper) fetchActressMetadataDocErr(ctx context.Context, dmmID int) (*goquery.Document, error) {
	profileURL := fmt.Sprintf("https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=%d/", dmmID)
	if s.useBrowser {
		bodyHTML, err := fetchActressPageWithBrowser(ctx, profileURL, s.browserConfig.Timeout, s.proxyProfile, s.getEnvLookup(), s.getFs())
		if err != nil {
			return nil, err
		}
		doc, err := parseActressPageHTML(bodyHTML)
		if err != nil {
			return nil, err
		}
		return doc, nil
	}
	return s.fetchActressPageDocErr(ctx, dmmID)
}

// fetchActressPageDoc keeps its silent-failure shape for non-resolver
// callers; resolver flows use fetchActressPageDocErr.
func (s *scraper) fetchActressPageDoc(ctx context.Context, dmmID int) *goquery.Document {
	doc, err := s.fetchActressPageDocErr(ctx, dmmID)
	if err != nil {
		logging.Debugf("DMM: Failed to fetch actress page for ID %d: %v", dmmID, err)
		return nil
	}
	return doc
}

func (s *scraper) fetchActressPageDocErr(ctx context.Context, dmmID int) (*goquery.Document, error) {
	profileURL := fmt.Sprintf("https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=%d/", dmmID)
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := s.client.R().SetContext(ctx).Get(profileURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("DMM actress page %d: HTTP %s", dmmID, resp.Status())
	}
	doc, err := parseActressPageHTML(resp.String())
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func extractActressProfileMetadata(doc *goquery.Document, dmmID int) models.ActressInfo {
	metadata := models.ActressInfo{DMMID: dmmID}
	if doc == nil || dmmID <= 0 {
		return metadata
	}
	name := strings.TrimSpace(doc.Find("h1.list-title .bold").First().Text())
	if name == "" {
		name = strings.TrimSpace(doc.Find("h1.list-title").First().Text())
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, "の商品一覧"))
	if name == "" {
		return metadata
	}
	if open := strings.Index(name, "("); open >= 0 {
		name = strings.TrimSpace(name[:open])
	}
	if actressJapaneseCharRe.MatchString(name) {
		metadata.JapaneseName = name
		return metadata
	}
	parts := strings.Fields(name)
	if len(parts) >= 2 {
		metadata.FirstName = parts[0]
		metadata.LastName = parts[1]
	}
	return metadata
}

func extractRomajiVariantsFromActressDoc(doc *goquery.Document) []string {
	if doc == nil {
		return nil
	}
	title := doc.Find("title").Text()
	if title == "" {
		title = doc.Find("h1.list-title .bold").First().Text()
	}
	re := regexp.MustCompile(`\(([ぁ-ん]+)\)`)
	matches := re.FindStringSubmatch(title)
	if len(matches) < 2 {
		logging.Debugf("DMM: No hiragana reading found in actress page title")
		return nil
	}
	hiragana := matches[1]
	logging.Debugf("DMM: Extracted hiragana reading: %s", hiragana)
	romaji := hiraganaToRomaji(hiragana)
	logging.Debugf("DMM: Converted to romaji: %s", romaji)
	variants := make([]string, 0)
	if len(romaji) >= 4 {
		splitPoints := []int{8, 7, 6, 5, 4, 3, 9, 10, 2}
		for _, splitPoint := range splitPoints {
			if splitPoint < len(romaji)-1 {
				variants = append(variants, romaji[:splitPoint]+"_"+romaji[splitPoint:])
			}
		}
	}
	variants = append(variants, romaji)
	logging.Debugf("DMM: Generated %d romaji variants from hiragana", len(variants))
	return variants
}

func buildActressThumbCandidates(firstName, lastName string, dmmID int, profileDoc *goquery.Document) []string {
	candidates := make([]string, 0, 4)
	if firstName != "" && lastName != "" {
		firstLower := strings.ToLower(firstName)
		lastLower := strings.ToLower(lastName)
		candidates = append(candidates,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", lastLower, firstLower),
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", firstLower, lastLower),
		)
	}
	if dmmID > 0 && profileDoc != nil {
		candidates = append(candidates, extractActressProfileImageCandidates(profileDoc)...)
	}
	return dedupeActressImageCandidates(candidates)
}

func extractActressProfileImageCandidates(doc *goquery.Document) []string {
	if doc == nil {
		return nil
	}
	awsCandidates := make([]string, 0)
	otherCandidates := make([]string, 0)
	add := func(raw string) {
		normalized := normalizeActressThumbURL(raw)
		if normalized == "" || !strings.Contains(normalized, "/mono/actjpgs/") {
			return
		}
		parsed, _ := url.Parse(normalized)
		if strings.HasPrefix(strings.ToLower(parsed.Hostname()), "awsimgsrc.") {
			awsCandidates = append(awsCandidates, normalized)
			return
		}
		otherCandidates = append(otherCandidates, normalized)
	}
	doc.Find("img, source").Each(func(_ int, sel *goquery.Selection) {
		for _, attr := range []string{"data-src", "src", "srcset"} {
			if value, exists := sel.Attr(attr); exists {
				add(value)
			}
		}
	})
	return dedupeActressImageCandidates(append(awsCandidates, otherCandidates...))
}

func dedupeActressImageCandidates(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func actressImageExists(ctx context.Context, client *resty.Client, candidate string) bool {
	resp, err := client.R().SetContext(ctx).SetDoNotParseResponse(true).Head(candidate)
	status := actressProbeStatus(resp)
	if err == nil && status == http.StatusOK {
		return true
	}
	if err == nil && status != http.StatusMethodNotAllowed {
		return false
	}
	resp, err = client.R().SetContext(ctx).SetDoNotParseResponse(true).Get(candidate)
	status = actressProbeStatus(resp)
	return err == nil && status == http.StatusOK
}

func actressProbeStatus(resp *resty.Response) int {
	if resp == nil {
		return 0
	}
	status := resp.StatusCode()
	if body := resp.RawBody(); body != nil {
		_ = body.Close()
	}
	return status
}

//nolint:unused // used by tests
func (s *scraper) extractRomajiVariantsFromActressPageCtx(ctx context.Context, dmmID int) []string {
	doc := s.fetchActressPageDoc(ctx, dmmID)
	if doc == nil {
		return nil
	}
	return extractRomajiVariantsFromActressDoc(doc)
}
