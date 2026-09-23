package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type statusFailureRepo struct {
	*p3OpRepo
	err error
}

func (r *statusFailureRepo) UpdateRevertStatus(context.Context, uint, models.RevertStatusEnum) error {
	return r.err
}

type installedMatchFaultFS struct {
	afero.Fs
	open func(string) (afero.File, error)
}

func (f *installedMatchFaultFS) Open(name string) (afero.File, error) {
	if f.open != nil {
		return f.open(name)
	}
	return f.Fs.Open(name)
}

type readFailureFile struct {
	afero.File
	readErr, closeErr error
}

func (f *readFailureFile) Read([]byte) (int, error) { return 0, f.readErr }
func (f *readFailureFile) Close() error {
	_ = f.File.Close()
	return f.closeErr
}

type readlinkFailureFS struct {
	afero.Fs
	err error
}

func (f *readlinkFailureFS) ReadlinkIfPossible(string) (string, error) { return "", f.err }
func (f *readlinkFailureFS) SymlinkIfPossible(oldname, newname string) error {
	if linker, ok := f.Fs.(afero.Linker); ok {
		return linker.SymlinkIfPossible(oldname, newname)
	}
	return afero.ErrNoSymlink
}

func primaryReplacementOperation(t *testing.T, fs afero.Fs, repo *p3OpRepo, withInstalled bool, withBackup bool) *models.BatchFileOperation {
	t.Helper()
	newPath, original, backup := "/dst/movie.mkv", "/src/movie.mkv", "/dst/movie.mkv.dlbak.primary"
	require.NoError(t, fs.MkdirAll("/dst", config.DirPerm))
	require.NoError(t, fs.MkdirAll("/src", config.DirPerm))
	if withInstalled {
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("installed"), config.FilePerm))
	}
	if withBackup {
		require.NoError(t, afero.WriteFile(fs, backup, []byte("prior"), config.FilePerm))
	}
	gf := models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: newPath, Backup: backup, DestSeq: 1}}}
	op := &models.BatchFileOperation{BatchJobID: "primary-replacement", MovieID: "MOVIE", OriginalPath: original, NewPath: newPath, OperationType: models.OperationTypeMove, GeneratedFiles: models.MarshalLedgerJSON(gf), RevertStatus: models.RevertStatusApplied}
	require.NoError(t, repo.Create(t.Context(), op))
	return op
}

func TestMissingInstalledPrimaryRestoresPriorDestinationWithoutInventingSource(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op := primaryReplacementOperation(t, fs, repo, false, true)
	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	require.Contains(t, result.Error, "source could not be reconstructed")
	require.Equal(t, "prior", p3ReadFile(t, fs, op.NewPath))
	_, err = fs.Stat(op.OriginalPath)
	require.True(t, os.IsNotExist(err))
	stored, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, stored.RevertStatus)
}

func TestMissingInstalledPrimaryKeepsJournalWhenRestoreFails(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op := primaryReplacementOperation(t, fs, repo, false, false)
	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	stored, findErr := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.RevertStatusApplied, stored.RevertStatus)
}

func TestMissingInstalledPrimarySurfacesStatusPersistenceFailure(t *testing.T) {
	fs, baseRepo := afero.NewMemMapFs(), newP3OpRepo()
	op := primaryReplacementOperation(t, fs, baseRepo, false, true)
	repo := &statusFailureRepo{p3OpRepo: baseRepo, err: errors.New("status unavailable")}
	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.Nil(t, result)
	require.ErrorContains(t, err, "status unavailable")
	require.Equal(t, "prior", p3ReadFile(t, fs, op.NewPath))
}

func TestMoveBackFailureToRestoreReplacementIsRetryable(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op := primaryReplacementOperation(t, fs, repo, true, false)
	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Equal(t, "installed", p3ReadFile(t, fs, op.OriginalPath))
}

func TestReplacementJournalMalformedDataDoesNotClaimPrimary(t *testing.T) {
	op := &models.BatchFileOperation{GeneratedFiles: "{"}
	require.False(t, replacementJournalContainsDestination(op, "/dest"))
}

func TestInstalledDestinationVerificationPropagatesReadFailures(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/dest", []byte("installed"), 0o644))
	info, err := base.Stat("/dest")
	require.NoError(t, err)
	entry := models.ReplacementEntry{InstalledSize: info.Size(), InstalledSHA256: "unused"}

	t.Run("open", func(t *testing.T) {
		sentinel := errors.New("open failed")
		fs := &installedMatchFaultFS{Fs: base, open: func(string) (afero.File, error) { return nil, sentinel }}
		_, err := installedDestinationMatches(fs, "/dest", info, entry)
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("read", func(t *testing.T) {
		sentinel := errors.New("read failed")
		fs := &installedMatchFaultFS{Fs: base, open: func(name string) (afero.File, error) {
			f, openErr := base.Open(name)
			return &readFailureFile{File: f, readErr: sentinel}, openErr
		}}
		_, err := installedDestinationMatches(fs, "/dest", info, entry)
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("close", func(t *testing.T) {
		sentinel := errors.New("close failed")
		fs := &installedMatchFaultFS{Fs: base, open: func(name string) (afero.File, error) {
			f, openErr := base.Open(name)
			return &readFailureFile{File: f, readErr: io.EOF, closeErr: sentinel}, openErr
		}}
		_, err := installedDestinationMatches(fs, "/dest", info, entry)
		require.ErrorIs(t, err, sentinel)
	})
}

func TestInstalledSymlinkVerificationRequiresReadableTarget(t *testing.T) {
	dir := t.TempDir()
	target, link := dir+"/target", dir+"/link"
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o644))
	require.NoError(t, os.Symlink(target, link))
	base := afero.NewOsFs()
	info, err := os.Lstat(link)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(target))
	entry := models.ReplacementEntry{InstalledSize: int64(len(target)), InstalledSHA256: hex.EncodeToString(sum[:])}

	_, err = installedDestinationMatches(&installedMatchFaultFS{Fs: base}, link, info, entry)
	require.ErrorContains(t, err, "cannot read symlink")
	sentinel := errors.New("readlink failed")
	_, err = installedDestinationMatches(&readlinkFailureFS{Fs: base, err: sentinel}, link, info, entry)
	require.ErrorIs(t, err, sentinel)
}

func TestRevertPreservesJournalWhenInstalledDestinationCannotBeVerified(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "verify-fault", "VERIFY", false, "installed", p3Replacement{seq: 1, backupBytes: "prior"})
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte("installed"))
	gf.Replacements[0].Installed = true
	gf.Replacements[0].InstalledSize = int64(len("installed"))
	gf.Replacements[0].InstalledSHA256 = hex.EncodeToString(sum[:])
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, fixture.repo.Update(t.Context(), op))
	sentinel := errors.New("verification read failed")
	fs := &installedMatchFaultFS{Fs: fixture.fs, open: func(name string) (afero.File, error) {
		if name == dest {
			return nil, sentinel
		}
		return fixture.fs.Open(name)
	}}
	result, err := NewReverter(fs, fixture.repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Contains(t, result.Error, sentinel.Error())
	require.Equal(t, "installed", p3ReadFile(t, fixture.fs, dest))
}
