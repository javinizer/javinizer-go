package aggregator

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

func validateRequiredFieldsScraped(scraped *models.Movie, requiredFields []string) error {
	missingFields := []string{}

	for _, fieldName := range requiredFields {
		fieldLower := strings.ToLower(fieldName)

		switch fieldLower {
		case "id":
			if scraped.ID == "" {
				missingFields = append(missingFields, "ID")
			}
		case "contentid", "content_id":
			if scraped.ContentID == "" {
				missingFields = append(missingFields, "ContentID")
			}
		case "title":
			if scraped.Title == "" {
				missingFields = append(missingFields, "Title")
			}
		case "originaltitle", "original_title":
			if scraped.OriginalTitle == "" {
				missingFields = append(missingFields, "OriginalTitle")
			}
		case "description", "plot":
			if scraped.Description == "" {
				missingFields = append(missingFields, "Description")
			}
		case "director":
			if scraped.Director == "" {
				missingFields = append(missingFields, "Director")
			}
		case "maker", "studio":
			if scraped.Maker == "" {
				missingFields = append(missingFields, "Maker")
			}
		case "label":
			if scraped.Label == "" {
				missingFields = append(missingFields, "Label")
			}
		case "series", "set":
			if scraped.Series == "" {
				missingFields = append(missingFields, "Series")
			}
		case "releasedate", "release_date", "premiered":
			if scraped.ReleaseDate == nil {
				missingFields = append(missingFields, "ReleaseDate")
			}
		case "runtime":
			if scraped.Runtime == 0 {
				missingFields = append(missingFields, "Runtime")
			}
		case "coverurl", "cover_url", "cover":
			if scraped.Poster.CoverURL == "" {
				missingFields = append(missingFields, "CoverURL")
			}
		case "posterurl", "poster_url", "poster":
			if scraped.Poster.PosterURL == "" {
				missingFields = append(missingFields, "PosterURL")
			}
		case "trailerurl", "trailer_url", "trailer":
			if scraped.TrailerURL == "" {
				missingFields = append(missingFields, "TrailerURL")
			}
		case "screenshots", "screenshot_url", "screenshoturl":
			if len(scraped.Screenshots) == 0 {
				missingFields = append(missingFields, "Screenshots")
			}
		case "actresses", "actress":
			if len(scraped.Actresses) == 0 {
				missingFields = append(missingFields, "Actresses")
			}
		case "genres", "genre":
			if len(scraped.Genres) == 0 {
				missingFields = append(missingFields, "Genres")
			}
		case "rating", "ratingscore", "rating_score":
			// RatingScore == 0 is a valid value — accept any.
		default:
			continue
		}
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missingFields, ", "))
	}

	return nil
}
