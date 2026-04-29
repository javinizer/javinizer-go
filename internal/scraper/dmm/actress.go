package dmm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func (s *Scraper) extractActresses(ctx context.Context, doc *goquery.Document) []models.ActressInfo {
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

func (s *Scraper) extractActressesFromStreamingPage(ctx context.Context, doc *goquery.Document) []models.ActressInfo {
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

func (s *Scraper) extractActressFromLink(ctx context.Context, sel *goquery.Selection) models.ActressInfo {
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
	extractFrom := func(root *goquery.Selection) string {
		if root == nil || root.Length() == 0 {
			return ""
		}

		if img := root.Find("img").First(); img.Length() > 0 {
			for _, attr := range []string{"data-src", "src", "srcset"} {
				if value, exists := img.Attr(attr); exists && value != "" && !strings.HasPrefix(value, "data:image") {
					return value
				}
			}
		}

		if source := root.Find("source").First(); source.Length() > 0 {
			if value, exists := source.Attr("srcset"); exists && value != "" {
				return value
			}
		}

		return ""
	}

	if thumbURL := normalizeActressThumbURL(extractFrom(sel)); thumbURL != "" {
		return thumbURL
	}

	return normalizeActressThumbURL(extractFrom(sel.Parent()))
}

func normalizeActressThumbURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	url = strings.ReplaceAll(url, "&amp;", "&")
	if commaIdx := strings.Index(url, ","); commaIdx != -1 {
		url = strings.TrimSpace(url[:commaIdx])
	}
	if whitespaceIdx := strings.IndexAny(url, " \t\r\n"); whitespaceIdx != -1 {
		url = url[:whitespaceIdx]
	}

	if strings.HasPrefix(url, "//") {
		url = "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		url = "https://video.dmm.co.jp" + url
	}
	url = strings.Replace(url, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)

	if queryIdx := strings.Index(url, "?"); queryIdx != -1 {
		url = url[:queryIdx]
	}

	return strings.TrimSpace(url)
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

func (s *Scraper) tryActressThumbURLs(ctx context.Context, firstName, lastName string, dmmID int) string {
	candidates := make([]string, 0)

	if firstName != "" && lastName != "" {
		firstLower := strings.ToLower(firstName)
		lastLower := strings.ToLower(lastName)

		candidates = append(candidates,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", lastLower, firstLower),
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", firstLower, lastLower),
		)
	}

	if dmmID > 0 {
		romajiVariants := s.extractRomajiVariantsFromActressPageCtx(ctx, dmmID)
		for _, romaji := range romajiVariants {
			candidates = append(candidates,
				fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s.jpg", romaji),
			)
		}
	}

	testClient, err := httpclient.NewRestyClient(s.proxyProfile, 5*time.Second, 0)
	if err != nil {
		logging.Debugf("DMM: Failed to create thumbnail probe client with scraper proxy: %v, using explicit no-proxy fallback", err)
		testClient = httpclient.NewRestyClientNoProxy(5*time.Second, 0)
	}
	testClient.SetRedirectPolicy(resty.NoRedirectPolicy())

	for _, url := range candidates {
		resp, err := testClient.R().
			SetContext(ctx).
			SetDoNotParseResponse(true).
			Head(url)

		if err == nil && resp.StatusCode() == 200 {
			logging.Debugf("DMM: Found actress thumbnail via fallback: %s", url)
			return url
		}
	}

	logging.Debugf("DMM: No actress thumbnail found (tried %d candidates)", len(candidates))
	return ""
}

func (s *Scraper) extractRomajiVariantsFromActressPageCtx(ctx context.Context, dmmID int) []string {
	url := fmt.Sprintf("https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=%d/", dmmID)

	if err := s.rateLimiter.Wait(ctx); err != nil {
		logging.Debugf("DMM: Rate limit wait failed for actress page: %v", err)
		return nil
	}

	resp, err := s.client.R().SetContext(ctx).Get(url)
	if err != nil || resp.StatusCode() != 200 {
		logging.Debugf("DMM: Failed to fetch actress page for ID %d", dmmID)
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil
	}

	title := doc.Find("title").Text()

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
				lastName := romaji[:splitPoint]
				firstName := romaji[splitPoint:]
				variant := lastName + "_" + firstName
				variants = append(variants, variant)
			}
		}
	}

	variants = append(variants, romaji)

	logging.Debugf("DMM: Generated %d romaji variants from hiragana", len(variants))
	return variants
}
