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

func (s *Scraper) parseHTML(ctx context.Context, doc *goquery.Document, sourceURL string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  "ja",
	}

	if cid := extractContentIDFromURL(sourceURL); cid != "" {
		if strings.Contains(sourceURL, "/rental/") {
			cid = stripRentalSuffix(cid)
		}
		if cleaned := cleanPrefixRegex.ReplaceAllString(strings.ToLower(cid), "$1"); cleaned != "" {
			cid = cleaned
		}
		result.ContentID = cid
		result.ID = normalizeID(cid)
	}

	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")

	var jsonldMetadata map[string]interface{}
	if isNewSite {
		jsonldMetadata = extractMetadataFromJSONLD(doc)
	}

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

	if isNewSite {
		if desc := getStringFromMetadata(jsonldMetadata, "description"); desc != "" {
			result.Description = desc
		} else {
			result.Description = s.extractDescription(doc, isNewSite)
		}
	} else {
		result.Description = s.extractDescription(doc, isNewSite)
	}

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

	result.Runtime = s.extractRuntime(doc)

	result.Director = s.extractDirector(doc)

	if isNewSite {
		if maker := getStringFromMetadata(jsonldMetadata, "maker"); maker != "" {
			result.Maker = maker
		} else {
			result.Maker = s.extractMaker(doc, isNewSite)
		}
	} else {
		result.Maker = s.extractMaker(doc, isNewSite)
	}

	result.Label = s.extractLabel(doc)

	result.Series = s.extractSeries(doc, isNewSite)

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

	if isNewSite {
		if genres := getStringSliceFromMetadata(jsonldMetadata, "genres"); len(genres) > 0 {
			result.Genres = genres
		} else {
			result.Genres = s.extractGenres(doc)
		}
	} else {
		result.Genres = s.extractGenres(doc)
	}

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

	if isNewSite {
		if coverURL := getStringFromMetadata(jsonldMetadata, "cover_url"); coverURL != "" {
			result.CoverURL = coverURL
		} else {
			result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
		}
	} else {
		result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
	}

	if result.CoverURL != "" {
		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(result.CoverURL, s.client.GetClient())
		result.ShouldCropPoster = shouldCrop
		if shouldCrop {
			result.PosterURL = result.CoverURL
		} else {
			result.PosterURL = posterURL
		}
	}

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

	if isNewSite {
		if trailerURL := getStringFromMetadata(jsonldMetadata, "trailer_url"); trailerURL != "" {
			result.TrailerURL = trailerURL
		} else {
			result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
		}
	} else {
		result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
	}

	return result, nil
}

func (s *Scraper) extractDescription(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractDescriptionNewSite(doc)
	}

	desc := doc.Find("div.mg-b20.lh4 p.mg-b20").Text()
	if desc == "" {
		desc = doc.Find("div.mg-b20.lh4").Text()
	}
	return scraperutil.CleanString(desc)
}

func (s *Scraper) extractReleaseDate(doc *goquery.Document) *time.Time {
	dateStr := dateRegex.FindString(doc.Text())

	if dateStr != "" {
		t, err := time.Parse("2006/01/02", dateStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

func (s *Scraper) extractRuntime(doc *goquery.Document) int {
	matches := runtimeRegex.FindStringSubmatch(doc.Text())

	if len(matches) > 1 {
		runtime, _ := strconv.Atoi(matches[1])
		return runtime
	}
	return 0
}

func (s *Scraper) extractDirector(doc *goquery.Document) string {
	html, _ := doc.Html()
	matches := directorRegex.FindStringSubmatch(html)

	if len(matches) > 2 {
		return scraperutil.CleanString(matches[2])
	}
	return ""
}

func (s *Scraper) extractMaker(doc *goquery.Document, isNewSite bool) string {
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

func (s *Scraper) extractLabel(doc *goquery.Document) string {
	var label string
	doc.Find("a[href*='?label='], a[href*='/article=label/id=']").Each(func(i int, sel *goquery.Selection) {
		if label == "" {
			label = scraperutil.CleanString(sel.Text())
		}
	})
	return label
}

func (s *Scraper) extractSeries(doc *goquery.Document, isNewSite bool) string {
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

func (s *Scraper) extractRating(doc *goquery.Document, isNewSite bool) *models.Rating {
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

func (s *Scraper) extractGenres(doc *goquery.Document) []string {
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
