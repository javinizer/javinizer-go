package nfo

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MergeStrategy defines how to merge metadata from different sources
type MergeStrategy int

const (
	// PreferScraper uses scraper data when available, falls back to NFO (default)
	PreferScraper MergeStrategy = iota
	// PreferNFO uses NFO data when available, falls back to scraper (conservative)
	PreferNFO
	// MergeArrays combines arrays from both sources and deduplicates
	MergeArrays
)

// MergeResult contains the merged movie and metadata about the merge
type MergeResult struct {
	Merged     *models.Movie
	Provenance map[string]DataSource
	Stats      MergeStats
}

// DataSource indicates where a field's data came from
type DataSource struct {
	Source      string     // "scraper:r18dev", "nfo", "merged", "empty"
	Confidence  float64    // 0.0-1.0 (for future use)
	LastUpdated *time.Time // When this data was last updated
}

// MergeStats tracks what happened during the merge
type MergeStats struct {
	TotalFields       int
	FromScraper       int
	FromNFO           int
	MergedArrays      int
	ConflictsResolved int // Both had data, chose one
	EmptyFields       int
}

// MergeMovieMetadata merges scraped and NFO data non-destructively
// scraped: Movie from scraper results
// nfo: Movie from existing NFO file
// strategy: How to handle conflicts
func MergeMovieMetadata(scraped, nfo *models.Movie, strategy MergeStrategy) (*MergeResult, error) {
	if scraped == nil && nfo == nil {
		return nil, fmt.Errorf("both scraped and nfo are nil")
	}

	// If only one source exists, use it
	if scraped == nil {
		return &MergeResult{
			Merged:     nfo,
			Provenance: makeProvenanceMap(nfo, "nfo"),
			Stats: MergeStats{
				TotalFields: countNonEmptyFields(nfo),
				FromNFO:     countNonEmptyFields(nfo),
			},
		}, nil
	}
	if nfo == nil {
		return &MergeResult{
			Merged:     scraped,
			Provenance: makeProvenanceMap(scraped, "scraper"),
			Stats: MergeStats{
				TotalFields: countNonEmptyFields(scraped),
				FromScraper: countNonEmptyFields(scraped),
			},
		}, nil
	}

	// Both exist - perform merge
	merged := &models.Movie{}
	provenance := make(map[string]DataSource)
	stats := MergeStats{}

	// Capture timestamp for provenance tracking
	now := time.Now()

	// Merge each field
	merged.ContentID = mergeStringField("ContentID", scraped.ContentID, nfo.ContentID, strategy, &stats, provenance, now)
	merged.ID = mergeStringField("ID", scraped.ID, nfo.ID, strategy, &stats, provenance, now)
	merged.DisplayName = mergeStringField("DisplayName", scraped.DisplayName, nfo.DisplayName, strategy, &stats, provenance, now)
	merged.Title = mergeStringField("Title", scraped.Title, nfo.Title, strategy, &stats, provenance, now)
	merged.OriginalTitle = mergeStringField("OriginalTitle", scraped.OriginalTitle, nfo.OriginalTitle, strategy, &stats, provenance, now)
	merged.Description = mergeStringField("Description", scraped.Description, nfo.Description, strategy, &stats, provenance, now)
	merged.Director = mergeStringField("Director", scraped.Director, nfo.Director, strategy, &stats, provenance, now)
	merged.Maker = mergeStringField("Maker", scraped.Maker, nfo.Maker, strategy, &stats, provenance, now)
	merged.Label = mergeStringField("Label", scraped.Label, nfo.Label, strategy, &stats, provenance, now)
	merged.Series = mergeStringField("Series", scraped.Series, nfo.Series, strategy, &stats, provenance, now)
	merged.PosterURL = mergeStringField("PosterURL", scraped.PosterURL, nfo.PosterURL, strategy, &stats, provenance, now)
	merged.CoverURL = mergeStringField("CoverURL", scraped.CoverURL, nfo.CoverURL, strategy, &stats, provenance, now)
	merged.CroppedPosterURL = mergeStringField("CroppedPosterURL", scraped.CroppedPosterURL, nfo.CroppedPosterURL, strategy, &stats, provenance, now)
	merged.TrailerURL = mergeStringField("TrailerURL", scraped.TrailerURL, nfo.TrailerURL, strategy, &stats, provenance, now)
	merged.OriginalFileName = mergeStringField("OriginalFileName", scraped.OriginalFileName, nfo.OriginalFileName, strategy, &stats, provenance, now)
	merged.SourceName = mergeStringField("SourceName", scraped.SourceName, nfo.SourceName, strategy, &stats, provenance, now)
	merged.SourceURL = mergeStringField("SourceURL", scraped.SourceURL, nfo.SourceURL, strategy, &stats, provenance, now)

	// Merge int fields
	merged.ReleaseYear = mergeIntField("ReleaseYear", scraped.ReleaseYear, nfo.ReleaseYear, strategy, &stats, provenance, now)
	merged.Runtime = mergeIntField("Runtime", scraped.Runtime, nfo.Runtime, strategy, &stats, provenance, now)
	merged.RatingVotes = mergeIntField("RatingVotes", scraped.RatingVotes, nfo.RatingVotes, strategy, &stats, provenance, now)

	// Merge float fields
	merged.RatingScore = mergeFloatField("RatingScore", scraped.RatingScore, nfo.RatingScore, strategy, &stats, provenance, now)

	// Merge bool fields
	merged.ShouldCropPoster = mergeBoolField("ShouldCropPoster", scraped.ShouldCropPoster, nfo.ShouldCropPoster, strategy, &stats, provenance, now)

	// Merge pointer fields
	merged.ReleaseDate = mergeDateField("ReleaseDate", scraped.ReleaseDate, nfo.ReleaseDate, strategy, &stats, provenance, now)

	// Merge array fields
	merged.Actresses = mergeActresses("Actresses", scraped.Actresses, nfo.Actresses, strategy, &stats, provenance, now)
	merged.Genres = mergeGenres("Genres", scraped.Genres, nfo.Genres, strategy, &stats, provenance, now)
	merged.Screenshots = mergeStringSlice("Screenshots", scraped.Screenshots, nfo.Screenshots, strategy, &stats, provenance, now)

	// Timestamps - always use most recent non-zero CreatedAt
	merged.CreatedAt = scraped.CreatedAt
	if !nfo.CreatedAt.IsZero() && (scraped.CreatedAt.IsZero() || nfo.CreatedAt.After(scraped.CreatedAt)) {
		merged.CreatedAt = nfo.CreatedAt
	}
	merged.UpdatedAt = time.Now()

	// Calculate total fields consistently using the merged result
	stats.TotalFields = countNonEmptyFields(merged)

	return &MergeResult{
		Merged:     merged,
		Provenance: provenance,
		Stats:      stats,
	}, nil
}

// mergeStringField merges two string fields according to strategy
func mergeStringField(fieldName, scrapedVal, nfoVal string, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) string {
	scrapedEmpty := scrapedVal == ""
	nfoEmpty := nfoVal == ""

	// Both empty
	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return ""
	}

	// Only one has data
	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}

	// Both have data - resolve conflict
	stats.ConflictsResolved++

	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	case PreferScraper, MergeArrays: // MergeArrays falls back to PreferScraper for strings
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	default:
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}
}

// mergeIntField merges two int fields
// NOTE: Treats 0 as "empty" - this is intentional but means legitimate zero values
// are treated as absent. For fields where 0 is meaningful, consider using pointer types.
func mergeIntField(fieldName string, scrapedVal, nfoVal int, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) int {
	scrapedEmpty := scrapedVal == 0
	nfoEmpty := nfoVal == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return 0
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	default:
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}
}

// mergeFloatField merges two float64 fields
// NOTE: Treats 0.0 as "empty" - this is intentional but means legitimate zero values
// are treated as absent. For fields where 0.0 is meaningful, consider using pointer types.
func mergeFloatField(fieldName string, scrapedVal, nfoVal float64, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) float64 {
	scrapedEmpty := scrapedVal == 0
	nfoEmpty := nfoVal == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return 0
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	default:
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}
}

// mergeBoolField merges two bool fields
// NOTE: Treats false as "empty" - this is intentional but means explicit false values
// are treated as absent. For fields where false is meaningful, consider using pointer types.
func mergeBoolField(fieldName string, scrapedVal, nfoVal bool, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) bool {
	scrapedEmpty := !scrapedVal
	nfoEmpty := !nfoVal

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return false
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	default:
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}
}

// mergeDateField merges two time.Time pointer fields
func mergeDateField(fieldName string, scrapedVal, nfoVal *time.Time, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) *time.Time {
	scrapedEmpty := scrapedVal == nil || scrapedVal.IsZero()
	nfoEmpty := nfoVal == nil || nfoVal.IsZero()

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfoVal
	default:
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scrapedVal
	}
}

// mergeActresses merges actress slices
func mergeActresses(fieldName string, scraped, nfo []models.Actress, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) []models.Actress {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	case MergeArrays:
		// Merge and deduplicate by normalized name (case-insensitive, trimmed)
		merged := make([]models.Actress, 0, len(scraped)+len(nfo))
		seen := make(map[string]bool)

		for _, actress := range scraped {
			key := actressKey(actress)
			if !seen[key] {
				merged = append(merged, actress)
				seen[key] = true
			}
		}
		for _, actress := range nfo {
			key := actressKey(actress)
			if !seen[key] {
				merged = append(merged, actress)
				seen[key] = true
			}
		}

		stats.MergedArrays++
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &timestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}
}

// mergeGenres merges genre slices
func mergeGenres(fieldName string, scraped, nfo []models.Genre, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) []models.Genre {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	case MergeArrays:
		// Merge and deduplicate by normalized name (case-insensitive, trimmed)
		merged := make([]models.Genre, 0, len(scraped)+len(nfo))
		seen := make(map[string]bool)

		for _, genre := range scraped {
			normalizedName := strings.ToLower(strings.TrimSpace(genre.Name))
			if normalizedName != "" && !seen[normalizedName] {
				merged = append(merged, genre)
				seen[normalizedName] = true
			}
		}
		for _, genre := range nfo {
			normalizedName := strings.ToLower(strings.TrimSpace(genre.Name))
			if normalizedName != "" && !seen[normalizedName] {
				merged = append(merged, genre)
				seen[normalizedName] = true
			}
		}

		stats.MergedArrays++
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &timestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}
}

// mergeStringSlice merges string slices (screenshots, etc.)
func mergeStringSlice(fieldName string, scraped, nfo []string, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, timestamp time.Time) []string {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO:
		stats.FromNFO++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &timestamp}
		return nfo
	case MergeArrays:
		// Merge and deduplicate by normalized URL (lowercase, trimmed, no trailing slash)
		merged := make([]string, 0, len(scraped)+len(nfo))
		seen := make(map[string]bool)

		for _, s := range scraped {
			normalized := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(s, "/")))
			if normalized != "" && !seen[normalized] {
				merged = append(merged, s)
				seen[normalized] = true
			}
		}
		for _, s := range nfo {
			normalized := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(s, "/")))
			if normalized != "" && !seen[normalized] {
				merged = append(merged, s)
				seen[normalized] = true
			}
		}

		stats.MergedArrays++
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &timestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &timestamp}
		return scraped
	}
}

// actressKey creates a normalized unique key for deduplication
// Uses case-insensitive, trimmed comparison to catch variants like "Yui Hatano" vs "yui hatano"
func actressKey(actress models.Actress) string {
	firstName := strings.ToLower(strings.TrimSpace(actress.FirstName))
	lastName := strings.ToLower(strings.TrimSpace(actress.LastName))

	// Use normalized FirstName + LastName as key, fall back to normalized JapaneseName
	if firstName != "" || lastName != "" {
		return fmt.Sprintf("%s|%s", firstName, lastName)
	}
	return strings.ToLower(strings.TrimSpace(actress.JapaneseName))
}

// makeProvenanceMap creates a provenance map for a single source
// Skips CreatedAt/UpdatedAt as these are internal timestamps, not metadata fields
func makeProvenanceMap(movie *models.Movie, source string) map[string]DataSource {
	provenance := make(map[string]DataSource)

	// Guard against nil input
	if movie == nil {
		return provenance
	}

	v := reflect.ValueOf(movie)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return provenance
		}
		v = v.Elem()
	}

	if !v.IsValid() || v.Kind() != reflect.Struct {
		return provenance
	}

	t := v.Type()

	// Determine timestamp for provenance
	var timestamp *time.Time
	if !movie.UpdatedAt.IsZero() {
		timestamp = &movie.UpdatedAt
	} else if !movie.CreatedAt.IsZero() {
		timestamp = &movie.CreatedAt
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := t.Field(i).Name

		// Skip CreatedAt and UpdatedAt - these are internal timestamps, not user-visible metadata
		if fieldName == "CreatedAt" || fieldName == "UpdatedAt" {
			continue
		}

		// Check if field has data
		if !isEmptyValue(field) {
			provenance[fieldName] = DataSource{
				Source:      source,
				Confidence:  1.0,
				LastUpdated: timestamp,
			}
		}
	}

	return provenance
}

// countNonEmptyFields counts non-empty fields in a movie
func countNonEmptyFields(movie *models.Movie) int {
	count := 0

	// Guard against nil input
	if movie == nil {
		return 0
	}

	v := reflect.ValueOf(movie)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}

	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := t.Field(i).Name

		// Skip CreatedAt and UpdatedAt - these are internal timestamps, not user-visible metadata
		if fieldName == "CreatedAt" || fieldName == "UpdatedAt" {
			continue
		}

		if !isEmptyValue(field) {
			count++
		}
	}

	return count
}

// isEmptyValue checks if a reflect.Value is considered empty
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return true
		}
		return isEmptyValue(v.Elem())
	case reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0
	case reflect.Struct:
		// For time.Time, check IsZero
		if t, ok := v.Interface().(time.Time); ok {
			return t.IsZero()
		}
		return false
	default:
		return false
	}
}
