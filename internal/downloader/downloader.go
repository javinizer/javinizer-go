package downloader

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// restoreOpenReplacementSource opens a rollback-restore backup for reading
// with each platform's strongest protection against a final-component
// symlink swap between the Lstat gate and the open. The default passes the
// platform's no-follow flag through afero.OsFs to os.OpenFile (see
// restore_source_nofollow_unix.go / restore_source_nofollow_other.go); the
// Windows build replaces this at init with a reparse-point handle open (see
// restore_source_nofollow_windows.go).
var restoreOpenReplacementSource = openRestoreSourceNoFollow

// openRestoreSourceNoFollow is the POSIX/default rollback source open: OsFs
// forwards restoreSourceNoFollow to os.OpenFile, while MemMapFs ignores the
// unknown flag bit and relies on the caller's Lstat gate.
func openRestoreSourceNoFollow(fsys afero.Fs, backup string) (afero.File, error) {
	return fsys.OpenFile(backup, os.O_RDONLY|restoreSourceNoFollow, 0)
}

// Downloader handles media file downloads
type Downloader struct {
	fs             afero.Fs
	config         *Config
	httpClient     httpclient.HTTPClient
	templateEngine template.EngineInterface // Shared template engine (safe for concurrent use)
	pathResolver   *MediaPathResolver       // Shared path resolver for consistent media naming

	// destLocks serializes create-vs-replace classification + every byte swap
	// per destination path (POSTER-WRITE-HARDENING P3 D8). Defaults to the
	// process-wide registry shared with the history reverter's restore path.
	destLocks *fsutil.KeyedLockRegistry

	// Name formatting resolved from config at construction time
	actorFirstNameOrder bool // true = FirstName LastName, false = LastName FirstName
}

// DownloadCmd carries all parameters for the single-method Download seam.
// Per Phase 48: replaces the multi-method DownloaderInterface with one command struct.
type DownloadCmd struct {
	Movie                  *models.Movie
	DestDir                string
	Multipart              *MultipartInfo
	DownloadExtrafanart    *bool // Optional override for config.DownloadExtrafanart; nil = use config
	OverwriteExistingMedia bool
	Dedup                  *sync.Map
	// OperationID + Recorder arm the revert ledger for destructive overwrites
	// (POSTER-WRITE-HARDENING P3): any replaced pre-existing byte pair is
	// journaled (backup-aside + record BEFORE the swap). An unarmed overwrite
	// is refused with skip+warn — existing artwork is never destroyed without
	// a recorded operation.
	OperationID string
	Recorder    ReplacementRecorder
}

// DownloadOutcome wraps the results of a Download call.
// Per Phase 48: provides aggregate access to all download results, with
// helper fields for the common case of extracting just the downloaded paths.
type DownloadOutcome struct {
	Results         []DownloadResult
	DownloadedPaths []string
	CreatedPaths    []string
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
	Replaced   bool
	Skipped    bool
	Error      error
	Type       MediaType
	Duration   time.Duration
	// producerIdentity is the wave-67 (codex P2, PR#215 — producer-side
	// provenance binding) record the byte-install filed with its result: on a
	// completed publish it carries the installer's POST-PUBLISH-VERIFIED
	// destination identity (copyBackupToDestPublish's facts.restored shape),
	// captured before the producer returned — downloadPoster's candidate bind
	// and identity-bound scratch cleanup authenticate against THIS record,
	// never a post-return re-lookup of the mutable name. Unexported: the
	// record type is package-internal and only downloader legs consume it.
	// Zero (unknown) on every non-publishing exit — consumers keep the
	// wave-53 fail-closed posture there.
	producerIdentity installedDestIdentity
}

// MediaType represents the type of media being downloaded
type MediaType string

// MediaType values are the kinds of media the downloader can fetch.
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
		destLocks:           fsutil.SharedDestLocks(),
		actorFirstNameOrder: cfg.ActorFirstNameOrder,
	}
}

// WithDestLocks returns a copy sharing everything but the destination lock
// registry — tests use isolated registries.
func (d *Downloader) WithDestLocks(reg *fsutil.KeyedLockRegistry) *Downloader {
	cp := *d
	cp.destLocks = reg
	return &cp
}

// buildTemplateContext creates a template.Context for media path resolution.
// The context includes GroupActress, GroupActressName, FirstNameOrder, and
// multipart info so that the MediaPathResolver can execute templates correctly.
func (d *Downloader) buildTemplateContext(movie *models.Movie, multipart *MultipartInfo) *template.Context {
	ctx := template.NewContextFromMovie(movie)
	ctx.Index = 0
	ctx.GroupActress = d.config.GroupActress
	ctx.GroupActressMin = d.config.GroupActressMin
	ctx.GroupActressName = d.config.GroupActressName
	ctx.GroupUnknownActressName = d.config.GroupUnknownActressName
	ctx.UnknownActressMode = d.config.UnknownActressMode
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
	ctx.GroupActressMin = d.config.GroupActressMin
	ctx.GroupActressName = d.config.GroupActressName
	ctx.GroupUnknownActressName = d.config.GroupUnknownActressName
	ctx.UnknownActressMode = d.config.UnknownActressMode
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

	results, err := d.downloadAllWithExtrafanart(ctx, cmd.Movie, cmd.DestDir, cmd.Multipart, extrafanartEnabled, cmd.OverwriteExistingMedia, cmd.Dedup, downloadLedger{opID: cmd.OperationID, recorder: cmd.Recorder})
	createdPaths := make([]string, 0, len(results))
	downloadedPaths := make([]string, 0, len(results))
	for _, r := range results {
		if r.Downloaded && r.LocalPath != "" {
			downloadedPaths = append(downloadedPaths, r.LocalPath)
			if !r.Replaced {
				createdPaths = append(createdPaths, r.LocalPath)
			}
		}
	}
	if err != nil {
		if _, partial := err.(*DownloadPartialError); partial {
			return &DownloadOutcome{
				Results:         results,
				DownloadedPaths: downloadedPaths,
				CreatedPaths:    createdPaths,
			}, err
		}
		return nil, err
	}

	return &DownloadOutcome{
		Results:         results,
		DownloadedPaths: downloadedPaths,
		CreatedPaths:    createdPaths,
	}, nil
}
