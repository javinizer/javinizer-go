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
	"github.com/javinizer/javinizer-go/internal/progress"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// applyOrchestrator is the internal interface for the Apply phase.
// Unexported — only the composition root (Workflow) uses it.
type applyOrchestrator interface {
	Execute(ctx context.Context, cmd ApplyCmd) (*ApplyResult, error)
	// planDuplicatePriming runs ONLY the organize step's planning half
	// (read-only) so the apply phase can prime each sorted batch item's
	// duplicate claim before worker fan-out (#240 finding A).
	planDuplicatePriming(ctx context.Context, cmd ApplyCmd) (organizer.DuplicatePriming, error)
}

// applyOrchImpl owns the 6-step Apply sequence: revert begin, organize, merge, DisplayTitle,
// download, NFO, revert complete. runDownload/runNFO nil-check wrappers are
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
	name         string                // step identifier (used in FailedStep)
	failMsg      string                // human-readable error prefix on failure (e.g. "organization", "NFO generation")
	progressMsg  string                // empty if no progress report for this step
	progressPct  float64               // progress percentage for this step
	progressStep progress.ProgressStep // progress step enum value
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
//
// skipDownstream, when non-nil, is evaluated before EVERY step; once it returns
// true the loop stops WITHOUT failing — the duplicate-ownership gate uses this
// to halt the entire downstream output pipeline (merge/title/download/NFO) as
// one boundary decision instead of per-step checks.
func (o *applyOrchImpl) executeSteps(
	ctx context.Context,
	steps []applyStep,
	completed *stepCompletion,
	onStepFail func(stepName string, failMsg string, stepErr error, stepsSoFar stepCompletion) onStepFailResult,
	skipDownstream func() bool,
) (*ApplyResult, error) {
	for _, s := range steps {
		if skipDownstream != nil && skipDownstream() {
			break
		}
		if s.progressMsg != "" {
			progress.FromContext(ctx).Report(s.progressStep, s.progressPct, s.progressMsg)
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
func (o *applyOrchImpl) Execute(ctx context.Context, cmd ApplyCmd) (*ApplyResult, error) {
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
	opID, beginErr := o.beginRevertLog(ctx, cmd)
	if beginErr != nil {
		return &ApplyResult{
			Movie:       cmd.Movie,
			OperationID: opID,
			FailedStep:  "revert_begin",
		}, beginErr
	}

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
	// codex PR #241 batch-2 F1/F2: an organize-step failure that produced NO
	// OrganizeResult is a pre-publication terminal by construction — plan
	// errors, validation/conflict rejections (incl. unauthorized intra-batch
	// duplicates), and context aborts all return (nil, err) from Organize
	// before any execute — and a strategy failure marked
	// OrganizeResult.PrePublication published nothing either. The Begin row
	// already exists, so the marker flows to CompleteFailed and the returned
	// result: the row finalizes completed-noop (nothing to unwind) instead of
	// lingering failed-with-empty-NewPath — whose "" anchor the reverter
	// probes as anchor_missing forever, holding the batch off fully-reverted —
	// or worse, journaling a shared intent path a promoted claimant publishes.
	onStepFail := func(stepName string, failMsg string, stepErr error, stepsSoFar stepCompletion) onStepFailResult {
		prePub := stepName == "organize" && (state.organizeResult == nil || state.organizeResult.PrePublication)
		o.completeRevertLogWithState(ctx, opID, state, prePub)
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
				PrePublication: prePub,
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
			progressStep: progress.ProgressStepOrganize,
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
		progressStep: progress.ProgressStepDownload,
		execute:      func() error { return o.stepDownload(ctx, cmd, opID, state, &steps) },
	}

	// Step 5: Generate NFO.
	stepNFO := applyStep{
		name:         "nfo_generation",
		failMsg:      "NFO generation",
		progressMsg:  "Generating NFO...",
		progressPct:  0.5,
		progressStep: progress.ProgressStepNFO,
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

	// codex P1 (PR #241): duplicate-ownership gate at the step-loop boundary.
	// An authorized intra-batch duplicate (ForceUpdate) SKIPPED its move, and
	// its OrganizeResult.FolderPath/NewPath name the WINNER's shared
	// destination for display only. Running merge/download/NFO from here would
	// aim those writes at the winner's folder while the winner's own pipeline
	// produces the same artifacts — both workers concurrently writing/
	// truncating ONE NFO (or media shared by different logical IDs) — and
	// would journal those shared generated paths onto the loser's revert row,
	// so reverting the loser would DELETE the winner's artifacts. Once the
	// organize step lands DuplicateSkipped, EVERY remaining output step is
	// skipped: only the destination owner produces and journals ancillary
	// outputs. The duplicate warning surface is untouched — it rides back on
	// OrganizeResult.Warnings.
	dupGate := func() bool {
		return state.organizeResult != nil && state.organizeResult.DuplicateSkipped
	}

	failResult, failErr := o.executeSteps(ctx, pipelineSteps, &steps, onStepFail, dupGate)
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

	progress.FromContext(ctx).Report(progress.ProgressStepApply, 1.0, "Completed")
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

// planDuplicatePriming computes the organize plan for cmd WITHOUT executing
// it, mirroring stepOrganize's command assembly. Plans that cannot register
// a claim — organize skipped, plan error, or an organizer lacking the
// read-only planning seam — yield no priming; the item's worker then skips
// execution or fails with the identical plan error.
//
// codex r2 P2: a claim is returned ONLY for a plan that proves executable at
// priming time — PrimeBatch never releases a failed owner on its own, so a
// claimant whose source already vanished would otherwise own the canonical
// key for the whole run and block (or, under ForceUpdate, ghost-skip) every
// valid later claimant. The priming seam therefore pairs the read-only
// planner with PlanSourceExists; a claimant failing the existence check
// registers nothing and the next valid sorted claimant takes the key. A
// source vanishing AFTER priming is covered by the tracker's release on
// organize failure (see Organizer.Organize).
//
// codex P2 (PR #241 F1): the existence check covers STATIONARY residents
// too — an unverified WillMove=false plan would otherwise park a born-
// settled ghost claim on its canonical key, rejecting (or authorized-
// skipping) every mover of the run behind a destination nothing fills.
// Verified-before-parking is the primary defense; the resident's own
// Organize failure releasing its parked claim covers the residual
// priming→worker vanish window.
func (o *applyOrchImpl) planDuplicatePriming(ctx context.Context, cmd ApplyCmd) (organizer.DuplicatePriming, error) {
	if cmd.Organize.Skip {
		return organizer.DuplicatePriming{}, nil
	}
	planner, ok := o.organizer.(interface {
		PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error)
		PlanSourceExists(*organizer.OrganizePlan) bool
	})
	if !ok {
		return organizer.DuplicatePriming{}, nil
	}
	plan, err := planner.PlanOrganize(ctx, organizer.OrganizeCmd{
		Match:           cmd.Match,
		Movie:           cmd.Movie,
		DestDir:         cmd.DestPath,
		ForceUpdate:     cmd.Organize.ForceUpdate,
		MoveFiles:       cmd.Organize.MoveFiles,
		LinkMode:        cmd.Organize.LinkMode,
		DryRun:          cmd.DryRun,
		OperationMode:   cmd.OperationMode,
		ForceRenameFile: cmd.Organize.ForceRenameFile,
	})
	if err != nil {
		return organizer.DuplicatePriming{}, err
	}
	if plan.TargetPath != "" && !planner.PlanSourceExists(plan) {
		return organizer.DuplicatePriming{}, nil
	}
	return organizer.DuplicatePriming{
		SourcePath: plan.SourcePath,
		TargetPath: plan.TargetPath,
		WillMove:   plan.WillMove,
	}, nil
}

// stepOrganize executes the organize step: move/link files to destination.
func (o *applyOrchImpl) stepOrganize(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	organizeCmd := organizer.OrganizeCmd{
		Match:            cmd.Match,
		Movie:            state.movie,
		DestDir:          cmd.DestPath,
		ForceUpdate:      cmd.Organize.ForceUpdate,
		MoveFiles:        cmd.Organize.MoveFiles,
		LinkMode:         cmd.Organize.LinkMode,
		DryRun:           cmd.DryRun,
		OperationMode:    cmd.OperationMode,
		ForceRenameFile:  cmd.Organize.ForceRenameFile,
		DuplicateTracker: cmd.Organize.DuplicateTracker,
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
	state.scrapedMediaURLs = snapshotScrapedMedia(cmd.Movie)
	if o.nfo == nil {
		steps.Merged = true
		return nil
	}
	// Capture the manual review-page crop geometry before the merge: the
	// merger rebuilds a fresh Movie and does not know about the runtime-only
	// geometry, so carry/clear is decided here at the apply boundary.
	preSource := effectivePosterSource(state.movie)
	var preBounds *models.CropBounds
	var preFull bool
	if state.movie != nil {
		preBounds = state.movie.Poster.PosterCropBounds
		preFull = state.movie.Poster.PosterCropSourceFull
	}
	mergeRes := o.nfo.MergeWithExistingNFO(state.movie, nfo.MergeWithExistingOptions{
		Match:          cmd.Match,
		ForceOverwrite: cmd.Merge.ForceOverwrite,
		PreserveNFO:    cmd.Merge.PreserveNFO,
		ScalarStrategy: cmd.Merge.ScalarStrategy,
		ArrayStrategy:  cmd.Merge.ArrayStrategy,
	})
	state.movie = mergeRes.Movie
	carryPosterCropAcrossMerge(state.movie, preSource, preBounds, preFull)
	state.merged = mergeRes.Merged
	state.foundNFOPath = mergeRes.FoundNFOPath
	steps.Merged = true
	return nil
}

// effectivePosterSource mirrors the downloader's poster source selection:
// PosterURL when present, otherwise CoverURL.
func effectivePosterSource(m *models.Movie) string {
	if m == nil {
		return ""
	}
	if m.Poster.PosterURL != "" {
		return m.Poster.PosterURL
	}
	return m.Poster.CoverURL
}

// carryPosterCropAcrossMerge retains manual crop geometry across the
// pre-organize merge only when the merge left the effective poster source
// unchanged; any source change (or absent/non-full-source geometry) clears
// it so a stale crop can never be applied to a different image. Runs at the
// apply boundary only — the generic NFO merger never sees the field.
func carryPosterCropAcrossMerge(merged *models.Movie, preSource string, preBounds *models.CropBounds, preFull bool) {
	if merged == nil {
		return
	}
	if preBounds != nil && preFull && preSource != "" && effectivePosterSource(merged) == preSource {
		b := *preBounds
		merged.Poster.PosterCropBounds = &b
		merged.Poster.PosterCropSourceFull = true
		return
	}
	merged.Poster.PosterCropBounds = nil
	merged.Poster.PosterCropSourceFull = false
}

// stepDisplayTitle applies the display title template or falls back to Title.
func (o *applyOrchImpl) stepDisplayTitle(ctx context.Context, cmd ApplyCmd, state *applyPipelineState, steps *stepCompletion) error {
	if o.applyCfg.DisplayTitle != "" && o.templateEngine != nil {
		titleSrc := cmd.DisplayTitleSrc
		if titleSrc == nil {
			titleSrc = cmd.Movie
		}
		ApplyDisplayTitleFromSource(ctx, state.movie, titleSrc, o.applyCfg.DisplayTitle, o.templateEngine, o.applyCfg.NFONameCfg)
	} else {
		state.movie.DisplayTitle = state.movie.Title
	}
	steps.DisplayTitle = true
	return nil
}

// stepDownload downloads cover, poster, trailer, and extrafanart media.
func (o *applyOrchImpl) stepDownload(ctx context.Context, cmd ApplyCmd, opID OperationID, state *applyPipelineState, steps *stepCompletion) error {
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
	downloadMovie := state.movie
	if cmd.OverwriteExistingMedia {
		downloadMovie = state.movie.Clone()
		if state.scrapedMediaURLs != nil {
			downloadMovie = state.scrapedMediaURLs.overlay(downloadMovie)
		}
	}
	// R7-3/R12-2: media install into the organizer's leaf folder — seed it
	// as the discovery root pre-download; a DESTRUCTIVE run must never
	// proceed with an unseeded discovery path (the startup sweep would be
	// blind to a pre-journal crash window there).
	if rl, ok := o.revertLog.(*dbRevertLog); ok && opID != "" && state.finalDir != "" {
		if sErr := rl.seedRoot(ctx, opID, state.finalDir); sErr != nil && cmd.OverwriteExistingMedia {
			return fmt.Errorf("discovery-root seed failed for overwrite run: %w", sErr)
		}
	}
	outcome, dlErr := o.downloader.Download(ctx, downloader.DownloadCmd{
		Movie:                  downloadMovie,
		DestDir:                state.finalDir,
		Multipart:              multipart,
		DownloadExtrafanart:    cmd.DownloadExtrafanart,
		OverwriteExistingMedia: cmd.OverwriteExistingMedia,
		Dedup:                  cmd.Dedup,
		DedupOwnerKey:          cmd.DedupOwnerKey,
		DedupLogicalKey:        cmd.DedupLogicalKey,
		OperationID:            opID,
		Recorder:               replacementRecorder(o.revertLog),
	})
	if dlErr != nil {
		resolveLogger(o.logger).Warnf("[workflow] Download failed for %s: %v (continuing to NFO generation)", state.movie.ID, dlErr)
		if outcome != nil {
			state.downloadPaths = outcome.CreatedPaths
		}
		steps.Downloaded = false
		return nil
	}
	state.downloadPaths = outcome.CreatedPaths
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
	nameCfg.PartNumber = cmd.Match.PartNumber

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
	movie            *models.Movie
	targetDir        string
	finalDir         string
	organizeResult   *organizer.OrganizeResult
	merged           bool
	foundNFOPath     string
	downloadPaths    []string
	nfoPath          string
	scrapedMediaURLs *scrapedMediaSnapshot
}

// completeRevertLogWithState marks an in-progress revert operation as failed,
// passing the partial pipeline state so filesystem mutations already performed
// (e.g. an organize that moved the file) are recorded for revert. The record is
// marked RevertStatusFailed but retains NewPath, allowing revert to locate the
// moved file. Per CONTEXT.md: called on error paths to prevent orphaned
// RevertStatusApplied records while keeping revert actionable.
func (o *applyOrchImpl) completeRevertLogWithState(ctx context.Context, opID OperationID, state *applyPipelineState, prePublication bool) {
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
			PrePublication: prePublication,
		}
		if completeErr := o.revertLog.CompleteFailed(ctx, opID, partial); completeErr != nil {
			resolveLogger(o.logger).Warnf("[workflow] RevertLog.CompleteFailed error for %s: %v", opID, completeErr)
		}
	}
}

// beginRevertLog starts a revert log entry before filesystem mutation.
// Per CONTEXT.md: Begin must be called BEFORE any filesystem mutation.
// Begin is a pure DB write; CaptureSnapshot reads NFO separately.
// replacementRecorder arms the downloader's revert ledger only when the
// operation rows are durably journalled — the no-op recorder would accept
// replacements silently, so it must never arm a destructive overwrite.
func replacementRecorder(rl RevertLog) downloader.ReplacementRecorder {
	if _, ok := rl.(*dbRevertLog); !ok {
		return nil
	}
	return rl
}

// Returns an empty OperationID with no error when revert logging is disabled.
// A configured logger's failed Begin is fatal for destructive apply work: no
// filesystem step may run without a committed inverse in the ledger.
//
// #245: dry-run never starts journal writing at all — a dry-run plan mutates
// nothing, so Begin's pre-record would sit in the real revert ledger as an
// 'applied' phantom row for a planned-but-unexecuted organize. Returning the
// empty OperationID rides the existing downstream guards instead of needing
// dry-run branches there: Complete and completeRevertLogWithState are gated
// on opID != "", no database row exists for CaptureSnapshot to fetch, and the
// downloader's seedRoot/RecordReplacement lanes are unreachable with output
// steps disabled. dbRevertLog's Complete/CompleteFailed additionally treat an
// empty or unparsable OperationID as a nil-error no-op, so a skipped Begin
// can never make a later completion throw.
func (o *applyOrchImpl) beginRevertLog(ctx context.Context, cmd ApplyCmd) (OperationID, error) {
	if o.revertLog == nil || cmd.DryRun {
		return "", nil
	}
	opID, beginErr := o.revertLog.Begin(ctx, cmd)
	if beginErr != nil {
		resolveLogger(o.logger).Errorf("[workflow] RevertLog.Begin failed for %s: %v — aborting before destructive steps", cmd.Movie.ID, beginErr)
		return opID, fmt.Errorf("revert log begin failed: %w", beginErr)
	}
	// snapshot is optional enrichment — failure doesn't block Apply.
	o.revertLog.CaptureSnapshot(ctx, opID, cmd)
	return opID, nil
}

// noOpApplyOrchestrator returns an error — Apply is not configured for ScrapeOnly workflows.
// Callers that need ScrapeOnly behavior should use factory.NewScrapeOnlyWorkflow(), which wires the real
// scrape orchestrator but leaves Apply unconfigured.
type noOpApplyOrchestrator struct{}

var _ applyOrchestrator = (*noOpApplyOrchestrator)(nil)

func (noOpApplyOrchestrator) Execute(_ context.Context, _ ApplyCmd) (*ApplyResult, error) {
	return nil, fmt.Errorf("apply not configured")
}

func (noOpApplyOrchestrator) planDuplicatePriming(context.Context, ApplyCmd) (organizer.DuplicatePriming, error) {
	return organizer.DuplicatePriming{}, nil
}
