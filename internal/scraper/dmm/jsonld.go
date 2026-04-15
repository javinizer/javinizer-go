package dmm

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// JSONLDProduct represents the Product schema from JSON-LD
type JSONLDProduct struct {
	Context         string                 `json:"@context"`
	Type            string                 `json:"@type"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	Image           interface{}            `json:"image"` // Can be string or array
	SKU             string                 `json:"sku"`
	Brand           *JSONLDBrand           `json:"brand"`
	SubjectOf       *JSONLDVideoObject     `json:"subjectOf"`
	Offers          *JSONLDOffer           `json:"offers"`
	AggregateRating *JSONLDAggregateRating `json:"aggregateRating"`
}

// JSONLDBrand represents the Brand schema
type JSONLDBrand struct {
	Type string `json:"@type"`
	Name string `json:"name"`
}

// JSONLDVideoObject represents the VideoObject schema
type JSONLDVideoObject struct {
	Type         string   `json:"@type"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ContentURL   string   `json:"contentUrl"`
	ThumbnailURL string   `json:"thumbnailUrl"`
	UploadDate   string   `json:"uploadDate"`
	Genre        []string `json:"genre"`
}

// JSONLDOffer represents the Offer schema
type JSONLDOffer struct {
	Type          string  `json:"@type"`
	Availability  string  `json:"availability"`
	PriceCurrency string  `json:"priceCurrency"`
	Price         float64 `json:"price"`
}

// JSONLDAggregateRating represents the AggregateRating schema
type JSONLDAggregateRating struct {
	Type        string  `json:"@type"`
	RatingValue float64 `json:"ratingValue"`
	RatingCount int     `json:"ratingCount"`
}

// extractJSONLD extracts and parses JSON-LD data from video.dmm.co.jp pages
// Returns the first Product-type JSON-LD found, or nil if none
func extractJSONLD(doc *goquery.Document) *JSONLDProduct {
	var product *JSONLDProduct

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(i int, sel *goquery.Selection) bool {
		jsonText := sel.Text()

		// Try to parse as JSONLDProduct
		var p JSONLDProduct
		if err := json.Unmarshal([]byte(jsonText), &p); err != nil {
			logging.Debugf("DMM JSON-LD: Failed to parse JSON-LD #%d: %v", i+1, err)
			return true // Continue to next script
		}

		// Check if this is a Product type
		if p.Type == "Product" {
			product = &p
			logging.Debugf("DMM JSON-LD: Successfully parsed Product JSON-LD (length: %d)", len(jsonText))
			return false // Found it, stop iteration
		}

		return true // Continue looking
	})

	return product
}

// getImagesFromJSONLD extracts image URLs from JSON-LD image field
// Handles both string and array formats
func getImagesFromJSONLD(imageField interface{}) []string {
	images := make([]string, 0)

	switch v := imageField.(type) {
	case string:
		// Single image as string
		if v != "" {
			images = append(images, normalizeImageURL(v))
		}
	case []interface{}:
		// Array of images
		for _, img := range v {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				images = append(images, normalizeImageURL(imgStr))
			}
		}
	default:
		logging.Debugf("DMM JSON-LD: Unknown image field type: %T", imageField)
	}

	return images
}

// parseReleaseDateFromJSONLD parses the uploadDate field
func parseReleaseDateFromJSONLD(uploadDate string) *time.Time {
	if uploadDate == "" {
		return nil
	}

	// Try parsing as ISO 8601 date (YYYY-MM-DD)
	t, err := time.Parse("2006-01-02", uploadDate)
	if err != nil {
		logging.Debugf("DMM JSON-LD: Failed to parse uploadDate '%s': %v", uploadDate, err)
		return nil
	}

	return &t
}

// normalizeJSONLDImageURL normalizes image URLs from JSON-LD
// Converts awsimgsrc.dmm.co.jp to pics.dmm.co.jp
func normalizeJSONLDImageURL(url string) string {
	// Convert awsimgsrc to pics.dmm.co.jp
	url = strings.Replace(url, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)

	// Remove query parameters
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}

	return url
}

// extractMetadataFromJSONLD is a helper function that extracts all available metadata
// from JSON-LD and returns it in a structured way for easy use in parseHTML
func extractMetadataFromJSONLD(doc *goquery.Document) map[string]interface{} {
	metadata := make(map[string]interface{})

	product := extractJSONLD(doc)
	if product == nil {
		logging.Debug("DMM JSON-LD: No Product JSON-LD found")
		return metadata
	}

	// Extract title
	if product.Name != "" {
		metadata["title"] = scraperutil.CleanString(product.Name)
	}

	// Extract description
	if product.Description != "" {
		metadata["description"] = scraperutil.CleanString(product.Description)
	}

	// Extract content ID
	if product.SKU != "" {
		metadata["content_id"] = product.SKU
	}

	// Extract maker/brand
	if product.Brand != nil && product.Brand.Name != "" {
		metadata["maker"] = scraperutil.CleanString(product.Brand.Name)
	}

	// Extract images (cover + screenshots)
	if product.Image != nil {
		images := getImagesFromJSONLD(product.Image)
		if len(images) > 0 {
			// First image is usually the cover
			metadata["cover_url"] = normalizeJSONLDImageURL(images[0])

			// Rest are screenshots (skip first which is cover)
			if len(images) > 1 {
				screenshots := make([]string, 0, len(images)-1)
				for _, img := range images[1:] {
					screenshots = append(screenshots, normalizeJSONLDImageURL(img))
				}
				metadata["screenshots"] = screenshots
				logging.Debugf("DMM JSON-LD: Extracted %d screenshots from image array", len(screenshots))
			}
		}
	}

	// Extract video/trailer information
	if product.SubjectOf != nil {
		// Trailer URL
		if product.SubjectOf.ContentURL != "" {
			metadata["trailer_url"] = normalizeTrailerURL(product.SubjectOf.ContentURL)
			logging.Debugf("DMM JSON-LD: Found trailer URL: %s", metadata["trailer_url"])
		}

		// Release date
		if product.SubjectOf.UploadDate != "" {
			if releaseDate := parseReleaseDateFromJSONLD(product.SubjectOf.UploadDate); releaseDate != nil {
				metadata["release_date"] = releaseDate
			}
		}

		// Genres
		if len(product.SubjectOf.Genre) > 0 {
			metadata["genres"] = product.SubjectOf.Genre
		}
	}

	// Extract rating
	if product.AggregateRating != nil {
		// Convert 5-point scale to 10-point scale
		ratingValue := product.AggregateRating.RatingValue * 2
		ratingCount := product.AggregateRating.RatingCount

		metadata["rating_value"] = ratingValue
		metadata["rating_count"] = ratingCount
		logging.Debugf("DMM JSON-LD: Found rating: %.1f/10 (%d votes)", ratingValue, ratingCount)
	}

	// Log what we found
	fieldCount := len(metadata)
	if fieldCount > 0 {
		fields := make([]string, 0, fieldCount)
		for k := range metadata {
			fields = append(fields, k)
		}
		logging.Debugf("DMM JSON-LD: Extracted %d metadata fields: %v", fieldCount, fields)
	}

	return metadata
}

// getStringFromMetadata safely retrieves a string value from metadata map
func getStringFromMetadata(metadata map[string]interface{}, key string) string {
	if val, ok := metadata[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}

// getStringSliceFromMetadata safely retrieves a []string value from metadata map
func getStringSliceFromMetadata(metadata map[string]interface{}, key string) []string {
	if val, ok := metadata[key]; ok {
		if sliceVal, ok := val.([]string); ok {
			return sliceVal
		}
	}
	return nil
}

// getTimeFromMetadata safely retrieves a *time.Time value from metadata map
func getTimeFromMetadata(metadata map[string]interface{}, key string) *time.Time {
	if val, ok := metadata[key]; ok {
		if timeVal, ok := val.(*time.Time); ok {
			return timeVal
		}
	}
	return nil
}

// getFloat64FromMetadata safely retrieves a float64 value from metadata map
func getFloat64FromMetadata(metadata map[string]interface{}, key string) float64 {
	if val, ok := metadata[key]; ok {
		// Handle both float64 and int
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// getIntFromMetadata safely retrieves an int value from metadata map
func getIntFromMetadata(metadata map[string]interface{}, key string) int {
	if val, ok := metadata[key]; ok {
		// Handle both int and float64
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
	}
	return 0
}
