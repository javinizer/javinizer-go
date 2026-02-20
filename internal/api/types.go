package api

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status   string   `json:"status" example:"ok"`
	Scrapers []string `json:"scrapers" example:"r18dev,dmm"`
}

// ScrapeRequest represents the scrape request payload
type ScrapeRequest struct {
	ID               string   `json:"id" binding:"required" example:"IPX-535"`
	Force            bool     `json:"force" example:"false"`
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
}

// ScrapeResponse represents the scrape response
type ScrapeResponse struct {
	Cached      bool          `json:"cached" example:"false"`
	Movie       *models.Movie `json:"movie"`
	SourcesUsed int           `json:"sources_used,omitempty" example:"2"`
	Errors      []string      `json:"errors,omitempty"`
}

// MovieResponse represents a movie response
type MovieResponse struct {
	Movie      *models.Movie         `json:"movie"`
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
	Movies []models.Movie `json:"movies"`
	Count  int            `json:"count" example:"20"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error  string   `json:"error" example:"Movie not found"`
	Errors []string `json:"errors,omitempty"`
}

// ScraperOption represents a configurable option for a scraper
type ScraperOption struct {
	Key         string          `json:"key" example:"scrape_actress"`
	Label       string          `json:"label" example:"Scrape Actress Information"`
	Description string          `json:"description" example:"Enable detailed actress data scraping from DMM (may be slower)"`
	Type        string          `json:"type" example:"boolean"` // boolean, string, number, select
	Min         *int            `json:"min,omitempty" example:"5"`
	Max         *int            `json:"max,omitempty" example:"120"`
	Unit        string          `json:"unit,omitempty" example:"seconds"`
	Choices     []ScraperChoice `json:"choices,omitempty"` // For select type: available choices
}

// ScraperChoice represents a choice for a select-type scraper option
type ScraperChoice struct {
	Value string `json:"value" example:"en"`
	Label string `json:"label" example:"English"`
}

// ScraperInfo represents information about a scraper
type ScraperInfo struct {
	Name        string          `json:"name" example:"r18dev"`
	DisplayName string          `json:"display_name" example:"R18.dev"`
	Enabled     bool            `json:"enabled" example:"true"`
	Options     []ScraperOption `json:"options,omitempty"`
}

// AvailableScrapersResponse represents the list of available scrapers
type AvailableScrapersResponse struct {
	Scrapers []ScraperInfo `json:"scrapers"`
}

// ProxyTestRequest represents a proxy connectivity test request.
type ProxyTestRequest struct {
	Mode      string             `json:"mode" binding:"required,oneof=direct flaresolverr"` // direct or flaresolverr
	Proxy     config.ProxyConfig `json:"proxy"`
	TargetURL string             `json:"target_url,omitempty"` // Optional override target URL
}

// ProxyTestResponse represents proxy connectivity test results.
type ProxyTestResponse struct {
	Success         bool   `json:"success"`
	Mode            string `json:"mode"`
	TargetURL       string `json:"target_url"`
	StatusCode      int    `json:"status_code,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	Message         string `json:"message"`
	ProxyURL        string `json:"proxy_url,omitempty"`        // Redacted proxy URL
	FlareSolverrURL string `json:"flaresolverr_url,omitempty"` // FlareSolverr endpoint used
}

// TranslationModelsRequest represents a request to fetch available translation models.
type TranslationModelsRequest struct {
	Provider string `json:"provider" binding:"required"` // openai (OpenAI-compatible only for now)
	BaseURL  string `json:"base_url" binding:"required"` // API base URL (e.g., https://api.openai.com/v1)
	APIKey   string `json:"api_key"`                     // Provider API key
}

// TranslationModelsResponse represents the model discovery response.
type TranslationModelsResponse struct {
	Models []string `json:"models"`
}

// ScanRequest represents a directory scan request
type ScanRequest struct {
	Path      string `json:"path" binding:"required" example:"/path/to/videos"`
	Recursive bool   `json:"recursive" example:"true"`
	Filter    string `json:"filter,omitempty" example:"STSK"` // Filter folder/file names (case-insensitive substring match)
}

// ScanResponse represents scan results
type ScanResponse struct {
	Files   []FileInfo `json:"files"`
	Count   int        `json:"count" example:"10"`
	Skipped []string   `json:"skipped,omitempty"`
}

// FileInfo represents file or directory information
type FileInfo struct {
	Name        string `json:"name" example:"video.mp4"`
	Path        string `json:"path" example:"/path/to/video.mp4"`
	IsDir       bool   `json:"is_dir" example:"false"`
	Size        int64  `json:"size" example:"1024000000"`
	ModTime     string `json:"mod_time" example:"2024-01-15T10:30:00Z"`
	MovieID     string `json:"movie_id,omitempty" example:"IPX-535"`
	Matched     bool   `json:"matched" example:"true"`
	IsMultiPart bool   `json:"is_multi_part,omitempty" example:"true"`
	PartNumber  int    `json:"part_number,omitempty" example:"1"`
	PartSuffix  string `json:"part_suffix,omitempty" example:"-pt1"`
}

// BatchScrapeRequest represents a batch scrape request
type BatchScrapeRequest struct {
	Files            []string `json:"files" binding:"required"`
	Strict           bool     `json:"strict" example:"false"`
	Force            bool     `json:"force" example:"false"`
	Destination      string   `json:"destination,omitempty" example:"/path/to/output"`
	Update           bool     `json:"update" example:"false"` // Update mode: only create/update metadata files without moving video files
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
	Preset           string   `json:"preset,omitempty" example:"conservative"`        // Merge strategy preset: conservative, gap-fill, aggressive (overrides scalar/array strategies)
	ScalarStrategy   string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"` // For Update mode: prefer-nfo, prefer-scraper, preserve-existing, fill-missing-only
	ArrayStrategy    string   `json:"array_strategy,omitempty" example:"merge"`       // For Update mode: merge, replace
}

// BatchScrapeResponse represents batch scrape response
type BatchScrapeResponse struct {
	JobID string `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// OrganizeRequest represents an organize request
type OrganizeRequest struct {
	Destination string `json:"destination" binding:"required" example:"/path/to/output"`
	CopyOnly    bool   `json:"copy_only" example:"false"`
}

// OrganizePreviewRequest represents a preview request
type OrganizePreviewRequest struct {
	Destination string `json:"destination" binding:"required" example:"/path/to/output"`
	CopyOnly    bool   `json:"copy_only" example:"false"`
}

// OrganizePreviewResponse represents the expected output structure
type OrganizePreviewResponse struct {
	FolderName      string   `json:"folder_name" example:"IPX-535 [IdeaPocket] - Beautiful Woman (2021)"`
	FileName        string   `json:"file_name" example:"IPX-535"`
	FullPath        string   `json:"full_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.mp4"`
	VideoFiles      []string `json:"video_files,omitempty"`                                                                        // For multi-part files: all video file paths
	NFOPath         string   `json:"nfo_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.nfo"` // Single NFO (backward compatibility)
	NFOPaths        []string `json:"nfo_paths,omitempty"`                                                                          // For per_file=true multi-part: all NFO file paths
	PosterPath      string   `json:"poster_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-poster.jpg"`
	FanartPath      string   `json:"fanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-fanart.jpg"`
	ExtrafanartPath string   `json:"extrafanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/extrafanart"`
	Screenshots     []string `json:"screenshots" example:"fanart1.jpg,fanart2.jpg,fanart3.jpg"`
}

// BatchFileResult wraps worker.FileResult with additional API-specific fields
type BatchFileResult struct {
	FilePath    string      `json:"file_path"`
	MovieID     string      `json:"movie_id"`
	Status      string      `json:"status"`
	Error       string      `json:"error,omitempty"`
	Data        interface{} `json:"data,omitempty"` // Movie data
	StartedAt   string      `json:"started_at"`
	EndedAt     *string     `json:"ended_at,omitempty"`
	IsMultiPart bool        `json:"is_multi_part,omitempty"`
	PartNumber  int         `json:"part_number,omitempty"`
	PartSuffix  string      `json:"part_suffix,omitempty"`
}

// BatchJobResponse represents a batch job status
type BatchJobResponse struct {
	ID          string                      `json:"id"`
	Status      string                      `json:"status"`
	TotalFiles  int                         `json:"total_files"`
	Completed   int                         `json:"completed"`
	Failed      int                         `json:"failed"`
	Excluded    map[string]bool             `json:"excluded"` // Files excluded from organization
	Progress    float64                     `json:"progress"`
	Results     map[string]*BatchFileResult `json:"results"`
	StartedAt   string                      `json:"started_at"`
	CompletedAt *string                     `json:"completed_at,omitempty"`
}

// BrowseRequest represents a browse request
type BrowseRequest struct {
	Path string `json:"path" example:"/path/to/directory"`
}

// BrowseResponse represents browse results
type BrowseResponse struct {
	CurrentPath string     `json:"current_path" example:"/path/to/directory"`
	ParentPath  string     `json:"parent_path,omitempty" example:"/path/to"`
	Items       []FileInfo `json:"items"`
}

// UpdateMovieRequest represents the update movie request payload
type UpdateMovieRequest struct {
	Movie *models.Movie `json:"movie" binding:"required"`
}

// PosterCropRequest represents manual poster crop coordinates in source-image pixels.
type PosterCropRequest struct {
	X      int `json:"x" binding:"min=0"`
	Y      int `json:"y" binding:"min=0"`
	Width  int `json:"width" binding:"min=1"`
	Height int `json:"height" binding:"min=1"`
}

// PosterCropResponse returns the updated temp cropped poster URL.
type PosterCropResponse struct {
	CroppedPosterURL string `json:"cropped_poster_url"`
}

// RescrapeRequest represents a request to rescrape with specific scrapers
type RescrapeRequest struct {
	SelectedScrapers []string `json:"selected_scrapers" binding:"required" example:"r18dev,dmm"`
	Force            bool     `json:"force" example:"false"`
}

// BatchRescrapeRequest represents a batch rescrape request for manual search/rescraping
type BatchRescrapeRequest struct {
	Force             bool     `json:"force" example:"false"`
	SelectedScrapers  []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
	ManualSearchInput string   `json:"manual_search_input,omitempty" example:"IPX-535"`
	Preset            string   `json:"preset,omitempty" example:"conservative"`        // Merge strategy preset: conservative, gap-fill, aggressive (overrides scalar/array strategies)
	ScalarStrategy    string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"` // For Update mode: prefer-nfo, prefer-scraper, preserve-existing, fill-missing-only
	ArrayStrategy     string   `json:"array_strategy,omitempty" example:"merge"`       // For Update mode: merge, replace
}

// BatchRescrapeResponse represents a batch rescrape response with movie
type BatchRescrapeResponse struct {
	Movie *models.Movie `json:"movie"`
}

// NFOComparisonRequest represents a request to compare NFO with scraped data
type NFOComparisonRequest struct {
	NFOPath          string   `json:"nfo_path,omitempty" example:"/path/to/movie.nfo"`   // Optional: explicit NFO path
	MergeStrategy    string   `json:"merge_strategy,omitempty" example:"prefer-scraper"` // Deprecated: prefer-scraper, prefer-nfo, merge-arrays (use preset or scalar/array strategies instead)
	Preset           string   `json:"preset,omitempty" example:"conservative"`           // Merge strategy preset: conservative, gap-fill, or aggressive (overrides scalar/array strategies)
	ScalarStrategy   string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"`    // Scalar field merge strategy: prefer-nfo, prefer-scraper, preserve-existing, or fill-missing-only
	ArrayStrategy    string   `json:"array_strategy,omitempty" example:"merge"`          // Array field merge strategy: merge or replace
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`  // Optional: custom scrapers for comparison
}

// NFOComparisonResponse represents the result of comparing NFO with scraped data
type NFOComparisonResponse struct {
	MovieID     string                `json:"movie_id" example:"IPX-535"`
	NFOExists   bool                  `json:"nfo_exists" example:"true"`
	NFOPath     string                `json:"nfo_path,omitempty" example:"movie.nfo"` // Returns filename only for security
	NFOData     *models.Movie         `json:"nfo_data,omitempty"`                     // Data from NFO file
	ScrapedData *models.Movie         `json:"scraped_data,omitempty"`                 // Fresh scraped data
	MergedData  *models.Movie         `json:"merged_data,omitempty"`                  // Result of merging
	Provenance  map[string]DataSource `json:"provenance,omitempty"`                   // Field-level provenance
	MergeStats  *MergeStatistics      `json:"merge_stats,omitempty"`                  // Merge statistics
	Differences []FieldDifference     `json:"differences,omitempty"`                  // List of fields that differ
}

// FieldDifference represents a difference between NFO and scraped data
type FieldDifference struct {
	Field        string      `json:"field" example:"title"`
	NFOValue     interface{} `json:"nfo_value,omitempty" example:"Beautiful Woman"`
	ScrapedValue interface{} `json:"scraped_value,omitempty" example:"Pretty Lady"`
	MergedValue  interface{} `json:"merged_value,omitempty" example:"Beautiful Woman"`
	Reason       string      `json:"reason,omitempty" example:"NFO preferred by merge strategy"`
}
