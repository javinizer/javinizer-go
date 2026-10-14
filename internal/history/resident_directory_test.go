package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type residentDirFaultFS struct {
	afero.Fs
	fault             string
	original, current string
	fired             bool
}

func (f *residentDirFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.fault == "lstat" {
		return nil, true, os.ErrPermission
	}
	info, nofollow, err := f.Fs.(afero.Lstater).LstatIfPossible(path)
	if f.fault == "following" {
		nofollow = false
	}
	return info, nofollow, err
}
func (f *residentDirFaultFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	if strings.Contains(filepath.Base(path), ".javinizer-directory-") {
		if f.fault == "open" {
			return nil, os.ErrPermission
		}
		file, err := f.Fs.OpenFile(path, flags, mode)
		if err != nil {
			return nil, err
		}
		return &residentDirFaultFile{File: file, fault: f.fault}, nil
	}
	return f.Fs.OpenFile(path, flags, mode)
}
func (f *residentDirFaultFS) Rename(source, target string) error {
	if f.fault == "take" && source == f.current {
		return os.ErrPermission
	}
	if f.fault == "publish" && target == f.original {
		return os.ErrPermission
	}
	if f.fault == "swap" && source == f.current && !f.fired {
		f.fired = true
		if err := f.Fs.Rename(source, source+".saved"); err != nil {
			return err
		}
		if err := f.Fs.Mkdir(source, 0755); err != nil {
			return err
		}
	}
	return f.Fs.Rename(source, target)
}

type residentDirFaultFile struct {
	afero.File
	fault string
}

func (f *residentDirFaultFile) Stat() (os.FileInfo, error) {
	if f.fault == "stat" {
		return nil, os.ErrPermission
	}
	return f.File.Stat()
}
func (f *residentDirFaultFile) Close() error {
	err := f.File.Close()
	if f.fault == "close" {
		return os.ErrPermission
	}
	return err
}

type residentDirFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
	fault string
}

func (r residentDirFaultRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.fault == "transaction" {
		return errors.New("injected transaction failure")
	}
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(op *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		if r.fault == "malformed" {
			op.GeneratedFiles = "{"
		}
		if r.fault == "missing" {
			op.GeneratedFiles = "{}"
		}
		return fn(op)
	})
}

func TestResidentDirectoryRecoveryFaults(t *testing.T) {
	for _, fault := range []string{"none", "transaction", "malformed", "missing", "open", "stat", "close", "take", "publish", "swap", "lstat", "following", "no-lstat", "missing-object", "occupied-original", "already-restored", "symlink"} {
		t.Run(fault, func(t *testing.T) {
			base := afero.NewOsFs()
			root := t.TempDir()
			old, current := filepath.Join(root, "old"), filepath.Join(root, "new")
			require.NoError(t, base.Mkdir(current, 0755))
			info, err := statResidentTestIdentity(t, base, current)
			require.NoError(t, err)
			dev, ino, strong := fsutil.BoundObjectIdentity(base, current, info)
			require.True(t, strong)
			move := &models.DirectoryMove{Original: old, Locations: []string{current}, Device: dev, Inode: ino}
			repo := newP3OpRepo()
			op := &models.BatchFileOperation{OriginalPath: filepath.Join(old, "video.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: move})}
			require.NoError(t, repo.Create(t.Context(), op))
			wrapped := &residentDirFaultFS{Fs: base, fault: fault, original: old, current: current}
			var fs afero.Fs = wrapped
			switch fault {
			case "no-lstat":
				fs = struct{ afero.Fs }{base}
			case "missing-object":
				require.NoError(t, base.Remove(current))
			case "occupied-original":
				require.NoError(t, base.Mkdir(old, 0755))
			case "already-restored":
				require.NoError(t, base.Rename(current, old))
			case "symlink":
				require.NoError(t, base.Rename(current, current+".saved"))
				require.NoError(t, os.Symlink(current+".saved", current))
			}
			r := NewReverter(fs, residentDirFaultRepo{repo, fault})
			err = r.restoreResidentDirectory(t.Context(), op, move)
			if fault == "none" || fault == "already-restored" {
				require.NoError(t, err)
				after, e := statResidentTestIdentity(t, base, old)
				require.NoError(t, e)
				require.True(t, sameResidentTestIdentity(info, after))
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestResidentDirectoryRemapAndGuard(t *testing.T) {
	move := &models.DirectoryMove{Original: "/old", Locations: []string{"/new", "/scratch"}}
	require.Empty(t, remapResidentDirectoryPath(move, ""))
	require.Equal(t, filepath.FromSlash("/old/subtitle"), remapResidentDirectoryPath(move, "/new/subtitle"))
	require.Equal(t, "/outside/file", remapResidentDirectoryPath(move, "/outside/file"))
	require.False(t, directoryIdentityMatches(afero.NewOsFs(), "", nil, move))
	repo := newP3OpRepo()
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{DirectoryMove: move})}
	require.NoError(t, repo.Create(t.Context(), op))
	result, err := NewReverter(afero.NewOsFs(), repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, models.RevertReasonUnexpectedPathState, result.Reason)
}

func TestResidentDirectoryGeneratedCleanup(t *testing.T) {
	fs := afero.NewMemMapFs()
	old, current := "/old", "/new"
	require.NoError(t, fs.Mkdir(old, 0755))
	require.NoError(t, afero.WriteFile(fs, "/old/delete.nfo", []byte("generated"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/old/pinned.jpg", []byte("keep unproven"), 0644))
	move := &models.DirectoryMove{Original: old, Locations: []string{current}}
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: move, Delete: []string{"/new/delete.nfo"}, PlannedDeletes: []models.DeleteEntry{{Path: "/new/pinned.jpg"}}})}
	cleanupGeneratedFilesFS(fs, op, "/")
	_, err := fs.Stat("/old/delete.nfo")
	require.True(t, os.IsNotExist(err))
	body, err := afero.ReadFile(fs, "/old/pinned.jpg")
	require.NoError(t, err)
	require.Equal(t, "keep unproven", string(body))
}
