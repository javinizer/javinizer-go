package contracts

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string   `json:"status" example:"ok"`
	Scrapers  []string `json:"scrapers" example:"r18dev,dmm"`
	Version   string   `json:"version" example:"v1.2.3"`
	Commit    string   `json:"commit" example:"abc123def456"`
	BuildDate string   `json:"build_date" example:"2026-02-23T00:00:00Z"`
}

// AuthStatusResponse represents authentication state for first-run/login gating.
type AuthStatusResponse struct {
	Initialized   bool   `json:"initialized" example:"true"`
	Authenticated bool   `json:"authenticated" example:"false"`
	Username      string `json:"username,omitempty" example:"admin"`
}

// AuthCredentialsRequest represents username/password login/setup payload.
type AuthCredentialsRequest struct {
	Username   string `json:"username" binding:"required" example:"admin"`
	Password   string `json:"password" binding:"required" example:"your-password"`
	RememberMe bool   `json:"remember_me,omitempty" example:"true"`
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

// ScraperOption is an alias for models.ScraperOption
type ScraperOption = models.ScraperOption

// ScraperChoice is an alias for models.ScraperChoice
type ScraperChoice = models.ScraperChoice

// ScraperInfo represents information about a scraper
type ScraperInfo struct {
	Name         string          `json:"name" example:"r18dev"`
	DisplayTitle string          `json:"display_title" example:"R18.dev"`
	Enabled      bool            `json:"enabled" example:"true"`
	Options      []ScraperOption `json:"options,omitempty"`
}

// AvailableScrapersResponse represents the list of available scrapers
type AvailableScrapersResponse struct {
	Scrapers []ScraperInfo `json:"scrapers"`
}

// ProxyTestRequest represents a proxy connectivity test request.
type ProxyTestRequest struct {
	Mode         string                    `json:"mode" binding:"required,oneof=direct flaresolverr"` // direct or flaresolverr
	Proxy        config.ProxyConfig        `json:"proxy"`
	FlareSolverr config.FlareSolverrConfig `json:"flaresolverr"`         // FlareSolverr config (separate from ProxyConfig)
	TargetURL    string                    `json:"target_url,omitempty"` // Optional override target URL
}

// ProxyTestResponse represents proxy connectivity test results.
type ProxyTestResponse struct {
	Success           bool   `json:"success"`
	Mode              string `json:"mode"`
	TargetURL         string `json:"target_url"`
	StatusCode        int    `json:"status_code,omitempty"`
	DurationMS        int64  `json:"duration_ms"`
	Message           string `json:"message"`
	ProxyURL          string `json:"proxy_url,omitempty"`          // Redacted proxy URL
	FlareSolverrURL   string `json:"flaresolverr_url,omitempty"`   // FlareSolverr endpoint used
	VerificationToken string `json:"verification_token,omitempty"` // Token for save authorization
	TokenExpiresAt    int64  `json:"token_expires_at,omitempty"`   // Unix timestamp when token expires
}

// UpdateConfigRequest represents a configuration update request with proxy verification.
// The proxy_verification_tokens map contains tokens keyed by scope ("global", "flaresolverr", or "profile:{name}")
// that prove the proxy settings were tested before saving.
type UpdateConfigRequest struct {
	config.Config
	ProxyVerificationTokens map[string]string `json:"proxy_verification_tokens,omitempty"`
}
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
	Files               []string `json:"files" binding:"required"`
	Strict              bool     `json:"strict" example:"false"`
	Force               bool     `json:"force" example:"false"`
	Destination         string   `json:"destination,omitempty" example:"/path/to/output"`
	Update              bool     `json:"update" example:"false"` // Update mode: only create/update metadata files without moving video files
	SelectedScrapers    []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
	Preset              string   `json:"preset,omitempty" example:"conservative"`          // Merge strategy preset: conservative, gap-fill, aggressive (overrides scalar/array strategies)
	ScalarStrategy      string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"`   // For Update mode: prefer-nfo, prefer-scraper, preserve-existing, fill-missing-only
	ArrayStrategy       string   `json:"array_strategy,omitempty" example:"merge"`         // For Update mode: merge, replace
	MoveToFolder        *bool    `json:"move_to_folder,omitempty" example:"true"`          // Override config.output.move_to_folder
	RenameFolderInPlace *bool    `json:"rename_folder_in_place,omitempty" example:"false"` // Override config.output.rename_folder_in_place
	OperationMode       string   `json:"operation_mode,omitempty" example:"organize"`      // Override config.output.operation_mode: organize, in-place, in-place-norenamefolder, metadata-only, preview
}

// BatchScrapeResponse represents batch scrape response
type BatchScrapeResponse struct {
	JobID string `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

type UpdateRequest struct {
	ForceOverwrite bool   `json:"force_overwrite"`
	PreserveNFO    bool   `json:"preserve_nfo"`
	Preset         string `json:"preset,omitempty" binding:"omitempty,oneof=conservative gap-fill aggressive"`
	ScalarStrategy string `json:"scalar_strategy,omitempty" binding:"omitempty,oneof=prefer-scraper prefer-nfo preserve-existing fill-missing-only"`
	ArrayStrategy  string `json:"array_strategy,omitempty" binding:"omitempty,oneof=merge replace"`
	SkipNFO        bool   `json:"skip_nfo"`
	SkipDownload   bool   `json:"skip_download"`
}

// OrganizeRequest represents an organize request
type OrganizeRequest struct {
	Destination   string `json:"destination" binding:"required" example:"/path/to/output"`
	CopyOnly      bool   `json:"copy_only" example:"false"`
	LinkMode      string `json:"link_mode,omitempty" binding:"omitempty,oneof=hard soft" example:"hard"`
	OperationMode string `json:"operation_mode,omitempty" example:"organize"`
	SkipNFO       bool   `json:"skip_nfo"`
	SkipDownload  bool   `json:"skip_download"`
}

// OrganizePreviewRequest represents a preview request
type OrganizePreviewRequest struct {
	Destination   string `json:"destination" binding:"required" example:"/path/to/output"`
	CopyOnly      bool   `json:"copy_only" example:"false"`
	LinkMode      string `json:"link_mode,omitempty" binding:"omitempty,oneof=hard soft" example:"hard"`
	OperationMode string `json:"operation_mode,omitempty" example:"organize"`
	SkipNFO       bool   `json:"skip_nfo"`
	SkipDownload  bool   `json:"skip_download"`
}

// OrganizePreviewResponse represents the expected output structure
type OrganizePreviewResponse struct {
	FolderName      string   `json:"folder_name" example:"IPX-535 [IdeaPocket] - Beautiful Woman (2021)"`
	FileName        string   `json:"file_name" example:"IPX-535"`
	SubfolderPath   string   `json:"subfolder_path,omitempty" example:"IdeaPocket/2025"` // Subfolder hierarchy relative to destination (e.g. "Studio/Year")
	FullPath        string   `json:"full_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.mp4"`
	VideoFiles      []string `json:"video_files,omitempty"`                                                                                  // For multi-part files: all video file paths
	NFOPath         string   `json:"nfo_path,omitempty" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.nfo"` // Single NFO (backward compatibility)
	NFOPaths        []string `json:"nfo_paths,omitempty"`                                                                                    // For per_file=true multi-part: all NFO file paths
	PosterPath      string   `json:"poster_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-poster.jpg"`
	FanartPath      string   `json:"fanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-fanart.jpg"`
	ExtrafanartPath string   `json:"extrafanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/extrafanart"`
	Screenshots     []string `json:"screenshots,omitempty" example:"fanart1.jpg,fanart2.jpg,fanart3.jpg"`
	SourcePath      string   `json:"source_path,omitempty" example:"/source/folder/ABC-123.mp4"` // Original file path (for in-place modes)
	OperationMode   string   `json:"operation_mode,omitempty" example:"organize"`                // Which mode was used for preview
}

// BatchFileResult wraps worker.FileResult with additional API-specific fields
type BatchFileResult struct {
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`   // Field-level source by scraper/NFO
	ActressSources map[string]string `json:"actress_sources,omitempty"` // Actress-level source by scraper/NFO
	Data           interface{}       `json:"data,omitempty"`            // Movie data
	StartedAt      string            `json:"started_at"`
	EndedAt        *string           `json:"ended_at,omitempty"`
	IsMultiPart    bool              `json:"is_multi_part,omitempty"`
	PartNumber     int               `json:"part_number,omitempty"`
	PartSuffix     string            `json:"part_suffix,omitempty"`
}

// BatchFileResultSlim is a lightweight BatchFileResult without the Data field
// for efficient status polling.
type BatchFileResultSlim struct {
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	StartedAt      string            `json:"started_at"`
	EndedAt        *string           `json:"ended_at,omitempty"`
	IsMultiPart    bool              `json:"is_multi_part,omitempty"`
	PartNumber     int               `json:"part_number,omitempty"`
	PartSuffix     string            `json:"part_suffix,omitempty"`
}

// BatchJobResponse represents a batch job status
type BatchJobResponse struct {
	ID                    string                      `json:"id"`
	Status                string                      `json:"status"`
	TotalFiles            int                         `json:"total_files"`
	Completed             int                         `json:"completed"`
	Failed                int                         `json:"failed"`
	OperationCount        int64                       `json:"operation_count"`
	RevertedCount         int64                       `json:"reverted_count"`
	Excluded              map[string]bool             `json:"excluded"`
	Progress              float64                     `json:"progress"`
	Destination           string                      `json:"destination"`
	Results               map[string]*BatchFileResult `json:"results"`
	StartedAt             string                      `json:"started_at"`
	CompletedAt           *string                     `json:"completed_at,omitempty"`
	OperationModeOverride string                      `json:"operation_mode_override,omitempty"`
	PersistError          string                      `json:"persist_error,omitempty"`
}

// BatchJobResponseSlim is a lightweight batch job status response without movie Data.
type BatchJobResponseSlim struct {
	ID                    string                          `json:"id"`
	Status                string                          `json:"status"`
	TotalFiles            int                             `json:"total_files"`
	Completed             int                             `json:"completed"`
	Failed                int                             `json:"failed"`
	Excluded              map[string]bool                 `json:"excluded"`
	Progress              float64                         `json:"progress"`
	Destination           string                          `json:"destination"`
	Results               map[string]*BatchFileResultSlim `json:"results"`
	StartedAt             string                          `json:"started_at"`
	CompletedAt           *string                         `json:"completed_at,omitempty"`
	OperationModeOverride string                          `json:"operation_mode_override,omitempty"`
	PersistError          string                          `json:"persist_error,omitempty"`
}

type BatchJobListResponse struct {
	Jobs []BatchJobResponse `json:"jobs"`
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

// PathAutocompleteRequest represents a partial path autocomplete request.
type PathAutocompleteRequest struct {
	Path  string `json:"path" binding:"required" example:"/path/to/vid"`
	Limit int    `json:"limit,omitempty" example:"10"`
}

// PathAutocompleteSuggestion represents a single autocomplete suggestion.
type PathAutocompleteSuggestion struct {
	Name  string `json:"name" example:"videos"`
	Path  string `json:"path" example:"/path/to/videos"`
	IsDir bool   `json:"is_dir" example:"true"`
}

// PathAutocompleteResponse represents directory suggestions for a partial path.
type PathAutocompleteResponse struct {
	InputPath   string                       `json:"input_path" example:"/path/to/vid"`
	BasePath    string                       `json:"base_path" example:"/path/to"`
	Suggestions []PathAutocompleteSuggestion `json:"suggestions"`
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
	Movie          *models.Movie     `json:"movie"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
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
	NFOValue     interface{} `json:"nfo_value,omitempty"`
	ScrapedValue interface{} `json:"scraped_value,omitempty"`
	MergedValue  interface{} `json:"merged_value,omitempty"`
	Reason       string      `json:"reason,omitempty" example:"NFO preferred by merge strategy"`
}

// ActressMergePreviewRequest represents a merge preview request for two actresses.
type ActressMergePreviewRequest struct {
	TargetID uint `json:"target_id" binding:"required" example:"12"`
	SourceID uint `json:"source_id" binding:"required" example:"34"`
}

// ActressMergeConflict represents a conflicting field between target and source actress.
type ActressMergeConflict struct {
	Field             string      `json:"field" example:"japanese_name"`
	TargetValue       interface{} `json:"target_value,omitempty"`
	SourceValue       interface{} `json:"source_value,omitempty"`
	DefaultResolution string      `json:"default_resolution" example:"target"`
}

// ActressMergePreviewResponse represents a preview of an actress merge operation.
type ActressMergePreviewResponse struct {
	Target             models.Actress         `json:"target"`
	Source             models.Actress         `json:"source"`
	ProposedMerged     models.Actress         `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict `json:"conflicts"`
	DefaultResolutions map[string]string      `json:"default_resolutions"`
}

// ActressMergeRequest represents a merge request with conflict resolutions.
type ActressMergeRequest struct {
	TargetID    uint              `json:"target_id" binding:"required" example:"12"`
	SourceID    uint              `json:"source_id" binding:"required" example:"34"`
	Resolutions map[string]string `json:"resolutions,omitempty" example:"japanese_name:source,dmm_id:target"`
}

// ActressMergeResponse represents a completed actress merge operation.
type ActressMergeResponse struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id" example:"34"`
	UpdatedMovies     int            `json:"updated_movies" example:"27"`
	ConflictsResolved int            `json:"conflicts_resolved" example:"3"`
	AliasesAdded      int            `json:"aliases_added" example:"5"`
}

// JobListItem represents a job in the history-oriented listing (HIST-01)
type JobListItem struct {
	ID             string  `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status         string  `json:"status" example:"organized"`
	TotalFiles     int     `json:"total_files" example:"10"`
	Completed      int     `json:"completed" example:"9"`
	Failed         int     `json:"failed" example:"1"`
	OperationCount int64   `json:"operation_count" example:"10"`
	RevertedCount  int64   `json:"reverted_count,omitempty" example:"7"`
	Progress       float64 `json:"progress" example:"0.9"`
	Destination    string  `json:"destination" example:"/path/to/output"`
	StartedAt      string  `json:"started_at" example:"2026-04-12T10:00:00Z"`
	CompletedAt    *string `json:"completed_at,omitempty" example:"2026-04-12T10:05:00Z"`
	OrganizedAt    *string `json:"organized_at,omitempty" example:"2026-04-12T10:05:00Z"`
	RevertedAt     *string `json:"reverted_at,omitempty" example:"2026-04-12T11:00:00Z"`
}

// JobListResponse is the response for listing jobs
type JobListResponse struct {
	Jobs []JobListItem `json:"jobs"`
}

// OperationItem represents a single BatchFileOperation in API responses (HIST-02)
type OperationItem struct {
	ID             uint    `json:"id" example:"1"`
	MovieID        string  `json:"movie_id" example:"ABC-123"`
	OriginalPath   string  `json:"original_path" example:"/source/ABC-123.mp4"`
	NewPath        string  `json:"new_path" example:"/dest/ABC-123 [Studio]/ABC-123.mp4"`
	OperationType  string  `json:"operation_type" example:"move"`
	RevertStatus   string  `json:"revert_status" example:"pending"`
	RevertedAt     *string `json:"reverted_at,omitempty" example:"2026-04-12T11:00:00Z"`
	InPlaceRenamed bool    `json:"in_place_renamed" example:"false"`
	CreatedAt      string  `json:"created_at" example:"2026-04-12T10:05:00Z"`
}

// OperationListResponse is the response for listing operations for a job
type OperationListResponse struct {
	JobID      string          `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	JobStatus  string          `json:"job_status" example:"organized"`
	Operations []OperationItem `json:"operations"`
	Total      int64           `json:"total" example:"10"`
}

// RevertResultResponse represents the result of a revert operation
type RevertResultResponse struct {
	JobID     string            `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status    string            `json:"status" example:"reverted"`
	Total     int               `json:"total" example:"10"`
	Succeeded int               `json:"succeeded" example:"9"`
	Skipped   int               `json:"skipped" example:"1"`
	Failed    int               `json:"failed" example:"1"`
	Errors    []RevertFileError `json:"errors,omitempty"`
}

// RevertFileError represents a per-file result during revert (includes skipped and failed)
type RevertFileError struct {
	OperationID  uint   `json:"operation_id" example:"5"`
	MovieID      string `json:"movie_id" example:"ABC-123"`
	OriginalPath string `json:"original_path" example:"/source/ABC-123.mp4"`
	NewPath      string `json:"new_path" example:"/dest/ABC-123 [Studio]/ABC-123.mp4"`
	Error        string `json:"error" example:"file not found"`
	Outcome      string `json:"outcome,omitempty" example:"skipped"`
	Reason       string `json:"reason,omitempty" example:"anchor_missing"`
}

// RevertCheckResponse represents overlap detection for a batch revert (D-07)
type RevertCheckResponse struct {
	JobID              string        `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	OverlappingBatches []OverlapInfo `json:"overlapping_batches"`
}

// OverlapInfo represents a later batch with path overlaps (D-07)
type OverlapInfo struct {
	JobID          string `json:"job_id" example:"660e8400-e29b-41d4-a716-446655440001"`
	CreatedAt      string `json:"created_at" example:"2026-04-12T12:00:00Z"`
	OperationCount int    `json:"operation_count" example:"3"`
}
