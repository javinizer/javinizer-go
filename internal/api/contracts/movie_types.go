package contracts

import (
	"encoding/json"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ScrapeRequest represents the scrape request payload
type ScrapeRequest struct {
	ID               string   `json:"id" binding:"required" example:"IPX-535"`
	Force            bool     `json:"force" example:"false"`
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
}

// ScrapeResponse represents the scrape response
type ScrapeResponse struct {
	Cached      bool       `json:"cached" example:"false"`
	Movie       *MovieView `json:"movie"`
	SourcesUsed int        `json:"sources_used,omitempty" example:"2"`
	Errors      []string   `json:"errors,omitempty"`
}

// MovieResponse represents a movie response
type MovieResponse struct {
	Movie      *MovieView            `json:"movie"`
	Provenance map[string]DataSource `json:"provenance,omitempty"`  // Field-level data source tracking
	MergeStats *MergeStatistics      `json:"merge_stats,omitempty"` // Merge statistics when NFO merging occurred
	// Revision is the fresh post-commit result revision (POSTER-WRITE-HARDENING D12).
	Revision *uint64 `json:"revision,omitempty"`
	// Revisions carries EVERY family part's fresh revision keyed by
	// result_id (multipart-safe CAS baselines, codex r26).
	Revisions map[string]uint64 `json:"revisions,omitempty"`
}

// DataSource represents the source of a metadata field
type DataSource struct {
	Source      string  `json:"source" example:"nfo"`                                  // "scraper" or "nfo"
	Confidence  float64 `json:"confidence" example:"0.9"`                              // Confidence score (0.0-1.0)
	LastUpdated *string `json:"last_updated,omitempty" example:"2024-01-15T10:30:00Z"` // ISO 8601 timestamp
}

// MergeStatistics represents statistics about a merge operation
type MergeStatistics struct {
	TotalFields       int `json:"total_fields" example:"15"`
	FromScraper       int `json:"from_scraper" example:"10"`
	FromNFO           int `json:"from_nfo" example:"3"`
	MergedArrays      int `json:"merged_arrays" example:"2"`
	ConflictsResolved int `json:"conflicts_resolved" example:"5"`
	EmptyFields       int `json:"empty_fields" example:"2"`
}

// MoviesResponse represents a list of movies response
type MoviesResponse struct {
	Movies []MovieView `json:"movies"`
	Count  int         `json:"count" example:"20"`
}

// UpdateMovieRequest represents the update movie request payload
type UpdateMovieRequest struct {
	Movie *MovieView `json:"movie" binding:"required"`
	// PosterCropBoundsFieldPresent records whether the movie payload contained
	// the poster_crop_bounds key (even as an explicit null). PATCH semantics
	// per the poster-crop-persistence contract: omitted preserves stored
	// geometry; explicit null clears it. Not part of the wire format.
	PosterCropBoundsFieldPresent bool `json:"-"`

	// ExpectedResultRevision (POSTER-WRITE-HARDENING D12): optional CAS
	// guard — when set, the save 409s if the target result's current
	// revision differs, so a stale-snapshot client cannot silently clobber a
	// newer committed edit.
	ExpectedResultRevision *uint64 `json:"expected_result_revision,omitempty"`
	// ExpectedResultRevisions: per-part CAS for multipart families (codex
	// r39) - EVERY listed result must currently sit at the mapped revision or
	// the whole save 409s before any write.
	ExpectedResultRevisions map[string]uint64 `json:"expected_result_revisions,omitempty"`
}

// UnmarshalJSON decodes the request while tracking presence of the
// poster_crop_bounds key on the nested movie object — required to
// distinguish an omitted field (preserve stored geometry) from an explicit
// null (clear geometry).
func (r *UpdateMovieRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Movie                   json.RawMessage   `json:"movie"`
		ExpectedResultRevision  *uint64           `json:"expected_result_revision"`
		ExpectedResultRevisions map[string]uint64 `json:"expected_result_revisions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.ExpectedResultRevision = raw.ExpectedResultRevision
	r.ExpectedResultRevisions = raw.ExpectedResultRevisions
	r.PosterCropBoundsFieldPresent = false
	if string(raw.Movie) == "null" || len(raw.Movie) == 0 {
		r.Movie = nil // binding:"required" reports this downstream
		return nil
	}
	var mv MovieView
	if err := json.Unmarshal(raw.Movie, &mv); err != nil {
		return err
	}
	r.Movie = &mv
	var keys map[string]json.RawMessage
	// raw.Movie decoded into a struct above, so it is a JSON object and the
	// map decode cannot fail; on the impossible failure the key reads absent
	// (the safe default: preserve stored geometry).
	_ = json.Unmarshal(raw.Movie, &keys)
	_, r.PosterCropBoundsFieldPresent = keys["poster_crop_bounds"]
	return nil
}

// PosterCropRequest represents manual poster crop coordinates in source-image pixels.
type PosterCropRequest struct {
	X      int `json:"x" binding:"min=0"`
	Y      int `json:"y" binding:"min=0"`
	Width  int `json:"width" binding:"min=1"`
	Height int `json:"height" binding:"min=1"`
	// MaxPosterHeight optional override for the max poster height (px). 0 = no cap.
	// When omitted, the configured output.max_poster_height is used.
	MaxPosterHeight *int `json:"max_poster_height,omitempty" binding:"omitempty,min=0"`
	// ExpectedPosterRevision/Fingerprint bind the crop camera to the exact
	// installed full-size bytes measured by the review client. Both are
	// optional only for legacy clients; if either is supplied, both are
	// required and validated.
	ExpectedPosterRevision    *uint64 `json:"expected_poster_revision,omitempty"`
	ExpectedPosterFingerprint string  `json:"expected_poster_fingerprint,omitempty"`
}

// PosterCropResponse returns the updated temp cropped poster URL plus the
// effective normalized crop geometry (fractions of the full-size source),
// or null when the crop was measured against a legacy already-cropped
// preview and no applyable geometry exists.
type PosterCropResponse struct {
	CroppedPosterURL string             `json:"cropped_poster_url"`
	PosterCropBounds *models.CropBounds `json:"poster_crop_bounds"`
	// ShouldCropPoster echoes the stored crop intent after a manual crop
	// (always false: the manual crop replaces the scraper auto-crop), so
	// clients can sync their pending-edits overlay from this response alone.
	ShouldCropPoster bool `json:"should_crop_poster"`
	// PosterCropSourceFull echoes whether the bounds were measured against the
	// full-size source. Clients must round-trip it with the bounds: the apply
	// gate refuses geometry without it.
	PosterCropSourceFull bool `json:"poster_crop_source_full"`
	// Original poster fields echo the server-side pre-edit snapshot
	// (backupPosterOriginals), so the client reset baseline never has to guess.
	// Empty when no snapshot was needed (no prior poster).
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
	// Revision is the revision AFTER the crop commit (D12).
	Revision *uint64 `json:"revision,omitempty"`
	// Revisions carries EVERY family part's fresh revision keyed by
	// result_id.
	Revisions map[string]uint64 `json:"revisions,omitempty"`
	// PosterRevision/Fingerprint identify the installed full-size bytes.
	PosterRevision    *uint64 `json:"poster_revision,omitempty"`
	PosterFingerprint string  `json:"poster_fingerprint,omitempty"`
}

// PosterFromURLRequest represents a request to download a poster from a URL.
type PosterFromURLRequest struct {
	URL string `json:"url" binding:"required"`
}

// PosterFromURLResponse represents the result of downloading a poster from a URL.
type PosterFromURLResponse struct {
	CroppedPosterURL string `json:"cropped_poster_url"`
	PosterURL        string `json:"poster_url"`
	// Revision is the revision AFTER the from-URL commit (D12).
	Revision *uint64 `json:"revision,omitempty"`
	// Revisions carries EVERY family part's fresh revision keyed by
	// result_id.
	Revisions map[string]uint64 `json:"revisions,omitempty"`
	// PosterRevision/Fingerprint identify the installed full-size bytes.
	PosterRevision    *uint64 `json:"poster_revision,omitempty"`
	PosterFingerprint string  `json:"poster_fingerprint,omitempty"`
}

// NFOComparisonRequest represents a request to compare NFO with scraped data
type NFOComparisonRequest struct {
	NFOPath          string   `json:"nfo_path,omitempty" example:"/path/to/movie.nfo"`  // Required: explicit NFO path
	Preset           string   `json:"preset,omitempty" example:"conservative"`          // Merge strategy preset: conservative, gap-fill, or aggressive (overrides scalar/array strategies)
	ScalarStrategy   string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"`   // Scalar field merge strategy: prefer-nfo, prefer-scraper, preserve-existing, or fill-missing-only
	ArrayStrategy    string   `json:"array_strategy,omitempty" example:"merge"`         // Array field merge strategy: merge or replace
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"` // Optional: custom scrapers for comparison
}

// NFOComparisonResponse represents the result of comparing NFO with scraped data
type NFOComparisonResponse struct {
	MovieID     string                `json:"movie_id" example:"IPX-535"`
	NFOExists   bool                  `json:"nfo_exists" example:"true"`
	NFOPath     string                `json:"nfo_path,omitempty" example:"movie.nfo"` // Returns filename only for security
	NFOData     *MovieView            `json:"nfo_data,omitempty"`                     // Data from NFO file
	ScrapedData *MovieView            `json:"scraped_data,omitempty"`                 // Fresh scraped data
	MergedData  *MovieView            `json:"merged_data,omitempty"`                  // Result of merging
	Provenance  map[string]DataSource `json:"provenance,omitempty"`                   // Field-level provenance
	MergeStats  *MergeStatistics      `json:"merge_stats,omitempty"`                  // Merge statistics
	Differences []FieldDifference     `json:"differences,omitempty"`                  // List of fields that differ
}

// FieldDifference represents a difference between NFO and scraped data
type FieldDifference struct {
	Field        string `json:"field" example:"title"`
	NFOValue     any    `json:"nfo_value,omitempty"`
	ScrapedValue any    `json:"scraped_value,omitempty"`
	MergedValue  any    `json:"merged_value,omitempty"`
	Reason       string `json:"reason,omitempty" example:"NFO preferred by merge strategy"`
}
