package workflow

import (
	"context"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// OrganizeOptions controls the file organization step within Apply.
// Per AD-09: grouped by step, not flat flags.
// LinkMode is resolved at the factory boundary — callers
// pass a validated link mode, not raw CLI input.
type OrganizeOptions struct {
	Skip            bool
	MoveFiles       bool
	LinkMode        organizer.LinkMode // resolved at factory boundary, not inside orchestrator
	ForceUpdate     bool
	ForceRenameFile bool
	// DuplicateTracker carries the batch run's intra-batch duplicate
	// preflight registry into every file's plan (#224 phase E). The apply
	// phase allocates one per run (non-probing for dry runs, #240 finding B),
	// primes it in sorted order before fan-out (#240 finding A); nil disables
	// detection.
	DuplicateTracker *organizer.DuplicateTracker
}

// MergeOptions controls the NFO merge step within Apply.
// Per AD-09: grouped by step. Priority chain: ForceOverwrite > PreserveNFO > Preset > ScalarStrategy/ArrayStrategy.
// Preset is resolved at the factory boundary — ScalarStrategy and ArrayStrategy
// reflect the final resolved values (after preset application).
type MergeOptions struct {
	ForceOverwrite bool
	PreserveNFO    bool
	ScalarStrategy nfo.MergeStrategy // resolved at factory boundary (includes preset application)
	ArrayStrategy  bool              // true=merge, false=replace. Resolved at factory boundary
}

// ApplyCmd is the command struct that crosses the Apply seam.
// Per CONTEXT.md: contains inputs (Movie, Match, DestPath), global DryRun flag,
// and step-control options grouped by step (Organize, Merge).
// OperationMode is resolved at the factory boundary.
type ApplyCmd struct {
	Movie                  *models.Movie
	Match                  models.FileMatchInfo
	DestPath               string
	DryRun                 bool
	Organize               OrganizeOptions
	Merge                  MergeOptions
	Download               bool
	GenerateNFO            bool
	DisplayTitleSrc        *models.Movie
	DownloadExtrafanart    *bool // Optional override for extrafanart downloads; nil = use config default
	OverwriteExistingMedia bool
	Dedup                  *sync.Map
	// DedupOwnerKey/LogicalKey are populated by apply before fan-out so
	// shared poster destinations have a deterministic first owner.
	DedupOwnerKey   string
	DedupLogicalKey string
	OperationMode   operationmode.OperationMode // resolved at factory boundary
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
	// "nfo_generation"). Empty on success. callers can identify
	// which step failed without parsing error strings.
	FailedStep string
	// PrePublication is true when the apply failed before the organize step
	// published ANY filesystem mutation (codex PR #241 batch-2 F1/F2): plan
	// rejections (validation/conflict — including unauthorized intra-batch
	// duplicate conflicts), context aborts, and pre-publish strategy failures
	// all terminate with the destination untouched. Revert journaling
	// (RevertLog.CompleteFailed) treats such results exactly like authorized
	// duplicate skips — no target fields journaled, row finalized
	// completed-noop — because their intent paths may name a SHARED batch
	// destination a promoted claimant later publishes, and a revert armed
	// with those paths would drag the claimant's bytes onto this source.
	PrePublication bool
}

// PreviewCmd is the command struct that crosses the Preview seam (ADR-0004).
// OperationMode is resolved at the factory boundary.
type PreviewCmd struct {
	Movie           *models.Movie
	FileResults     []models.FileMatchInfo
	Destination     string
	OperationMode   operationmode.OperationMode // resolved at factory boundary
	SkipNFO         bool
	SkipDownload    bool
	ForceNFO        bool
	ForceRenameFile bool
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

// DuplicatePrimingPlanner is the OPTIONAL workflow capability the apply
// phase discovers by type assertion to pre-assign deterministic intra-batch
// duplicate owners (#240 finding A): for each sorted batch item the phase
// calls PlanDuplicatePriming exactly once BEFORE worker fan-out and primes
// the run's duplicate tracker with the returned claims in that sorted order.
// Workflows that do not implement it keep first-come duplicate observation
// (single-item or otherwise unprimed runs only).
type DuplicatePrimingPlanner interface {
	PlanDuplicatePriming(ctx context.Context, cmd ApplyCmd) (organizer.DuplicatePriming, error)
}

// WorkflowInterface exposes the high-level scrape, apply, preview, compare, and scan workflows.
type WorkflowInterface interface {
	Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *OrchestrationMeta, error)
	Apply(ctx context.Context, cmd ApplyCmd) (*ApplyResult, error)
	Preview(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error)
	Compare(ctx context.Context, cmd CompareCmd) (*CompareResult, error)
	ScanAndMatch(ctx context.Context, cmd ScanAndMatchCmd) (*ScanAndMatchResult, error)
}

// CompareCmd is the command struct that crosses the Compare seam.
// The seam handles the full scrape-aggregate-merge pipeline internally,
// so the API layer never imports nfo, aggregator, or matcher directly.
// ScalarStrategy and ArrayStrategy are resolved at the factory boundary.
type CompareCmd struct {
	MovieID          string            // The movie ID to compare
	NFOPath          string            // Path to existing NFO file
	ScalarStrategy   nfo.MergeStrategy // resolved at factory boundary (including preset application)
	ArrayStrategy    bool              // true=merge, false=replace. Resolved at factory boundary
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
