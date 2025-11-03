package dmm

import (
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// video_dmm.go contains extraction functions specific to video.dmm.co.jp (new site format).
//
// Architecture Pattern:
// - dmm.go: Main orchestrator that detects site version via isNewSite boolean
// - video_dmm.go: Specialized extractors for video.dmm.co.jp (*NewSite functions)
//
// All functions in this file:
// 1. Are methods on *Scraper for consistency with the main scraper
// 2. Accept *goquery.Document as the primary parameter
// 3. Use the *NewSite naming convention to indicate video.dmm.co.jp specificity
// 4. Are called from dmm.go's main extraction functions when isNewSite is true
//
// The isNewSite boolean is determined in parseHTML() by checking if the source URL
// contains "video.dmm.co.jp" versus "www.dmm.co.jp".

// extractDescriptionNewSite extracts description from video.dmm.co.jp
func (s *Scraper) extractDescriptionNewSite(doc *goquery.Document) string {
	// 1. Try to extract from JSON-LD structured data (most reliable)
	doc.Find(`script[type="application/ld+json"]`).Each(func(i int, sel *goquery.Selection) {
		jsonText := sel.Text()
		// Check if this JSON contains description field
		if strings.Contains(jsonText, `"description"`) {
			// Extract description using simple string parsing (more reliable than full JSON parsing)
			// Look for pattern: "description":"text"
			if idx := strings.Index(jsonText, `"description":"`); idx != -1 {
				start := idx + len(`"description":"`)
				// Find the end quote (accounting for escaped quotes)
				remaining := jsonText[start:]
				var desc strings.Builder
				escaped := false
				for _, ch := range remaining {
					if escaped {
						desc.WriteRune(ch)
						escaped = false
						continue
					}
					if ch == '\\' {
						escaped = true
						continue
					}
					if ch == '"' {
						// Found the end
						result := strings.TrimSpace(desc.String())
						if len(result) > 30 {
							return // Will use this description
						}
						break
					}
					desc.WriteRune(ch)
				}
			}
		}
	})

	// If we found a description in JSON-LD, return it
	var jsonDesc string
	doc.Find(`script[type="application/ld+json"]`).Each(func(i int, sel *goquery.Selection) {
		jsonText := sel.Text()
		if idx := strings.Index(jsonText, `"description":"`); idx != -1 {
			start := idx + len(`"description":"`)
			remaining := jsonText[start:]
			var desc strings.Builder
			escaped := false
			for _, ch := range remaining {
				if escaped {
					desc.WriteRune(ch)
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					break
				}
				desc.WriteRune(ch)
			}
			result := strings.TrimSpace(desc.String())
			if len(result) > 30 {
				jsonDesc = result
			}
		}
	})
	if jsonDesc != "" {
		return cleanString(jsonDesc)
	}

	// 2. Try og:description meta tag as fallback
	desc, exists := doc.Find(`meta[property="og:description"]`).Attr("content")
	if exists && desc != "" {
		return cleanString(desc)
	}

	// 3. Try regular meta description as last resort
	desc, exists = doc.Find(`meta[name="description"]`).Attr("content")
	if exists && desc != "" {
		return cleanString(desc)
	}

	return ""
}

// extractCoverURLNewSite extracts cover image from video.dmm.co.jp
func (s *Scraper) extractCoverURLNewSite(doc *goquery.Document, contentID string) string {
	// Try og:image meta tag
	coverURL, exists := doc.Find(`meta[property="og:image"]`).Attr("content")
	logging.Debugf("DMM Streaming: og:image exists=%v, value=%s", exists, coverURL)
	if exists && coverURL != "" {
		// Convert to regular pics.dmm.co.jp URL if needed
		coverURL = strings.Replace(coverURL, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)
		// Replace 'ps.jpg' with 'pl.jpg' for larger image
		coverURL = strings.Replace(coverURL, "ps.jpg", "pl.jpg", 1)
		// Remove query parameters
		if idx := strings.Index(coverURL, "?"); idx != -1 {
			coverURL = coverURL[:idx]
		}
		logging.Debugf("DMM Streaming: Final cover URL from og:image: %s", coverURL)
		return coverURL
	}

	// As fallback, try to extract from img tags
	logging.Debug("DMM Streaming: og:image not found, trying img tag fallback")
	coverURL, _ = doc.Find(`img[src*="pl.jpg"]`).First().Attr("src")
	logging.Debugf("DMM Streaming: img[src*='pl.jpg'] found: %s", coverURL)
	if coverURL != "" {
		// Convert to regular pics.dmm.co.jp URL and remove query parameters
		coverURL = strings.Replace(coverURL, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)
		if idx := strings.Index(coverURL, "?"); idx != -1 {
			coverURL = coverURL[:idx]
		}
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
	// Amateur videos use pattern: https://pics.dmm.co.jp/digital/amateur/{contentid}/{contentid}pl.jpg
	// DMM serves cover assets on lowercase paths, so normalize to lowercase
	if contentID != "" {
		// Normalize to lowercase to match DMM's URL structure
		normalizedID := strings.ToLower(contentID)
		// Try amateur video pattern
		coverURL = "https://pics.dmm.co.jp/digital/amateur/" + normalizedID + "/" + normalizedID + "pl.jpg"
		logging.Debugf("DMM Streaming: Constructed amateur cover URL from content ID '%s': %s", contentID, coverURL)
		return coverURL
	}

	logging.Debug("DMM Streaming: No cover URL found")
	return ""
}

// extractScreenshotsNewSite extracts screenshots from video.dmm.co.jp
func (s *Scraper) extractScreenshotsNewSite(doc *goquery.Document) []string {
	screenshots := make([]string, 0)
	seen := make(map[string]bool)

	// video.dmm.co.jp uses awsimgsrc.dmm.co.jp with numbered screenshots
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

	return screenshots
}

// extractSeriesNewSite extracts series from video.dmm.co.jp
func (s *Scraper) extractSeriesNewSite(doc *goquery.Document) string {
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
	return cleanString(series)
}

// extractMakerNewSite extracts maker from video.dmm.co.jp
func (s *Scraper) extractMakerNewSite(doc *goquery.Document) string {
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
	return cleanString(maker)
}

// extractRatingNewSite extracts rating from video.dmm.co.jp JSON-LD data
func (s *Scraper) extractRatingNewSite(doc *goquery.Document) (float64, int) {
	var rating float64
	var votes int

	// Extract from JSON-LD structured data
	doc.Find(`script[type="application/ld+json"]`).Each(func(i int, sel *goquery.Selection) {
		jsonText := sel.Text()

		// Look for aggregateRating
		if strings.Contains(jsonText, `"aggregateRating"`) {
			// Extract ratingValue
			if idx := strings.Index(jsonText, `"ratingValue":`); idx != -1 {
				start := idx + len(`"ratingValue":`)
				remaining := jsonText[start:]
				var ratingStr strings.Builder
				for _, ch := range remaining {
					if ch == ',' || ch == '}' {
						break
					}
					if ch != ' ' {
						ratingStr.WriteRune(ch)
					}
				}
				ratingVal := strings.TrimSpace(ratingStr.String())
				if parsedRating, err := strconv.ParseFloat(ratingVal, 64); err == nil {
					rating = parsedRating * 2 // Convert 5-point scale to 10-point scale
				}
			}

			// Extract ratingCount
			if idx := strings.Index(jsonText, `"ratingCount":`); idx != -1 {
				start := idx + len(`"ratingCount":`)
				remaining := jsonText[start:]
				var votesStr strings.Builder
				for _, ch := range remaining {
					if ch == ',' || ch == '}' {
						break
					}
					if ch != ' ' {
						votesStr.WriteRune(ch)
					}
				}
				votesVal := strings.TrimSpace(votesStr.String())
				if parsedVotes, err := strconv.Atoi(votesVal); err == nil {
					votes = parsedVotes
				}
			}
		}
	})

	return rating, votes
}
