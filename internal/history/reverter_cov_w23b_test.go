package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRevertW23B_SerializesOperationAndRunsPrimaryOnce(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dests := newW23BMultiDestinationOp(t, base, repo)
	fs := &w23bRestoreGateFs{
		Fs:              base,
		firstBackupOpen: make(chan struct{}),
		releaseFirst:    make(chan struct{}),
		primarySource:   op.NewPath,
		primaryTarget:   op.OriginalPath,
	}
	first := *op
	second := *op
	r1 := NewReverter(fs, repo)
	r2 := NewReverter(fs, repo)

	var wg sync.WaitGroup
	results := make([]*RevertFileResult, 2)
	errs := make([]error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0], errs[0] = r1.revertFile(context.Background(), &first)
	}()

	select {
	case <-fs.firstBackupOpen:
	case <-time.After(time.Second):
		t.Fatal("first revert did not reach the restore gate")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		results[1], errs[1] = r2.revertFile(context.Background(), &second)
	}()
	waitForW23BRevertWaiter(t, op.ID)
	close(fs.releaseFirst)
	wg.Wait()

	require.NoError(t, errs[0])
	require.NotNil(t, results[0])
	require.Equal(t, models.RevertOutcomeReverted, results[0].Outcome)
	require.ErrorIs(t, errs[1], ErrBatchAlreadyReverted)
	require.Nil(t, results[1])
	require.Equal(t, 1, fs.primaryMoveCount())
	require.Equal(t, 4, fs.backupOpenCount(),
		"the winner opens each backup exactly twice: the no-follow restore read + the pre-unlink identity verify open (quarantine-reservation draws ride along and are excluded)")

	for _, dest := range dests {
		rowBytes, err := afero.ReadFile(base, dest)
		require.NoError(t, err)
		if strings.HasSuffix(dest, "poster.jpg") {
			require.Equal(t, "old-poster", string(rowBytes))
		} else {
			require.Equal(t, "old-fanart", string(rowBytes))
		}
	}
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestRevertW23B_LateFreshStatusSkipsPrimary(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dests := newW23BSingleDestinationOp(t, base, repo)
	dest := dests[0]
	backup := dest + ".dlbak.3333333333333333"
	stale := *op

	// The winner completed the destination restore and status transition before
	// this stale caller reached revertFile. Leave the stale journal in its input
	// snapshot but make the fresh row authoritative and backup-free.
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, base.Remove(backup))
	fresh, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(fresh.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements = nil
	fresh.GeneratedFiles = models.MarshalLedgerJSON(gf)
	fresh.RevertStatus = models.RevertStatusReverted
	require.NoError(t, repo.Update(context.Background(), fresh))

	fs := &w23bRestoreGateFs{
		Fs:              base,
		firstBackupOpen: make(chan struct{}),
		releaseFirst:    make(chan struct{}),
		primarySource:   stale.NewPath,
		primaryTarget:   stale.OriginalPath,
	}
	result, err := NewReverter(fs, repo).revertFile(context.Background(), &stale)
	require.ErrorIs(t, err, ErrBatchAlreadyReverted)
	require.Nil(t, result)
	require.Equal(t, 0, fs.primaryMoveCount())
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
}

func TestRestoreReplacementJournalW23B_SkipsConsumedSnapshotEntry(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dests := newW23BMultiDestinationOp(t, base, repo)
	firstDest := dests[0]
	firstBackup := firstDest + ".dlbak.1111111111111111"
	secondDest := dests[1]
	secondBackup := secondDest + ".dlbak.2222222222222222"

	// Model the other loop's completed restore: its destination already has
	// the old bytes, its backup is gone, and the fresh row no longer journals it.
	require.NoError(t, afero.WriteFile(base, firstDest, []byte("old-poster"), config.FilePerm))
	require.NoError(t, base.Remove(firstBackup))
	fresh, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(fresh.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements = gf.Replacements[1:]
	fresh.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), fresh))

	fs := &w23bBackupCountFs{Fs: base}
	stale := *op
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), &stale)
	require.NoError(t, err)
	require.False(t, restored[firstDest], "consumed-and-restored entry must not be restored twice")
	require.True(t, restored[secondDest])
	require.Equal(t, 2, fs.openCount(secondBackup),
		"restore read + the wave-25 pre-unlink identity verify open")
	require.Equal(t, 0, fs.openCount(firstBackup),
		"the consumed entry is never even verified for removal")
	require.Equal(t, "old-poster", string(mustRead2(t, base, firstDest)))
	require.Equal(t, "old-fanart", string(mustRead2(t, base, secondDest)))

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestRestoreReplacementJournalW23B_HoldsBusyAndJournalLocksThroughConsumption(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	op, dests := newW23BSingleDestinationOp(t, base, baseRepo)
	repo := &w23bBlockingUpdateRepo{
		p3OpRepo: baseRepo,
		fs:       base,
		dest:     dests[0],
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}

	done := make(chan error, 1)
	go func() {
		_, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
		done <- err
	}()

	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("restore did not reach journal persistence")
	}
	require.True(t, repo.busyHeld)
	acquired := make(chan struct{})
	go func() {
		release := fsutil.SharedJournalLocks().Acquire(fmt.Sprintf("%d", op.ID))
		close(acquired)
		release()
	}()
	select {
	case <-acquired:
		t.Fatal("journal lock was released before consumption update completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(repo.release)

	require.NoError(t, <-done)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("journal lock waiter did not proceed after consumption")
	}
	_, err := base.Stat(fsutil.ReplacementBusyPath(dests[0]))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRestoreReplacementJournalW23B_FreshRowFailures(t *testing.T) {
	t.Run("find error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		baseRepo := newP3OpRepo()
		op, _ := newW23BSingleDestinationOp(t, base, baseRepo)
		sentinel := errors.New("fresh row unavailable")
		repo := &w23bFindByIDErrorRepo{p3OpRepo: baseRepo, err: sentinel}
		_, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("nil row", func(t *testing.T) {
		base := afero.NewMemMapFs()
		baseRepo := newP3OpRepo()
		op, _ := newW23BSingleDestinationOp(t, base, baseRepo)
		repo := &w23bNilFindByIDRepo{p3OpRepo: baseRepo}
		_, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
		require.Error(t, err)
		require.Contains(t, err.Error(), "row not found")
	})

	t.Run("malformed row", func(t *testing.T) {
		base := afero.NewMemMapFs()
		baseRepo := newP3OpRepo()
		op, _ := newW23BSingleDestinationOp(t, base, baseRepo)
		repo := &w23bMalformedFindByIDRepo{p3OpRepo: baseRepo}
		_, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to re-parse journal")
	})
}

func TestConsumeReplacementEntryW23B_AlreadyConsumedIsNoop(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, _ := newW23BSingleDestinationOp(t, base, repo)
	fresh, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(fresh.GeneratedFiles)
	require.NoError(t, err)
	entry := gf.Replacements[0]
	gf.Replacements = nil
	fresh.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), fresh))

	stale := *op
	require.NoError(t, NewReverter(base, repo).consumeReplacementEntry(context.Background(), &stale, entry))
	require.Empty(t, mustJournal(&stale))
}

func TestRevertW23B_WaiterRefreshFailures(t *testing.T) {
	cases := []struct {
		name string
		repo database.BatchFileOperationRepositoryInterface
		want string
	}{
		{name: "find error", repo: &w23bFindByIDErrorRepo{p3OpRepo: newP3OpRepo(), err: errors.New("waiter row unavailable")}, want: "after acquiring revert lock"},
		{name: "nil row", repo: &w23bNilFindByIDRepo{p3OpRepo: newP3OpRepo()}, want: "row not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldLocks := revertOperationLocks
			revertOperationLocks = newOperationRevertLockRegistry()
			defer func() { revertOperationLocks = oldLocks }()
			op := &models.BatchFileOperation{ID: 23000 + uint(len(tc.name)), RevertStatus: models.RevertStatusApplied}
			key := fmt.Sprintf("rev-op:%d", op.ID)
			release, _ := revertOperationLocks.acquire(key)
			released := false
			defer func() {
				if !released {
					release()
				}
			}()
			done := make(chan error, 1)
			go func() {
				_, err := NewReverter(afero.NewMemMapFs(), tc.repo).revertFile(context.Background(), op)
				done <- err
			}()
			waitForW23BRevertWaiter(t, op.ID)
			release()
			released = true
			err := <-done
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

type w23bFindByIDErrorRepo struct {
	*p3OpRepo
	err error
}

func (r *w23bFindByIDErrorRepo) FindByID(context.Context, uint) (*models.BatchFileOperation, error) {
	return nil, r.err
}

type w23bNilFindByIDRepo struct{ *p3OpRepo }

func (r *w23bNilFindByIDRepo) FindByID(context.Context, uint) (*models.BatchFileOperation, error) {
	return nil, nil
}

type w23bMalformedFindByIDRepo struct{ *p3OpRepo }

func (r *w23bMalformedFindByIDRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	row, err := r.p3OpRepo.FindByID(ctx, id)
	if err != nil {
		return row, err
	}
	row.GeneratedFiles = `{"replacements":broken`
	return row, nil
}

func waitForW23BRevertWaiter(t *testing.T, id uint) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	key := fmt.Sprintf("rev-op:%d", id)
	for {
		revertOperationLocks.mu.Lock()
		active := revertOperationLocks.active[key]
		revertOperationLocks.mu.Unlock()
		if active >= 2 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("second revert did not wait on the operation lock")
		default:
			runtime.Gosched()
		}
	}
}

func newW23BMultiDestinationOp(t *testing.T, fs afero.Fs, repo *p3OpRepo) (*models.BatchFileOperation, []string) {
	t.Helper()
	newPath := "/dst/W23B/W23B.mkv"
	originalPath := "/src/W23B.mkv"
	dests := []string{"/dst/W23B/poster.jpg", "/dst/W23B/fanart.jpg"}
	backups := []string{dests[0] + ".dlbak.1111111111111111", dests[1] + ".dlbak.2222222222222222"}
	require.NoError(t, fs.MkdirAll(filepath.Dir(newPath), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dests[0], []byte("new-poster"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dests[1], []byte("new-fanart"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backups[0], []byte("old-poster"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backups[1], []byte("old-fanart"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w23b",
		MovieID:       "W23B",
		OriginalPath:  originalPath,
		NewPath:       newPath,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{
				{Destination: dests[0], Backup: backups[0], DestSeq: 1, Installed: true},
				{Destination: dests[1], Backup: backups[1], DestSeq: 1, Installed: true},
			},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, dests
}

func newW23BSingleDestinationOp(t *testing.T, fs afero.Fs, repo *p3OpRepo) (*models.BatchFileOperation, []string) {
	t.Helper()
	newPath := "/dst/W23B-SINGLE/W23B-SINGLE.mkv"
	originalPath := "/src/W23B-SINGLE.mkv"
	dest := "/dst/W23B-SINGLE/poster.jpg"
	backup := dest + ".dlbak.3333333333333333"
	require.NoError(t, fs.MkdirAll(filepath.Dir(newPath), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("new"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w23b-single",
		MovieID:       "W23B-SINGLE",
		OriginalPath:  originalPath,
		NewPath:       newPath,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, DestSeq: 1, Installed: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, []string{dest}
}

type w23bRestoreGateFs struct {
	afero.Fs
	firstBackupOpen chan struct{}
	releaseFirst    chan struct{}
	primarySource   string
	primaryTarget   string
	mu              sync.Mutex
	blocked         bool
	backupOpens     int
	primaryMoves    int
}

func (f *w23bRestoreGateFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	// Wave-26: backup=...dlbak.<hex>.dlq.<token> names are the removal gate's
	// quarantine RESERVATIONS, not backup reads — exclude them so the count
	// keeps pinning the winner's backup opens (restore read + identity
	// verify) and, through it, the loser's serialization.
	if strings.Contains(filepath.ToSlash(name), ".dlbak.") && !strings.Contains(filepath.ToSlash(name), backupQuarantineSuffix) {
		f.mu.Lock()
		f.backupOpens++
		block := !f.blocked
		if block {
			f.blocked = true
			close(f.firstBackupOpen)
		}
		f.mu.Unlock()
		if block {
			<-f.releaseFirst
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w23bRestoreGateFs) Rename(oldpath, newpath string) error {
	f.mu.Lock()
	if oldpath == f.primarySource && newpath == f.primaryTarget {
		f.primaryMoves++
	}
	f.mu.Unlock()
	return f.Fs.Rename(oldpath, newpath)
}

func (f *w23bRestoreGateFs) backupOpenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.backupOpens
}

func (f *w23bRestoreGateFs) primaryMoveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.primaryMoves
}

type w23bBackupCountFs struct {
	afero.Fs
	mu    sync.Mutex
	opens map[string]int
}

func (f *w23bBackupCountFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(filepath.ToSlash(name), ".dlbak.") {
		f.mu.Lock()
		if f.opens == nil {
			f.opens = make(map[string]int)
		}
		f.opens[name]++
		f.mu.Unlock()
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w23bBackupCountFs) openCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens[name]
}

// w23bBlockingUpdateRepo gates the journal consumption write; consumption
// moved from Update to UpdateJournalInTx (review 4960250562), so the gate
// follows — busy marker and journal lock must both be held across the
// transaction.
type w23bBlockingUpdateRepo struct {
	*p3OpRepo
	fs       afero.Fs
	dest     string
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	busyHeld bool
}

func (r *w23bBlockingUpdateRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.once.Do(func() {
		_, err := r.fs.Stat(fsutil.ReplacementBusyPath(r.dest))
		r.busyHeld = err == nil
		close(r.entered)
		<-r.release
	})
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}
