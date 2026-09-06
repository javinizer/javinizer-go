package commandutil

// Regression pins for #248 codex P2: when the initial jobs-row persist fails
// inside JobStore.createJob (shallow disk-full / locked database AFTER
// bootstrap succeeded), the CLI batch must report a real STARTUP failure —
// RunBatchCommand returns a non-nil, ErrJobPersistenceFailed-matching error,
// the console lands on the NOT-persisted audit branch (never advertising the
// unqueryable pre-generated id) and says setup failed, and `history list
// --batch <id>` answers 'batch job not found' consistently with that message.
//
// Fixtures build on the batch_history_w244_test.go pattern (the e2emock
// scraper seam, filepath.Join/t.TempDir paths) so the tests stay
// Windows-safe.

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunBatchCommand_InitialPersistFailure_NotPersistedBranchAndSetupFailure
// injects the failing persist seed through the runtime-construction seam and
// pins the end-to-end contract: error propagation, the NOT-persisted console
// branch, the setup-failed statement, and history-lookup 'not found'
// consistency for the never-persisted id.
func TestRunBatchCommand_InitialPersistFailure_NotPersistedBranchAndSetupFailure(t *testing.T) {
	configPath, src, dest, dbPath := setupDuplicateBatch(t)

	// Failing persist seed: the runtime constructor reports the jobs-row
	// persist failure exactly like newCLIBatchRuntime does against a real
	// failing store (sentinel-wrapped so RunBatchCommand's errors.Is branch
	// triggers), and captures the pre-generated batch id for the history
	// consistency assertions below.
	var seededID string
	orig := newCLIBatchRuntimeFn
	t.Cleanup(func() { newCLIBatchRuntimeFn = orig })
	newCLIBatchRuntimeFn = func(_ *bootstrapResult, _ *config.Config, o BatchCommandOptions, _ worker.BatchJobConfig, _ []string) (*cliBatchRuntime, error) {
		seededID = o.BatchJobID
		return nil, fmt.Errorf("%w: upsert failed: simulated disk full", ErrJobPersistenceFailed)
	}

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})

	// Error propagation: non-nil, sentinel-visible through both wraps, and
	// carrying the underlying failure detail.
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJobPersistenceFailed)
	assert.Contains(t, err.Error(), "simulated disk full")
	require.NotEmpty(t, seededID, "the failing seed observed the pre-generated batch id")

	out := buf.String()
	// NOT-persisted branch: the presenter never prints the queryable id; the
	// no-audit sentence and the setup-failed statement carry the run's truth.
	assert.NotContains(t, out, "Batch Job: "+seededID, "the unqueryable id must not be advertised\n%s", out)
	assert.NotContains(t, out, "Batch Job:", "no persisted identity line at all on the failure branch\n%s", out)
	assert.Contains(t, out, "Preview (not persisted; no audit ID)", "the NOT-persisted audit line prints\n%s", out)
	assert.Contains(t, out, "Batch setup failed", "the summary states the setup failure\n%s", out)
	assert.Contains(t, out, ErrJobPersistenceFailed.Error(), "the failure statement names the cause class\n%s", out)

	// History lookup consistency: the id the console deliberately did NOT
	// advertise answers 'batch job not found' exactly like the messaging
	// claims — no jobs row, no history rows, no revert-ledger rows.
	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()
	_, findErr := repos.JobRepo.FindByID(ctx, seededID)
	assert.True(t, database.IsNotFound(findErr),
		"history list --batch %s must answer 'batch job not found', got: %v", seededID, findErr)
	hist, histErr := repos.HistoryRepo.FindByBatchJobID(ctx, seededID)
	require.NoError(t, histErr)
	assert.Empty(t, hist, "no history rows bind the never-persisted batch id")
	opCount, opErr := repos.BatchFileOpRepo.CountByBatchJobID(ctx, seededID)
	require.NoError(t, opErr)
	assert.Zero(t, opCount, "no revert-ledger rows bind the never-persisted batch id")
}

// TestRunBatchCommand_InitialPersistFailure_DryRunSkipsAuditLine pins the
// dry-run leg of the error branch: a dry run persists nothing BY DESIGN and
// the header already carried the preview label, so the failure surface prints
// only the setup-failed statement — no duplicate no-audit sentence.
func TestRunBatchCommand_InitialPersistFailure_DryRunSkipsAuditLine(t *testing.T) {
	configPath, src, dest, _ := setupDuplicateBatch(t)

	orig := newCLIBatchRuntimeFn
	t.Cleanup(func() { newCLIBatchRuntimeFn = orig })
	newCLIBatchRuntimeFn = func(_ *bootstrapResult, _ *config.Config, _ BatchCommandOptions, _ worker.BatchJobConfig, _ []string) (*cliBatchRuntime, error) {
		return nil, fmt.Errorf("%w: upsert failed: simulated disk full", ErrJobPersistenceFailed)
	}

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		DryRun:       true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJobPersistenceFailed)

	out := buf.String()
	assert.Equal(t, 1, bytes.Count([]byte(out), []byte("Preview (not persisted; no audit ID)")),
		"only the header's preview label prints — the failure branch adds no second no-audit sentence\n%s", out)
	assert.Contains(t, out, "Batch setup failed")
}

// TestNewCLIBatchRuntime_InitialPersistFailure_ReturnsSentinel drives the
// REAL runtime constructor against a real-but-closed jobs database — the
// shallow post-bootstrap failure class from the finding (disk full / locked):
// every jobs-row upsert now fails, so the constructor must surface
// ErrJobPersistenceFailed instead of returning a live job whose id the CLI
// would advertise. The dry-run noop-persistence path stays silent (previews
// persist nothing by design), proving the good dry path is unchanged.
func TestNewCLIBatchRuntime_InitialPersistFailure_ReturnsSentinel(t *testing.T) {
	configPath, _, _, _ := setupDuplicateBatch(t)

	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	config.ApplyEnvironmentOverrides(cfg)
	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	bs, err := Bootstrap(cfg)
	require.NoError(t, err)

	// The jobs database went away AFTER bootstrap: every jobs-row upsert now
	// fails with 'sql: database is closed' (deterministic on every platform,
	// including Windows — a closed database/sql pool never reopens).
	require.NoError(t, bs.Close())

	files := []string{filepath.Join("src", "GOOD-700.mp4")}
	rt, err := newCLIBatchRuntime(bs, cfg,
		BatchCommandOptions{BatchJobID: models.NewJobID().String()},
		BatchJobConfigFromAppConfig(cfg), files)
	require.Error(t, err, "a failed initial jobs-row persist must become a startup error")
	assert.Nil(t, rt)
	assert.ErrorIs(t, err, ErrJobPersistenceFailed)

	// Dry-run parity: NoopJobPersistence previews persist nothing by design,
	// so the dead database handle produces NO sentinel and the runtime still
	// constructs — the good dry path is byte-identical to pre-fix behavior.
	rt, err = newCLIBatchRuntime(bs, cfg,
		BatchCommandOptions{BatchJobID: models.NewJobID().String(), DryRun: true},
		BatchJobConfigFromAppConfig(cfg), files)
	assert.NoError(t, err, "dry-run previews never persist, so a dead DB cannot fail them")
	assert.NotNil(t, rt)
}

// TestRunBatchCommand_UnrelatedRuntimeError_UnchangedSurface pins the
// non-persist runtime error branch: a plain construction failure (no sentinel)
// keeps its pre-fix surface — wrapped error, NO audit line, NO setup-failed
// print (the persist-failure presentation is gated on errors.Is).
func TestRunBatchCommand_UnrelatedRuntimeError_UnchangedSurface(t *testing.T) {
	configPath, src, dest, _ := setupDuplicateBatch(t)

	var seededID string
	orig := newCLIBatchRuntimeFn
	t.Cleanup(func() { newCLIBatchRuntimeFn = orig })
	newCLIBatchRuntimeFn = func(_ *bootstrapResult, _ *config.Config, o BatchCommandOptions, _ worker.BatchJobConfig, _ []string) (*cliBatchRuntime, error) {
		seededID = o.BatchJobID
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
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrJobPersistenceFailed)
	assert.Contains(t, err.Error(), "failed to create batch job runtime")
	assert.Contains(t, err.Error(), "simulated runtime failure")

	out := buf.String()
	assert.NotContains(t, out, "Batch setup failed", "generic runtime errors keep the pre-fix silent-console surface\n%s", out)
	assert.NotContains(t, out, "Batch Job: "+seededID, "no audit identity ever prints on the unconstructed runtime\n%s", out)
}
