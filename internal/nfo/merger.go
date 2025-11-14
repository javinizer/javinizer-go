package nfo

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
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
	// PreserveExisting never overwrites non-empty fields (strictest preservation)
	PreserveExisting
	// FillMissingOnly only populates completely empty fields (safe gap filling)
	FillMissingOnly
)

// criticalFields defines fields that must never be empty, regardless of merge strategy
// These fields will always fall back to NFO if scraper returns empty
var criticalFields = map[string]bool{
	"ID":        true,
	"ContentID": true,
	"Title":     true,
}

// ParseMergeStrategy converts a merge strategy string to MergeStrategy enum
// Valid values: "prefer-scraper", "prefer-nfo", "merge-arrays"
// Returns PreferNFO as default for Update Mode
//
// Deprecated: This function is from the legacy single-parameter merge strategy system.
// Use ParseScalarStrategy() for scalar fields and ParseArrayStrategy() for array fields instead.
// The new two-parameter system provides finer-grained control with additional options like
// "preserve-existing" and "fill-missing-only" for scalar fields, and "merge"/"replace" for arrays.
func ParseMergeStrategy(strategy string) MergeStrategy {
	switch strategy {
	case "prefer-scraper":
		return PreferScraper
	case "prefer-nfo":
		return PreferNFO
	case "merge-arrays":
		return MergeArrays
	default:
		return PreferNFO // default for Update Mode
	}
}

// ParseScalarStrategy converts scalar strategy string to MergeStrategy
// Valid values: "prefer-scraper", "prefer-nfo", "preserve-existing", "fill-missing-only" (case-insensitive)
// Returns PreferNFO as default
func ParseScalarStrategy(strategy string) MergeStrategy {
	switch strings.ToLower(strategy) {
	case "prefer-scraper":
		return PreferScraper
	case "preserve-existing":
		return PreserveExisting
	case "fill-missing-only":
		return FillMissingOnly
	default:
		return PreferNFO
	}
}

// ParseArrayStrategy converts array strategy string to boolean
// Valid values: "merge", "replace" (case-insensitive)
// Returns true (merge) as default
func ParseArrayStrategy(strategy string) bool {
	return strings.ToLower(strategy) != "replace" // merge is default
}

// ApplyPreset applies a preset configuration to scalar and array strategy strings.
// Presets:
//   - "conservative": preserve-existing + merge (strictest preservation)
//   - "gap-fill": fill-missing-only + merge (safe gap filling)
//   - "aggressive": prefer-scraper + replace (trust scrapers completely)
//
// Returns the resolved scalar and array strategy strings, or an error if preset is invalid.
// If preset is empty, returns the original strategy strings unchanged.
func ApplyPreset(preset string, scalarStrategy string, arrayStrategy string) (string, string, error) {
	if preset == "" {
		return scalarStrategy, arrayStrategy, nil
	}

	switch strings.ToLower(preset) {
	case "conservative":
		return "preserve-existing", "merge", nil
	case "gap-fill":
		return "fill-missing-only", "merge", nil
	case "aggressive":
		return "prefer-scraper", "replace", nil
	default:
		return scalarStrategy, arrayStrategy, fmt.Errorf("invalid preset: %s (valid options: conservative, gap-fill, aggressive)", preset)
	}
}

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
//
// IMPORTANT: Zero values are treated as "empty" for primitive types:
// - int/float fields: 0 and 0.0 are considered absent data
// - bool fields: false is considered absent data
// This means explicit zero/false values cannot be distinguished from missing values.
// For fields where zero/false are meaningful, consider using pointer types (*int, *float64, *bool).
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

	// Get source timestamps for provenance tracking
	// Use UpdatedAt if available, fall back to CreatedAt, then current time
	scrapedTS := scraped.UpdatedAt
	if scrapedTS.IsZero() && !scraped.CreatedAt.IsZero() {
		scrapedTS = scraped.CreatedAt
	}
	if scrapedTS.IsZero() {
		scrapedTS = time.Now()
	}

	nfoTS := nfo.UpdatedAt
	if nfoTS.IsZero() && !nfo.CreatedAt.IsZero() {
		nfoTS = nfo.CreatedAt
	}
	if nfoTS.IsZero() {
		nfoTS = time.Now()
	}

	// Merge each field (passing both source timestamps)
	merged.ContentID = mergeStringField("ContentID", scraped.ContentID, nfo.ContentID, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.ID = mergeStringField("ID", scraped.ID, nfo.ID, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.DisplayName = mergeStringField("DisplayName", scraped.DisplayName, nfo.DisplayName, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Title = mergeStringField("Title", scraped.Title, nfo.Title, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.OriginalTitle = mergeStringField("OriginalTitle", scraped.OriginalTitle, nfo.OriginalTitle, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Description = mergeStringField("Description", scraped.Description, nfo.Description, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Director = mergeStringField("Director", scraped.Director, nfo.Director, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Maker = mergeStringField("Maker", scraped.Maker, nfo.Maker, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Label = mergeStringField("Label", scraped.Label, nfo.Label, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Series = mergeStringField("Series", scraped.Series, nfo.Series, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.PosterURL = mergeStringField("PosterURL", scraped.PosterURL, nfo.PosterURL, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.CoverURL = mergeStringField("CoverURL", scraped.CoverURL, nfo.CoverURL, strategy, &stats, provenance, scrapedTS, nfoTS)
	// CroppedPosterURL: Always use scraped value (not stored in NFO, runtime-generated temp URL)
	merged.CroppedPosterURL = scraped.CroppedPosterURL
	merged.TrailerURL = mergeStringField("TrailerURL", scraped.TrailerURL, nfo.TrailerURL, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.OriginalFileName = mergeStringField("OriginalFileName", scraped.OriginalFileName, nfo.OriginalFileName, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.SourceName = mergeStringField("SourceName", scraped.SourceName, nfo.SourceName, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.SourceURL = mergeStringField("SourceURL", scraped.SourceURL, nfo.SourceURL, strategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge int fields
	merged.ReleaseYear = mergeIntField("ReleaseYear", scraped.ReleaseYear, nfo.ReleaseYear, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Runtime = mergeIntField("Runtime", scraped.Runtime, nfo.Runtime, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.RatingVotes = mergeIntField("RatingVotes", scraped.RatingVotes, nfo.RatingVotes, strategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge float fields
	merged.RatingScore = mergeFloatField("RatingScore", scraped.RatingScore, nfo.RatingScore, strategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge bool fields
	merged.ShouldCropPoster = mergeBoolField("ShouldCropPoster", scraped.ShouldCropPoster, nfo.ShouldCropPoster, strategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge pointer fields
	merged.ReleaseDate = mergeDateField("ReleaseDate", scraped.ReleaseDate, nfo.ReleaseDate, strategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge array fields
	merged.Actresses = mergeActresses("Actresses", scraped.Actresses, nfo.Actresses, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Genres = mergeGenres("Genres", scraped.Genres, nfo.Genres, strategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Screenshots = mergeStringSlice("Screenshots", scraped.Screenshots, nfo.Screenshots, strategy, &stats, provenance, scrapedTS, nfoTS)

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

// MergeMovieMetadataWithOptions merges scraped and NFO data with granular control
// scraped: Movie from scraper results
// nfo: Movie from existing NFO file
// scalarStrategy: How to handle scalar fields (PreferNFO or PreferScraper)
// mergeArrays: If true, combine arrays from both sources; if false, use scalarStrategy for arrays too
//
// This provides independent control over:
// - Scalar fields (title, studio, etc): prefer NFO or prefer scraped
// - Array fields (actresses, genres): merge both sources or replace
func MergeMovieMetadataWithOptions(scraped, nfo *models.Movie, scalarStrategy MergeStrategy, mergeArrays bool) (*MergeResult, error) {
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

	// Get source timestamps
	scrapedTS := scraped.UpdatedAt
	if scrapedTS.IsZero() && !scraped.CreatedAt.IsZero() {
		scrapedTS = scraped.CreatedAt
	}
	if scrapedTS.IsZero() {
		scrapedTS = time.Now()
	}

	nfoTS := nfo.UpdatedAt
	if nfoTS.IsZero() && !nfo.CreatedAt.IsZero() {
		nfoTS = nfo.CreatedAt
	}
	if nfoTS.IsZero() {
		nfoTS = time.Now()
	}

	// Merge scalar fields using scalarStrategy
	merged.ContentID = mergeStringField("ContentID", scraped.ContentID, nfo.ContentID, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.ID = mergeStringField("ID", scraped.ID, nfo.ID, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.DisplayName = mergeStringField("DisplayName", scraped.DisplayName, nfo.DisplayName, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Title = mergeStringField("Title", scraped.Title, nfo.Title, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.OriginalTitle = mergeStringField("OriginalTitle", scraped.OriginalTitle, nfo.OriginalTitle, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Description = mergeStringField("Description", scraped.Description, nfo.Description, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Director = mergeStringField("Director", scraped.Director, nfo.Director, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Maker = mergeStringField("Maker", scraped.Maker, nfo.Maker, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Label = mergeStringField("Label", scraped.Label, nfo.Label, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Series = mergeStringField("Series", scraped.Series, nfo.Series, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.PosterURL = mergeStringField("PosterURL", scraped.PosterURL, nfo.PosterURL, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.CoverURL = mergeStringField("CoverURL", scraped.CoverURL, nfo.CoverURL, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	// CroppedPosterURL: Always use scraped value (not stored in NFO, runtime-generated temp URL)
	merged.CroppedPosterURL = scraped.CroppedPosterURL
	merged.TrailerURL = mergeStringField("TrailerURL", scraped.TrailerURL, nfo.TrailerURL, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.OriginalFileName = mergeStringField("OriginalFileName", scraped.OriginalFileName, nfo.OriginalFileName, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.SourceName = mergeStringField("SourceName", scraped.SourceName, nfo.SourceName, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.SourceURL = mergeStringField("SourceURL", scraped.SourceURL, nfo.SourceURL, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge int fields
	merged.ReleaseYear = mergeIntField("ReleaseYear", scraped.ReleaseYear, nfo.ReleaseYear, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Runtime = mergeIntField("Runtime", scraped.Runtime, nfo.Runtime, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.RatingVotes = mergeIntField("RatingVotes", scraped.RatingVotes, nfo.RatingVotes, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge float fields
	merged.RatingScore = mergeFloatField("RatingScore", scraped.RatingScore, nfo.RatingScore, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge bool fields
	merged.ShouldCropPoster = mergeBoolField("ShouldCropPoster", scraped.ShouldCropPoster, nfo.ShouldCropPoster, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge pointer fields
	merged.ReleaseDate = mergeDateField("ReleaseDate", scraped.ReleaseDate, nfo.ReleaseDate, scalarStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Merge array fields using mergeArrays flag
	arrayStrategy := scalarStrategy
	if mergeArrays {
		arrayStrategy = MergeArrays
	}
	merged.Actresses = mergeActresses("Actresses", scraped.Actresses, nfo.Actresses, arrayStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Genres = mergeGenres("Genres", scraped.Genres, nfo.Genres, arrayStrategy, &stats, provenance, scrapedTS, nfoTS)
	merged.Screenshots = mergeStringSlice("Screenshots", scraped.Screenshots, nfo.Screenshots, arrayStrategy, &stats, provenance, scrapedTS, nfoTS)

	// Timestamps
	merged.CreatedAt = scraped.CreatedAt
	if !nfo.CreatedAt.IsZero() && (scraped.CreatedAt.IsZero() || nfo.CreatedAt.After(scraped.CreatedAt)) {
		merged.CreatedAt = nfo.CreatedAt
	}
	merged.UpdatedAt = time.Now()

	stats.TotalFields = countNonEmptyFields(merged)

	return &MergeResult{
		Merged:     merged,
		Provenance: provenance,
		Stats:      stats,
	}, nil
}

// mergeStringField merges two string fields according to strategy
// fieldName: Field name for logging and critical field checking
// Uses scrapedTS when choosing scraper data, nfoTS when choosing NFO data
func mergeStringField(fieldName, scrapedVal, nfoVal string, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) string {
	scrapedEmpty := strings.TrimSpace(scrapedVal) == ""
	nfoEmpty := strings.TrimSpace(nfoVal) == ""

	// CRITICAL FIELD SAFETY VALVE: Never allow critical fields to be empty
	if criticalFields[fieldName] {
		if scrapedEmpty && nfoEmpty {
			// Both sources empty - this is a data integrity failure
			// Use Warn instead of Error to reduce noise in production logs
			logging.Warnf("Critical field %s is empty in both scraper and NFO - using fallback", fieldName)
			stats.EmptyFields++
			provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
			return "[Unknown " + fieldName + "]" // Last resort fallback
		}
		if scrapedEmpty {
			// Scraper empty but NFO has value - use NFO
			// Only log for non-strict strategies where this might be unexpected
			if strategy != PreferNFO {
				logging.Debugf("Critical field %s empty in scraper, using NFO value", fieldName)
			}
			stats.FromNFO++
			nfoTimestamp := nfoTS
			provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
			return nfoVal
		}
	}

	// Both empty
	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return ""
	}

	// Only one has data - handle differently for strict strategies
	if scrapedEmpty {
		// For PreferScraper (strict), use empty scraper value instead of falling back
		if strategy == PreferScraper {
			logging.Debugf("Using empty scraper value for %s (strategy: PreferScraper, strict mode)", fieldName)
			stats.FromScraper++
			scrapedTimestamp := scrapedTS
			provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
			return scrapedVal // Empty string
		}
		// Other strategies: fall back to NFO
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	}
	if nfoEmpty {
		// For PreferNFO (strict), use empty NFO value instead of falling back
		if strategy == PreferNFO {
			logging.Debugf("Using empty NFO value for %s (strategy: PreferNFO, strict mode)", fieldName)
			stats.FromNFO++
			nfoTimestamp := nfoTS
			provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
			return nfoVal // Empty string
		}
		// Other strategies: use scraper value
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}

	// Both have data - resolve conflict
	stats.ConflictsResolved++

	switch strategy {
	case PreferScraper:
		// Strict: always use scraper value, even if it means overwriting NFO
		logging.Debugf("Using scraper value for %s (strategy: PreferScraper)", fieldName)
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal

	case PreferNFO:
		// Strict: always use NFO value
		logging.Debugf("Using NFO value for %s (strategy: PreferNFO)", fieldName)
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal

	case PreserveExisting, FillMissingOnly:
		// Smart fallback: prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal

	case MergeArrays:
		// MergeArrays falls back to PreferScraper for strings
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal

	default:
		logging.Warnf("Unknown merge strategy %v for field %s, using scraper value", strategy, fieldName)
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}
}

// mergeIntField merges two int fields
// NOTE: Treats 0 as "empty" - this is intentional but means legitimate zero values
// are treated as absent. For fields where 0 is meaningful, consider using pointer types.
func mergeIntField(fieldName string, scrapedVal, nfoVal int, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) int {
	scrapedEmpty := scrapedVal == 0
	nfoEmpty := nfoVal == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return 0
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		// PreferNFO: Trust NFO over scraper (standard)
		// NOTE: Currently behave identically due to zero-value = missing semantics.
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	case PreferScraper, MergeArrays:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	default:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}
}

// mergeFloatField merges two float64 fields
// NOTE: Treats 0.0 as "empty" - this is intentional but means legitimate zero values
// are treated as absent. For fields where 0.0 is meaningful, consider using pointer types.
func mergeFloatField(fieldName string, scrapedVal, nfoVal float64, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) float64 {
	scrapedEmpty := scrapedVal == 0
	nfoEmpty := nfoVal == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return 0
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		// PreferNFO: Trust NFO over scraper (standard)
		// NOTE: Currently behave identically due to zero-value = missing semantics.
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	case PreferScraper, MergeArrays:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	default:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}
}

// mergeBoolField merges two bool fields
// NOTE: Treats false as "empty" - this is intentional but means explicit false values
// are treated as absent. For fields where false is meaningful, consider using pointer types.
func mergeBoolField(fieldName string, scrapedVal, nfoVal bool, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) bool {
	scrapedEmpty := !scrapedVal
	nfoEmpty := !nfoVal

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return false
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		// PreferNFO: Trust NFO over scraper (standard)
		// NOTE: Currently behave identically due to zero-value = missing semantics.
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	case PreferScraper, MergeArrays:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	default:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}
}

// mergeDateField merges two time.Time pointer fields
func mergeDateField(fieldName string, scrapedVal, nfoVal *time.Time, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) *time.Time {
	scrapedEmpty := scrapedVal == nil || scrapedVal.IsZero()
	nfoEmpty := nfoVal == nil || nfoVal.IsZero()

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}

	stats.ConflictsResolved++
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		// PreferNFO: Trust NFO over scraper (standard)
		// NOTE: Currently behave identically due to zero-value = missing semantics.
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfoVal
	case PreferScraper, MergeArrays:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	default:
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scrapedVal
	}
}

// mergeActresses merges actress slices
func mergeActresses(fieldName string, scraped, nfo []models.Actress, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) []models.Actress {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// For arrays: PreserveExisting/FillMissingOnly behave same as PreferNFO
		stats.FromNFO++
		stats.ConflictsResolved++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
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
		// Use the newer timestamp when merging arrays from both sources - create unique copy
		mergedTimestamp := scrapedTS
		if nfoTS.After(scrapedTS) {
			mergedTimestamp = nfoTS
		}
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &mergedTimestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}
}

// mergeGenres merges genre slices
func mergeGenres(fieldName string, scraped, nfo []models.Genre, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) []models.Genre {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// For arrays: PreserveExisting/FillMissingOnly behave same as PreferNFO
		stats.FromNFO++
		stats.ConflictsResolved++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
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
		// Use the newer timestamp when merging arrays from both sources - create unique copy
		mergedTimestamp := scrapedTS
		if nfoTS.After(scrapedTS) {
			mergedTimestamp = nfoTS
		}
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &mergedTimestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}
}

// mergeStringSlice merges string slices (screenshots, etc.)
func mergeStringSlice(fieldName string, scraped, nfo []string, strategy MergeStrategy, stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) []string {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		stats.EmptyFields++
		provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
		return nil
	}

	if scrapedEmpty {
		stats.FromNFO++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
		return nfo
	}
	if nfoEmpty {
		stats.FromScraper++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}

	// Both have data
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// All three strategies prefer existing NFO data when both sources have data
		// For arrays: PreserveExisting/FillMissingOnly behave same as PreferNFO
		stats.FromNFO++
		stats.ConflictsResolved++
		nfoTimestamp := nfoTS
		provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &nfoTimestamp}
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
		// Use the newer timestamp when merging arrays from both sources - create unique copy
		mergedTimestamp := scrapedTS
		if nfoTS.After(scrapedTS) {
			mergedTimestamp = nfoTS
		}
		provenance[fieldName] = DataSource{Source: "merged", Confidence: 0.9, LastUpdated: &mergedTimestamp}
		return merged
	default: // PreferScraper
		stats.FromScraper++
		stats.ConflictsResolved++
		scrapedTimestamp := scrapedTS
		provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &scrapedTimestamp}
		return scraped
	}
}

// actressKey creates a normalized unique key for deduplication
// Priority: JapaneseName (most consistent) → DMMID → FirstName+LastName (least reliable due to order variations)
func actressKey(actress models.Actress) string {
	// 1. JapaneseName is most consistent across scraped and NFO data
	//    Scraped data has DMMID but NFO typically doesn't, but both have JapaneseName
	japaneseName := strings.ToLower(strings.TrimSpace(actress.JapaneseName))
	if japaneseName != "" {
		return fmt.Sprintf("jp:%s", japaneseName)
	}

	// 2. DMMID is reliable but only present in scraped data (not in NFO)
	if actress.DMMID > 0 {
		return fmt.Sprintf("dmm:%d", actress.DMMID)
	}

	// 3. Fall back to normalized romanized FirstName + LastName (least reliable)
	firstName := strings.ToLower(strings.TrimSpace(actress.FirstName))
	lastName := strings.ToLower(strings.TrimSpace(actress.LastName))
	if firstName != "" || lastName != "" {
		return fmt.Sprintf("name:%s|%s", firstName, lastName)
	}

	// No identifying information
	return ""
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
			// Create a unique timestamp copy for each field to avoid pointer aliasing
			var fieldTimestamp *time.Time
			if timestamp != nil {
				ts := *timestamp
				fieldTimestamp = &ts
			}
			provenance[fieldName] = DataSource{
				Source:      source,
				Confidence:  1.0,
				LastUpdated: fieldTimestamp,
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
		// Guard against unexported fields which can't be accessed via Interface()
		if v.CanInterface() {
			if t, ok := v.Interface().(time.Time); ok {
				return t.IsZero()
			}
		}
		return false
	default:
		return false
	}
}
