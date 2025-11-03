package api

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
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
	Movie *models.Movie `json:"movie"`
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
	Key         string `json:"key" example:"scrape_actress"`
	Label       string `json:"label" example:"Scrape Actress Information"`
	Description string `json:"description" example:"Enable detailed actress data scraping from DMM (may be slower)"`
	Type        string `json:"type" example:"boolean"` // boolean, string, number, etc.
	Min         *int   `json:"min,omitempty" example:"5"`
	Max         *int   `json:"max,omitempty" example:"120"`
	Unit        string `json:"unit,omitempty" example:"seconds"`
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

// ScanRequest represents a directory scan request
type ScanRequest struct {
	Path      string `json:"path" binding:"required" example:"/path/to/videos"`
	Recursive bool   `json:"recursive" example:"true"`
}

// ScanResponse represents scan results
type ScanResponse struct {
	Files   []FileInfo `json:"files"`
	Count   int        `json:"count" example:"10"`
	Skipped []string   `json:"skipped,omitempty"`
}

// FileInfo represents file or directory information
type FileInfo struct {
	Name    string `json:"name" example:"video.mp4"`
	Path    string `json:"path" example:"/path/to/video.mp4"`
	IsDir   bool   `json:"is_dir" example:"false"`
	Size    int64  `json:"size" example:"1024000000"`
	ModTime string `json:"mod_time" example:"2024-01-15T10:30:00Z"`
	MovieID string `json:"movie_id,omitempty" example:"IPX-535"`
	Matched bool   `json:"matched" example:"true"`
}

// BatchScrapeRequest represents a batch scrape request
type BatchScrapeRequest struct {
	Files            []string `json:"files" binding:"required"`
	Strict           bool     `json:"strict" example:"false"`
	Force            bool     `json:"force" example:"false"`
	Destination      string   `json:"destination,omitempty" example:"/path/to/output"`
	Update           bool     `json:"update" example:"false"` // Update mode: only create/update metadata files without moving video files
	SelectedScrapers []string `json:"selected_scrapers,omitempty" example:"r18dev,dmm"`
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
	NFOPath         string   `json:"nfo_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535.nfo"`
	PosterPath      string   `json:"poster_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-poster.jpg"`
	FanartPath      string   `json:"fanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/IPX-535-fanart.jpg"`
	ExtrafanartPath string   `json:"extrafanart_path" example:"/path/to/output/IPX-535 [IdeaPocket] - Beautiful Woman (2021)/extrafanart"`
	Screenshots     []string `json:"screenshots" example:"fanart1.jpg,fanart2.jpg,fanart3.jpg"`
}

// BatchJobResponse represents a batch job status
type BatchJobResponse struct {
	ID          string                        `json:"id"`
	Status      string                        `json:"status"`
	TotalFiles  int                           `json:"total_files"`
	Completed   int                           `json:"completed"`
	Failed      int                           `json:"failed"`
	Progress    float64                       `json:"progress"`
	Results     map[string]*worker.FileResult `json:"results"`
	StartedAt   string                        `json:"started_at"`
	CompletedAt *string                       `json:"completed_at,omitempty"`
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

// RescrapeRequest represents a request to rescrape with specific scrapers
type RescrapeRequest struct {
	SelectedScrapers []string `json:"selected_scrapers" binding:"required" example:"r18dev,dmm"`
	Force            bool     `json:"force" example:"false"`
}
