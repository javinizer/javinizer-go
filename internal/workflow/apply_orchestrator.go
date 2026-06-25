package workflow

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// applyOrchestrator is the internal interface for the Apply phase.
// Unexported — only the composition root (Workflow) uses it.
type applyOrchestrator interface {
	Execute(ctx context.Context, cmd ApplyCmd, progress scrape.ProgressFunc) (*ApplyResult, error)
}

// applyOrchImpl owns the 6-step Apply sequence: revert begin, organize, merge, DisplayTitle,
// download, NFO, revert complete. Per ADR-0017: runDownload/runNFO nil-check wrappers are
// eliminated — the orchestrator always receives real dependencies (or no-ops), so nil-checks
// are honest checks for optional features, not defensive nil guards.
type applyOrchImpl struct {
	fs             afero.Fs
	organizer      organizer.OrganizerInterface
	downloader     downloader.DownloaderInterface
	nfoGen         nfo.GeneratorInterface
	nfo            nfo.NFOFileMerger
	applyCfg       ApplyConfig
	templateEngine template.EngineInterface
	revertLog      RevertLog
	tagRepo        database.MovieTagRepositoryInterface
	logger         logging.Logger
}

var _ applyOrchestrator = (*applyOrchImpl)(nil)

func newApplyOrchestrator(
	fs afero.Fs,
	org organizer.OrganizerInterface,
	dl downloader.DownloaderInterface,
	nfoGen nfo.GeneratorInterface,
	nfoIface nfo.NFOFileMerger,
	applyCfg ApplyConfig,
	templateEngine template.EngineInterface,
	revertLog RevertLog,
	tagRepo database.MovieTagRepositoryInterface,
	logger logging.Logger,
) *applyOrchImpl {
	return &applyOrchImpl{
		fs:             fs,
		organizer:      org,
		downloader:     dl,
		nfoGen:         nfoGen,
		nfo:            nfoIface,
		applyCfg:       applyCfg,
		templateEngine: templateEngine,
		revertLog:      revertLog,
		tagRepo:        tagRepo,
		logger:         logger,
	}
}

// applyStep defines a named, executable step in the Apply pipeline.
// Each step can report progress and returns an error on failure.
type applyStep struct {
	name         string              // step identifier (used in FailedStep)
	failMsg      string              // human-readable error prefix on failure (e.g. "organization", "NFO generation")
	progressMsg  string              // empty if no progress report for this step
	progressPct  float64             // progress percentage for this step
	progressStep scrape.ProgressStep // progress step enum value
	execute      func() error
}

// onStepFailResult is returned by onStepFail to signal a step failure with
// the partial result and wrapped error.
type onStepFailResult struct {
	result *ApplyResult
	err    error
}

// executeSteps iterates through steps with progress reporting and revert-log
// completion on failure. If a step fails, onStepFail is called to produce the
// partial ApplyResult and wrapped error. Returns nil on success (all steps passed).
func (o *applyOrchImpl) executeSteps(
	steps []applyStep,
	progress scrape.ProgressFunc,
	completed *stepCompletion,
	onStepFail func(stepName string, failMsg string, stepErr error, stepsSoFar stepCompletion) onStepFailResult,
) (*ApplyResult, error) {
	for _, s := range steps {
		if s.progressMsg != "" && progress != nil {
			progress(s.progressStep, s.progressPct, s.progressMsg)
		}
		if err := s.execute(); err != nil {
			fail := onStepFail(s.name, s.failMsg, err, *completed)
			return fail.result, fail.err
		}
	}

	return nil, nil
}

// Execute runs the 6-step Apply sequence. Per CONTEXT.md: Apply is NOT atomic — if
// organize succeeds but download or NFO generation fails, files have already been moved.
// The caller must handle partial results.
func (o *applyOrchImpl) Execute(ctx context.Context, cmd ApplyCmd, progress scrape.ProgressFunc) (*ApplyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.fs == nil {
		return nil, fmt.Errorf("workflow not configured for apply operations (filesystem is nil)")
	}
	if cmd.Movie == nil {
		return nil, fmt.Errorf("movie is nil")
	}

	// Step 0: Begin revert log BEFORE any filesystem mutation.
	opID := o.beginRevertLog(ctx, cmd)

	movie := cmd.Movie
	targetDir := cmd.DestPath

	var steps stepCompletion

	// Build the pipeline state that steps mutate via closure.
	state := &applyPipelineState{
		movie:          movie,
		targetDir:      targetDir,
		finalDir:       targetDir,
		organizeResult: nil,
		merged:         false,
		foundNFOPath:   "",
		downloadPaths:  nil,
		nfoPath:        "",
	}

	// onStepFail produces the partial ApplyResult and wraps the error,
	// completing the revert log on failure with the partial state so any
	// filesystem mutations already performed (e.g. an organize that moved
	// the file) are recorded for revert. Passing nil here would blank
	// NewPath and leave a moved file non-revertable (regression vs main,
	// which persisted NewPath inline within OrganizeTask.Execute).
	onStepFail := func(stepName string, failMsg string, stepErr error, stepsSoFar stepCompletion) onStepFailResult {
		o.completeRevertLogWithState(ctx, opID, state)
		return onStepFailResult{
			result: &ApplyResult{
				OrganizeResult: state.organizeResult,
				Movie:          state.movie,
				DownloadPaths:  state.downloadPaths,
				NFOPath:        state.nfoPath,
				FoundNFOPath:   state.foundNFOPath,
				Merged:         state.merged,
				OperationID:    opID,
				Steps:          stepsSoFar,
				FailedStep:     stepName,
			},
			err: fmt.Errorf("%s failed: %w", failMsg, stepErr),
		}
	}

	// Step 1: Organize (if not skipped).
	var stepOrganize applyStep
	if !cmd.Organize.Skip && o.organizer != nil {
		stepOrganize = applyStep{
			name:         "organize",
			failMsg:      "organization",
			progressMsg:  "Planning organization...",
			progressPct:  0.3,
			progressStep: scrape.ProgressStepOrganize,
			execute:      func() error { return o.stepOrganize(ctx, cmd, state, &steps) },
		}
	} else {
		stepOrganize = applyStep{
			name:    "organize",
			execute: func() error { return nil },
		}
	}

	// Step 2: NFO merge.
	stepMerge := applyStep{
		name:    "merge",
		failMsg: "merge",
		execute: func() error { return o.stepMerge(cmd, state, &steps) },
	}

	// Step 3: Display title.
	stepDisplayTitle := applyStep{
		name:    "display_title",
		failMsg: "display title",
		execute: func() error { return o.stepDisplayTitle(ctx, cmd, state, &steps) },
	}

	// Step 4: Download media.
	stepDownload := applyStep{
		name:         "download",
		failMsg:      "download",
		progressMsg:  "Downloading media...",
		progressPct:  0.5,
		progressStep: scrape.ProgressStepDownload,
		execute:      func() error { return o.stepDownload(ctx, cmd, state, &steps) },
	}

	// Step 5: Generate NFO.
	stepNFO := applyStep{
		name:         "nfo_generation",
		failMsg:      "NFO generation",
		progressMsg:  "Generating NFO...",
		progressPct:  0.5,
		progressStep: scrape.ProgressStepNFO,
		execute:      func() error { return o.stepNFO(ctx, cmd, state, &steps) },
	}

	// Execute all steps.
	pipelineSteps := []applyStep{
		stepOrganize,
		stepMerge,
		stepDisplayTitle,
		stepDownload,
		stepNFO,
	}

	failResult, failErr := o.executeSteps(pipelineSteps, progress, &steps, onStepFail)
	if failResult != nil {
		return failResult, failErr
	}

	// Step 6: Complete revert log AFTER all filesystem mutations succeed.
	if o.revertLog != nil && opID != "" {
		applyResult := &ApplyResult{
			OrganizeResult: state.organizeResult,
			Movie:          state.movie,
			DownloadPaths:  state.downloadPaths,
			NFOPath:        state.nfoPath,
			FoundNFOPath:   state.foundNFOPath,
			Merged:         state.merged,
			OperationID:    opID,
			Steps:          steps,
		}
		if completeErr := o.revertLog.Complete(ctx, opID, applyResult); completeErr != nil {
			resolveLogger(o.logger).Warnf("[workflow] RevertLog.Complete failed for %s: %v (apply still succeeded)", cmd.Movie.ID, completeErr)
		}
	}

	if progress != nil {
		progress(scrape.ProgressStepApply, 1.0, "Completed")
	}
	return &ApplyResult{
		OrganizeResult: state.organizeResult,
		Movie:          state.movie,
		DownloadPaths:  state.downloadPaths,
		NFOPath:        state.nfoPath,
		FoundNFOPath:   state.foundNFOPath,
		Merged:         state.merged,
		OperationID:    opID,
		Steps:          steps,
	}, nil
}

// stepOrganize executes the organize step: move/link files to destination.
func (o *applyOrchImpl) stepOrganize(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	organizeCmd := organizer.OrganizeCmd{
		Match:       cmd.Match,
		Movie:       state.movie,
		DestDir:     cmd.DestPath,
		ForceUpdate: cmd.Organize.ForceUpdate,
		MoveFiles:   cmd.Organize.MoveFiles,
		LinkMode:    cmd.Organize.LinkMode,
		DryRun:      cmd.DryRun,
	}
	var organizeErr error
	state.organizeResult, organizeErr = o.organizer.Organize(ctx, organizeCmd)
	if organizeErr != nil {
		return organizeErr
	}
	if state.organizeResult != nil && state.organizeResult.FolderPath != "" {
		state.targetDir = state.organizeResult.FolderPath
		state.finalDir = state.organizeResult.FolderPath
	}
	steps.Organized = true
	return nil
}

// stepMerge merges scraped data with any existing NFO on disk.
func (o *applyOrchImpl) stepMerge(cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	if o.nfo == nil {
		steps.Merged = true
		return nil
	}
	mergeRes := o.nfo.MergeWithExistingNFO(state.movie, nfo.MergeWithExistingOptions{
		Match:          cmd.Match,
		ForceOverwrite: cmd.Merge.ForceOverwrite,
		PreserveNFO:    cmd.Merge.PreserveNFO,
		ScalarStrategy: cmd.Merge.ScalarStrategy,
		ArrayStrategy:  cmd.Merge.ArrayStrategy,
	})
	state.movie = mergeRes.Movie
	state.merged = mergeRes.Merged
	state.foundNFOPath = mergeRes.FoundNFOPath
	steps.Merged = true
	return nil
}

// stepDisplayTitle applies the display title template or falls back to Title.
func (o *applyOrchImpl) stepDisplayTitle(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	if o.applyCfg.DisplayTitle != "" && o.templateEngine != nil {
		titleSrc := cmd.DisplayTitleSrc
		if titleSrc == nil {
			titleSrc = cmd.Movie
		}
		ApplyDisplayTitleFromSource(ctx, state.movie, titleSrc, o.applyCfg.DisplayTitle, o.templateEngine, o.applyCfg.NFONameCfg)
	} else if state.movie.DisplayTitle == "" {
		state.movie.DisplayTitle = state.movie.Title
	}
	steps.DisplayTitle = true
	return nil
}

// stepDownload downloads cover, poster, trailer, and extrafanart media.
func (o *applyOrchImpl) stepDownload(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	downloadEnabled := cmd.Download && !cmd.DryRun
	if !downloadEnabled || o.downloader == nil {
		return nil
	}
	var multipart *downloader.MultipartInfo
	if cmd.Match.IsMultiPart {
		multipart = &downloader.MultipartInfo{
			IsMultiPart: true,
			PartNumber:  cmd.Match.PartNumber,
			PartSuffix:  cmd.Match.PartSuffix,
		}
	}
	outcome, dlErr := o.downloader.Download(ctx, downloader.DownloadCmd{
		Movie:               state.movie,
		DestDir:             state.finalDir,
		Multipart:           multipart,
		DownloadExtrafanart: cmd.DownloadExtrafanart,
	})
	// Download failures (including DownloadPartialError, where all critical
	// media failed) are non-fatal: log and continue so NFO generation still
	// runs. This mirrors main's ProcessFileTask.Execute, which logged the
	// download error and still generated the NFO — the project guarantee is
	// that a correct NFO is produced regardless of artwork availability.
	if dlErr != nil {
		resolveLogger(o.logger).Warnf("[workflow] Download failed for %s: %v (continuing to NFO generation)", state.movie.ID, dlErr)
		// Preserve any artifacts the downloader produced before the error (e.g.
		// non-critical media that succeeded in a DownloadPartialError) so later
		// Complete/CompleteFailed can record them for revert cleanup. The
		// downloader returns a non-nil outcome alongside a partial error; guard
		// for nil on total failures so this never panics.
		if outcome != nil {
			state.downloadPaths = outcome.DownloadedPaths
		}
		steps.Downloaded = false
		return nil
	}
	state.downloadPaths = outcome.DownloadedPaths
	steps.Downloaded = true
	return nil
}

// stepNFO generates the NFO metadata file for the movie.
func (o *applyOrchImpl) stepNFO(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	if !cmd.GenerateNFO || cmd.DryRun || o.nfoGen == nil {
		return nil
	}
	partSuffix := ""
	if cmd.Match.IsMultiPart {
		partSuffix = cmd.Match.PartSuffix
	}

	var movieTags []string
	if o.tagRepo != nil {
		tags, tagErr := o.tagRepo.GetTagsForMovie(ctx, state.movie.ID)
		if tagErr != nil {
			resolveLogger(o.logger).Warnf("[workflow] Failed to load tags for %s: %v", state.movie.ID, tagErr)
		} else {
			movieTags = tags
		}
	}

	nameCfg := o.applyCfg.NFONameCfg
	nameCfg.IsMultiPart = cmd.Match.IsMultiPart
	nameCfg.PartSuffix = partSuffix

	// Use the post-organize video path when the file was moved, so that
	// stream details (runtime/codec/resolution) can still be extracted.
	// Falling back to cmd.Match.Path preserves the original behavior when
	// organize is skipped or copy/in-place (file remains at source).
	videoPath := cmd.Match.Path
	if state.organizeResult != nil && state.organizeResult.NewPath != "" {
		videoPath = state.organizeResult.NewPath
	}

	resolvedPath, genErr := o.nfoGen.ResolveAndGenerate(ctx, state.movie, state.finalDir, nameCfg, videoPath, movieTags)
	if genErr != nil {
		return genErr
	}
	if resolvedPath != "" {
		state.nfoPath = resolvedPath
		steps.NFOGenerated = true
	}
	return nil
}

// applyPipelineState holds mutable state shared across the apply pipeline steps.
// Steps mutate this via closure — eliminating the need for per-step return value plumbing.
type applyPipelineState struct {
	movie          *models.Movie
	targetDir      string
	finalDir       string
	organizeResult *organizer.OrganizeResult
	merged         bool
	foundNFOPath   string
	downloadPaths  []string
	nfoPath        string
}

// completeRevertLogWithState marks an in-progress revert operation as failed,
// passing the partial pipeline state so filesystem mutations already performed
// (e.g. an organize that moved the file) are recorded for revert. The record is
// marked RevertStatusFailed but retains NewPath, allowing revert to locate the
// moved file. Per CONTEXT.md: called on error paths to prevent orphaned
// RevertStatusApplied records while keeping revert actionable.
func (o *applyOrchImpl) completeRevertLogWithState(ctx context.Context, opID OperationID, state *applyPipelineState) {
	if o.revertLog != nil && opID != "" {
		partial := &ApplyResult{
			OrganizeResult: state.organizeResult,
			Movie:          state.movie,
			DownloadPaths:  state.downloadPaths,
			NFOPath:        state.nfoPath,
			FoundNFOPath:   state.foundNFOPath,
			Merged:         state.merged,
			OperationID:    opID,
			Steps:          stepCompletion{},
		}
		if completeErr := o.revertLog.CompleteFailed(ctx, opID, partial); completeErr != nil {
			resolveLogger(o.logger).Warnf("[workflow] RevertLog.CompleteFailed error for %s: %v", opID, completeErr)
		}
	}
}

// beginRevertLog starts a revert log entry before filesystem mutation.
// Per CONTEXT.md: Begin must be called BEFORE any filesystem mutation.
// Per ADR-0033: Begin is a pure DB write; CaptureSnapshot reads NFO separately.
// Returns empty OperationID if revertLog is nil or Begin fails.
func (o *applyOrchImpl) beginRevertLog(ctx context.Context, cmd ApplyCmd) OperationID {
	if o.revertLog == nil {
		return ""
	}
	opID, beginErr := o.revertLog.Begin(ctx, cmd)
	if beginErr != nil {
		if cmd.DryRun {
			resolveLogger(o.logger).Warnf("[workflow] RevertLog.Begin failed for %s: %v", cmd.Movie.ID, beginErr)
		} else {
			resolveLogger(o.logger).Errorf("[workflow] RevertLog.Begin failed for %s: %v — proceeding without revert safety", cmd.Movie.ID, beginErr)
		}
		return opID // may be partial — still return it
	}
	// Per ADR-0033: snapshot is optional enrichment — failure doesn't block Apply.
	o.revertLog.CaptureSnapshot(ctx, opID, cmd)
	return opID
}

// noOpApplyOrchestrator returns an error — Apply is not configured for ScrapeOnly workflows.
// Callers that need ScrapeOnly behavior should use factory.NewScrapeOnlyWorkflow(), which wires the real
// scrape orchestrator but leaves Apply unconfigured.
type noOpApplyOrchestrator struct{}

var _ applyOrchestrator = (*noOpApplyOrchestrator)(nil)

func (noOpApplyOrchestrator) Execute(_ context.Context, _ ApplyCmd, _ scrape.ProgressFunc) (*ApplyResult, error) {
	return nil, fmt.Errorf("apply not configured")
}
