package worker

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// fieldOverrideKeys is the canonical set of field-source keys a user may
// re-pick from another scraper's raw results. It mirrors the keys emitted by
// the aggregator (stringFieldSpecs + the dedicated assign* methods) and
// buildFieldSourcesFromCachedMovie, so the override speaks the same language
// as the existing "via {source}" provenance tooltips.
var supportedFieldOverrideKeys = []string{
	"id", "content_id", "title", "display_title", "original_title",
	"description", "director", "maker", "label", "series", "runtime",
	"release_date", "rating_score", "rating_votes", "actresses", "genres",
	"screenshot_urls", "poster_url", "cover_url", "trailer_url",
	"should_crop_poster",
}

var fieldOverrideKeys = func() map[string]struct{} {
	m := make(map[string]struct{}, len(supportedFieldOverrideKeys))
	for _, k := range supportedFieldOverrideKeys {
		m[k] = struct{}{}
	}
	return m
}()

// SupportedFieldOverrideKeys returns the field-source keys a user may override
// via the review-page source viewer, in a stable order for UI rendering.
func SupportedFieldOverrideKeys() []string {
	return append([]string(nil), supportedFieldOverrideKeys...)
}

// applyFieldOverride overwrites a single field on movie with the value from the
// named source's raw ScraperResult, and updates the provenance maps so the
// review UI's "via {source}" tooltip reflects the new attribution. Mirrors the
// original PowerShell Javinizer "Replace" button (javinizergui.ps1:2538):
//
//	$cache:findData[$cache:index].Data.($prop.Name) = $prop.Value
//	$cache:findData[$cache:index].Selected.($prop.Name) = $source
//
// This is a raw assignment from the chosen source — it does not re-run genre
// replacement, actress alias resolution, or word processing. That matches the
// original's semantics (the user explicitly cherry-picked this source's value)
// and avoids re-instantiating the full Aggregator in the review path. movie and
// prov are mutated in place; the caller is expected to persist both.
func applyFieldOverride(movie *models.Movie, prov *ProvenanceData, fieldKey, source string) error {
	if movie == nil {
		return fmt.Errorf("cannot override field on nil movie")
	}
	if prov == nil {
		return fmt.Errorf("no provenance available for field override")
	}
	if _, ok := fieldOverrideKeys[fieldKey]; !ok {
		return fmt.Errorf("unsupported field: %s", fieldKey)
	}
	result := findScraperResult(prov.ScraperResults, source)
	if result == nil {
		// Legacy/cache-hit movies may carry no persisted raw ScraperResults,
		// but getBatchMovieSources synthesizes a single-source envelope from
		// the cached movie for display. Mirror that fallback here so the
		// displayed source remains selectable.
		if synth := scrape.ScraperResultFromCachedMovie(movie); synth != nil {
			result = findScraperResult([]*models.ScraperResult{synth}, source)
		}
	}
	if result == nil {
		return fmt.Errorf("source %q did not contribute to this movie", source)
	}

	setFieldSource := func(key string) {
		if prov.FieldSources == nil {
			prov.FieldSources = make(map[string]string)
		}
		prov.FieldSources[key] = source
	}

	switch fieldKey {
	case "id":
		movie.ID = result.ID
		setFieldSource("id")
	case "content_id":
		movie.ContentID = result.ContentID
		setFieldSource("content_id")
	case "title", "display_title":
		// Title and DisplayTitle are linked: the aggregator attributes both to
		// the same source, and the workflow derives DisplayTitle from Title.
		// Keep them in sync so the review Title input (bound to display_title)
		// and the persisted NFO <title> stay consistent.
		movie.Title = result.Title
		movie.DisplayTitle = result.Title
		setFieldSource("title")
		setFieldSource("display_title")
	case "original_title":
		movie.OriginalTitle = result.OriginalTitle
		setFieldSource("original_title")
	case "description":
		movie.Description = result.Description
		setFieldSource("description")
	case "director":
		movie.Director = result.Director
		setFieldSource("director")
	case "maker":
		movie.Maker = result.Maker
		setFieldSource("maker")
	case "label":
		movie.Label = result.Label
		setFieldSource("label")
	case "series":
		movie.Series = result.Series
		setFieldSource("series")
	case "runtime":
		movie.Runtime = result.Runtime
		setFieldSource("runtime")
	case "release_date":
		movie.ReleaseDate = result.ReleaseDate
		if result.ReleaseDate != nil {
			movie.ReleaseYear = result.ReleaseDate.Year()
		} else {
			movie.ReleaseYear = 0
		}
		setFieldSource("release_date")
	case "rating_score":
		movie.RatingScore = scraperRatingScore(result)
		setFieldSource("rating_score")
	case "rating_votes":
		movie.RatingVotes = scraperRatingVotes(result)
		setFieldSource("rating_votes")
	case "actresses":
		movie.Actresses = actressesFromScraperInfo(result.Actresses)
		setFieldSource("actresses")
		rebuildActressSources(prov, movie.Actresses, source)
	case "genres":
		movie.Genres = genresFromScraperStrings(result.Genres)
		setFieldSource("genres")
	case "screenshot_urls":
		movie.Screenshots = append([]string(nil), result.ScreenshotURL...)
		setFieldSource("screenshot_urls")
	case "poster_url":
		movie.Poster.PosterURL = result.PosterURL
		setFieldSource("poster_url")
	case "cover_url":
		movie.Poster.CoverURL = result.CoverURL
		setFieldSource("cover_url")
	case "trailer_url":
		movie.TrailerURL = result.TrailerURL
		setFieldSource("trailer_url")
	case "should_crop_poster":
		movie.Poster.ShouldCropPoster = result.ShouldCropPoster
		setFieldSource("should_crop_poster")
	default:
		return fmt.Errorf("unhandled field: %s", fieldKey)
	}
	return nil
}

// findScraperResult returns the first raw result whose Source matches, or nil.
func findScraperResult(results []*models.ScraperResult, source string) *models.ScraperResult {
	for _, r := range results {
		if r != nil && r.Source == source {
			return r
		}
	}
	return nil
}

func scraperRatingScore(r *models.ScraperResult) float64 {
	if r.Rating != nil {
		return r.Rating.Score
	}
	return 0
}

func scraperRatingVotes(r *models.ScraperResult) int {
	if r.Rating != nil {
		return r.Rating.Votes
	}
	return 0
}

// actressesFromScraperInfo converts a scraper's ActressInfo slice to the model
// Actress slice stored on Movie. Mirrors the field mapping in the aggregator's
// actressMerger without the alias/dedup pass.
func actressesFromScraperInfo(infos []models.ActressInfo) []models.Actress {
	if len(infos) == 0 {
		return nil
	}
	out := make([]models.Actress, 0, len(infos))
	for _, info := range infos {
		out = append(out, models.Actress{
			DMMID:        info.DMMID,
			FirstName:    info.FirstName,
			LastName:     info.LastName,
			JapaneseName: info.JapaneseName,
			ThumbURL:     info.ThumbURL,
		})
	}
	return out
}

func genresFromScraperStrings(names []string) []models.Genre {
	if len(names) == 0 {
		return nil
	}
	out := make([]models.Genre, 0, len(names))
	for _, name := range names {
		out = append(out, models.Genre{Name: name})
	}
	return out
}

// rebuildActressSources re-attributes every actress in the overridden list to
// the chosen source. The list was wholesale-replaced, so any prior per-actress
// attribution is stale; this keeps the ActressSources map consistent with the
// new Actresses slice. Keying uses scrape.ActressSourceKey so the review
// tooltip lookup matches.
func rebuildActressSources(prov *ProvenanceData, actresses []models.Actress, source string) {
	if len(actresses) == 0 {
		prov.ActressSources = nil
		return
	}
	sources := make(map[string]string, len(actresses))
	for _, a := range actresses {
		key := scrape.ActressSourceKey(a)
		if key == "" {
			continue
		}
		sources[key] = source
	}
	if len(sources) == 0 {
		prov.ActressSources = nil
		return
	}
	prov.ActressSources = sources
}
