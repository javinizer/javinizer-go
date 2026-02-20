package worker

import (
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
)

// buildFieldSourcesFromScrapeResults returns a map of frontend movie field keys
// to the scraper that provided the winning value for that field.
func buildFieldSourcesFromScrapeResults(
	results []*models.ScraperResult,
	resolvedPriorities map[string][]string,
	customPriority []string,
) map[string]string {
	if len(results) == 0 {
		return nil
	}

	resultsBySource := make(map[string]*models.ScraperResult, len(results))
	resultOrder := make([]string, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		source := strings.TrimSpace(result.Source)
		if source == "" {
			continue
		}
		if _, exists := resultsBySource[source]; !exists {
			resultOrder = append(resultOrder, source)
		}
		resultsBySource[source] = result
	}
	if len(resultsBySource) == 0 {
		return nil
	}

	getPriority := func(field string) []string {
		if len(customPriority) > 0 {
			return customPriority
		}
		if resolvedPriorities != nil {
			if p, ok := resolvedPriorities[field]; ok && len(p) > 0 {
				return p
			}
		}
		return resultOrder
	}

	findSource := func(priority []string, hasValue func(*models.ScraperResult) bool) string {
		for _, source := range priority {
			result, exists := resultsBySource[source]
			if !exists || result == nil {
				continue
			}
			if hasValue(result) {
				return source
			}
		}
		return ""
	}

	assign := func(fieldKey, source string, fieldSources map[string]string) {
		source = strings.TrimSpace(source)
		if source == "" {
			return
		}
		fieldSources[fieldKey] = source
	}

	fieldSources := make(map[string]string)

	assign("id", findSource(getPriority("ID"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.ID) != ""
	}), fieldSources)
	assign("content_id", findSource(getPriority("ContentID"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.ContentID) != ""
	}), fieldSources)

	titleSource := findSource(getPriority("Title"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Title) != ""
	})
	assign("title", titleSource, fieldSources)
	assign("display_name", titleSource, fieldSources)

	assign("original_title", findSource(getPriority("OriginalTitle"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.OriginalTitle) != ""
	}), fieldSources)
	assign("description", findSource(getPriority("Description"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Description) != ""
	}), fieldSources)
	assign("director", findSource(getPriority("Director"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Director) != ""
	}), fieldSources)
	assign("maker", findSource(getPriority("Maker"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Maker) != ""
	}), fieldSources)
	assign("label", findSource(getPriority("Label"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Label) != ""
	}), fieldSources)
	assign("series", findSource(getPriority("Series"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.Series) != ""
	}), fieldSources)
	assign("poster_url", findSource(getPriority("PosterURL"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.PosterURL) != ""
	}), fieldSources)
	assign("cover_url", findSource(getPriority("CoverURL"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.CoverURL) != ""
	}), fieldSources)
	assign("trailer_url", findSource(getPriority("TrailerURL"), func(r *models.ScraperResult) bool {
		return strings.TrimSpace(r.TrailerURL) != ""
	}), fieldSources)
	assign("runtime", findSource(getPriority("Runtime"), func(r *models.ScraperResult) bool {
		return r.Runtime > 0
	}), fieldSources)
	assign("release_date", findSource(getPriority("ReleaseDate"), func(r *models.ScraperResult) bool {
		return r.ReleaseDate != nil
	}), fieldSources)

	ratingSource := findSource(getPriority("Rating"), func(r *models.ScraperResult) bool {
		return r.Rating != nil && (r.Rating.Score > 0 || r.Rating.Votes > 0)
	})
	assign("rating_score", ratingSource, fieldSources)
	assign("rating_votes", ratingSource, fieldSources)

	assign("actresses", findSource(getPriority("Actress"), func(r *models.ScraperResult) bool {
		return len(r.Actresses) > 0
	}), fieldSources)
	assign("genres", findSource(getPriority("Genre"), func(r *models.ScraperResult) bool {
		return len(r.Genres) > 0
	}), fieldSources)
	assign("screenshot_urls", findSource(getPriority("ScreenshotURL"), func(r *models.ScraperResult) bool {
		return len(r.ScreenshotURL) > 0
	}), fieldSources)

	// ShouldCropPoster should match the poster source selected by the aggregator.
	if posterSource, ok := fieldSources["poster_url"]; ok {
		assign("should_crop_poster", posterSource, fieldSources)
	}

	if len(fieldSources) == 0 {
		return nil
	}
	return fieldSources
}

// buildFieldSourcesFromCachedMovie creates a best-effort source map for cache hits.
func buildFieldSourcesFromCachedMovie(movie *models.Movie) map[string]string {
	if movie == nil {
		return nil
	}

	source := strings.TrimSpace(movie.SourceName)
	if source == "" {
		source = "scraper"
	}

	fieldSources := make(map[string]string)
	assign := func(fieldKey string, hasValue bool) {
		if hasValue {
			fieldSources[fieldKey] = source
		}
	}

	assign("id", strings.TrimSpace(movie.ID) != "")
	assign("content_id", strings.TrimSpace(movie.ContentID) != "")
	assign("title", strings.TrimSpace(movie.Title) != "")
	assign("display_name", strings.TrimSpace(movie.DisplayName) != "")
	assign("original_title", strings.TrimSpace(movie.OriginalTitle) != "")
	assign("description", strings.TrimSpace(movie.Description) != "")
	assign("director", strings.TrimSpace(movie.Director) != "")
	assign("maker", strings.TrimSpace(movie.Maker) != "")
	assign("label", strings.TrimSpace(movie.Label) != "")
	assign("series", strings.TrimSpace(movie.Series) != "")
	assign("poster_url", strings.TrimSpace(movie.PosterURL) != "")
	assign("cover_url", strings.TrimSpace(movie.CoverURL) != "")
	assign("trailer_url", strings.TrimSpace(movie.TrailerURL) != "")
	assign("runtime", movie.Runtime > 0)
	assign("release_date", movie.ReleaseDate != nil)
	assign("rating_score", movie.RatingScore > 0 || movie.RatingVotes > 0)
	assign("rating_votes", movie.RatingScore > 0 || movie.RatingVotes > 0)
	assign("actresses", len(movie.Actresses) > 0)
	assign("genres", len(movie.Genres) > 0)
	assign("screenshot_urls", len(movie.Screenshots) > 0)

	if _, ok := fieldSources["poster_url"]; ok {
		fieldSources["should_crop_poster"] = source
	}

	if len(fieldSources) == 0 {
		return nil
	}
	return fieldSources
}

// applyNFOMergeProvenance overlays NFO merge provenance onto scraper field sources.
func applyNFOMergeProvenance(fieldSources map[string]string, provenance map[string]nfo.DataSource) map[string]string {
	if len(provenance) == 0 {
		return fieldSources
	}
	if fieldSources == nil {
		fieldSources = make(map[string]string)
	}

	nfoToFrontendField := map[string]string{
		"ID":               "id",
		"ContentID":        "content_id",
		"DisplayName":      "display_name",
		"Title":            "title",
		"OriginalTitle":    "original_title",
		"Description":      "description",
		"Director":         "director",
		"Maker":            "maker",
		"Label":            "label",
		"Series":           "series",
		"PosterURL":        "poster_url",
		"CoverURL":         "cover_url",
		"TrailerURL":       "trailer_url",
		"ReleaseDate":      "release_date",
		"Runtime":          "runtime",
		"RatingScore":      "rating_score",
		"RatingVotes":      "rating_votes",
		"Actresses":        "actresses",
		"Genres":           "genres",
		"Screenshots":      "screenshot_urls",
		"ShouldCropPoster": "should_crop_poster",
	}

	for nfoField, sourceInfo := range provenance {
		frontendField, ok := nfoToFrontendField[nfoField]
		if !ok {
			continue
		}

		rawSource := strings.TrimSpace(sourceInfo.Source)
		if rawSource == "" {
			continue
		}

		lower := strings.ToLower(rawSource)
		switch {
		case strings.HasPrefix(lower, "scraper:"):
			scraperName := strings.TrimSpace(rawSource[len("scraper:"):])
			if scraperName != "" {
				fieldSources[frontendField] = scraperName
			}
		case lower == "scraper":
			// Keep existing scraper-specific source when available.
			if _, exists := fieldSources[frontendField]; !exists {
				fieldSources[frontendField] = "scraper"
			}
		case lower == "nfo" || lower == "merged" || lower == "empty":
			fieldSources[frontendField] = lower
		default:
			fieldSources[frontendField] = rawSource
		}
	}

	if len(fieldSources) == 0 {
		return nil
	}
	return fieldSources
}

func actressSourceKeyFromModel(actress models.Actress) string {
	if actress.DMMID > 0 {
		return "dmmid:" + strconv.Itoa(actress.DMMID)
	}
	if normalized := normalizeNameForKey(actress.JapaneseName); normalized != "" {
		return "name:" + normalized
	}
	if normalized := normalizeNameForKey(strings.TrimSpace(actress.FirstName + " " + actress.LastName)); normalized != "" {
		return "name:" + normalized
	}
	if normalized := normalizeNameForKey(strings.TrimSpace(actress.LastName + " " + actress.FirstName)); normalized != "" {
		return "name:" + normalized
	}
	return ""
}

func actressSourceKeysFromInfo(info models.ActressInfo) []string {
	keys := make([]string, 0, 4)
	if info.DMMID > 0 {
		keys = append(keys, "dmmid:"+strconv.Itoa(info.DMMID))
	}
	if normalized := normalizeNameForKey(info.JapaneseName); normalized != "" {
		keys = append(keys, "name:"+normalized)
	}
	if normalized := normalizeNameForKey(strings.TrimSpace(info.FirstName + " " + info.LastName)); normalized != "" {
		keys = append(keys, "name:"+normalized)
	}
	if normalized := normalizeNameForKey(strings.TrimSpace(info.LastName + " " + info.FirstName)); normalized != "" {
		keys = append(keys, "name:"+normalized)
	}

	deduped := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, key)
	}
	return deduped
}

func normalizeNameForKey(name string) string {
	normalized := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
	return normalized
}

// buildActressSourcesFromScrapeResults returns actress-key -> scraper source.
// Keys use "dmmid:<id>" when available; otherwise normalized name keys.
func buildActressSourcesFromScrapeResults(
	results []*models.ScraperResult,
	resolvedPriorities map[string][]string,
	customPriority []string,
	actresses []models.Actress,
) map[string]string {
	if len(results) == 0 || len(actresses) == 0 {
		return nil
	}

	resultsBySource := make(map[string]*models.ScraperResult, len(results))
	resultOrder := make([]string, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		source := strings.TrimSpace(result.Source)
		if source == "" {
			continue
		}
		if _, exists := resultsBySource[source]; !exists {
			resultOrder = append(resultOrder, source)
		}
		resultsBySource[source] = result
	}
	if len(resultsBySource) == 0 {
		return nil
	}

	priority := customPriority
	if len(priority) == 0 && resolvedPriorities != nil {
		if p, ok := resolvedPriorities["Actress"]; ok && len(p) > 0 {
			priority = p
		}
	}
	if len(priority) == 0 {
		priority = resultOrder
	}

	sourcesByActressKey := make(map[string]string)
	for _, actress := range actresses {
		targetKey := actressSourceKeyFromModel(actress)
		if targetKey == "" {
			continue
		}

		for _, source := range priority {
			result, exists := resultsBySource[source]
			if !exists || result == nil || len(result.Actresses) == 0 {
				continue
			}

			matched := false
			for _, info := range result.Actresses {
				infoKeys := actressSourceKeysFromInfo(info)
				for _, infoKey := range infoKeys {
					if infoKey == targetKey {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}

			if matched {
				sourcesByActressKey[targetKey] = source
				break
			}
		}
	}

	if len(sourcesByActressKey) == 0 {
		return nil
	}
	return sourcesByActressKey
}

func buildActressSourcesFromCachedMovie(movie *models.Movie) map[string]string {
	if movie == nil || len(movie.Actresses) == 0 {
		return nil
	}

	source := strings.TrimSpace(movie.SourceName)
	if source == "" {
		source = "scraper"
	}

	sourcesByActressKey := make(map[string]string)
	for _, actress := range movie.Actresses {
		key := actressSourceKeyFromModel(actress)
		if key == "" {
			continue
		}
		sourcesByActressKey[key] = source
	}

	if len(sourcesByActressKey) == 0 {
		return nil
	}
	return sourcesByActressKey
}

func applyActressMergeProvenance(
	actressSources map[string]string,
	provenance map[string]nfo.DataSource,
	actresses []models.Actress,
) map[string]string {
	if len(actresses) == 0 || len(provenance) == 0 {
		return actressSources
	}

	actressProv, ok := provenance["Actresses"]
	if !ok {
		return actressSources
	}

	rawSource := strings.TrimSpace(actressProv.Source)
	if rawSource == "" {
		return actressSources
	}

	if actressSources == nil {
		actressSources = make(map[string]string)
	}

	sourceToApply := rawSource
	lower := strings.ToLower(rawSource)
	switch {
	case strings.HasPrefix(lower, "scraper:"):
		if parsed := strings.TrimSpace(rawSource[len("scraper:"):]); parsed != "" {
			sourceToApply = parsed
		}
	case lower == "scraper":
		sourceToApply = "scraper"
	case lower == "nfo", lower == "merged", lower == "empty":
		sourceToApply = lower
	}

	for _, actress := range actresses {
		key := actressSourceKeyFromModel(actress)
		if key == "" {
			continue
		}
		// Keep scraper-specific attribution where already known.
		if existing, exists := actressSources[key]; exists {
			existingLower := strings.ToLower(strings.TrimSpace(existing))
			if existingLower != "" && existingLower != "scraper" {
				continue
			}
			if sourceToApply == "scraper" {
				continue
			}
		}
		actressSources[key] = sourceToApply
	}

	if len(actressSources) == 0 {
		return nil
	}
	return actressSources
}
