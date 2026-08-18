package history

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 review — the CLI's pre-revert replacement sweep is
// bounded: scoped to the target batch's roots, time-capped, deadline-tolerant.
// The sweep seams are swapped for capture/blocking doubles; t.Cleanup always
// restores the production wiring.

type capturedSweep struct {
	mu        sync.Mutex
	scoped    bool
	roots     []string
	sawCtxErr error
	deadline  time.Duration
}

func (c *capturedSweep) record(ctx context.Context, roots []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scoped = true
	c.roots = roots
	if dl, ok := ctx.Deadline(); ok {
		c.deadline = time.Until(dl)
	}
}

func swapSweepSeams(t *testing.T) *capturedSweep {
	t.Helper()
	cap := &capturedSweep{}
	oldScoped, oldFull, oldFind := preRevertScopedSweep, preRevertFullSweep, preRevertFindOps
	t.Cleanup(func() {
		preRevertScopedSweep, preRevertFullSweep, preRevertFindOps = oldScoped, oldFull, oldFind
	})
	return cap
}

// TestRunPreRevertSweep_ScopedSweepReceivesExactlyOpRoots: the target batch's
// operations are resolved FIRST and the scoped sweep receives exactly their
// unique root set (media columns + generated-files ledger).
func TestRunPreRevertSweep_ScopedSweepReceivesExactlyOpRoots(t *testing.T) {
	cap := swapSweepSeams(t)
	ops := []models.BatchFileOperation{
		{
			BatchJobID:   "job-scoped",
			MovieID:      "SCP-001",
			OriginalPath: "/src/scp-001.mkv",
			NewPath:      "/dst/scp-001/scp-001.mkv",
			GeneratedFiles: `{"delete":["/dst/scp-001/scp-001.nfo"],"roots":["/dst/scp-001/nested"],` +
				`"replacements":[{"destination":"/dst/scp-001/poster.jpg","backup":"/dst/scp-001/poster.jpg.dlbak.0123456789abcdef","dest_seq":1}]}`,
			RevertStatus: models.RevertStatusApplied,
		},
	}
	preRevertFindOps = func(_ context.Context, _ database.BatchFileOperationRepositoryInterface, batchID string) ([]models.BatchFileOperation, error) {
		require.Equal(t, "job-scoped", batchID)
		return ops, nil
	}
	fullCalled := false
	preRevertFullSweep = func(context.Context, afero.Fs, database.BatchFileOperationRepositoryInterface) {
		fullCalled = true
	}
	preRevertScopedSweep = func(ctx context.Context, _ afero.Fs, _ database.BatchFileOperationRepositoryInterface, roots []string) {
		cap.record(ctx, roots)
	}

	runPreRevertReplacementSweep(context.Background(), nil, "job-scoped")

	require.True(t, cap.scoped, "scoped sweep ran")
	require.False(t, fullCalled, "full sweep must not run when op resolution succeeds")

	expected := []string{
		filepath.Dir("/dst/scp-001/scp-001.mkv"), // media + delete + replacement dest + backup all share it
		"/dst/scp-001/nested",
		filepath.Dir("/src/scp-001.mkv"),
	}
	sort.Strings(expected)
	require.Equal(t, expected, cap.roots, "exactly the op's unique root set, no unrelated journaled roots")

	require.Greater(t, cap.deadline, 25*time.Second, "the sweep ctx carries (nearly) the full 30s timeout")
	require.LessOrEqual(t, cap.deadline, 30*time.Second)
}

// TestRunPreRevertSweep_OpResolutionFailureFallsBackToAllRoots: when the
// target batch's operations cannot be resolved, the sweep falls back to the
// previous all-journaled-roots behavior rather than skipping the heal.
func TestRunPreRevertSweep_OpResolutionFailureFallsBackToAllRoots(t *testing.T) {
	cap := swapSweepSeams(t)
	sentinel := errors.New("db wedged mid-revert")
	preRevertFindOps = func(context.Context, database.BatchFileOperationRepositoryInterface, string) ([]models.BatchFileOperation, error) {
		return nil, sentinel
	}
	fullCalled := false
	preRevertFullSweep = func(ctx context.Context, _ afero.Fs, _ database.BatchFileOperationRepositoryInterface) {
		fullCalled = true
		if dl, ok := ctx.Deadline(); ok {
			cap.mu.Lock()
			cap.deadline = time.Until(dl)
			cap.mu.Unlock()
		}
	}
	preRevertScopedSweep = func(context.Context, afero.Fs, database.BatchFileOperationRepositoryInterface, []string) {
		cap.mu.Lock()
		cap.scoped = true
		cap.mu.Unlock()
	}

	runPreRevertReplacementSweep(context.Background(), nil, "job-broken")

	require.True(t, fullCalled, "all-roots fallback sweep ran")
	require.False(t, cap.scoped, "scoped swept must not run without a resolved op set")
	require.Greater(t, cap.deadline, 25*time.Second, "the fallback sweep is time-capped too")
}

// TestRunPreRevertSweep_DeadlineExceededLogsAndProceeds: a sweep that hangs
// past its cap is cut by the sweep context — the revert is never aborted.
func TestRunPreRevertSweep_DeadlineExceededLogsAndProceeds(t *testing.T) {
	cap := swapSweepSeams(t)
	oldTimeout := preRevertSweepTimeout
	preRevertSweepTimeout = 30 * time.Millisecond
	t.Cleanup(func() { preRevertSweepTimeout = oldTimeout })

	preRevertFindOps = func(context.Context, database.BatchFileOperationRepositoryInterface, string) ([]models.BatchFileOperation, error) {
		return []models.BatchFileOperation{{BatchJobID: "job-hang", OriginalPath: "/src/h.mkv"}}, nil
	}
	preRevertScopedSweep = func(ctx context.Context, _ afero.Fs, _ database.BatchFileOperationRepositoryInterface, _ []string) {
		<-ctx.Done() // hang until the sweep deadline kills us (a stuck FS stand-in)
		cap.mu.Lock()
		cap.sawCtxErr = ctx.Err()
		cap.mu.Unlock()
	}
	preRevertFullSweep = func(context.Context, afero.Fs, database.BatchFileOperationRepositoryInterface) {
		t.Error("full sweep must not run")
	}

	start := time.Now()
	runPreRevertReplacementSweep(context.Background(), nil, "job-hang")
	elapsed := time.Since(start)

	require.Less(t, elapsed, 25*time.Second, "the caller proceeds at the deadline instead of hanging on the sweep")
	// Since wave-8 the caller stops waiting at the deadline and then cancels
	// the sweep context; the stub records the teardown error from its own
	// goroutine, so wait for that record rather than sampling synchronously.
	require.Eventually(t, func() bool {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		return cap.sawCtxErr != nil
	}, 2*time.Second, 5*time.Millisecond, "the sweep context is torn down after the deadline")
	cap.mu.Lock()
	defer cap.mu.Unlock()
	require.Error(t, cap.sawCtxErr, "the sweep observes the deadline teardown")
}

// TestRunHistoryRevert_PreRevertSweepHealsInScopeOnly drives the full command
// against a real DB + real filesystem: the crash window of the TARGET batch
// is healed before revert, while another job's journaled crash window in an
// unrelated directory is NOT scanned (pre-fix, the all-roots startup sweep
// would have healed it).
func TestRunHistoryRevert_PreRevertSweepHealsInScopeOnly(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	ctx := context.Background()
	opRepo := database.NewBatchFileOperationRepository(db)

	seedCrashWindowOp := func(batchID, movieID, hexTag string) (dir string, dest, backup string) {
		dir = t.TempDir()
		dest = filepath.Join(dir, "poster.jpg")
		backup = dest + ".dlbak." + hexTag
		require.NoError(t, afero.WriteFile(afero.NewOsFs(), backup, []byte("original-"+movieID), 0o644))
		require.NoError(t, opRepo.Create(ctx, &models.BatchFileOperation{
			BatchJobID:    batchID,
			MovieID:       movieID,
			OriginalPath:  filepath.Join(dir, movieID+".mkv"),
			NewPath:       filepath.Join(dir, movieID+".mkv"), // in-place update shape: anchor present iff file exists
			OperationType: models.OperationTypeUpdate,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				Replacements: []models.ReplacementEntry{{Destination: filepath.ToSlash(dest), Backup: filepath.ToSlash(backup), DestSeq: 1}},
			}),
			RevertStatus: models.RevertStatusApplied,
		}))
		return dir, dest, backup
	}

	createOrganizedJob(t, db, "job-target-sweep")
	createOrganizedJob(t, db, "job-bystander-sweep")
	_, destTarget, _ := seedCrashWindowOp("job-target-sweep", "TRG-001", "0123456789abcdef")
	_, destBystander, backupBystander := seedCrashWindowOp("job-bystander-sweep", "BYS-001", "fedcba9876543210")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", "job-target-sweep"})

	output := captureMissOutput(t, func() {
		require.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, output, "Reverted batch")

	// Target crash window: healed — the destination is its pre-replace bytes
	// again and the journal entry was consumed.
	require.Equal(t, "original-TRG-001", string(readOSFile(t, destTarget)), "in-scope crash window healed before the revert")
	targetRow := findOpByMovie(t, ctx, opRepo, "job-target-sweep")
	targetGf, err := models.ParseGeneratedFiles(targetRow.GeneratedFiles)
	require.NoError(t, err)
	assert.Empty(t, targetGf.Replacements, "in-scope journal entry consumed")

	// Bystander crash window: completely untouched — scope pin.
	exists, err := afero.Exists(afero.NewOsFs(), backupBystander)
	require.NoError(t, err)
	assert.True(t, exists, "out-of-scope backup must not be consumed by a scoped sweep")
	destByExists, err := afero.Exists(afero.NewOsFs(), destBystander)
	require.NoError(t, err)
	assert.False(t, destByExists, "out-of-scope destination must not be restored by a scoped sweep")
	bystanderRow := findOpByMovie(t, ctx, opRepo, "job-bystander-sweep")
	bystanderGf, err := models.ParseGeneratedFiles(bystanderRow.GeneratedFiles)
	require.NoError(t, err)
	assert.Len(t, bystanderGf.Replacements, 1, "out-of-scope journal entry untouched")
}

func readOSFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(afero.NewOsFs(), path)
	require.NoError(t, err)
	return data
}

func findOpByMovie(t *testing.T, ctx context.Context, repo *database.BatchFileOperationRepository, batchID string) models.BatchFileOperation {
	t.Helper()
	ops, err := repo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	return ops[0]
}
