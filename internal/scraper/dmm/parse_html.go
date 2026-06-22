package dmm

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

var (
	dateRegex      = regexp.MustCompile(`\d{4}/\d{2}/\d{2}`)
	runtimeRegex   = regexp.MustCompile(`(\d{2,3})\s?(?:minutes|分)`)
	directorRegex  = regexp.MustCompile(`<a.*?href="[^"]*\?director=(\d+)".*?>([^<]+)</a>`)
	seriesRegex    = regexp.MustCompile(`<a href="(?:/digital/videoa/|(?:/en)?/mono/dvd/)-/list/=/article=series/id=\d*/"[^>]*?>(.*)</a></td>`)
	ratingRegex    = regexp.MustCompile(`<strong>(.*)\s?(?:points|点)</strong>`)
	votesRegex     = regexp.MustCompile(`<p class="d-review__evaluates">.*?<strong>(\d+)</strong>`)
	genreNameRegex = regexp.MustCompile(`>([^<]+)</a>`)
)

func (s *scraper) parseHTML(ctx context.Context, doc *goquery.Document, sourceURL string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  "ja",
	}

	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")
	var jsonldMetadata map[string]any
	if isNewSite {
		jsonldMetadata = extractMetadataFromJSONLD(doc)
	}

	// 4-step pipeline
	s.extractIdentifiers(result, sourceURL)
	s.extractTextualMetadata(result, doc, isNewSite, jsonldMetadata)
	s.extractStructuredData(ctx, result, doc, sourceURL, isNewSite, jsonldMetadata)
	s.extractMediaFields(ctx, result, doc, sourceURL, isNewSite, jsonldMetadata)

	return result, nil
}

// extractIdentifiers populates ContentID and ID from the source URL.
func (s *scraper) extractIdentifiers(result *models.ScraperResult, sourceURL string) {
	if cid := extractContentIDFromURL(sourceURL); cid != "" {
		// Always strip rental 'r' suffix from content IDs regardless of URL path.
		// DMM uses 'r' suffix for rental content IDs across all URL types, not just /rental/ pages.
		cid = stripRentalSuffix(cid)
		if cleaned := cleanPrefixRegex.ReplaceAllString(strings.ToLower(cid), "$1"); cleaned != "" {
			cid = cleaned
		}
		result.ContentID = cid
		result.ID = normalizeID(cid)
	}
}

// extractTextualMetadata populates Title, Description, Director, Maker, Label, Series.
func (s *scraper) extractTextualMetadata(result *models.ScraperResult, doc *goquery.Document, isNewSite bool, jsonldMetadata map[string]any) {
	// Title
	var japaneseTitle string
	if isNewSite {
		if title := getStringFromMetadata(jsonldMetadata, "title"); title != "" {
			japaneseTitle = title
		} else {
			japaneseTitle = scraperutil.CleanString(doc.Find("h1").First().Text())
			if japaneseTitle == "" {
				ogTitle, _ := doc.Find(`meta[property="og:title"]`).Attr("content")
				japaneseTitle = scraperutil.CleanString(ogTitle)
			}
		}
	} else {
		japaneseTitle = scraperutil.CleanString(doc.Find("h1#title.item").Text())
	}
	result.Title = japaneseTitle
	result.OriginalTitle = japaneseTitle

	// Description
	if isNewSite {
		if desc := getStringFromMetadata(jsonldMetadata, "description"); desc != "" {
			result.Description = desc
		} else {
			result.Description = s.extractDescription(doc, isNewSite)
		}
	} else {
		result.Description = s.extractDescription(doc, isNewSite)
	}

	// Director
	result.Director = s.extractDirector(doc)

	// Maker
	if isNewSite {
		if maker := getStringFromMetadata(jsonldMetadata, "maker"); maker != "" {
			result.Maker = maker
		} else {
			result.Maker = s.extractMaker(doc, isNewSite)
		}
	} else {
		result.Maker = s.extractMaker(doc, isNewSite)
	}

	// Label
	result.Label = s.extractLabel(doc)

	// Series
	result.Series = s.extractSeries(doc, isNewSite)
}

// extractStructuredData populates ReleaseDate, Runtime, Rating, Genres, Actresses.
func (s *scraper) extractStructuredData(ctx context.Context, result *models.ScraperResult, doc *goquery.Document, sourceURL string, isNewSite bool, jsonldMetadata map[string]any) {
	// ReleaseDate
	if isNewSite {
		if date := getTimeFromMetadata(jsonldMetadata, "release_date"); date != nil {
			result.ReleaseDate = date
		} else if date := s.extractReleaseDate(doc); date != nil {
			result.ReleaseDate = date
		}
	} else {
		if date := s.extractReleaseDate(doc); date != nil {
			result.ReleaseDate = date
		}
	}

	// Runtime
	result.Runtime = s.extractRuntime(doc)

	// Rating
	if isNewSite {
		ratingValue := getFloat64FromMetadata(jsonldMetadata, "rating_value")
		ratingCount := getIntFromMetadata(jsonldMetadata, "rating_count")
		if ratingValue > 0 || ratingCount > 0 {
			result.Rating = &models.Rating{
				Score: ratingValue,
				Votes: ratingCount,
			}
		} else {
			result.Rating = s.extractRating(doc, isNewSite)
		}
	} else {
		result.Rating = s.extractRating(doc, isNewSite)
	}

	// Genres
	if isNewSite {
		if genres := getStringSliceFromMetadata(jsonldMetadata, "genres"); len(genres) > 0 {
			result.Genres = genres
		} else {
			result.Genres = s.extractGenres(doc)
		}
	} else {
		result.Genres = s.extractGenres(doc)
	}

	// Actresses
	isMonthlyPage := strings.Contains(sourceURL, "/monthly/")
	isStreamingPage := strings.Contains(sourceURL, "video.dmm.co.jp")

	if s.scrapeActress && !isMonthlyPage {
		if isStreamingPage {
			result.Actresses = s.extractActressesFromStreamingPage(ctx, doc)
			logging.Debugf("DMM: Extracted %d actresses from streaming page", len(result.Actresses))
		} else {
			result.Actresses = s.extractActresses(ctx, doc)
			logging.Debugf("DMM: Extracted %d actresses", len(result.Actresses))
		}
	} else if isMonthlyPage {
		logging.Debug("DMM: Skipping actress extraction (monthly page - no actress data)")
	} else {
		logging.Debug("DMM: Skipping actress extraction (scrape_actress=false)")
	}
}

// extractMediaFields populates CoverURL, PosterURL, ScreenshotURL, TrailerURL.
func (s *scraper) extractMediaFields(ctx context.Context, result *models.ScraperResult, doc *goquery.Document, sourceURL string, isNewSite bool, jsonldMetadata map[string]any) {
	// CoverURL
	if isNewSite {
		if coverURL := getStringFromMetadata(jsonldMetadata, "cover_url"); coverURL != "" {
			result.CoverURL = coverURL
		} else {
			result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
		}
	} else {
		result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
	}

	// PosterURL
	if result.CoverURL != "" {
		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(result.CoverURL, s.client.GetClient())
		result.ShouldCropPoster = shouldCrop
		if shouldCrop {
			result.PosterURL = result.CoverURL
		} else {
			result.PosterURL = posterURL
		}
	}

	// Screenshots
	var screenshots []string
	if isNewSite {
		if ss := getStringSliceFromMetadata(jsonldMetadata, "screenshots"); len(ss) > 0 {
			screenshots = ss
		} else {
			screenshots = s.extractScreenshots(doc, isNewSite)
		}
	} else {
		screenshots = s.extractScreenshots(doc, isNewSite)
	}
	result.ScreenshotURL = s.filterPlaceholderScreenshots(ctx, screenshots)

	// TrailerURL
	if isNewSite {
		if trailerURL := getStringFromMetadata(jsonldMetadata, "trailer_url"); trailerURL != "" {
			result.TrailerURL = trailerURL
		} else {
			result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
		}
	} else {
		result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
	}
}

func (s *scraper) extractDescription(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractDescriptionNewSite(doc)
	}

	desc := doc.Find("div.mg-b20.lh4 p.mg-b20").Text()
	if desc == "" {
		desc = doc.Find("div.mg-b20.lh4").Text()
	}
	return scraperutil.CleanString(desc)
}

func (s *scraper) extractReleaseDate(doc *goquery.Document) *time.Time {
	dateStr := dateRegex.FindString(doc.Text())

	if dateStr != "" {
		t, err := time.Parse("2006/01/02", dateStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

func (s *scraper) extractRuntime(doc *goquery.Document) int {
	matches := runtimeRegex.FindStringSubmatch(doc.Text())

	if len(matches) > 1 {
		runtime, _ := strconv.Atoi(matches[1])
		return runtime
	}
	return 0
}

func (s *scraper) extractDirector(doc *goquery.Document) string {
	html, _ := doc.Html()
	matches := directorRegex.FindStringSubmatch(html)

	if len(matches) > 2 {
		return scraperutil.CleanString(matches[2])
	}
	return ""
}

func (s *scraper) extractMaker(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractMakerNewSite(doc)
	}

	var maker string
	doc.Find("a[href*='?maker='], a[href*='/article=maker/id=']").Each(func(i int, sel *goquery.Selection) {
		if maker == "" {
			maker = scraperutil.CleanString(sel.Text())
		}
	})
	return maker
}

func (s *scraper) extractLabel(doc *goquery.Document) string {
	var label string
	doc.Find("a[href*='?label='], a[href*='/article=label/id=']").Each(func(i int, sel *goquery.Selection) {
		if label == "" {
			label = scraperutil.CleanString(sel.Text())
		}
	})
	return label
}

func (s *scraper) extractSeries(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractSeriesNewSite(doc)
	}

	html, _ := doc.Html()
	matches := seriesRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		return scraperutil.CleanString(matches[1])
	}
	return ""
}

func (s *scraper) extractRating(doc *goquery.Document, isNewSite bool) *models.Rating {
	if isNewSite {
		rating, votes := s.extractRatingNewSite(doc)
		if rating > 0 || votes > 0 {
			return &models.Rating{
				Score: rating,
				Votes: votes,
			}
		}
		return nil
	}

	html, _ := doc.Html()
	matches := ratingRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		ratingStr := matches[1]

		ratingMap := map[string]float64{
			"One":   1.0,
			"Two":   2.0,
			"Three": 3.0,
			"Four":  4.0,
			"Five":  5.0,
		}

		rating := 0.0
		if val, exists := ratingMap[ratingStr]; exists {
			rating = val
		} else {
			rating, _ = strconv.ParseFloat(ratingStr, 64)
		}

		rating = rating * 2

		votesMatches := votesRegex.FindStringSubmatch(html)
		votes := 0
		if len(votesMatches) > 1 {
			votes, _ = strconv.Atoi(votesMatches[1])
		}

		if rating > 0 || votes > 0 {
			return &models.Rating{
				Score: rating,
				Votes: votes,
			}
		}
	}
	return nil
}

func (s *scraper) extractGenres(doc *goquery.Document) []string {
	genres := make([]string, 0)
	html, _ := doc.Html()

	if strings.Contains(html, "Genre:") || strings.Contains(html, "ジャンル：") {
		genreSection := ""
		parts := strings.Split(html, "Genre:")
		if len(parts) < 2 {
			parts = strings.Split(html, "ジャンル：")
		}

		if len(parts) >= 2 {
			endParts := strings.Split(parts[1], "</tr>")
			if len(endParts) > 0 {
				genreSection = endParts[0]
			}
		}

		matches := genreNameRegex.FindAllStringSubmatch(genreSection, -1)

		for _, match := range matches {
			if len(match) > 1 {
				genre := scraperutil.CleanString(match[1])
				if genre != "" {
					genres = append(genres, genre)
				}
			}
		}
	}

	return genres
}
