package dmm

import (
	"encoding/json"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// video_dmm.go contains extraction functions specific to video.dmm.co.jp (new site format).
//
// Architecture Pattern:
// - dmm.go: Main orchestrator that detects site version via isNewSite boolean
// - video_dmm.go: Specialized extractors for video.dmm.co.jp (*NewSite functions)
//
// All functions in this file:
// 1. Are methods on *scraper for consistency with the main scraper
// 2. Accept *goquery.Document as the primary parameter
// 3. Use the *NewSite naming convention to indicate video.dmm.co.jp specificity
// 4. Are called from dmm.go's main extraction functions when isNewSite is true
//
// The isNewSite boolean is determined in parseHTML() by checking if the source URL
// contains "video.dmm.co.jp" versus "www.dmm.co.jp".

// extractDescriptionNewSite extracts description from video.dmm.co.jp
func (s *scraper) extractDescriptionNewSite(doc *goquery.Document) string {
	// Try og:description meta tag
	desc, exists := doc.Find(`meta[property="og:description"]`).Attr("content")
	if exists && desc != "" {
		return scraperutil.CleanString(desc)
	}

	// Try regular meta description as fallback
	desc, exists = doc.Find(`meta[name="description"]`).Attr("content")
	if exists && desc != "" {
		return scraperutil.CleanString(desc)
	}

	return ""
}

// extractCoverURLNewSite extracts cover image from video.dmm.co.jp
func (s *scraper) extractCoverURLNewSite(doc *goquery.Document, contentID string) string {
	// Try og:image meta tag
	coverURL, exists := doc.Find(`meta[property="og:image"]`).Attr("content")
	logging.Debugf("DMM Streaming: og:image exists=%v, value=%s", exists, coverURL)
	if exists && coverURL != "" {
		coverURL = imageutil.UpgradeDMMCoverCDN(imageutil.UpgradeCoverResolution(imageutil.NormalizeDMMScreenshotURL(coverURL)))
		logging.Debugf("DMM Streaming: Final cover URL from og:image: %s", coverURL)
		return coverURL
	}

	// Try to extract from CSS background-image style attributes
	// Some amateur videos use: style="background-image: url(//pics.dmm.co.jp/digital/amateur/oreco183/oreco183jp.jpg);"
	logging.Debug("DMM Streaming: og:image not found, trying CSS background-image")
	var bgImageURL string
	doc.Find("*[style*='background-image']").Each(func(i int, sel *goquery.Selection) {
		if bgImageURL != "" {
			return // Already found one
		}
		style, exists := sel.Attr("style")
		if !exists {
			return
		}
		// Extract URL from background-image: url(...)
		// Handle both url(...) and url("...") and url('...')
		// Also handle protocol-relative URLs starting with //
		bgURL := extractBackgroundImageURL(style)
		if bgURL != "" {
			logging.Debugf("DMM Streaming: Found background-image URL: %s", bgURL)
			bgImageURL = bgURL
		}
	})

	if bgImageURL != "" {
		coverURL = imageutil.UpgradeDMMCoverCDN(imageutil.UpgradeCoverResolution(imageutil.NormalizeDMMScreenshotURL(bgImageURL)))
		logging.Debugf("DMM Streaming: Final cover URL from background-image: %s", coverURL)
		return coverURL
	}

	// As fallback, try to extract from img tags
	logging.Debug("DMM Streaming: background-image not found, trying img tag fallback")
	coverURL, _ = doc.Find(`img[src*="pl.jpg"]`).First().Attr("src")
	logging.Debugf("DMM Streaming: img[src*='pl.jpg'] found: %s", coverURL)
	if coverURL != "" {
		coverURL = imageutil.UpgradeDMMCoverCDN(imageutil.UpgradeCoverResolution(imageutil.NormalizeDMMScreenshotURL(coverURL)))
		logging.Debugf("DMM Streaming: Final cover URL from img tag: %s", coverURL)
		return coverURL
	}

	// Debug: List all img tags to see what's available
	imgCount := 0
	doc.Find("img").Each(func(i int, sel *goquery.Selection) {
		src, _ := sel.Attr("src")
		if imgCount < 5 { // Only log first 5 to avoid spam
			logging.Debugf("DMM Streaming: Found img[%d]: %s", i, src)
		}
		imgCount++
	})
	logging.Debugf("DMM Streaming: Total img tags found: %d", imgCount)

	// Final fallback for amateur videos: construct URL from content ID
	// Amateur videos use pattern: https://pics.dmm.co.jp/digital/amateur/{contentid}/{contentid}jp.jpg
	// Note: Amateur videos use 'jp.jpg' suffix, not 'pl.jpg' (pl.jpg doesn't exist for amateur videos)
	// DMM serves cover assets on lowercase paths, so normalize to lowercase
	if contentID != "" {
		normalizedID := strings.ToLower(contentID)
		coverURL = imageutil.UpgradeDMMCoverCDN(imageutil.UpgradeCoverResolution("https://pics.dmm.co.jp/digital/amateur/" + normalizedID + "/" + normalizedID + "jp.jpg"))
		logging.Debugf("DMM Streaming: Constructed amateur cover URL from content ID '%s': %s", contentID, coverURL)
		return coverURL
	}

	logging.Debug("DMM Streaming: No cover URL found")
	return ""
}

// extractScreenshotsNewSite extracts screenshots from video.dmm.co.jp
// JSON-LD screenshot extraction is handled by extractMetadataFromJSONLD (called from parseHTML).
// This function only handles the img-tag fallback path.
func (s *scraper) extractScreenshotsNewSite(doc *goquery.Document) []string {
	screenshots := make([]string, 0)
	seen := make(map[string]bool)

	// Extract from img tags
	doc.Find(`img[src*="awsimgsrc.dmm.co.jp"]`).Each(func(i int, sel *goquery.Selection) {
		src, exists := sel.Attr("src")
		if !exists {
			return
		}

		// Only process screenshot images (those with -1.jpg, -2.jpg, etc.)
		if !strings.Contains(src, "-") || strings.HasSuffix(src, "pl.jpg") {
			return
		}

		// Convert awsimgsrc to pics.dmm.co.jp and remove query parameters
		src = strings.Replace(src, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)
		if idx := strings.Index(src, "?"); idx != -1 {
			src = src[:idx]
		}

		// Deduplicate
		if !seen[src] {
			seen[src] = true
			screenshots = append(screenshots, src)
		}
	})

	if len(screenshots) > 0 {
		logging.Debugf("DMM Streaming: Found %d screenshots from img tags", len(screenshots))
	} else {
		logging.Debug("DMM Streaming: No screenshots found")
	}

	return screenshots
}

// extractSeriesNewSite extracts series from video.dmm.co.jp
func (s *scraper) extractSeriesNewSite(doc *goquery.Document) string {
	// Look for table rows containing "シリーズ" (Series)
	var series string
	doc.Find("table tr").Each(func(i int, row *goquery.Selection) {
		// Check if the row header contains "シリーズ"
		th := row.Find("th").Text()
		if strings.Contains(th, "シリーズ") {
			// Extract the link text from the td
			link := row.Find("td a")
			if link.Length() > 0 {
				series = strings.TrimSpace(link.Text())
				return
			}
		}
	})
	return scraperutil.CleanString(series)
}

// extractMakerNewSite extracts maker from video.dmm.co.jp
func (s *scraper) extractMakerNewSite(doc *goquery.Document) string {
	// Look for table rows containing "メーカー" (Maker)
	var maker string
	doc.Find("table tr").Each(func(i int, row *goquery.Selection) {
		// Check if the row header contains "メーカー"
		th := row.Find("th").Text()
		if strings.Contains(th, "メーカー") {
			// Extract the link text from the td
			link := row.Find("td a")
			if link.Length() > 0 {
				maker = strings.TrimSpace(link.Text())
				return
			}
		}
	})
	return scraperutil.CleanString(maker)
}

func (s *scraper) extractRatingNewSite(doc *goquery.Document) (float64, int) {
	var rating float64
	var votes int

	doc.Find(`script[type="application/ld+json"]`).Each(func(i int, sel *goquery.Selection) {
		jsonText := sel.Text()

		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
			return
		}

		if rawRating, ok := data["aggregateRating"]; ok {
			var aggRating map[string]json.RawMessage
			if err := json.Unmarshal(rawRating, &aggRating); err != nil {
				return
			}

			if rawVal, ok := aggRating["ratingValue"]; ok {
				var val float64
				if err := json.Unmarshal(rawVal, &val); err == nil {
					rating = val * 2
				}
			}
			if rawCount, ok := aggRating["ratingCount"]; ok {
				var count int
				if err := json.Unmarshal(rawCount, &count); err == nil {
					votes = count
				}
			}
		}
	})

	return rating, votes
}

// extractBackgroundImageURL extracts URL from CSS background-image property
// Handles formats: url(...), url("..."), url('...')
// Returns empty string if no URL found
func extractBackgroundImageURL(style string) string {
	// Look for background-image: url(...)
	startIdx := strings.Index(style, "background-image:")
	if startIdx == -1 {
		return ""
	}

	// Find url( part
	urlIdx := strings.Index(style[startIdx:], "url(")
	if urlIdx == -1 {
		return ""
	}

	// Start after "url("
	start := startIdx + urlIdx + 4
	if start >= len(style) {
		return ""
	}

	// Skip any leading quotes or whitespace
	for start < len(style) && (style[start] == '"' || style[start] == '\'' || style[start] == ' ') {
		start++
	}

	// Find the end (closing paren, quote, or semicolon)
	end := start
	for end < len(style) {
		ch := style[end]
		if ch == ')' || ch == '"' || ch == '\'' || ch == ';' || ch == ' ' {
			break
		}
		end++
	}

	if end <= start {
		return ""
	}

	url := style[start:end]
	return strings.TrimSpace(url)
}

// extractTrailerURLNewSite extracts trailer video URL from video.dmm.co.jp
func (s *scraper) extractTrailerURLNewSite(doc *goquery.Document) string {
	var trailerURL string

	// Strategy 1: Look for video tags or source tags with sample/trailer URLs
	doc.Find("video source").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return
		}
		src, exists := sel.Attr("src")
		if exists && (strings.Contains(src, "litevideo") || strings.Contains(src, "sample") || strings.Contains(src, ".mp4")) {
			trailerURL = src
			logging.Debugf("DMM Streaming: Found trailer URL from video source: %s", trailerURL)
		}
	})

	if trailerURL != "" {
		return normalizeTrailerURL(trailerURL)
	}

	// Strategy 2: Look for data attributes on video elements
	doc.Find("video").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return
		}
		// Check various data attributes that might contain the video URL
		for _, attr := range []string{"data-src", "data-video-url", "data-sample-url", "src"} {
			if src, exists := sel.Attr(attr); exists {
				if strings.Contains(src, ".mp4") {
					trailerURL = src
					logging.Debugf("DMM Streaming: Found trailer URL from video[%s]: %s", attr, trailerURL)
					return
				}
			}
		}
	})

	if trailerURL != "" {
		return normalizeTrailerURL(trailerURL)
	}

	// Strategy 3: Look for onclick attributes with video URLs (similar to old site)
	doc.Find("a[onclick*='video']").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return
		}

		onclick, exists := sel.Attr("onclick")
		if !exists {
			return
		}

		// Extract URL from onclick
		if idx := strings.Index(onclick, "http"); idx != -1 {
			remaining := onclick[idx:]
			endIdx := strings.IndexAny(remaining, `"'&`)
			if endIdx != -1 {
				url := remaining[:endIdx]
				url = strings.ReplaceAll(url, `\/`, `/`)
				if strings.Contains(url, ".mp4") {
					trailerURL = url
					logging.Debugf("DMM Streaming: Found trailer URL from onclick: %s", trailerURL)
				}
			}
		}
	})

	if trailerURL != "" {
		return normalizeTrailerURL(trailerURL)
	}

	// Strategy 4: Look for script tags with video URL in JSON
	doc.Find("script").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return
		}

		scriptContent := sel.Text()
		// Look for patterns like "sampleUrl":"https://..." or 'sampleUrl':'https://...'
		for _, pattern := range []string{`"sampleUrl"`, `'sampleUrl'`, `"videoUrl"`, `'videoUrl'`} {
			if idx := strings.Index(scriptContent, pattern); idx != -1 {
				remaining := scriptContent[idx:]
				if urlIdx := strings.Index(remaining, "http"); urlIdx != -1 {
					urlPart := remaining[urlIdx:]
					endIdx := strings.IndexAny(urlPart, `"',}]`)
					if endIdx != -1 {
						url := urlPart[:endIdx]
						url = strings.ReplaceAll(url, `\/`, `/`)
						if strings.Contains(url, ".mp4") {
							trailerURL = url
							logging.Debugf("DMM Streaming: Found trailer URL from script JSON: %s", trailerURL)
							return
						}
					}
				}
			}
		}
	})

	if trailerURL != "" {
		return normalizeTrailerURL(trailerURL)
	}

	logging.Debug("DMM Streaming: No trailer URL found")
	return ""
}

// normalizeTrailerURL normalizes trailer URLs from DMM
// Handles protocol-relative URLs and unescapes slashes
func normalizeTrailerURL(url string) string {
	// Handle protocol-relative URLs
	if strings.HasPrefix(url, "//") {
		url = "https:" + url
	}

	// Unescape slashes
	url = strings.ReplaceAll(url, `\/`, `/`)

	// Remove query parameters if any
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}

	return url
}
