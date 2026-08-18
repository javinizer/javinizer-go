package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W26 pins the gap between operation-lock active accounting and the keyed lock:
// a caller can be preempted after incrementing active, allowing a later caller
// to acquire the keyed lock first while still being classified as waited.
func TestRevertW26_PreemptAfterIncrementRefreshesBeforeAbort(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op := newW26UpdateOp(t, base, repo, 26001)
	calls := &w26RevertCalls{}

	locks := newOperationRevertLockRegistry()
	firstIncremented := make(chan struct{})
	releaseFirst := make(chan struct{})
	acquired := make(chan bool, 2)
	var firstGap atomic.Bool
	var releaseGap sync.Once
	locks.afterActiveIncrement = func(string) {
		if firstGap.CompareAndSwap(false, true) {
			close(firstIncremented)
			<-releaseFirst
		}
	}
	locks.afterAcquire = func(_ string, waited bool) {
		acquired <- waited
	}

	oldLocks := revertOperationLocks
	revertOperationLocks = locks
	t.Cleanup(func() {
		revertOperationLocks = oldLocks
		releaseGap.Do(func() { close(releaseFirst) })
	})

	r1 := NewReverter(base, repo)
	r2 := NewReverter(base, repo)
	r1.fsReverter = calls
	r2.fsReverter = calls
	first := *op
	second := *op
	type revertCall struct {
		result *RevertFileResult
		err    error
	}
	firstDone := make(chan revertCall, 1)
	secondDone := make(chan revertCall, 1)

	go func() {
		result, err := r1.revertFile(context.Background(), &first)
		firstDone <- revertCall{result: result, err: err}
	}()
	waitForW26(t, firstIncremented, "first revert did not reach the post-increment gap")

	go func() {
		result, err := r2.revertFile(context.Background(), &second)
		secondDone <- revertCall{result: result, err: err}
	}()

	select {
	case waited := <-acquired:
		require.True(t, waited, "the waiter classification must remain true even when it acquires first")
	case <-time.After(time.Second):
		t.Fatal("waiter did not acquire the keyed operation lock first")
	}

	select {
	case got := <-secondDone:
		require.NoError(t, got.err)
		require.NotNil(t, got.result)
		require.Equal(t, models.RevertOutcomeReverted, got.result.Outcome)
	case <-time.After(time.Second):
		t.Fatal("waiter did not complete the revert")
	}

	releaseGap.Do(func() { close(releaseFirst) })
	select {
	case waited := <-acquired:
		require.False(t, waited, "the preempted primary still acquired with waited=false")
	case <-time.After(time.Second):
		t.Fatal("preempted revert did not acquire the keyed operation lock")
	}
	select {
	case got := <-firstDone:
		require.ErrorIs(t, got.err, ErrBatchAlreadyReverted)
		require.Nil(t, got.result)
	case <-time.After(time.Second):
		t.Fatal("preempted revert did not finish after the waiter")
	}

	require.Equal(t, 1, calls.cleanupGeneratedCount())
	require.Equal(t, 1, calls.restoreNFOCount())
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	require.Equal(t, models.RevertStatusReverted, first.RevertStatus,
		"the preempted caller must resume from the fresh terminal row")
}

func TestRevertW26_FreshJournalStatusAbortsBeforePrimary(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	dest := "/dst/W26-FRESH/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	newPath := "/dst/W26-FRESH/W26-FRESH.mkv"
	require.NoError(t, base.MkdirAll(filepath.Dir(newPath), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, newPath, []byte("video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("new"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w26-fresh",
		MovieID:       "W26-FRESH",
		OriginalPath:  "/src/W26-FRESH.mkv",
		NewPath:       newPath,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, DestSeq: 1, Installed: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, baseRepo.Create(context.Background(), op))
	repo := &w26FreshStatusRepo{p3OpRepo: baseRepo}
	calls := &w26RevertCalls{}
	r := NewReverter(base, repo)
	r.fsReverter = calls
	stale := *op

	result, err := r.revertFile(context.Background(), &stale)
	require.ErrorIs(t, err, ErrBatchAlreadyReverted)
	require.Nil(t, result)
	require.Equal(t, 0, calls.primaryCount())
	require.Equal(t, 0, calls.cleanupGeneratedCount())
	require.Equal(t, 0, calls.restoreNFOCount())
	require.Equal(t, models.RevertStatusReverted, stale.RevertStatus)
}

func TestRestoreNFOHardFailureW26_MkdirError(t *testing.T) {
	repo := newP3OpRepo()
	op := &models.BatchFileOperation{ID: 26003, NFOSnapshot: "<original/>", NFOPath: "/w26-nfo/movie.nfo"}
	fs := &w26MkdirErrorFs{Fs: afero.NewMemMapFs()}

	warning, result := restoreNFOFS(context.Background(), fs, repo, op, true)
	require.Empty(t, warning)
	require.NotNil(t, result)
	require.Equal(t, models.RevertReasonNFORestoreFailed, result.Reason)
}

func TestRevertW26_SerialRevertRefreshesAndCompletesOnce(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op := newW26MoveOp(t, base, repo, 26002)
	calls := &w26RevertCalls{}
	r := NewReverter(base, repo)
	r.fsReverter = calls

	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	require.Equal(t, 1, calls.primaryCount())
	require.Equal(t, 1, calls.cleanupGeneratedCount())
	require.Equal(t, 1, calls.restoreNFOCount())

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
}

type w26FreshStatusRepo struct {
	*p3OpRepo
	mu    sync.Mutex
	calls int
}

func (r *w26FreshStatusRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	row, err := r.p3OpRepo.FindByID(ctx, id)
	if err != nil {
		return row, err
	}
	if call >= 2 {
		row.GeneratedFiles = ""
		row.RevertStatus = models.RevertStatusReverted
	}
	return row, nil
}

type w26MkdirErrorFs struct{ afero.Fs }

func (f *w26MkdirErrorFs) MkdirAll(string, os.FileMode) error {
	return errors.New("mkdir blocked")
}

type w26RevertCalls struct {
	mu      sync.Mutex
	primary int
	cleanup int
	nfo     int
}

func (c *w26RevertCalls) revertPrimaryFile(context.Context, *models.BatchFileOperation) (*RevertFileResult, error) {
	c.mu.Lock()
	c.primary++
	c.mu.Unlock()
	return nil, nil
}

func (c *w26RevertCalls) cleanupGeneratedFiles(*models.BatchFileOperation, string) {
	c.mu.Lock()
	c.cleanup++
	c.mu.Unlock()
}

func (c *w26RevertCalls) cleanupEmptyDir(string, string) {}

func (c *w26RevertCalls) restoreNFO(context.Context, *models.BatchFileOperation, bool) (string, *RevertFileResult) {
	c.mu.Lock()
	c.nfo++
	c.mu.Unlock()
	return "", nil
}

func (c *w26RevertCalls) primaryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.primary
}

func (c *w26RevertCalls) cleanupGeneratedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cleanup
}

func (c *w26RevertCalls) restoreNFOCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nfo
}

func newW26UpdateOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, id uint) *models.BatchFileOperation {
	t.Helper()
	anchor := fmt.Sprintf("/src/W26-%d/W26-%d.mkv", id, id)
	require.NoError(t, fs.MkdirAll(filepath.Dir(anchor), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, anchor, []byte("video"), config.FilePerm))
	op := &models.BatchFileOperation{
		ID:            id,
		BatchJobID:    fmt.Sprintf("job-w26-%d", id),
		MovieID:       fmt.Sprintf("W26-%d", id),
		OriginalPath:  anchor,
		NewPath:       anchor,
		OperationType: models.OperationTypeUpdate,
		NFOSnapshot:   "<original/>",
		NFOPath:       fmt.Sprintf("/src/W26-%d/W26-%d.nfo", id, id),
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Delete: []string{fmt.Sprintf("/src/W26-%d/generated.jpg", id)},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

func newW26MoveOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, id uint) *models.BatchFileOperation {
	t.Helper()
	newPath := fmt.Sprintf("/dst/W26-%d/W26-%d.mkv", id, id)
	originalPath := fmt.Sprintf("/src/W26-%d.mkv", id)
	require.NoError(t, fs.MkdirAll(filepath.Dir(newPath), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
	op := &models.BatchFileOperation{
		ID:            id,
		BatchJobID:    fmt.Sprintf("job-w26-%d", id),
		MovieID:       fmt.Sprintf("W26-%d", id),
		OriginalPath:  originalPath,
		NewPath:       newPath,
		OperationType: models.OperationTypeMove,
		NFOSnapshot:   "<original/>",
		NFOPath:       fmt.Sprintf("/src/W26-%d.nfo", id),
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Delete: []string{fmt.Sprintf("/dst/W26-%d/generated.jpg", id)},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

func waitForW26(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
