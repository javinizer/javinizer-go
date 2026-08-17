package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementPendingCovW9_MarkerPersistFailureUndoesRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	baseRepo := newP3OpRepo()
	repo := &flakySweepRepo{p3OpRepo: baseRepo, fail: true}
	ctx := context.Background()
	dest := "/out/W9-MARKER/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := journalRow(t, baseRepo, "job-1", "W9-MARKER", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists, "a failed marker update must undo the restore")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker update must leave the row armed")

	fs.fail = false
	repo.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementPendingCovW9_ConcurrentSweepsSynchronizePendingState(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W9-RACE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := journalRow(t, repo, "job-1", "W9-RACE", dest, backup, 1, models.RevertStatusApplied)
	s := NewReplacementSweeper(fs, repo)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Sweep(ctx)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)))

	fs.fail = false
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementPendingCovW9_RearmPreservesModeAndMtime(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := &flakySweepRepo{p3OpRepo: newP3OpRepo(), fail: true}
	ctx := context.Background()
	dest := "/out/W9-PERM/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o600))
	originalMtime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, base.Chtimes(backup, originalMtime, originalMtime))
	originalInfo, err := base.Stat(backup)
	require.NoError(t, err)
	journalRow(t, repo.p3OpRepo, "job-1", "W9-PERM", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(base, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the injected consumption failure must re-arm the backup")
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	rearmedInfo, err := base.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), rearmedInfo.Mode().Perm())
	require.Equal(t, originalInfo.ModTime(), rearmedInfo.ModTime())

	repo.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementPendingCovW9_RearmChmodFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w9ChmodFailFs{Fs: base, failPath: "/out/W9-CHMOD/dest.jpg.dlbak." + p3HexA}
	dest := "/out/W9-CHMOD/dest.jpg"
	backup := fs.failPath
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	info, err := base.Stat(dest)
	require.NoError(t, err)
	require.Error(t, rearmReplacementBackup(fs, dest, backup, info))
}

type w9ChmodFailFs struct {
	afero.Fs
	failPath string
}

func (f *w9ChmodFailFs) Chmod(name string, mode os.FileMode) error {
	if filepath.Clean(name) == filepath.Clean(f.failPath) {
		return errors.New("chmod wedged")
	}
	return f.Fs.Chmod(name, mode)
}
