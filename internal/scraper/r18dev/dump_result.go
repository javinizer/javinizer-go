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

	// Poster: the dump's jacket_thumb_url is a low-res "ps" thumbnail on
	// pics.dmm.co.jp. The HTTP path probes the awsimgsrc poster dimensions and
	// falls back to cropping the high-res cover when the poster is too small or
	// missing (common for mono/movie titles — e.g. ABF-030's awsimgsrc ps.jpg
	// is a 147×200 placeholder). The dump path is zero-HTTP so it can't probe;
	// using the raw pics.dmm.co.jp thumb gives a 13KB low-quality poster.
	//
	// Instead, prefer the high-res cover (already upgraded to awsimgsrc CDN)
	// with ShouldCropPoster=true, matching the HTTP path's fallback. Only use
	// the thumb directly when no cover is available (a rare row with only a
	// thumbnail), and in that case upgrade it to awsimgsrc CDN too.
	if coverURL != "" {
		result.PosterURL = coverURL
		result.ShouldCropPoster = true
	} else if posterURL := r18devdump.NormalizeDumpURL(d.JacketThumbURL); posterURL != "" {
		posterURL = imageutil.NormalizeDMMScreenshotURL(posterURL)
		posterURL = imageutil.UpgradeDMMCoverCDN(posterURL)
		result.PosterURL = posterURL
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

// searchFromDump is the dump fast path for Search. On a dvd_id_norm hit it
// returns a complete ScraperResult with zero HTTP. On a miss it expands the ID
// into ordered content_id candidates (MatchByDisplayID) so Search can fetch
// each combined= URL with content_id validation and per-candidate fallthrough
// instead of running the multi-probe HTTP resolver — one request on the happy
// path. The
// candidate-hit dump row is never used as the metadata source — rows reachable
// only via candidates have no dvd_id upstream and typically no title_en.
//
// A genuine miss from either lookup (models.ErrDumpMiss, which includes
// ErrDumpNoDVDID) is logged at debug; a real database error is logged at warn
// so a degraded dump does not silently revert to rate-limit-prone HTTP with
// no signal. Context cancellation is logged at debug and skips candidate
// expansion entirely so a cancelled scrape stops immediately.
//
// Returns (result, candidates): a nil result with non-empty candidates means
// the dump resolved candidate content_ids (in HTTP-resolver variation order,
// so the dump never resolves a different product than HTTP would); Search
// fetches their combined= URLs in order with content_id validation. Both nil
// means the caller must fall back to the live HTTP URL resolver.
func (s *scraper) searchFromDump(ctx context.Context, id string) (*models.ScraperResult, []models.DumpMatch) {
	if s.dumpLookup == nil {
		return nil, nil
	}
	movie, err := s.dumpLookup.LookupMovie(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrDumpMiss):
			// fall through to candidate resolution
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			logging.Debugf("R18: dump lookup cancelled for %s: %v", id, err)
			return nil, nil
		default:
			logging.Warnf("R18: dump lookup error for %s, falling back to HTTP: %v", id, err)
			return nil, nil
		}

		matches, mErr := s.dumpLookup.MatchByDisplayID(ctx, id)
		if mErr != nil || len(matches) == 0 {
			switch {
			case mErr == nil || errors.Is(mErr, models.ErrDumpMiss):
				logging.Debugf("R18: dump lookup miss for %s, falling back to HTTP", id)
			case errors.Is(mErr, context.Canceled), errors.Is(mErr, context.DeadlineExceeded):
				logging.Debugf("R18: dump candidate lookup cancelled for %s: %v", id, mErr)
			default:
				logging.Warnf("R18: dump candidate lookup error for %s, falling back to HTTP: %v", id, mErr)
			}
			return nil, nil
		}
		candidates := make([]models.DumpMatch, 0, len(matches))
		for _, m := range matches {
			if m.ContentID == "" {
				continue
			}
			candidates = append(candidates, m)
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		// Parity gate: trust dump candidates only when they form a contiguous
		// prefix of the full canonical expansion. A partial or stale dump must
		// not short-circuit the HTTP resolver: a lone noncanonical row (e.g.
		// only 436abf00030 for ABF-030) would resolve a product the canonical
		// prefix order never picks, and a gappy list ([c0, c2]) would skip the
		// intermediate candidate the resolver tries over the wire first.
		all := r18devdump.ContentIDCandidates(id)
		trusted := len(all) >= len(candidates)
		if trusted {
			for i, c := range candidates {
				if c.ContentID != all[i] {
					trusted = false
					break
				}
			}
		}
		if !trusted {
			logging.Debugf("R18: dump candidates for %s are not a canonical prefix, falling back to HTTP", id)
			return nil, nil
		}
		logging.Debugf("R18: dump candidates for %s -> %s (+%d more)", id, candidates[0].ContentID, len(candidates)-1)
		return nil, candidates
	}
	logging.Debugf("R18: dump lookup resolved %s -> full metadata (zero HTTP)", id)
	return s.resultFromDump(movie), nil
}
