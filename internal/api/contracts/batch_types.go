package contracts

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// BatchScrapeRequest represents a batch scrape request
type BatchScrapeRequest struct {
	Files            []string `json:"files" binding:"required"`
	Strict           bool     `json:"strict" example:"false"`
	Force            bool     `json:"force" example:"false"`
	Destination      string   `json:"destination,omitempty" example:"/path/to/output"` // Persisted on job for UI retrieval; required for organize mode, optional for in-place modes
	Update           bool     `json:"update" example:"false"`                          // Update mode: only create/update metadata files without moving video files
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
	Preset           string   `json:"preset,omitempty" example:"conservative"`        // Merge strategy preset: conservative, gap-fill, aggressive (overrides scalar/array strategies)
	ScalarStrategy   string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"` // For Update mode: prefer-nfo, prefer-scraper, preserve-existing, fill-missing-only
	ArrayStrategy    string   `json:"array_strategy,omitempty" example:"merge"`       // For Update mode: merge, replace
	OperationMode    string   `json:"operation_mode,omitempty" example:"organize"`    // Override config.output.operation_mode: organize, in-place, in-place-norenamefolder, metadata-artwork, preview
}

// BatchScrapeResponse represents batch scrape response
type BatchScrapeResponse struct {
	JobID string `json:"job_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// BatchFileResult represents a per-file result in a batch job response.
// Flattened from FileMatchInfo to match the frontend FileResult contract.
type BatchFileResult struct {
	ResultID       string            `json:"result_id"` // Stable UUID — survives movie_id changes
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	IsMultiPart    bool              `json:"is_multi_part"`
	PartNumber     int               `json:"part_number"`
	PartSuffix     string            `json:"part_suffix"`
	Status         models.JobStatus  `json:"status"`
	Error          string            `json:"error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	Movie          *MovieView        `json:"movie,omitempty"`
	StartedAt      string            `json:"started_at"`
	EndedAt        *string           `json:"ended_at,omitempty"`
}

// BatchFileResultSlim is a lightweight per-file result without movie data.
type BatchFileResultSlim struct {
	ResultID       string            `json:"result_id"` // Stable UUID — survives movie_id changes
	FilePath       string            `json:"file_path"`
	MovieID        string            `json:"movie_id"`
	IsMultiPart    bool              `json:"is_multi_part"`
	PartNumber     int               `json:"part_number"`
	PartSuffix     string            `json:"part_suffix"`
	Status         models.JobStatus  `json:"status"`
	Error          string            `json:"error,omitempty"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	StartedAt      string            `json:"started_at"`
	EndedAt        *string           `json:"ended_at,omitempty"`
}

// BatchJobResponse represents a batch job status
type BatchJobResponse struct {
	ID                    string                      `json:"id"`
	Status                models.JobStatus            `json:"status"`
	TotalFiles            int                         `json:"total_files"`
	Completed             int                         `json:"completed"`
	Failed                int                         `json:"failed"`
	OperationCount        int64                       `json:"operation_count"`
	RevertedCount         int64                       `json:"reverted_count"`
	Excluded              map[string]bool             `json:"excluded"`
	Progress              float64                     `json:"progress"`
	Destination           string                      `json:"destination"`
	Files                 []string                    `json:"files,omitempty"`
	Results               map[string]*BatchFileResult `json:"results"`
	StartedAt             string                      `json:"started_at"`
	CompletedAt           *string                     `json:"completed_at,omitempty"`
	OperationModeOverride operationmode.OperationMode `json:"operation_mode_override,omitempty"`
	Update                bool                        `json:"update"`
	PersistError          string                      `json:"persist_error,omitempty"`
}

// BatchJobResponseSlim is a lightweight batch job status response without movie Data.
type BatchJobResponseSlim struct {
	ID                    string                          `json:"id"`
	Status                models.JobStatus                `json:"status"`
	TotalFiles            int                             `json:"total_files"`
	Completed             int                             `json:"completed"`
	Failed                int                             `json:"failed"`
	Excluded              map[string]bool                 `json:"excluded"`
	Progress              float64                         `json:"progress"`
	Destination           string                          `json:"destination"`
	Files                 []string                        `json:"files,omitempty"`
	Results               map[string]*BatchFileResultSlim `json:"results"`
	StartedAt             string                          `json:"started_at"`
	CompletedAt           *string                         `json:"completed_at,omitempty"`
	OperationModeOverride operationmode.OperationMode     `json:"operation_mode_override,omitempty"`
	Update                bool                            `json:"update"`
	PersistError          string                          `json:"persist_error,omitempty"`
}

// BatchJobListResponse represents a paginated list of batch jobs.
type BatchJobListResponse struct {
	Jobs  []BatchJobResponse `json:"jobs"`
	Total int                `json:"total"` // Total number of jobs (before pagination), for pagination metadata
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
	Movie          *MovieView        `json:"movie"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
}

// BatchExcludeRequest represents a request to exclude multiple movies from a batch job
type BatchExcludeRequest struct {
	ResultIDs []string `json:"result_ids" binding:"required" example:"uuid-1,uuid-2"`
}

// BatchExcludeFailed represents a per-result failure during batch exclude
type BatchExcludeFailed struct {
	ResultID string `json:"result_id" example:"uuid-1"`
	Error    string `json:"error" example:"Result not found in job"`
}

// BatchExcludeResponse represents the result of a batch exclude operation
type BatchExcludeResponse struct {
	Excluded []string             `json:"excluded"`
	Failed   []BatchExcludeFailed `json:"failed"`
	Job      *BatchJobResponse    `json:"job"`
}

// BulkRescrapeRequest represents a request to rescrape multiple movies in a batch job
type BulkRescrapeRequest struct {
	MovieIDs         []string `json:"movie_ids" binding:"required" example:"IPX-535,ABC-123"`
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
	Force            bool     `json:"force" example:"false"`
	Preset           string   `json:"preset,omitempty" example:"conservative"`
	ScalarStrategy   string   `json:"scalar_strategy,omitempty" example:"prefer-nfo"`
	ArrayStrategy    string   `json:"array_strategy,omitempty" example:"merge"`
}

// BulkRescrapeMovieResult represents the per-movie result of a bulk rescrape operation
type BulkRescrapeMovieResult struct {
	MovieID string                `json:"movie_id" example:"IPX-535"`
	Status  models.RescrapeStatus `json:"status" example:"success"`
	Error   string                `json:"error,omitempty" example:"Movie not found in job"`
	Movie   *MovieView            `json:"movie,omitempty"`
}

// BulkRescrapeResponse represents the result of a bulk rescrape operation
type BulkRescrapeResponse struct {
	Results   []BulkRescrapeMovieResult `json:"results"`
	Succeeded int                       `json:"succeeded"`
	Failed    int                       `json:"failed"`
	Job       *BatchJobResponse         `json:"job"`
}

// UpdateRequest represents a batch update request.
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
	Destination   string `json:"destination" example:"/path/to/output"` // Required for organize mode; optional for in-place modes
	CopyOnly      bool   `json:"copy_only" example:"false"`
	LinkMode      string `json:"link_mode,omitempty" example:"hard"`          // Validated at the API layer (HTTP 400 for invalid)
	OperationMode string `json:"operation_mode,omitempty" example:"organize"` // Validated at the API layer (HTTP 400 for invalid)
	SkipNFO       bool   `json:"skip_nfo"`
	SkipDownload  bool   `json:"skip_download"`
}

// OrganizePreviewRequest represents an organize preview request.
type OrganizePreviewRequest struct {
	Destination   string     `json:"destination" example:"/path/to/output"` // Required for organize and preview modes; optional for in-place modes
	CopyOnly      bool       `json:"copy_only" example:"false"`
	LinkMode      string     `json:"link_mode,omitempty" example:"hard"`          // Validated at the API layer (HTTP 400 for invalid)
	OperationMode string     `json:"operation_mode,omitempty" example:"organize"` // Validated at the API layer (HTTP 400 for invalid)
	SkipNFO       bool       `json:"skip_nfo"`
	SkipDownload  bool       `json:"skip_download"`
	Movie         *MovieView `json:"movie,omitempty"` // Optional movie override for previewing unsaved edits
}

// OrganizePreviewResponse represents the expected output structure
type OrganizePreviewResponse struct {
	FolderName      string                      `json:"folder_name" example:"IPX-535 [IdeaPocket] - Beautiful Woman (2021)"`
	FileName        string                      `json:"file_name" example:"IPX-535"`
	SubfolderPath   string                      `json:"subfolder_path,omitempty" example:"IdeaPocket/2025"` // Subfolder hierarchy relative to destination (e.g. "Studio/Year")
	FullPath        string                      `json:"full_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.mp4"`
	VideoFiles      []string                    `json:"video_files,omitempty"`                                                                                  // For multi-part files: all video file paths
	NFOPath         string                      `json:"nfo_path,omitempty" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.nfo"` // Deprecated: Use NFOPaths instead. Remove in v2.0.
	NFOPaths        []string                    `json:"nfo_paths,omitempty"`                                                                                    // For per_file=true multi-part: all NFO file paths
	PosterPath      string                      `json:"poster_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-poster.jpg"`
	FanartPath      string                      `json:"fanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-fanart.jpg"`
	ExtrafanartPath string                      `json:"extrafanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/extrafanart"`
	TrailerPath     string                      `json:"trailer_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-trailer.mp4"`
	Screenshots     []string                    `json:"screenshots,omitempty" example:"fanart1.jpg,fanart2.jpg,fanart3.jpg"`
	SourcePath      string                      `json:"source_path,omitempty" example:"/source/folder/ABC-123.mp4"` // Original file path (for in-place modes)
	OperationMode   operationmode.OperationMode `json:"operation_mode,omitempty" example:"organize"`                // Which mode was used for preview
}
