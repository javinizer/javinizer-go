package nfo

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// mergeResult contains the merged movie and metadata about the merge
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

// mergeResultTuple is a generic (value, source) tuple returned by merge functions.
// The provenance recorder uses the source string to apply timestamps and write
// the provenance map, separating the merge decision from the recording side effect.

// provenanceRecorder owns the provenance map and applies source-specific timestamps.
// Merge functions return (value, source) tuples; the recorder writes provenance
// based on the source, separating the merge decision from the recording side effect.

// record writes provenance for a field based on the source returned by a merge function.

// recordConflict increments the conflict counter.

// fieldMerger owns the provenance map and merge stats, providing a unified merge method
// for both string and scalar field types. This eliminates duplication of provenance
// tracking across mergeStringField, mergeScalarField, and mergeSlice helpers.
type fieldMerger struct {
	provenance map[string]DataSource
	stats      *MergeStats
	scrapedTS  time.Time
	nfoTS      time.Time
}

// newFieldMerger creates a fieldMerger with the given stats pointer, provenance map,
// and source timestamps.
func newFieldMerger(stats *MergeStats, provenance map[string]DataSource, scrapedTS, nfoTS time.Time) *fieldMerger {
	return &fieldMerger{
		provenance: provenance,
		stats:      stats,
		scrapedTS:  scrapedTS,
		nfoTS:      nfoTS,
	}
}

// recordScraper records provenance for a field sourced from the scraper.
func (fm *fieldMerger) recordScraper(fieldName string) {
	fm.stats.FromScraper++
	ts := fm.scrapedTS
	fm.provenance[fieldName] = DataSource{Source: "scraper", Confidence: 1.0, LastUpdated: &ts}
}

// recordNFO records provenance for a field sourced from NFO.
func (fm *fieldMerger) recordNFO(fieldName string) {
	fm.stats.FromNFO++
	ts := fm.nfoTS
	fm.provenance[fieldName] = DataSource{Source: "nfo", Confidence: 1.0, LastUpdated: &ts}
}

// recordEmpty records provenance for an empty field.
func (fm *fieldMerger) recordEmpty(fieldName string) {
	fm.stats.EmptyFields++
	fm.provenance[fieldName] = DataSource{Source: "empty", Confidence: 0}
}

// recordMerged records provenance for a field merged from both sources.
func (fm *fieldMerger) recordMerged(fieldName string, confidence float64) {
	fm.stats.MergedArrays++
	ts := fm.scrapedTS
	if fm.nfoTS.After(fm.scrapedTS) {
		ts = fm.nfoTS
	}
	fm.provenance[fieldName] = DataSource{Source: "merged", Confidence: confidence, LastUpdated: &ts}
}

// recordConflict increments the conflict counter.
func (fm *fieldMerger) recordConflict() {
	fm.stats.ConflictsResolved++
}

// mergeString merges two string fields according to strategy.
// fieldName: Field name for logging and critical field checking
func (fm *fieldMerger) mergeString(fieldName, scrapedVal, nfoVal string, strategy MergeStrategy) string {
	return mergeStringField(fieldName, scrapedVal, nfoVal, strategy, fm)
}

// mergeScalarFieldViaMerger merges two scalar fields using fieldMerger's provenance and stats.
func mergeScalarFieldViaMerger[T comparable](fm *fieldMerger, fieldName string, scrapedVal, nfoVal T, strategy MergeStrategy, isEmpty func(T) bool) T {
	return mergeScalarField(fieldName, scrapedVal, nfoVal, strategy, fm, isEmpty)
}

// mergeSliceViaMerger merges two slices using fieldMerger's provenance and stats.

// stringMergeSpec defines a mergeable string field with its scraped/NFO getters and merged setter.
type stringMergeSpec struct {
	name string
	getS func(*models.Movie) string  // scraped getter
	getN func(*models.Movie) string  // nfo getter
	setM func(*models.Movie, string) // merged setter
}

// scalarMergeSpec defines a mergeable scalar field with its scraped/NFO getters, merged setter, and empty check.
type scalarMergeSpec[T comparable] struct {
	name    string
	getS    func(*models.Movie) T  // scraped getter
	getN    func(*models.Movie) T  // nfo getter
	setM    func(*models.Movie, T) // merged setter
	isEmpty func(T) bool           // empty check
}

// stringMergeSpecs lists all string fields to merge in MergeMovieMetadataWithOptions.
var stringMergeSpecs = []stringMergeSpec{
	{"ContentID", func(m *models.Movie) string { return m.ContentID }, func(m *models.Movie) string { return m.ContentID }, func(m *models.Movie, v string) { m.ContentID = v }},
	{"ID", func(m *models.Movie) string { return m.ID }, func(m *models.Movie) string { return m.ID }, func(m *models.Movie, v string) { m.ID = v }},
	{"DisplayTitle", func(m *models.Movie) string { return m.DisplayTitle }, func(m *models.Movie) string { return m.DisplayTitle }, func(m *models.Movie, v string) { m.DisplayTitle = v }},
	{"Title", func(m *models.Movie) string { return m.Title }, func(m *models.Movie) string { return m.Title }, func(m *models.Movie, v string) { m.Title = v }},
	{"OriginalTitle", func(m *models.Movie) string { return m.OriginalTitle }, func(m *models.Movie) string { return m.OriginalTitle }, func(m *models.Movie, v string) { m.OriginalTitle = v }},
	{"Description", func(m *models.Movie) string { return m.Description }, func(m *models.Movie) string { return m.Description }, func(m *models.Movie, v string) { m.Description = v }},
	{"Director", func(m *models.Movie) string { return m.Director }, func(m *models.Movie) string { return m.Director }, func(m *models.Movie, v string) { m.Director = v }},
	{"Maker", func(m *models.Movie) string { return m.Maker }, func(m *models.Movie) string { return m.Maker }, func(m *models.Movie, v string) { m.Maker = v }},
	{"Label", func(m *models.Movie) string { return m.Label }, func(m *models.Movie) string { return m.Label }, func(m *models.Movie, v string) { m.Label = v }},
	{"Series", func(m *models.Movie) string { return m.Series }, func(m *models.Movie) string { return m.Series }, func(m *models.Movie, v string) { m.Series = v }},
	{"PosterURL", func(m *models.Movie) string { return m.Poster.PosterURL }, func(m *models.Movie) string { return m.Poster.PosterURL }, func(m *models.Movie, v string) { m.Poster.PosterURL = v }},
	{"CoverURL", func(m *models.Movie) string { return m.Poster.CoverURL }, func(m *models.Movie) string { return m.Poster.CoverURL }, func(m *models.Movie, v string) { m.Poster.CoverURL = v }},
	{"TrailerURL", func(m *models.Movie) string { return m.TrailerURL }, func(m *models.Movie) string { return m.TrailerURL }, func(m *models.Movie, v string) { m.TrailerURL = v }},
	{"OriginalFileName", func(m *models.Movie) string { return m.OriginalFileName }, func(m *models.Movie) string { return m.OriginalFileName }, func(m *models.Movie, v string) { m.OriginalFileName = v }},
	{"SourceName", func(m *models.Movie) string { return m.SourceName }, func(m *models.Movie) string { return m.SourceName }, func(m *models.Movie, v string) { m.SourceName = v }},
	{"SourceURL", func(m *models.Movie) string { return m.SourceURL }, func(m *models.Movie) string { return m.SourceURL }, func(m *models.Movie, v string) { m.SourceURL = v }},
	{"CroppedPosterURL", func(m *models.Movie) string { return m.Poster.CroppedPosterURL }, func(m *models.Movie) string { return m.Poster.CroppedPosterURL }, func(m *models.Movie, v string) { m.Poster.CroppedPosterURL = v }},
	{"OriginalPosterURL", func(m *models.Movie) string { return m.Poster.OriginalPosterURL }, func(m *models.Movie) string { return m.Poster.OriginalPosterURL }, func(m *models.Movie, v string) { m.Poster.OriginalPosterURL = v }},
	{"OriginalCroppedPosterURL", func(m *models.Movie) string { return m.Poster.OriginalCroppedPosterURL }, func(m *models.Movie) string { return m.Poster.OriginalCroppedPosterURL }, func(m *models.Movie, v string) { m.Poster.OriginalCroppedPosterURL = v }},
}

// intMergeSpecs lists all int scalar fields to merge.
var intMergeSpecs = []scalarMergeSpec[int]{
	{"ReleaseYear", func(m *models.Movie) int { return m.ReleaseYear }, func(m *models.Movie) int { return m.ReleaseYear }, func(m *models.Movie, v int) { m.ReleaseYear = v }, func(v int) bool { return v == 0 }},
	{"Runtime", func(m *models.Movie) int { return m.Runtime }, func(m *models.Movie) int { return m.Runtime }, func(m *models.Movie, v int) { m.Runtime = v }, func(v int) bool { return v == 0 }},
	{"RatingVotes", func(m *models.Movie) int { return m.RatingVotes }, func(m *models.Movie) int { return m.RatingVotes }, func(m *models.Movie, v int) { m.RatingVotes = v }, func(v int) bool { return v == 0 }},
}

// float64MergeSpecs lists all float64 scalar fields to merge.
var float64MergeSpecs = []scalarMergeSpec[float64]{
	{"RatingScore", func(m *models.Movie) float64 { return m.RatingScore }, func(m *models.Movie) float64 { return m.RatingScore }, func(m *models.Movie, v float64) { m.RatingScore = v }, func(v float64) bool { return v == 0 }},
}

// boolMergeSpecs lists all bool scalar fields to merge.
var boolMergeSpecs = []scalarMergeSpec[bool]{
	{"ShouldCropPoster", func(m *models.Movie) bool { return m.Poster.ShouldCropPoster }, func(m *models.Movie) bool { return m.Poster.ShouldCropPoster }, func(m *models.Movie, v bool) { m.Poster.ShouldCropPoster = v }, func(v bool) bool { return !v }},
}

// boolPtrMergeSpecs lists all *bool scalar fields to merge.
var boolPtrMergeSpecs = []scalarMergeSpec[*bool]{
	{"OriginalShouldCropPoster", func(m *models.Movie) *bool { return m.Poster.OriginalShouldCropPoster }, func(m *models.Movie) *bool { return m.Poster.OriginalShouldCropPoster }, func(m *models.Movie, v *bool) { m.Poster.OriginalShouldCropPoster = v }, func(v *bool) bool { return v == nil || !*v }},
}

// sliceIsEmptySpec defines a slice/array field with an isEmpty predicate for provenance tracking.
type sliceIsEmptySpec struct {
	name    string
	isEmpty func(*models.Movie) bool
}

// sliceIsEmptySpecs lists all slice/array fields with their isEmpty predicates.
// These are used by isFieldEmptySpec to check emptiness of array fields
// without a fallback to the legacy switch.
var sliceIsEmptySpecs = []sliceIsEmptySpec{
	{"Actresses", func(m *models.Movie) bool { return len(m.Actresses) == 0 }},
	{"Genres", func(m *models.Movie) bool { return len(m.Genres) == 0 }},
	{"Screenshots", func(m *models.Movie) bool { return len(m.Screenshots) == 0 }},
	{"Translations", func(m *models.Movie) bool { return len(m.Translations) == 0 }},
}

// timePtrMergeSpecs lists all *time.Time scalar fields to merge.
var timePtrMergeSpecs = []scalarMergeSpec[*time.Time]{
	{"ReleaseDate", func(m *models.Movie) *time.Time { return m.ReleaseDate }, func(m *models.Movie) *time.Time { return m.ReleaseDate }, func(m *models.Movie, v *time.Time) { m.ReleaseDate = v }, func(v *time.Time) bool { return v == nil || v.IsZero() }},
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

	// Create a fieldMerger to centralize provenance and stats tracking
	fm := newFieldMerger(&stats, provenance, scrapedTS, nfoTS)

	// Merge string fields using spec-driven loop via fieldMerger
	for _, spec := range stringMergeSpecs {
		spec.setM(merged, fm.mergeString(spec.name, spec.getS(scraped), spec.getN(nfo), scalarStrategy))
	}

	// CroppedPosterURL: Always use scraped value (not stored in NFO, runtime-generated temp URL).
	// Record provenance inline so it stays consistent with the final value
	// (this field is intentionally excluded from stringMergeSpecs to avoid the
	// merged NFO value recording stale provenance before being overwritten).
	merged.Poster.CroppedPosterURL = scraped.Poster.CroppedPosterURL
	if strings.TrimSpace(scraped.Poster.CroppedPosterURL) == "" {
		fm.recordEmpty("CroppedPosterURL")
	} else {
		fm.recordScraper("CroppedPosterURL")
	}

	// Merge int scalar fields using spec-driven loop via fieldMerger
	for _, spec := range intMergeSpecs {
		spec.setM(merged, mergeScalarFieldViaMerger(fm, spec.name, spec.getS(scraped), spec.getN(nfo), scalarStrategy, spec.isEmpty))
	}

	// Merge float64 scalar fields
	for _, spec := range float64MergeSpecs {
		spec.setM(merged, mergeScalarFieldViaMerger(fm, spec.name, spec.getS(scraped), spec.getN(nfo), scalarStrategy, spec.isEmpty))
	}

	// Merge bool scalar fields
	for _, spec := range boolMergeSpecs {
		spec.setM(merged, mergeScalarFieldViaMerger(fm, spec.name, spec.getS(scraped), spec.getN(nfo), scalarStrategy, spec.isEmpty))
	}

	// Merge *time.Time scalar fields
	for _, spec := range timePtrMergeSpecs {
		spec.setM(merged, mergeScalarFieldViaMerger(fm, spec.name, spec.getS(scraped), spec.getN(nfo), scalarStrategy, spec.isEmpty))
	}

	// Merge array fields using mergeArrays flag
	arrayStrategy := scalarStrategy
	if mergeArrays {
		arrayStrategy = MergeArrays
	}
	merged.Actresses = mergeActresses("Actresses", scraped.Actresses, nfo.Actresses, arrayStrategy, fm)
	merged.Genres = mergeGenres("Genres", scraped.Genres, nfo.Genres, arrayStrategy, fm)
	merged.Screenshots = mergeStringSlice("Screenshots", scraped.Screenshots, nfo.Screenshots, arrayStrategy, fm)

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
func mergeStringField(fieldName, scrapedVal, nfoVal string, strategy MergeStrategy, fm *fieldMerger) string {
	scrapedEmpty := strings.TrimSpace(scrapedVal) == ""
	nfoEmpty := strings.TrimSpace(nfoVal) == ""

	// CRITICAL FIELD SAFETY VALVE: Never allow critical fields to be empty
	if criticalFields[fieldName] {
		if scrapedEmpty && nfoEmpty {
			// Both sources empty - this is a data integrity failure
			// Use Warn instead of Error to reduce noise in production logs
			logging.Warnf("Critical field %s is empty in both scraper and NFO - using fallback", fieldName)
			fm.recordEmpty(fieldName)
			return "[Unknown " + fieldName + "]" // Last resort fallback
		}
		if scrapedEmpty {
			// Scraper empty but NFO has value - use NFO
			// Only log for non-strict strategies where this might be unexpected
			if strategy != PreferNFO {
				logging.Debugf("Critical field %s empty in scraper, using NFO value", fieldName)
			}
			fm.recordNFO(fieldName)
			return nfoVal
		}
	}

	// Both empty
	if scrapedEmpty && nfoEmpty {
		fm.recordEmpty(fieldName)
		return ""
	}

	// Only one has data - handle differently for strict strategies
	if scrapedEmpty {
		// For PreferScraper (strict), use empty scraper value instead of falling back
		if strategy == PreferScraper {
			logging.Debugf("Using empty scraper value for %s (strategy: PreferScraper, strict mode)", fieldName)
			fm.recordScraper(fieldName)
			return scrapedVal // Empty string
		}
		// Other strategies: fall back to NFO
		fm.recordNFO(fieldName)
		return nfoVal
	}
	if nfoEmpty {
		// For PreferNFO (strict), use empty NFO value instead of falling back
		if strategy == PreferNFO {
			logging.Debugf("Using empty NFO value for %s (strategy: PreferNFO, strict mode)", fieldName)
			fm.recordNFO(fieldName)
			return nfoVal // Empty string
		}
		// Other strategies: use scraper value
		fm.recordScraper(fieldName)
		return scrapedVal
	}

	// Both have data - resolve conflict
	fm.recordConflict()

	switch strategy {
	case PreferScraper:
		// Strict: always use scraper value, even if it means overwriting NFO
		logging.Debugf("Using scraper value for %s (strategy: PreferScraper)", fieldName)
		fm.recordScraper(fieldName)
		return scrapedVal

	case PreferNFO:
		// Strict: always use NFO value
		logging.Debugf("Using NFO value for %s (strategy: PreferNFO)", fieldName)
		fm.recordNFO(fieldName)
		return nfoVal

	case PreserveExisting, FillMissingOnly:
		// Smart fallback: prefer existing NFO data when both sources have data
		// PreserveExisting: Never overwrite non-empty NFO fields (strictest)
		// FillMissingOnly: Only fill gaps, never replace existing (conservative)
		fm.recordNFO(fieldName)
		return nfoVal

	case MergeArrays:
		// MergeArrays falls back to PreferScraper for strings
		fm.recordScraper(fieldName)
		return scrapedVal

	default:
		logging.Warnf("Unknown merge strategy %v for field %s, using scraper value", strategy, fieldName)
		fm.recordScraper(fieldName)
		return scrapedVal
	}
}

// mergeScalarField merges two scalar fields using the given strategy and empty check.
func mergeScalarField[T comparable](fieldName string, scrapedVal, nfoVal T, strategy MergeStrategy, fm *fieldMerger, isEmpty func(T) bool) T {
	scrapedEmpty := isEmpty(scrapedVal)
	nfoEmpty := isEmpty(nfoVal)

	if scrapedEmpty && nfoEmpty {
		fm.recordEmpty(fieldName)
		var zero T
		return zero
	}

	if scrapedEmpty {
		fm.recordNFO(fieldName)
		return nfoVal
	}
	if nfoEmpty {
		fm.recordScraper(fieldName)
		return scrapedVal
	}

	fm.recordConflict()
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		fm.recordNFO(fieldName)
		return nfoVal
	case PreferScraper, MergeArrays:
		fm.recordScraper(fieldName)
		return scrapedVal
	default:
		fm.recordScraper(fieldName)
		return scrapedVal
	}
}

// mergeSlice merges two slices according to strategy.
// dedupKey returns a normalization key for deduplication (empty = skip).
func mergeSlice[T any](fieldName string, scraped, nfo []T, strategy MergeStrategy, fm *fieldMerger, dedupKey func(T) string) []T {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		fm.recordEmpty(fieldName)
		return nil
	}

	if scrapedEmpty {
		fm.recordNFO(fieldName)
		return nfo
	}
	if nfoEmpty {
		fm.recordScraper(fieldName)
		return scraped
	}

	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		fm.recordNFO(fieldName)
		fm.recordConflict()
		return nfo
	case MergeArrays:
		merged := make([]T, 0, len(scraped)+len(nfo))
		seen := make(map[string]bool)

		for _, item := range scraped {
			key := dedupKey(item)
			if key == "" {
				merged = append(merged, item)
			} else if !seen[key] {
				merged = append(merged, item)
				seen[key] = true
			}
		}
		for _, item := range nfo {
			key := dedupKey(item)
			if key == "" {
				merged = append(merged, item)
			} else if !seen[key] {
				merged = append(merged, item)
				seen[key] = true
			}
		}

		fm.recordMerged(fieldName, 0.9)
		return merged
	default:
		fm.recordScraper(fieldName)
		fm.recordConflict()
		return scraped
	}
}

// mergeGenres merges genre slices
func mergeGenres(fieldName string, scraped, nfo []models.Genre, strategy MergeStrategy, fm *fieldMerger) []models.Genre {
	return mergeSlice(fieldName, scraped, nfo, strategy, fm, func(g models.Genre) string {
		return strings.ToLower(strings.TrimSpace(g.Name))
	})
}

// mergeStringSlice merges string slices (screenshots, etc.)
func mergeStringSlice(fieldName string, scraped, nfo []string, strategy MergeStrategy, fm *fieldMerger) []string {
	return mergeSlice(fieldName, scraped, nfo, strategy, fm, func(s string) string {
		return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(s, "/")))
	})
}

// metadataFields lists all Movie fields tracked for provenance, excluding CreatedAt/UpdatedAt.
var metadataFields = []string{
	"ID", "ContentID", "DisplayTitle", "Title", "OriginalTitle",
	"Description", "ReleaseDate", "ReleaseYear", "Runtime",
	"Director", "Maker", "Label", "Series",
	"RatingScore", "RatingVotes",
	"PosterURL", "CoverURL", "CroppedPosterURL",
	"ShouldCropPoster", "OriginalPosterURL", "OriginalCroppedPosterURL", "OriginalShouldCropPoster",
	"TrailerURL", "OriginalFileName",
	"Actresses", "Genres", "Screenshots",
	"Translations",
	"SourceName", "SourceURL",
}

// isFieldEmptySpec checks if a field is empty using the spec tables (string, int,
// float64, bool, *bool, *time.Time, slice). Spec tables are the single source
// of truth — there is no fallback to a hand-maintained switch.
func isFieldEmptySpec(fieldName string, movie *models.Movie) bool {
	// Check string specs
	for _, spec := range stringMergeSpecs {
		if spec.name == fieldName {
			return strings.TrimSpace(spec.getS(movie)) == ""
		}
	}
	// Check int specs
	for _, spec := range intMergeSpecs {
		if spec.name == fieldName {
			return spec.isEmpty(spec.getS(movie))
		}
	}
	// Check float64 specs
	for _, spec := range float64MergeSpecs {
		if spec.name == fieldName {
			return spec.isEmpty(spec.getS(movie))
		}
	}
	// Check bool specs
	for _, spec := range boolMergeSpecs {
		if spec.name == fieldName {
			return spec.isEmpty(spec.getS(movie))
		}
	}
	// Check *bool specs
	for _, spec := range boolPtrMergeSpecs {
		if spec.name == fieldName {
			return spec.isEmpty(spec.getS(movie))
		}
	}
	// Check *time.Time specs
	for _, spec := range timePtrMergeSpecs {
		if spec.name == fieldName {
			return spec.isEmpty(spec.getS(movie))
		}
	}
	// Check slice/array specs
	for _, spec := range sliceIsEmptySpecs {
		if spec.name == fieldName {
			return spec.isEmpty(movie)
		}
	}
	// Unknown field — treat as empty
	return true
}

// makeProvenanceMap creates a provenance map for a single source.
func makeProvenanceMap(movie *models.Movie, source string) map[string]DataSource {
	provenance := make(map[string]DataSource)
	if movie == nil {
		return provenance
	}

	var timestamp *time.Time
	if !movie.UpdatedAt.IsZero() {
		timestamp = &movie.UpdatedAt
	} else if !movie.CreatedAt.IsZero() {
		timestamp = &movie.CreatedAt
	}

	for _, fieldName := range metadataFields {
		if !isFieldEmptySpec(fieldName, movie) {
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

// countNonEmptyFields counts non-empty fields in a movie.
func countNonEmptyFields(movie *models.Movie) int {
	if movie == nil {
		return 0
	}
	count := 0
	for _, fieldName := range metadataFields {
		if !isFieldEmptySpec(fieldName, movie) {
			count++
		}
	}
	return count
}
