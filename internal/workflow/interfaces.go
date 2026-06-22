package workflow

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// OrganizeOptions controls the file organization step within Apply.
// Per AD-09: grouped by step, not flat flags.
// Per ADR-0030: LinkMode is resolved at the factory boundary — callers
// pass a validated link mode, not raw CLI input.
type OrganizeOptions struct {
	Skip        bool
	MoveFiles   bool
	LinkMode    organizer.LinkMode // Per ADR-0030: resolved at factory boundary, not inside orchestrator
	ForceUpdate bool
}

// MergeOptions controls the NFO merge step within Apply.
// Per AD-09: grouped by step. Priority chain: ForceOverwrite > PreserveNFO > Preset > ScalarStrategy/ArrayStrategy.
// Per ADR-0030: Preset is resolved at the factory boundary — ScalarStrategy and ArrayStrategy
// reflect the final resolved values (after preset application).
type MergeOptions struct {
	ForceOverwrite bool
	PreserveNFO    bool
	ScalarStrategy nfo.MergeStrategy // Per ADR-0030: resolved at factory boundary (includes preset application)
	ArrayStrategy  bool              // Per ADR-0030: true=merge, false=replace. Resolved at factory boundary
}

// ApplyCmd is the command struct that crosses the Apply seam.
// Per CONTEXT.md: contains inputs (Movie, Match, DestPath), global DryRun flag,
// and step-control options grouped by step (Organize, Merge).
// Per ADR-0030: OperationMode is resolved at the factory boundary.
type ApplyCmd struct {
	Movie               *models.Movie
	Match               models.FileMatchInfo
	DestPath            string
	DryRun              bool
	Organize            OrganizeOptions
	Merge               MergeOptions
	Download            bool
	GenerateNFO         bool
	DisplayTitleSrc     *models.Movie
	DownloadExtrafanart *bool                       // Optional override for extrafanart downloads; nil = use config default
	OperationMode       operationmode.OperationMode // Per ADR-0030: resolved at factory boundary
}

// stepCompletion records which Apply steps completed successfully.
// Per CONTEXT.md: Apply is NOT atomic — if organize succeeds and a later step
// fails, files have already been moved. Steps tracks what completed so callers
// can reason about partial state without probing the filesystem.
//
// A false value means the step either did not run (skipped via ApplyCmd flags
// or not reached because an earlier step failed) or ran and failed. Callers
// should cross-reference with ApplyCmd flags to distinguish "skipped" from
// "failed".
type stepCompletion struct {
	Organized    bool // file organization (move/copy/link) completed
	Merged       bool // NFO merge with existing NFO completed
	DisplayTitle bool // display title applied to movie
	Downloaded   bool // media download (poster, fanart) completed
	NFOGenerated bool // NFO file generation completed
}

// ApplyResult is everything the caller gets back from the Apply seam.
// Per CONTEXT.md: on partial failure, Apply returns a non-nil ApplyResult with
// partial Steps AND a non-nil error — callers should check result != nil even
// when err != nil. FailedStep identifies which step failed programmatically.
type ApplyResult struct {
	OrganizeResult *organizer.OrganizeResult
	Movie          *models.Movie
	DownloadPaths  []string
	NFOPath        string
	FoundNFOPath   string
	Merged         bool
	OperationID    string         // From RevertLog.Begin, for correlating with the revert record
	Steps          stepCompletion // Per-step completion tracking

	// FailedStep is the step that caused the error (e.g. "organize", "download",
	// "nfo_generation"). Empty on success. Per ADR-0033: callers can identify
	// which step failed without parsing error strings.
	FailedStep string
}

// PreviewCmd is the command struct that crosses the Preview seam (ADR-0004).
// Per ADR-0030: OperationMode is resolved at the factory boundary.
type PreviewCmd struct {
	Movie         *models.Movie
	FileResults   []models.FileMatchInfo
	Destination   string
	OperationMode operationmode.OperationMode // Per ADR-0030: resolved at factory boundary
	SkipNFO       bool
	SkipDownload  bool
}

// PreviewResult is the domain result from the Preview seam (ADR-0004).
type PreviewResult struct {
	FolderName      string
	FileName        string
	SubfolderPath   string
	FullPath        string
	VideoFiles      []string
	NFOPath         string
	NFOPaths        []string
	PosterPath      string
	FanartPath      string
	ExtrafanartPath string
	Screenshots     []string
	TrailerPath     string
	SourcePath      string
	OperationMode   operationmode.OperationMode
}

type WorkflowInterface interface {
	Scrape(ctx context.Context, cmd scrape.ScrapeCmd, progress scrape.ProgressFunc) (*scrape.ScrapeResult, *OrchestrationMeta, error)
	Apply(ctx context.Context, cmd ApplyCmd, progress scrape.ProgressFunc) (*ApplyResult, error)
	Preview(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error)
	Compare(ctx context.Context, cmd CompareCmd) (*CompareResult, error)
	ScanAndMatch(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error)
}

// CompareCmd is the command struct that crosses the Compare seam.
// The seam handles the full scrape-aggregate-merge pipeline internally,
// so the API layer never imports nfo, aggregator, or matcher directly.
// Per ADR-0030: ScalarStrategy and ArrayStrategy are resolved at the factory boundary.
type CompareCmd struct {
	MovieID          string            // The movie ID to compare
	NFOPath          string            // Path to existing NFO file
	ScalarStrategy   nfo.MergeStrategy // Per ADR-0030: resolved at factory boundary (including preset application)
	ArrayStrategy    bool              // Per ADR-0030: true=merge, false=replace. Resolved at factory boundary
	SelectedScrapers []string          // Optional scraper filter
}

// FieldDifference represents a difference between NFO and scraped data for a single field.
// Per the Compare seam: domain logic identifies differences; the API layer maps them
// to its JSON response type.
type FieldDifference struct {
	Field        string // Field name (e.g. "title", "actresses")
	NFOValue     any    // Value from the parsed NFO
	ScrapedValue any    // Value from the fresh scrape
	MergedValue  any    // Value in the merged result
}

// CompareResult is everything the caller gets back from the Compare seam.
type CompareResult struct {
	Movie       *models.Movie     // The merged result movie
	NFOData     *models.Movie     // Parsed NFO data
	ScrapedData *models.Movie     // Scraped data (before merge)
	MergeStats  *nfo.MergeStats   // Merge statistics
	Differences []FieldDifference // Per-field differences between NFO and scraped data
	NFOExists   bool              // Whether the NFO file was found
	NFOPath     string            // Sanitized filename only (not full path)
}

// ScanAndMatchCmd is the command struct that crosses the ScanAndMatch seam.
// The seam combines scan + match + multipart validation internally,
// so the API layer never imports scanner or matcher directly.
type ScanAndMatchCmd struct {
	Directory      string // Directory path to scan
	Recursive      bool   // Recursive scan
	Filter         string // Optional file name filter
	MaxFiles       int    // Optional limit (0 = use config default)
	TimeoutSeconds int    // Optional timeout (0 = use config default)
}

// ScanAndMatchResult is everything the caller gets back from the ScanAndMatch seam.
type ScanAndMatchResult struct {
	Files        []models.FileMatchInfo // Matched files with metadata
	Skipped      int                    // Count of skipped files
	SkippedPaths []string               // Sample of skipped file paths (capped at scanner limit)
	TimedOut     bool                   // Whether scan was limited/timed out
}
