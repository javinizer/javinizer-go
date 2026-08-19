package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// Wave-21 (codex P1) re-pointed this leg: the re-arm's mode application is
// the create-time Chmod on the EXCLUSIVELY STAGED name inside
// fsutil.CreateExclusiveStagingFile — never a Chmod against the published
// backup path. A wedge there is therefore a PRE-publish failure: no backup
// materializes, the staged copy is cleaned up, and the journal entry stays
// journaled (the marker merge fails in this fixture too, so it stays armed).
func TestReverterRearmMetaCovW17A_RearmChmodFailureLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	normalizingFS := &pathNormalizingChmodFs{Fs: base}
	repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo()}
	fixture := &p3Fixture{fs: normalizingFS, repo: repo.p3OpRepo}
	op, dest := fixture.addAppliedOp(t, "job-w17a", "W17A-CHMOD", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"
	require.NoError(t, normalizingFS.Chmod(backup, 0o600))

	fs := &w17aChmodFailFs{Fs: normalizingFS}
	consumeErr := errors.New("transient consumption outage")
	repo.updateErr = consumeErr
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.True(t, fs.fired, "the pre-publish staging chmod wedge fired")
	require.Equal(t, "old", p3ReadFile(t, base, dest))
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist, "a pre-publish mode failure never materializes the backup")
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
}

// Wave-21 (codex P1) re-pointed this leg too: the re-arm's Chtimes runs on
// the EXCLUSIVELY STAGED name BEFORE the publish, so a wedge tears the
// staging down and the backup path stays absent — there is no such thing as
// a post-publish metadata failure on this flow anymore.
func TestReverterRearmMetaCovW17A_RearmChtimesFailureLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W17A-CTIMES/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o600))
	info, err := base.Stat(dest)
	require.NoError(t, err)

	fs := &w17aChtimesFailFs{Fs: base}
	require.Error(t, rearmReplacementBackup(fs, dest, backup, info))
	require.True(t, fs.fired, "the pre-publish staged-name Chtimes wedge fired")
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist, "a pre-publish Chtimes failure publishes nothing")
}

// w17aChmodFailFs wedges the re-arm's pre-publish metadata legs on the
// exclusively staged `<backup>.dlrarm.<hex>` name (wave-21) — the create-time
// mode assert inside fsutil.CreateExclusiveStagingFile. No Chmod ever
// targets the published backup path; the fallthrough keeps the wave-17a
// MemMapFs Windows-Chmod normalization.
type w17aChmodFailFs struct {
	afero.Fs
	fired bool
}

func (f *w17aChmodFailFs) Chmod(name string, mode os.FileMode) error {
	if strings.Contains(name, rearmStagingSuffix+".") {
		f.fired = true
		return errors.New("re-arm chmod wedged")
	}
	return f.Fs.Chmod(filepath.FromSlash(name), mode)
}

// w17aChtimesFailFs wedges the pre-publish Chtimes on the staged re-arm name.
type w17aChtimesFailFs struct {
	afero.Fs
	fired bool
}

func (f *w17aChtimesFailFs) Chtimes(name string, atime, mtime time.Time) error {
	if strings.Contains(name, rearmStagingSuffix+".") {
		f.fired = true
		return errors.New("re-arm chtimes wedged")
	}
	return f.Fs.Chtimes(name, atime, mtime)
}
