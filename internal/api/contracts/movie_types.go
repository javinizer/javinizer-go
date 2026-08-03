package contracts

import "encoding/json"

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
	// Errors carries movie-level warnings on an otherwise-successful call
	// (e.g. a failed poster generation after a successful scrape) — parity
	// with ScrapeResponse.Errors.
	Errors []string `json:"errors,omitempty"`
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
	// PosterCropBoundsPresent records whether the raw body carried a
	// poster_crop_bounds key on the movie object. Cached or external clients
	// that predate the field omit it entirely; the batch edit handler must
	// preserve the stored bounds in that case instead of treating omission as
	// an explicit clear. Populated by UnmarshalJSON — never part of the wire
	// shape itself.
	PosterCropBoundsPresent bool `json:"-"`
}

// UnmarshalJSON decodes the request and records whether poster_crop_bounds
// was present on the movie object (probe pattern, cf.
// OutputConfig.UnmarshalJSON). Presence includes an explicit null — a
// deliberate clear; only a wholly absent key counts as omitted.
func (r *UpdateMovieRequest) UnmarshalJSON(data []byte) error {
	type updateMovieRequest UpdateMovieRequest
	var decoded updateMovieRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var probe struct {
		Movie map[string]json.RawMessage `json:"movie"`
	}
	// The same bytes already decoded successfully above, so this cannot fail;
	// a null or absent movie yields a nil map, which correctly reports the
	// field as absent.
	_ = json.Unmarshal(data, &probe)
	_, decoded.PosterCropBoundsPresent = probe.Movie["poster_crop_bounds"]

	*r = UpdateMovieRequest(decoded)
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
	// ExpectedSourceURL is the effective poster source (poster_url, else
	// cover_url) the client's crop coordinates were measured against. When
	// set, the server validates it UNDER the poster-source lock against the
	// current effective source and rejects a mismatch with 409 (stale
	// conflict) — this catches a cross-tab/device source swap that committed
	// BEFORE this request arrived, which the server's own pre/post-lock
	// source snapshots cannot see (both already name the new image). Empty
	// (older clients) falls back to the pre/post-lock guard alone.
	ExpectedSourceURL string `json:"expected_source_url,omitempty"`
}

// PosterCropResponse returns the updated temp cropped poster URL and the
// crop bounds actually stored server-side. Bounds are echoed verbatim from
// the persisted crop; a crop whose full-size source is gone (legacy job) is
// REJECTED with 400 before anything is written, never answered with
// deliberately dropped bounds, so clients may always overlay the returned
// bounds on success.
type PosterCropResponse struct {
	CroppedPosterURL string      `json:"cropped_poster_url"`
	PosterCropBounds *CropBounds `json:"poster_crop_bounds,omitempty"`
	// Original* revert-baseline fields echoed from the post-crop state: the
	// crop may have lazily STAMPED them (backupPosterOriginals) on a legacy
	// result that lacked a baseline. Clients must overlay these verbatim, or
	// a whole-movie Save issued before the next refetch resubmits empty
	// originals through UpdateMovie and destroys the reset target the crop
	// just created.
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
}

// PosterFromURLRequest represents a request to download a poster from a URL.
type PosterFromURLRequest struct {
	URL string `json:"url" binding:"required"`
}

// PosterFromURLResponse represents the result of downloading a poster from a URL.
type PosterFromURLResponse struct {
	CroppedPosterURL string `json:"cropped_poster_url"`
	PosterURL        string `json:"poster_url"`
	// ShouldCropPoster is the crop intent the server derived for the new
	// image (PosterEditor.cropIntentAfterPosterFromURL): the temp preview is
	// always auto-cropped, so clients MUST overlay this exact value or a later
	// whole-movie Save would resubmit a false that Organize treats as
	// deliberate, desyncing preview (cropped) from apply (uncropped).
	ShouldCropPoster bool `json:"should_crop_poster"`
	// Original* revert-baseline fields echoed from the post-update state —
	// same contract as PosterCropResponse: UpdatePosterFromURL may have
	// lazily stamped them (backupPosterOriginals) on a legacy result, and a
	// pre-refetch whole-movie Save must not resubmit empty originals.
	OriginalPosterURL        string `json:"original_poster_url"`
	OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
	OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
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
