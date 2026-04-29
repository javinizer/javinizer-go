package dmm

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraper/image/placeholder"
)

func (s *Scraper) extractCoverURL(doc *goquery.Document, isNewSite bool, contentID string) string {
	if isNewSite {
		return s.extractCoverURLNewSite(doc, contentID)
	}

	coverRegex := regexp.MustCompile(`(https://pics\.dmm\.co\.jp/(?:mono/movie/adult|digital/(?:video|amateur))/(.*)/(.*).jpg)`)
	html, _ := doc.Html()
	matches := coverRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		return strings.Replace(matches[1], "ps.jpg", "pl.jpg", 1)
	}
	return ""
}

func (s *Scraper) extractScreenshots(doc *goquery.Document, isNewSite bool) []string {
	if isNewSite {
		return s.extractScreenshotsNewSite(doc)
	}

	screenshots := make([]string, 0)

	doc.Find("a[name='sample-image']").Each(func(i int, sel *goquery.Selection) {
		if imgSrc, exists := sel.Find("img").Attr("data-lazy"); exists {
			imgSrc = strings.Replace(imgSrc, "-", "jp-", 1)
			screenshots = append(screenshots, imgSrc)
		}
	})

	return screenshots
}

func (s *Scraper) filterPlaceholderScreenshots(ctx context.Context, urls []string) []string {
	if len(urls) == 0 {
		return urls
	}

	cfg := placeholder.ConfigFromSettings(&s.settings, placeholder.DefaultDMMPlaceholderHashes)
	filtered, count, err := placeholder.FilterURLs(ctx, s.client, urls, cfg)
	if err != nil {
		logging.Warnf("DMM: Placeholder filter error: %v", err)
		return urls
	}
	if count > 0 {
		logging.Debugf("DMM: Filtered %d placeholder screenshots from results", count)
	}
	return filtered
}

func (s *Scraper) extractTrailerURL(doc *goquery.Document, sourceURL string) string {
	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")

	if isNewSite {
		return s.extractTrailerURLNewSite(doc)
	}

	var trailerURL string

	doc.Find("a.fn-sampleVideoBtn").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return
		}

		onclick, exists := sel.Attr("onclick")
		if !exists {
			return
		}

		if idx := strings.Index(onclick, `video_url`); idx != -1 {
			remaining := onclick[idx:]

			urlStart := -1
			if idx := strings.Index(remaining, `https:`); idx != -1 {
				urlStart = idx
			} else if idx := strings.Index(remaining, `http:`); idx != -1 {
				urlStart = idx
			}

			if urlStart != -1 {
				urlPart := remaining[urlStart:]
				endMarkers := []string{`\&quot;`, `&quot;`, `"`, `'`}
				urlEnd := len(urlPart)

				for _, marker := range endMarkers {
					if idx := strings.Index(urlPart, marker); idx != -1 && idx < urlEnd {
						urlEnd = idx
					}
				}

				rawURL := urlPart[:urlEnd]
				trailerURL = strings.ReplaceAll(rawURL, `\/`, `/`)
				logging.Debugf("DMM: Found trailer URL from onclick: %s", trailerURL)
			}
		}
	})

	return trailerURL
}
