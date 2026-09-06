package commandutil

// Regression pins for #244: the CLI batch runtime (sort/update) must persist
// batch history with API parity — a persisted jobs row, batch_file_operations
// revert-ledger rows (incl. the authorized duplicate-skip noop row), history
// rows whose organize metadata carries the skip warning text, and eventlog
// audit entries — instead of reporting "Applied ✓" while every audit trail
// the API batches get vanishes.
//
// Fixtures build on the multipart_partsuffix_test.go pattern: the
// JAVINIZER_E2E_SCRAPERS seam substitutes the offline e2emock scraper, and all
// paths are built with filepath.Join/t.TempDir (never POSIX literals) so the
// tests stay Windows-safe.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDuplicateBatch writes a two-file intra-batch duplicate fixture: both
// files scrape to GOOD-700 and plan onto the SAME destination
// (<dest>/GOOD-700/GOOD-700.mp4) because file_format carries no PARTSUFFIX.
// With ForceUpdate (-f) the second claimant is an AUTHORIZED duplicate:
// demoted to a warning + skip (only the winner's bytes move).
func setupDuplicateBatch(t *testing.T) (configPath, src, dest, dbPath string) {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	tmpDir := t.TempDir()
	src = filepath.Join(tmpDir, "src")
	dest = filepath.Join(tmpDir, "dest")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "b"), 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "GOOD-700.mp4"), []byte("fake video a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "b", "GOOD-700.mp4"), []byte("fake video b"), 0o600))

	dbPath = filepath.Join(tmpDir, "javinizer.db")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Database.LogLevel = "silent"
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Operation.RenameFile = true
	// e2emock media URLs are non-resolvable; downloads are irrelevant here.
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	configPath = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))
	return configPath, src, dest, dbPath
}

// batchIDFromOutput extracts the batch job UUID the presenter header prints.
func batchIDFromOutput(t *testing.T, out string) string {
	t.Helper()
	m := regexp.MustCompile(`Batch Job: ([0-9a-f-]{36})`).FindStringSubmatch(out)
	require.Len(t, m, 2, "header must print the batch job id\n%s", out)
	return m[1]
}

// openAssertionDB re-opens the run's sqlite database read/write for
// assertions. RunBatchCommand closes its own handle, so tests must not share it.
func openAssertionDB(t *testing.T, dbPath string) *database.DB {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestRunBatchCommand_DuplicateSkip_PersistsAuditRows is the #244 core pin:
// after a live 2-file authorized duplicate batch with -f, every audit surface
// the API batches get must be populated for the CLI flow too.
func TestRunBatchCommand_DuplicateSkip_PersistsAuditRows(t *testing.T) {
	configPath, src, dest, dbPath := setupDuplicateBatch(t)

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		MoveFiles:    true,
		ForceUpdate:  true, // -f authorizes the intra-batch duplicate
		GenerateNFO:  true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()

	// Console truth: the skip is reported, not hidden behind a bare "Applied ✓".
	batchID := batchIDFromOutput(t, out)
	assert.Contains(t, out, "⚠️", "skip warning must print to the console\n%s", out)
	assert.Contains(t, out, "duplicate destination within batch", "warning text must survive to the console\n%s", out)
	assert.Contains(t, out, "Skipped (authorized duplicates): 1", "summary must count the skip\n%s", out)

	// #248 codex P2 (F2): the visible counts must match the real file
	// movement — ONE winner moved, the authorized duplicate loser stayed put.
	// Completed-minus-skips keeps "Metadata found" intact (both files scraped)
	// while organize/NFO totals count only files whose bytes landed.
	assert.Contains(t, out, "Organized 1 file(s)", "organize headline counts only real moves\n%s", out)
	assert.Contains(t, out, "Metadata found: 2", "metadata-found counts both scraped files\n%s", out)
	assert.Contains(t, out, "NFOs generated: 1", "NFO totals exclude the skipped duplicate\n%s", out)
	assert.Contains(t, out, "Files organized: 1", "summary organized total excludes the skipped duplicate\n%s", out)
	countVideos := func(root string) int {
		t.Helper()
		count := 0
		walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && filepath.Ext(p) == ".mp4" {
				count++
			}
			return err
		})
		require.NoError(t, walkErr)
		return count
	}
	assert.Equal(t, 1, countVideos(dest), "exactly one winner's bytes landed at the destination")
	assert.FileExists(t, filepath.Join(dest, "GOOD-700", "GOOD-700.mp4"))
	assert.Equal(t, 1, countVideos(src), "the authorized duplicate loser never moved")

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	// (i) jobs row: the CLI batch is queryable via `history list --batch`.
	job, err := repos.JobRepo.FindByID(ctx, batchID)
	require.NoError(t, err, "jobs row for the CLI batch must persist")
	require.NotNil(t, job)
	assert.Equal(t, models.JobStatusOrganized, job.Status)
	assert.NotNil(t, job.OrganizedAt)

	// (i) batch_file_operations: winner applied, loser finalized noop with no
	// NewPath (its display path would name the winner's destination).
	ops, err := repos.BatchFileOpRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, ops, 2, "both files must journal an operation row. ops=%v", ops)
	var noopOps, appliedOps []models.BatchFileOperation
	for _, op := range ops {
		switch op.RevertStatus {
		case models.RevertStatusNoOp:
			noopOps = append(noopOps, op)
		case models.RevertStatusApplied:
			appliedOps = append(appliedOps, op)
		}
	}
	require.Len(t, noopOps, 1, "the authorized duplicate skip must finalize completed-noop")
	require.Len(t, appliedOps, 1, "the batch winner's move row stays applied")
	assert.Empty(t, noopOps[0].NewPath, "noop row must not arm revert against the winner's destination")
	assert.Equal(t,
		filepath.Join(dest, "GOOD-700", "GOOD-700.mp4"),
		appliedOps[0].NewPath,
		"the winner row names its real destination")

	// (i) history rows: scrape + organize per file, and the loser's organize
	// metadata carries the skip warning text (the text that previously vanished).
	rows, err := repos.HistoryRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 4, "scrape+organize rows for both files")
	warningRows := 0
	for _, h := range rows {
		if h.Operation == models.HistoryOpOrganize && h.Status == models.HistoryStatusSuccess {
			if h.Metadata != "" && strings.Contains(h.Metadata, "duplicate destination within batch") {
				warningRows++
			}
		}
	}
	assert.Equal(t, 1, warningRows, "exactly one organize history row carries the skip warning metadata")

	// (ii) eventlog: one organize warning audit entry per skip warning.
	events, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityWarn,
	}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, events, "the skip warning must land in the eventlog")
	assert.Contains(t, events[0].Message, "duplicate destination within batch")
}

// TestRunBatchCommand_DryRun_WritesNoOperationalRows pins the preview
// semantics: dry runs write NO revert-ledger rows (a preview never moves the
// bytes a revert would target), no jobs row, and no eventlog entries — while
// history rows still land with their dry_run flag so previews stay auditable.
func TestRunBatchCommand_DryRun_WritesNoOperationalRows(t *testing.T) {
	configPath, src, dest, dbPath := setupDuplicateBatch(t)

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		MoveFiles:    true,
		ForceUpdate:  true,
		GenerateNFO:  true,
		DryRun:       true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()
	batchID := batchIDFromOutput(t, out) // a preview still names its run identity

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	jobs, err := repos.JobRepo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, jobs, "dry run persists no jobs row")

	opCount, err := repos.BatchFileOpRepo.CountByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	assert.Zero(t, opCount, "dry run writes no batch_file_operations rows (revert ledger)")

	eventCount, err := repos.EventRepo.Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, eventCount, "dry run emits no audit events — previews are not operations")

	historyCount, err := repos.HistoryRepo.Count(ctx)
	require.NoError(t, err)
	assert.Greater(t, historyCount, int64(0), "dry-run history rows remain visible")
	rows, err := repos.HistoryRepo.FindByOperation(ctx, models.HistoryOpOrganize, 50)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, h := range rows {
		assert.True(t, h.DryRun, "organize history from a preview must carry the dry_run flag")
	}
}

// TestRunBatchCommand_RuntimeConstructionError covers the error branch where
// the batch runtime cannot be constructed (seam replaced by a failing stub).
func TestRunBatchCommand_RuntimeConstructionError(t *testing.T) {
	configPath, src, dest, _ := setupDuplicateBatch(t)

	orig := newCLIBatchRuntimeFn
	t.Cleanup(func() { newCLIBatchRuntimeFn = orig })
	newCLIBatchRuntimeFn = func(_ *bootstrapResult, _ *config.Config, _ BatchCommandOptions, _ worker.BatchJobConfig, _ []string) (*cliBatchRuntime, error) {
		return nil, fmt.Errorf("simulated runtime failure")
	}

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Presenter:    &SilentBatchCommandPresenter{},
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create batch job runtime")
	assert.Contains(t, err.Error(), "simulated runtime failure")
}

// TestNewCLIBatchRuntime_NilWorkflowFactory covers the defensive error branch
// for a bootstrapResult constructed without a workflow factory.
func TestNewCLIBatchRuntime_NilWorkflowFactory(t *testing.T) {
	rt, err := newCLIBatchRuntime(&bootstrapResult{}, config.DefaultConfig(nil, nil), BatchCommandOptions{BatchJobID: "job-x"}, BatchJobConfigFromAppConfig(config.DefaultConfig(nil, nil)), nil)
	require.Error(t, err)
	assert.Nil(t, rt)
	assert.Contains(t, err.Error(), "workflow factory unavailable")
}

// TestDefaultPresenter_HeaderPrintsBatchJobID pins the presenter surface the
// e2e suite parses: the run's persisted batch identity is printed once.
func TestDefaultPresenter_HeaderPrintsBatchJobID(t *testing.T) {
	var buf bytes.Buffer
	p := &defaultBatchCommandPresenter{}
	p.OnHeader(&buf, BatchCommandOptions{
		CommandLabel: "Javinizer Sort",
		SourcePath:   "src",
		Destination:  "dest",
		BatchJobID:   "job-123",
	})
	assert.Contains(t, buf.String(), "Batch Job: job-123")

	// Empty id (tests that never reach the batch stage) prints nothing.
	buf.Reset()
	p.OnHeader(&buf, BatchCommandOptions{CommandLabel: "Javinizer Sort"})
	assert.NotContains(t, buf.String(), "Batch Job:")
}

// TestDefaultSummaryPrinter_SkippedDuplicatesLine pins the summary truth:
// authorized duplicate skips are counted separately from organized files.
func TestDefaultSummaryPrinter_SkippedDuplicatesLine(t *testing.T) {
	var buf bytes.Buffer
	defaultSummaryPrinter(&buf, BatchCommandOptions{}, BatchCommandResult{
		ScanResult:        &workflow.ScanAndMatchResult{},
		SuccessCount:      2,
		SkippedDuplicates: 1,
	})
	assert.Contains(t, buf.String(), "Skipped (authorized duplicates): 1")

	// Zero skips keep the summary unchanged.
	buf.Reset()
	defaultSummaryPrinter(&buf, BatchCommandOptions{}, BatchCommandResult{ScanResult: &workflow.ScanAndMatchResult{}})
	assert.NotContains(t, buf.String(), "Skipped (authorized duplicates)")
}

// TestDefaultSummaryPrinter_SkipAdjustedOrganizeTotals pins #248 codex P2
// (F2): organize/output success totals are completed MINUS skipped duplicates
// (subtraction at this summary aggregation site only), while metadata-found
// counting stays intact.
func TestDefaultSummaryPrinter_SkipAdjustedOrganizeTotals(t *testing.T) {
	newResult := func(skips int) BatchCommandResult {
		return BatchCommandResult{
			ScanResult:        &workflow.ScanAndMatchResult{},
			SuccessCount:      2,
			SkippedDuplicates: skips,
		}
	}

	// Live sort: 2 completed, 1 authorized skip → exactly 1 file actually moved.
	var buf bytes.Buffer
	defaultSummaryPrinter(&buf, BatchCommandOptions{GenerateNFO: true}, newResult(1))
	out := buf.String()
	assert.Contains(t, out, "Organized 1 file(s)", "completed minus skips")
	assert.Contains(t, out, "Metadata found: 2", "metadata-found keeps counting every completed file")
	assert.Contains(t, out, "NFOs generated: 1")
	assert.Contains(t, out, "Files organized: 1")

	// Dry run inherits the same subtraction discipline.
	buf.Reset()
	defaultSummaryPrinter(&buf, BatchCommandOptions{DryRun: true, GenerateNFO: true}, newResult(1))
	out = buf.String()
	assert.Contains(t, out, "Would organize 1 file(s)")
	assert.Contains(t, out, "NFOs generated: 1 (dry-run)")
	assert.Contains(t, out, "Files organized: 1 (dry-run)")

	// A pathological skip surplus clamps at zero rather than printing a
	// negative count.
	buf.Reset()
	defaultSummaryPrinter(&buf, BatchCommandOptions{GenerateNFO: true}, BatchCommandResult{
		ScanResult:        &workflow.ScanAndMatchResult{},
		SuccessCount:      1,
		SkippedDuplicates: 2,
	})
	out = buf.String()
	assert.Contains(t, out, "Organized 0 file(s)")
	assert.Contains(t, out, "Files organized: 0")

	// Update-mode lines are driven by len(Movies) and stay untouched; the skip
	// line still reports truthfully if a skip somehow occurred.
	buf.Reset()
	defaultSummaryPrinter(&buf, BatchCommandOptions{SkipOrganize: true}, BatchCommandResult{
		ScanResult:        &workflow.ScanAndMatchResult{},
		SuccessCount:      2,
		SkippedDuplicates: 1,
		FailedCount:       0,
		Movies:            map[string]*models.Movie{"GOOD-700": {}},
	})
	out = buf.String()
	assert.Contains(t, out, "Updated: 1, Failed: 0")
	assert.Contains(t, out, "Skipped (authorized duplicates): 1")
}

// ---------------------------------------------------------------------------
// cliBatchPostApply unit pins
// ---------------------------------------------------------------------------

// recordingEmitter captures emitted organize events without a database.
type recordingEmitter struct {
	mu     sync.Mutex
	events []models.Event
}

func (e *recordingEmitter) record(source string, sev models.EventSeverity, message string, ctx map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, models.Event{EventType: models.EventCategoryOrganize, Source: source, Severity: sev, Message: message})
}

func (e *recordingEmitter) EmitScraperEvent(_ context.Context, source string, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	e.record(source, severity, message, eventCtx)
	return nil
}

func (e *recordingEmitter) EmitOrganizeEvent(_ context.Context, source string, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	e.record(source, severity, message, eventCtx)
	return nil
}

func (e *recordingEmitter) EmitSystemEvent(_ context.Context, source string, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	e.record(source, severity, message, eventCtx)
	return nil
}

func (e *recordingEmitter) Stats() (int64, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return int64(len(e.events)), 0
}

func (e *recordingEmitter) snapshot() []models.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]models.Event(nil), e.events...)
}

func TestCliBatchPostApply_SuccessWithDuplicateSkipWarning(t *testing.T) {
	var (
		emitter   = &recordingEmitter{}
		buf       bytes.Buffer
		skipCount = &atomic.Int64{}
		printMu   = &sync.Mutex{}
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, false, skipCount, printMu)

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: filepath.Join("src", "GOOD-700.mp4"), Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{Result: &workflow.ApplyResult{
			OrganizeResult: &organizer.OrganizeResult{
				NewPath:          filepath.Join("dest", "GOOD-700", "GOOD-700.mp4"),
				Warnings:         []string{"duplicate destination within batch: ... already claimed by /src/a/GOOD-700.mp4 (overwrite authorized)"},
				DuplicateSkipped: true,
			},
		}},
	)

	events := emitter.snapshot()
	require.Len(t, events, 2, "info event + warning event")
	assert.Equal(t, models.SeverityInfo, events[0].Severity)
	assert.Equal(t, "file_move", events[0].Source, "organize mode keeps the file_move taxonomy")
	assert.Equal(t, "Organized GOOD-700", events[0].Message)
	assert.Equal(t, models.SeverityWarn, events[1].Severity)
	assert.Equal(t, "file_move", events[1].Source)
	assert.Contains(t, events[1].Message, "duplicate destination within batch")
	assert.Equal(t, int64(1), skipCount.Load(), "skip counted for the summary")
	assert.Contains(t, buf.String(), "⚠️")
	assert.Contains(t, buf.String(), "duplicate destination within batch")
}

func TestCliBatchPostApply_ApplyError_EmitsErrorEvent(t *testing.T) {
	var (
		emitter   = &recordingEmitter{}
		buf       bytes.Buffer
		skipCount = &atomic.Int64{}
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, false, skipCount, &sync.Mutex{})

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{Err: fmt.Errorf("disk full")},
	)

	events := emitter.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, models.SeverityError, events[0].Severity)
	assert.Equal(t, "file_move", events[0].Source)
	assert.Contains(t, events[0].Message, "Organize failed for GOOD-700")
	assert.Empty(t, buf.String(), "failures surface via the event handler, not warning prints")
}

func TestCliBatchPostApply_DryRunEmitsNothing(t *testing.T) {
	var (
		emitter   = &recordingEmitter{}
		buf       bytes.Buffer
		skipCount = &atomic.Int64{}
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", true, false, skipCount, &sync.Mutex{})

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{Result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{
			Warnings:         []string{"w"},
			DuplicateSkipped: true,
		}}},
	)

	assert.Empty(t, emitter.snapshot())
	assert.Empty(t, buf.String())
}

func TestCliBatchPostApply_NilPayloads(t *testing.T) {
	hook := cliBatchPostApply(&recordingEmitter{}, &bytes.Buffer{}, "job-1", false, false, &atomic.Int64{}, &sync.Mutex{})
	movie := &models.Movie{ID: "GOOD-700"}
	afc := &worker.ApplyFileContext{FilePath: "x", Movie: movie}
	afr := &worker.ApplyFileResult{}

	hook(context.Background(), nil, afr)                                     // nil context
	hook(context.Background(), &worker.ApplyFileContext{FilePath: "x"}, afr) // nil movie
	hook(context.Background(), afc, nil)                                     // nil result

	// Success with NO OrganizeResult (result nil) still emits the info event.
	emitter := &recordingEmitter{}
	hook = cliBatchPostApply(emitter, &bytes.Buffer{}, "job-1", false, false, &atomic.Int64{}, &sync.Mutex{})
	hook(context.Background(), afc, &worker.ApplyFileResult{})
	events := emitter.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, models.SeverityInfo, events[0].Severity)
}

// ---------------------------------------------------------------------------
// Update-mode event taxonomy pins (#248 codex P2, F3): javinizer update runs
// (SkipOrganize) must speak the API update resolver's nfo_gen vocabulary and
// emit NO file_move success events — an in-place metadata refresh is not a
// file move.
// ---------------------------------------------------------------------------

// updateModeEvents runs the hook in update mode against the given result and
// returns the recorded events.
func updateModeEvents(t *testing.T, emitter *recordingEmitter, afr *worker.ApplyFileResult) []models.Event {
	t.Helper()
	var buf bytes.Buffer
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, true, &atomic.Int64{}, &sync.Mutex{})
	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: filepath.Join("src", "GOOD-700.mp4"), Movie: &models.Movie{ID: "GOOD-700"}},
		afr,
	)
	return emitter.snapshot()
}

// assertNoFileMoveEvents fails when any recorded event uses the organize-mode
// file_move source.
func assertNoFileMoveEvents(t *testing.T, events []models.Event) {
	t.Helper()
	for _, ev := range events {
		assert.NotEqual(t, "file_move", ev.Source, "update mode must never emit file_move events: %q", ev.Message)
	}
}

func TestCliBatchPostApply_UpdateMode_Success_NfoGenTaxonomy(t *testing.T) {
	emitter := &recordingEmitter{}
	events := updateModeEvents(t, emitter, &worker.ApplyFileResult{Result: &workflow.ApplyResult{}})
	require.Len(t, events, 1)
	assert.Equal(t, models.SeverityInfo, events[0].Severity)
	assert.Equal(t, "nfo_gen", events[0].Source, "update-mode success rides the API update path's nfo_gen taxonomy")
	assert.Equal(t, "Updated GOOD-700", events[0].Message)
	assertNoFileMoveEvents(t, events)
}

func TestCliBatchPostApply_UpdateMode_SuccessWithWarning(t *testing.T) {
	emitter := &recordingEmitter{}
	events := updateModeEvents(t, emitter, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{Warnings: []string{"nfo merged partial"}},
	}})
	require.Len(t, events, 2)
	assert.Equal(t, "nfo_gen", events[0].Source)
	assert.Equal(t, "Updated GOOD-700", events[0].Message)
	assert.Equal(t, models.SeverityWarn, events[1].Severity)
	assert.Equal(t, "nfo_gen", events[1].Source)
	assert.Equal(t, "Update warning for GOOD-700: nfo merged partial", events[1].Message)
	assertNoFileMoveEvents(t, events)
}

func TestCliBatchPostApply_UpdateMode_ApplyError(t *testing.T) {
	emitter := &recordingEmitter{}
	events := updateModeEvents(t, emitter, &worker.ApplyFileResult{Err: fmt.Errorf("read only")})
	require.Len(t, events, 1)
	assert.Equal(t, models.SeverityError, events[0].Severity)
	assert.Equal(t, "nfo_gen", events[0].Source, "update-mode failure parity with the API update resolver")
	assert.Equal(t, "Update failed for GOOD-700", events[0].Message)
	assertNoFileMoveEvents(t, events)
}
