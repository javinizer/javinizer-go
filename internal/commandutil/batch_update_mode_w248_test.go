package commandutil

// Regression pins for #248 codex P2 (F1 + F2):
//
// F1 — persisted CLI update jobs misclassified as organize: a non-dry-run
// `update` run must persist the API's update identity on the jobs row
// (update=true + operation_mode=metadata-artwork, the same projection the
// API's leave-in-place plan commits) instead of update=false + empty mode.
// F2 — audit writes with an expired per-file ctx dropped silently: the
// post-apply hook must emit to the eventlog with a context detached from the
// WorkerTimeout deadline so timeout failures still land, and log (not
// discard) if emission still fails.
//
// Fixtures follow batch_history_w244_test.go conventions: the
// JAVINIZER_E2E_SCRAPERS seam substitutes the offline e2emock scraper, and
// all paths are built with filepath.Join/t.TempDir (never POSIX literals) so
// the tests stay Windows-safe.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSingleFileBatch writes a one-file fixture: src/<ID>.mp4 matched by the
// e2emock scraper. The update run leaves the video in place; the sort control
// copies it into a separate dest root.
func setupSingleFileBatch(t *testing.T, id string) (configPath, src, dest, dbPath string) {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	tmpDir := t.TempDir()
	src = filepath.Join(tmpDir, "src")
	dest = filepath.Join(tmpDir, "dest")
	require.NoError(t, os.MkdirAll(src, 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(src, id+".mp4"), []byte("fake video"), 0o600))

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

// ---------------------------------------------------------------------------
// F1: persisted job identity pins
// ---------------------------------------------------------------------------

// TestCLIApplyOptions_ToApplyPhaseConfig_UpdateModeJobIdentity pins the
// ApplyPhaseConfig-side mapping an update-mode apply commits onto the job at
// apply entry: the same identity an API leave-in-place batch carries.
func TestCLIApplyOptions_ToApplyPhaseConfig_UpdateModeJobIdentity(t *testing.T) {
	upd := CLIApplyOptions{SkipOrganize: true}.ToApplyPhaseConfig()
	require.NotNil(t, upd.Update, "update mode must commit an explicit update=true")
	assert.True(t, *upd.Update)
	assert.Equal(t, operationmode.OperationModeMetadataArtwork, upd.OperationModeOverride,
		"CLI update (leave-in-place) must persist the API update path's mode")

	org := CLIApplyOptions{}.ToApplyPhaseConfig()
	require.NotNil(t, org.Update, "organize mode must commit an explicit update=false")
	assert.False(t, *org.Update)
	assert.Equal(t, operationmode.OperationMode(""), org.OperationModeOverride,
		"organize runs keep an empty mode override so the organizer's configured mode still governs planning")
}

// TestRunBatchCommand_UpdateMode_PersistsUpdateJobIdentity is the F1 core
// pin: a live CLI update run must persist a jobs row reading update=true +
// mode metadata-artwork (NOT misclassified as organize), and its eventlog
// audit must carry the update taxonomy (nfo_gen "Updated <id>", zero
// file_move events) with the video left in place.
func TestRunBatchCommand_UpdateMode_PersistsUpdateJobIdentity(t *testing.T) {
	configPath, src, _, dbPath := setupSingleFileBatch(t, "GOOD-701")

	resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{})
	require.NoError(t, err)

	var buf bytes.Buffer
	runErr := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  src, // update mode: files stay in place (mirrors the update command)
		Recursive:    true,
		SkipOrganize: true,
		GenerateNFO:  true,
		CommandLabel: "Javinizer Update",
		ActionVerb:   "Updating metadata",
		Resolved:     resolved,
	})
	require.NoError(t, runErr)
	batchID := batchIDFromOutput(t, buf.String())

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	// (i) jobs row: update identity persisted, not organize-misclassified.
	job, err := repos.JobRepo.FindByID(ctx, batchID)
	require.NoError(t, err, "jobs row for the CLI update batch must persist")
	require.NotNil(t, job)
	assert.True(t, job.Update, "update run must persist update=true, not the default false")
	assert.Equal(t, operationmode.OperationModeMetadataArtwork, job.OperationModeOverride,
		"update run must persist the API leave-in-place mode, not the empty/organize default")

	// (ii) eventlog: update taxonomy — an nfo_gen "Updated" info event and
	// NO file_move events (an in-place metadata refresh is not a file move).
	updateEvents, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Source:    "nfo_gen",
	}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, updateEvents, "update run must emit nfo_gen audit events")
	assert.Equal(t, "Updated GOOD-701", updateEvents[0].Message)
	assert.Equal(t, models.SeverityInfo, updateEvents[0].Severity)

	fileMoveEvents, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Source:    "file_move",
	}, 50, 0)
	require.NoError(t, err)
	assert.Empty(t, fileMoveEvents, "update run must never emit file_move events")

	// (iii) update semantics: the video stayed in place and the metadata
	// refresh still produced its NFO next to the removed-nowhere source.
	assert.FileExists(t, filepath.Join(src, "GOOD-701.mp4"))
	assert.FileExists(t, filepath.Join(src, "GOOD-701.nfo"))
	assert.NoDirExists(t, filepath.Join(src, "GOOD-701"),
		"update mode must not organize the video into a folder")
}

// TestRunBatchCommand_OrganizeMode_PersistsUpdateFalseJobIdentity is the F1
// control: a live sort run keeps update=false so update batches and sort
// batches classify disjointly.
func TestRunBatchCommand_OrganizeMode_PersistsUpdateFalseJobIdentity(t *testing.T) {
	configPath, src, dest, dbPath := setupSingleFileBatch(t, "GOOD-702")

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		GenerateNFO:  true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	batchID := batchIDFromOutput(t, buf.String())

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	job, err := db.Repositories().JobRepo.FindByID(ctx, batchID)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.False(t, job.Update, "sort must persist update=false")
	assert.Empty(t, string(job.OperationModeOverride),
		"sort persists no mode override — consumers read it as organize, unchanged")
	assert.Equal(t, models.JobStatusOrganized, job.Status)
}

// ---------------------------------------------------------------------------
// F2: detached-ctx audit emission pins
// ---------------------------------------------------------------------------

// newMigratedEventDB opens a fresh sqlite database and runs startup
// migrations so the eventlog repos tables exist (unlike openAssertionDB,
// which re-opens a DB Bootstrap already migrated).
func newMigratedEventDB(t *testing.T) *database.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "javinizer.db")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

// TestCliBatchPostApply_TimeoutExpiredCtx_StillAudits reproduces the F2 drop:
// a WorkerTimeout-exhausted apply hands the post-apply hook an ALREADY
// canceled task ctx together with the deadline-exceeded error, and the
// eventlog emitter refuses canceled contexts — pre-fix the timeout failure
// event vanished from the CLI eventlog while the history writer (fresh ctx)
// still recorded it. The hook must detach from the worker deadline so the
// audit lands: real emitter + real sqlite readback.
func TestCliBatchPostApply_TimeoutExpiredCtx_StillAudits(t *testing.T) {
	db := newMigratedEventDB(t)
	repos := db.Repositories()
	emitter := eventlog.NewEmitter(repos.EventRepo)

	expired, cancel := context.WithCancel(context.Background())
	cancel()                                // the WorkerTimeout-exhausted per-file task ctx
	deadlineErr := context.DeadlineExceeded // what wf.Apply returns on timeout

	afc := &worker.ApplyFileContext{
		FilePath: filepath.Join("src", "GOOD-703.mp4"),
		Movie:    &models.Movie{ID: "GOOD-703"},
	}
	afr := &worker.ApplyFileResult{Err: deadlineErr}

	// Pre-fix sanity: emitting with the expired ctx IS dropped by the real
	// emitter, proving this test pins the hook's detached-ctx behavior and
	// not the emitter's.
	preFixCount, err := repos.EventRepo.Count(context.Background())
	require.NoError(t, err)
	require.Error(t, emitter.EmitOrganizeEvent(expired, "file_move", "probe", models.SeverityError, nil))
	postProbeCount, err := repos.EventRepo.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, preFixCount, postProbeCount, "the emitter drops canceled-ctx events — this is the F2 mechanism")

	// The hook as interpretApplyResult invokes it at timeout: detached
	// emission must land the timeout failure with valid severity/message.
	hook := cliBatchPostApply(emitter, nopWriter{}, "job-timeout-248", false, false, &atomic.Int64{}, &sync.Mutex{})
	hook(expired, afc, afr)

	events, err := repos.EventRepo.FindFiltered(context.Background(), database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityError,
	}, 50, 0)
	require.NoError(t, err)
	require.Len(t, events, 1, "the timeout failure event must land in the CLI eventlog — no silent drop")
	assert.Equal(t, "file_move", events[0].Source)
	assert.Equal(t, "Organize failed for GOOD-703", events[0].Message)
	assert.Contains(t, events[0].Context, `"error":"context deadline exceeded"`, "audit context carries the timeout cause")
	assert.Contains(t, events[0].Context, `"job_id":"job-timeout-248"`)

	// The update taxonomy also survives the expired ctx — same detached budget.
	hookUpdate := cliBatchPostApply(emitter, nopWriter{}, "job-timeout-248", false, true, &atomic.Int64{}, &sync.Mutex{})
	hookUpdate(expired, afc, afr)

	updateEvents, err := repos.EventRepo.FindFiltered(context.Background(), database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Source:    "nfo_gen",
	}, 50, 0)
	require.NoError(t, err)
	require.Len(t, updateEvents, 1)
	assert.Equal(t, "Update failed for GOOD-703", updateEvents[0].Message)
	assert.Equal(t, models.SeverityError, updateEvents[0].Severity)
}

// nopWriter discards console side output without pulling in io.Discard's
// wrapper import surface — the hook's console prints are orthogonal here.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestCliBatchPostApply_SuccessWithExpiredCtx_StillAudits pins the symmetric
// case: an organized-success audit emitted after the task ctx expired (the
// runner's final accounting window) still reaches the eventlog.
func TestCliBatchPostApply_SuccessWithExpiredCtx_StillAudits(t *testing.T) {
	db := newMigratedEventDB(t)
	repos := db.Repositories()
	emitter := eventlog.NewEmitter(repos.EventRepo)

	expired, cancel := context.WithCancel(context.Background())
	cancel()

	hook := cliBatchPostApply(emitter, nopWriter{}, "job-success-248", false, false, &atomic.Int64{}, &sync.Mutex{})
	hook(expired,
		&worker.ApplyFileContext{
			FilePath: filepath.Join("src", "GOOD-704.mp4"),
			Movie:    &models.Movie{ID: "GOOD-704"},
		},
		&worker.ApplyFileResult{Result: &workflow.ApplyResult{}},
	)

	events, err := repos.EventRepo.FindFiltered(context.Background(), database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityInfo,
	}, 50, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "Organized GOOD-704", events[0].Message)
}

// failingEmitter always fails emission so the hook's log-and-continue path
// (F2: log, not discard) is exercised.
type failingEmitter struct{}

func (failingEmitter) EmitScraperEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return fmt.Errorf("simulated eventlog outage")
}
func (failingEmitter) EmitOrganizeEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return fmt.Errorf("simulated eventlog outage")
}
func (failingEmitter) EmitSystemEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return fmt.Errorf("simulated eventlog outage")
}
func (failingEmitter) Stats() (int64, int64) { return 0, 0 }

// TestCliBatchPostApply_EmissionFailure_LogsNotDiscards pins the F2 contract
// that an emission error is never silently discarded: the warning lands in
// the log carrying the batch identity, event detail, and underlying error.
func TestCliBatchPostApply_EmissionFailure_LogsNotDiscards(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "debug", Format: "text", Output: logPath}))
	t.Cleanup(func() { _ = logging.InitLogger(nil) })

	hook := cliBatchPostApply(failingEmitter{}, nopWriter{}, "job-log-248", false, false, &atomic.Int64{}, &sync.Mutex{})
	hook(context.Background(),
		&worker.ApplyFileContext{
			FilePath: filepath.Join("src", "GOOD-705.mp4"),
			Movie:    &models.Movie{ID: "GOOD-705"},
		},
		&worker.ApplyFileResult{Result: &workflow.ApplyResult{}},
	)

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logged := string(content)
	assert.Contains(t, logged, "eventlog audit emission failed")
	assert.Contains(t, logged, "job-log-248")
	assert.Contains(t, logged, "Organized GOOD-705")
	assert.Contains(t, logged, "simulated eventlog outage")
}
