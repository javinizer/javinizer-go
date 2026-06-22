package scrape

import (
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

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

func actressSourceKeyFromModel(actress models.Actress) string {
	if actress.DMMID > 0 {
		return "dmmid:" + strconv.Itoa(actress.DMMID)
	}
	if normalized := models.NormalizeActressNameKey(actress.JapaneseName); normalized != "" {
		return "name:" + normalized
	}
	if normalized := models.NormalizeActressNameKey(strings.TrimSpace(actress.FirstName + " " + actress.LastName)); normalized != "" {
		return "name:" + normalized
	}
	if normalized := models.NormalizeActressNameKey(strings.TrimSpace(actress.LastName + " " + actress.FirstName)); normalized != "" {
		return "name:" + normalized
	}
	return ""
}

func actressSourceKeysFromInfo(info models.ActressInfo) []string {
	keys := make([]string, 0, 4)
	if info.DMMID > 0 {
		keys = append(keys, "dmmid:"+strconv.Itoa(info.DMMID))
	}
	if normalized := models.NormalizeActressNameKey(info.JapaneseName); normalized != "" {
		keys = append(keys, "name:"+normalized)
	}
	if normalized := models.NormalizeActressNameKey(strings.TrimSpace(info.FirstName + " " + info.LastName)); normalized != "" {
		keys = append(keys, "name:"+normalized)
	}
	if normalized := models.NormalizeActressNameKey(strings.TrimSpace(info.LastName + " " + info.FirstName)); normalized != "" {
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
	assign("display_title", strings.TrimSpace(movie.DisplayTitle) != "")
	assign("original_title", strings.TrimSpace(movie.OriginalTitle) != "")
	assign("description", strings.TrimSpace(movie.Description) != "")
	assign("director", strings.TrimSpace(movie.Director) != "")
	assign("maker", strings.TrimSpace(movie.Maker) != "")
	assign("label", strings.TrimSpace(movie.Label) != "")
	assign("series", strings.TrimSpace(movie.Series) != "")
	assign("poster_url", strings.TrimSpace(movie.Poster.PosterURL) != "")
	assign("cover_url", strings.TrimSpace(movie.Poster.CoverURL) != "")
	assign("trailer_url", strings.TrimSpace(movie.TrailerURL) != "")
	assign("runtime", movie.Runtime > 0)
	assign("release_date", movie.ReleaseDate != nil)
	assign("rating_score", movie.RatingScore > 0 || movie.RatingVotes > 0)
	assign("rating_votes", movie.RatingScore > 0 || movie.RatingVotes > 0)
	assign("actresses", len(movie.Actresses) > 0)
	assign("genres", len(movie.Genres) > 0)
	assign("screenshot_urls", len(movie.Screenshots) > 0)

	if movie.Poster.ShouldCropPoster {
		fieldSources["should_crop_poster"] = source
	}

	if len(fieldSources) == 0 {
		return nil
	}
	return fieldSources
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
