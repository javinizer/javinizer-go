package r18dev

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// atoiSafe parses an integer string, returning 0 on failure (matching the
// DMMID=0 "not set" convention in models.ActressInfo).
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// resultFromDump builds a complete ScraperResult from a locally-cached r18.dev
// dump movie record. This is the zero-HTTP path: no r18.dev API call is made
// at all. Image URLs from the dump (relative DMM CDN paths) are resolved to
// absolute URLs and normalized using the same pure helpers as the HTTP path,
// but all HTTP-based probing (poster cropping detection, screenshot discovery,
// placeholder filtering) is skipped — the dump's stored URLs are used directly.
func (s *scraper) resultFromDump(d *models.DumpMovie) *models.ScraperResult {
	movieID := d.DVDID
	if movieID == "" && d.ContentID != "" {
		movieID = contentIDToID(d.ContentID)
	}

	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: baseURL + "/videos/vod/movies/detail/-/combined=" + d.ContentID + "/json",
		Language:  s.language,
		ID:        movieID,
		ContentID: d.ContentID,
		Runtime:   d.Runtime,
	}
	// SourceURL is the canonical r18.dev combined= URL for this content_id.
	// It is NOT fetched — this result is built entirely from the local dump —
	// but downstream code uses it as a stable canonical identifier.

	s.buildDumpTranslations(d, result)
	s.resolveDumpLocalizedStrings(d, result)
	s.resolveDumpReleaseDate(d, result)
	s.resolveDumpActresses(d, result)
	s.resolveDumpGenres(d, result)
	s.resolveDumpMediaURLs(d, result)

	return result
}

func (s *scraper) buildDumpTranslations(d *models.DumpMovie, result *models.ScraperResult) {
	translations := make([]models.MovieTranslation, 0, 2)

	directorEn, directorJa := "", ""
	if d.Director != nil {
		directorEn = scraperutil.CleanString(getPreferredString(d.Director.NameRomaji, d.Director.NameKanji))
		directorJa = scraperutil.CleanString(getPreferredString(d.Director.NameKanji, d.Director.NameRomaji))
	}

	makerEn, makerJa := "", ""
	if d.Maker != nil {
		makerEn = scraperutil.CleanString(d.Maker.NameEn)
		makerJa = scraperutil.CleanString(d.Maker.NameJa)
	}

	labelEn, labelJa := "", ""
	if d.Label != nil {
		labelEn = scraperutil.CleanString(d.Label.NameEn)
		labelJa = scraperutil.CleanString(d.Label.NameJa)
	}

	seriesEn, seriesJa := "", ""
	if d.Series != nil {
		seriesEn = scraperutil.CleanString(d.Series.NameEn)
		seriesJa = scraperutil.CleanString(d.Series.NameJa)
	}

	if d.TitleEn != "" || makerEn != "" || labelEn != "" || seriesEn != "" || d.CommentEn != "" {
		translations = append(translations, models.MovieTranslation{
			Language:      "en",
			Title:         scraperutil.CleanString(d.TitleEn),
			OriginalTitle: scraperutil.CleanString(d.TitleJa),
			Description:   scraperutil.CleanString(d.CommentEn),
			Director:      directorEn,
			Maker:         makerEn,
			Label:         labelEn,
			Series:        seriesEn,
			SourceName:    s.Name(),
		})
	}

	if d.TitleJa != "" || makerJa != "" || labelJa != "" || seriesJa != "" {
		translations = append(translations, models.MovieTranslation{
			Language:      "ja",
			Title:         scraperutil.CleanString(d.TitleJa),
			OriginalTitle: scraperutil.CleanString(d.TitleJa),
			Description:   scraperutil.CleanString(d.CommentJa),
			Director:      directorJa,
			Maker:         makerJa,
			Label:         labelJa,
			Series:        seriesJa,
			SourceName:    s.Name(),
		})
	}

	result.Translations = translations
}

func (s *scraper) resolveDumpLocalizedStrings(d *models.DumpMovie, result *models.ScraperResult) {
	result.Title = scraperutil.CleanString(selectLocalizedString(s.language, d.TitleEn, d.TitleJa))
	result.OriginalTitle = scraperutil.CleanString(d.TitleJa)
	result.Description = scraperutil.CleanString(selectLocalizedString(s.language, d.CommentEn, d.CommentJa))

	if d.Director != nil {
		if s.language == "ja" {
			result.Director = scraperutil.CleanString(getPreferredString(d.Director.NameKanji, d.Director.NameRomaji))
		} else {
			result.Director = scraperutil.CleanString(getPreferredString(d.Director.NameRomaji, d.Director.NameKanji))
		}
	}

	if d.Maker != nil {
		result.Maker = scraperutil.CleanString(selectLocalizedString(s.language, d.Maker.NameEn, d.Maker.NameJa))
	}
	if d.Label != nil {
		result.Label = scraperutil.CleanString(selectLocalizedString(s.language, d.Label.NameEn, d.Label.NameJa))
	}
	if d.Series != nil {
		if s.language == "ja" {
			result.Series = scraperutil.CleanString(getPreferredString(d.Series.NameJa, d.Series.NameEn))
		} else {
			result.Series = scraperutil.CleanString(getPreferredString(d.Series.NameEn, d.Series.NameJa))
		}
	}
}

func (s *scraper) resolveDumpReleaseDate(d *models.DumpMovie, result *models.ScraperResult) {
	if d.ReleaseDate == "" {
		return
	}
	t, err := time.Parse("2006-01-02", d.ReleaseDate)
	if err == nil {
		result.ReleaseDate = &t
	}
}

func (s *scraper) resolveDumpActresses(d *models.DumpMovie, result *models.ScraperResult) {
	result.Actresses = make([]models.ActressInfo, 0, len(d.Actresses))
	for _, a := range d.Actresses {
		thumbURL := a.ImageURL
		if thumbURL != "" && !strings.HasPrefix(thumbURL, "http") {
			thumbURL = "https://pics.dmm.co.jp/mono/actjpgs/" + thumbURL
		}
		if thumbURL == "" && a.NameRomaji != "" {
			parts := strings.Fields(a.NameRomaji)
			var filename string
			if len(parts) >= 2 {
				filename = strings.ToLower(parts[1]) + "_" + strings.ToLower(parts[0])
			} else if len(parts) == 1 {
				filename = strings.ToLower(parts[0])
			}
			filename = specialCharsRegex.ReplaceAllString(filename, "")
			if filename != "" {
				thumbURL = "https://pics.dmm.co.jp/mono/actjpgs/" + filename + ".jpg"
			}
		}

		firstName, lastName := "", ""
		if a.NameRomaji != "" {
			parts := strings.Fields(a.NameRomaji)
			if len(parts) > 0 {
				firstName = parts[0]
			}
			if len(parts) > 1 {
				lastName = parts[1]
			}
		}

		result.Actresses = append(result.Actresses, models.ActressInfo{
			DMMID:        atoiSafe(a.ID),
			FirstName:    firstName,
			LastName:     lastName,
			JapaneseName: scraperutil.CleanString(a.NameKanji),
			ThumbURL:     thumbURL,
		})
	}
}

func (s *scraper) resolveDumpGenres(d *models.DumpMovie, result *models.ScraperResult) {
	result.Genres = make([]string, 0, len(d.Categories))
	for _, c := range d.Categories {
		var name string
		if s.language == "ja" {
			name = scraperutil.CleanString(getPreferredString(c.NameJa, c.NameEn))
		} else {
			name = scraperutil.CleanString(getPreferredString(c.NameEn, c.NameJa))
		}
		if name != "" {
			result.Genres = append(result.Genres, name)
		}
	}
}

func (s *scraper) resolveDumpMediaURLs(d *models.DumpMovie, result *models.ScraperResult) {
	// Cover image: the dump's jacket_full_url is the "pl" (large) variant.
	coverURL := r18devdump.NormalizeDumpURL(d.JacketFullURL)
	if coverURL != "" {
		coverURL = imageutil.NormalizeDMMScreenshotURL(coverURL)
		coverURL = imageutil.UpgradeCoverResolution(coverURL)
		coverURL = imageutil.UpgradeDMMCoverCDN(coverURL)
		result.CoverURL = coverURL
	}

	// Poster: the dump's jacket_thumb_url is the "ps" (small/poster) variant.
	// Resolved independently of the cover so a row with only a thumb still
	// gets a poster. Falls back to the cover URL when no thumb is available.
	//
	// The online path (r18dev.go) probes the awsimgsrc poster dimensions via
	// GetOptimalPosterURL and sets ShouldCropPoster accordingly; the dump path
	// is zero-HTTP so it can't probe. The ps.jpg thumb is conventionally a
	// portrait poster, so shouldCrop stays false when a thumb is present.
	// When the thumb is missing and we fall back to the landscape cover,
	// ShouldCropPoster must be true so the frontend right-crops the cover
	// into a portrait (otherwise it letterboxes the full cover — see the
	// matching logic in r18dev.go's shouldCrop branch).
	posterURL := r18devdump.NormalizeDumpURL(d.JacketThumbURL)
	if posterURL != "" {
		posterURL = imageutil.NormalizeDMMScreenshotURL(posterURL)
		result.PosterURL = posterURL
	} else if coverURL != "" {
		result.PosterURL = coverURL
		result.ShouldCropPoster = true
	}

	// Screenshots: expand the dump's gallery range into individual URLs.
	for _, rel := range r18devdump.ExpandGallery(d.GalleryFirst, d.GalleryLast) {
		if u := r18devdump.NormalizeDumpURL(rel); u != "" {
			result.ScreenshotURL = append(result.ScreenshotURL, imageutil.NormalizeDMMScreenshotURL(u))
		}
	}

	// Trailer: prefer the trailers table, fall back to the video's sample_url.
	if d.TrailerURL != "" {
		result.TrailerURL = d.TrailerURL
	} else if d.SampleURL != "" {
		result.TrailerURL = d.SampleURL
	}
}

// searchFromDump is the zero-HTTP dump fast path for Search. It returns a
// complete ScraperResult when the dump has the movie, or (nil, false) on a
// miss or error so the caller falls back to live HTTP. A genuine miss
// (models.ErrDumpMiss) is logged at debug; a real database error is logged at
// warn so a degraded dump does not silently revert to rate-limit-prone HTTP
// with no signal.
func (s *scraper) searchFromDump(ctx context.Context, id string) (*models.ScraperResult, bool) {
	if s.dumpLookup == nil {
		return nil, false
	}
	movie, err := s.dumpLookup.LookupMovie(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrDumpMiss):
			logging.Debugf("R18: dump lookup miss for %s, falling back to HTTP", id)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// Benign user-initiated cancellation — not a degraded dump. Log at
			// debug so a cancel doesn't look like a dump failure.
			logging.Debugf("R18: dump lookup cancelled for %s: %v", id, err)
		default:
			logging.Warnf("R18: dump lookup error for %s, falling back to HTTP: %v", id, err)
		}
		return nil, false
	}
	logging.Debugf("R18: dump lookup resolved %s -> full metadata (zero HTTP)", id)
	return s.resultFromDump(movie), true
}
