package contracts

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
}

// PosterCropResponse returns the updated temp cropped poster URL.
type PosterCropResponse struct {
	CroppedPosterURL string `json:"cropped_poster_url"`
}

// PosterFromURLRequest represents a request to download a poster from a URL.
type PosterFromURLRequest struct {
	URL string `json:"url" binding:"required"`
}

// PosterFromURLResponse represents the result of downloading a poster from a URL.
type PosterFromURLResponse struct {
	CroppedPosterURL string `json:"cropped_poster_url"`
	PosterURL        string `json:"poster_url"`
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
