package downloader

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Downloader handles media file downloads
type Downloader struct {
	fs             afero.Fs
	config         *Config
	httpClient     httpclient.HTTPClient
	templateEngine template.EngineInterface // Shared template engine (safe for concurrent use)
	pathResolver   *MediaPathResolver       // Shared path resolver for consistent media naming

	// Name formatting resolved from config at construction time
	actorFirstNameOrder bool // true = FirstName LastName, false = LastName FirstName
}

// DownloadCmd carries all parameters for the single-method Download seam.
// Per Phase 48: replaces the multi-method DownloaderInterface with one command struct.
type DownloadCmd struct {
	Movie               *models.Movie
	DestDir             string
	Multipart           *MultipartInfo
	DownloadExtrafanart *bool // Optional override for config.DownloadExtrafanart; nil = use config
}

// DownloadOutcome wraps the results of a Download call.
// Per Phase 48: provides aggregate access to all download results, with
// helper fields for the common case of extracting just the downloaded paths.
type DownloadOutcome struct {
	Results         []DownloadResult
	DownloadedPaths []string // Convenience: LocalPath of each result where Downloaded=true
}

// DownloaderInterface is the single-method seam for media downloads.
// Per Phase 48: the Workflow-facing interface has one method — individual
// download methods (downloadCover, downloadPoster, etc.) are unexported
// implementation details of the concrete *Downloader type.
type DownloaderInterface interface {
	Download(ctx context.Context, cmd DownloadCmd) (*DownloadOutcome, error)
}

// DownloadPartialError is surfaced when all critical media (cover/poster)
// failed to download while non-critical media (actress images, extrafanart)
// may have succeeded. It carries the count of critical media types attempted
// and succeeded (Succeeded is 0 when this sentinel is returned). Per-item
// errors are captured in individual DownloadResult.Error fields. The apply
// orchestrator treats this sentinel as non-fatal: it logs the failure,
// preserves any non-critical artifacts that did download (for revert
// cleanup), and proceeds to NFO generation — the project guarantee is that a
// correct NFO is produced regardless of artwork availability. Total download
// failure (a non-partial error) returns a nil outcome alongside the error;
// callers must nil-check the outcome.
type DownloadPartialError struct {
	Attempted int // number of critical media types attempted (cover + poster)
	Succeeded int // number of critical media types that downloaded successfully
}

func (e *DownloadPartialError) Error() string {
	return fmt.Sprintf("download: %d critical media attempted, %d succeeded", e.Attempted, e.Succeeded)
}

var _ DownloaderInterface = (*Downloader)(nil)

// DownloadResult represents the result of a download operation
type DownloadResult struct {
	URL        string
	LocalPath  string
	Size       int64
	Downloaded bool
	Error      error
	Type       MediaType
	Duration   time.Duration
}

// MediaType represents the type of media being downloaded
type MediaType string

const (
	MediaTypeCover       MediaType = "cover"
	MediaTypePoster      MediaType = "poster"
	MediaTypeExtrafanart MediaType = "extrafanart"
	MediaTypeTrailer     MediaType = "trailer"
	MediaTypeActress     MediaType = "actress"
)

// MultipartInfo holds multipart file information for template rendering
type MultipartInfo struct {
	IsMultiPart bool   // Whether this is a multi-part file
	PartNumber  int    // Part number (1, 2, 3, etc.) - 0 means single file
	PartSuffix  string // Original part suffix detected from filename (e.g., "-pt1", "-A")
}

// NewDownloader creates a new media downloader
func NewDownloader(client httpclient.HTTPClient, fs afero.Fs, cfg *Config, engine template.EngineInterface) *Downloader {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &Downloader{
		fs:                  fs,
		config:              cfg,
		httpClient:          client,
		templateEngine:      engine,
		pathResolver:        NewMediaPathResolver(cfg.MediaFormatConfig, engine),
		actorFirstNameOrder: cfg.ActorFirstNameOrder,
	}
}

// buildTemplateContext creates a template.Context for media path resolution.
// The context includes GroupActress, GroupActressName, FirstNameOrder, and
// multipart info so that the MediaPathResolver can execute templates correctly.
func (d *Downloader) buildTemplateContext(movie *models.Movie, multipart *MultipartInfo) *template.Context {
	ctx := template.NewContextFromMovie(movie)
	ctx.Index = 0
	ctx.GroupActress = d.config.GroupActress
	ctx.GroupActressName = d.config.GroupActressName
	ctx.GroupUnknownActressName = d.config.GroupUnknownActressName
	ctx.ActressDelimiter = d.config.ActressDelimiter
	ctx.FirstNameOrder = d.actorFirstNameOrder
	ctx.ActressLanguageJa = d.config.ActorJapaneseNames

	if multipart != nil {
		ctx.IsMultiPart = multipart.IsMultiPart
		ctx.PartNumber = multipart.PartNumber
		ctx.PartSuffix = multipart.PartSuffix
	}
	return ctx
}

func (d *Downloader) generateActressFilename(movie *models.Movie, actressName string, templateStr string) string {
	if templateStr == "" {
		return ""
	}

	ctx := template.NewContextFromMovie(movie)
	ctx.ActressName = actressName
	ctx.GroupActress = d.config.GroupActress
	ctx.GroupActressName = d.config.GroupActressName
	ctx.GroupUnknownActressName = d.config.GroupUnknownActressName
	ctx.ActressDelimiter = d.config.ActressDelimiter
	ctx.FirstNameOrder = d.actorFirstNameOrder
	ctx.ActressLanguageJa = d.config.ActorJapaneseNames

	engine := d.templateEngine
	filename, err := engine.Execute(templateStr, ctx)
	if err != nil {
		name := template.SanitizeFilename(actressName)
		return fmt.Sprintf("%s.jpg", name)
	}

	return filename
}

// Download is the single-method seam that downloads all enabled media types.
// Per Phase 48: the Workflow-facing interface calls this one method instead
// of the multi-method protocol. Delegates to DownloadAll internally.
func (d *Downloader) Download(ctx context.Context, cmd DownloadCmd) (*DownloadOutcome, error) {
	// Resolve extrafanart override: command-level override wins over config
	extrafanartEnabled := d.config.DownloadExtrafanart
	if cmd.DownloadExtrafanart != nil {
		extrafanartEnabled = *cmd.DownloadExtrafanart
	}

	results, err := d.downloadAllWithExtrafanart(ctx, cmd.Movie, cmd.DestDir, cmd.Multipart, extrafanartEnabled)
	if err != nil {
		// On a DownloadPartialError sentinel, some non-critical media (actress
		// images, extrafanart) may have succeeded even though all critical media
		// (cover/poster) failed. Return the outcome with those partial paths
		// ALONGSIDE the error so callers can record the artifacts for revert
		// cleanup instead of discarding them. Total (non-partial) failures still
		// return a nil outcome. (Callers must nil-check the outcome on error.)
		if _, partial := err.(*DownloadPartialError); partial {
			downloadedPaths := make([]string, 0, len(results))
			for _, r := range results {
				if r.Downloaded && r.LocalPath != "" {
					downloadedPaths = append(downloadedPaths, r.LocalPath)
				}
			}
			return &DownloadOutcome{
				Results:         results,
				DownloadedPaths: downloadedPaths,
			}, err
		}
		return nil, err
	}

	downloadedPaths := make([]string, 0, len(results))
	for _, r := range results {
		if r.Downloaded && r.LocalPath != "" {
			downloadedPaths = append(downloadedPaths, r.LocalPath)
		}
	}

	return &DownloadOutcome{
		Results:         results,
		DownloadedPaths: downloadedPaths,
	}, nil
}
