package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReverterRearmMetaCovW17A_PreservesModeAndMtimeOnConsumptionFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &pathNormalizingChmodFs{Fs: base}
	repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo()}
	ctx := context.Background()

	fixture := &p3Fixture{fs: fs, repo: repo.p3OpRepo}
	op, dest := fixture.addAppliedOp(t, "job-w17a", "W17A-META", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"
	require.NoError(t, fs.Chmod(backup, 0o600))
	require.NoError(t, base.Chtimes(backup, time.Unix(946684800, 123456789), time.Unix(946684800, 123456789)))
	originalInfo, err := base.Stat(backup)
	require.NoError(t, err)

	consumeErr := errors.New("transient consumption outage")
	repo.updateErr = consumeErr
	r := NewReverter(fs, repo)
	restored, err := r.restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, base, dest))

	rearmedInfo, err := base.Stat(backup)
	require.NoError(t, err, "failed consumption must leave a retryable backup")
	require.Equal(t, os.FileMode(0o600), rearmedInfo.Mode().Perm())
	require.Equal(t, originalInfo.ModTime(), rearmedInfo.ModTime())
	require.Equal(t, "old", p3ReadFile(t, base, backup))
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "failed consumption keeps the journal armed")

	repo.updateErr = nil
	restored, err = r.restoreReplacementJournal(ctx, op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, base, dest))
	finalInfo, err := base.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), finalInfo.Mode().Perm(), "the retry must not widen restored permissions")
	require.Equal(t, originalInfo.ModTime(), finalInfo.ModTime(), "the retry must not refresh restored timestamps")
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist, "the healed retry consumes the re-armed backup")
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReverterRearmMetaCovW17A_RearmChmodFailureLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo()}
	fixture := &p3Fixture{fs: base, repo: repo.p3OpRepo}
	op, dest := fixture.addAppliedOp(t, "job-w17a", "W17A-CHMOD", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"
	require.NoError(t, base.Chmod(backup, 0o600))

	fs := &w17aChmodFailFs{Fs: base, failPath: backup}
	consumeErr := errors.New("transient consumption outage")
	repo.updateErr = consumeErr
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, base, dest))
	_, err = base.Stat(backup)
	require.NoError(t, err, "a metadata failure still leaves the copied backup present")
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
}

func TestReverterRearmMetaCovW17A_RearmChtimesFailureLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W17A-CTIMES/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o600))
	info, err := base.Stat(dest)
	require.NoError(t, err)

	fs := &w17aChtimesFailFs{Fs: base, failPath: backup}
	require.Error(t, rearmReplacementBackup(fs, dest, backup, info))
	_, err = base.Stat(backup)
	require.NoError(t, err, "the copy exists even when timestamp restoration fails")
}

type w17aChmodFailFs struct {
	afero.Fs
	failPath string
}

func (f *w17aChmodFailFs) Chmod(name string, mode os.FileMode) error {
	if filepath.Clean(filepath.FromSlash(name)) == filepath.Clean(filepath.FromSlash(f.failPath)) {
		return errors.New("re-arm chmod wedged")
	}
	return f.Fs.Chmod(filepath.FromSlash(name), mode)
}

type w17aChtimesFailFs struct {
	afero.Fs
	failPath string
}

func (f *w17aChtimesFailFs) Chtimes(name string, atime, mtime time.Time) error {
	if filepath.Clean(filepath.FromSlash(name)) == filepath.Clean(filepath.FromSlash(f.failPath)) {
		return errors.New("re-arm chtimes wedged")
	}
	return f.Fs.Chtimes(name, atime, mtime)
}
