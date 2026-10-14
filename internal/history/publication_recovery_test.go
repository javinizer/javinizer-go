package history

import (
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverPublicationMoveBoundIdentity(t *testing.T) {
	for _, kind := range []string{"same", "foreign", "weak"} {
		t.Run(kind, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			source, target := filepath.Join(root, "published"), filepath.Join(root, "original")
			require.NoError(t, afero.WriteFile(fs, source, []byte("bytes"), 0600))
			info, err := fs.Stat(source)
			require.NoError(t, err)
			dev, ino, strong := fsutil.BoundObjectIdentity(fs, source, info)
			require.True(t, strong)
			proof := &models.DeleteEntry{Path: source, IdentityPinned: true, IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}
			if kind == "foreign" {
				require.NoError(t, fs.Rename(source, source+".saved"))
				require.NoError(t, afero.WriteFile(fs, source, []byte("bytes"), 0600))
				require.NoError(t, fs.Chtimes(source, info.ModTime(), info.ModTime()))
			}
			if kind == "weak" {
				proof.IdentityStrong = false
			}
			err = recoverPublicationMove(fs, models.FileMove{OriginalPath: target, NewPath: source, Proof: proof})
			if kind == "same" {
				require.NoError(t, err)
				require.FileExists(t, target)
			} else {
				require.Error(t, err)
				require.FileExists(t, source)
				_, err = fs.Stat(target)
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

type recoveryNoLinkFS struct{ afero.Fs }
type recoveryReadlinkErrorFS struct{ afero.Fs }

func (f recoveryReadlinkErrorFS) ReadlinkIfPossible(string) (string, error) {
	return "", os.ErrPermission
}

type recoveryLstatErrorFS struct{ afero.Fs }

func (f recoveryLstatErrorFS) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, true, os.ErrPermission
}

func TestPublicationRecoveryStatusProof(t *testing.T) {
	for _, kind := range []string{"legacy", "missing", "unproven", "unobservable", "same before", "same after", "foreign", "directory", "weak", "link", "link foreign", "link regular", "link unavailable", "link error"} {
		t.Run(kind, func(t *testing.T) {
			native := afero.NewOsFs()
			var fs afero.Fs = native
			path := filepath.Join(t.TempDir(), "output")
			require.NoError(t, afero.WriteFile(native, path, []byte("bytes"), 0600))
			info, err := native.Stat(path)
			require.NoError(t, err)
			dev, ino, strong := fsutil.BoundObjectIdentity(native, path, info)
			require.True(t, strong)
			proof := models.DeleteEntry{Path: path, RecoveryPublication: true, IdentityStrong: true, IdentityPinned: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}
			after := false
			wantErr := false
			switch kind {
			case "legacy":
				proof.RecoveryPublication = false
			case "missing":
				require.NoError(t, native.Remove(path))
			case "unproven":
				proof.RecoveryUnproven = true
				wantErr = true
			case "unobservable":
				fs = recoveryLstatErrorFS{native}
				wantErr = true
			case "same after":
				after = true
				wantErr = true
			case "foreign":
				proof.IdentityIno++
				after = true
			case "directory":
				require.NoError(t, native.Remove(path))
				require.NoError(t, native.Mkdir(path, 0700))
				after = true
			case "weak":
				proof.IdentityStrong = false
				wantErr = true
			case "link regular":
				proof.LinkTarget = "source"
				after = true
			case "link", "link foreign", "link unavailable", "link error":
				require.NoError(t, native.Remove(path))
				if err := os.Symlink("source", path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				proof.LinkTarget = "source"
				switch kind {
				case "link":
					after = true
					wantErr = true
				case "link foreign":
					proof.LinkTarget = "different"
					after = true
				case "link unavailable":
					fs = recoveryNoLinkFS{native}
					wantErr = true
				case "link error":
					fs = recoveryReadlinkErrorFS{native}
					wantErr = true
				}
			}
			ledger := models.GeneratedFilesJSON{PlannedDeletes: []models.DeleteEntry{proof}, MoveBack: []models.FileMove{{Proof: &proof}, {}}}
			err = checkPublicationRecovery(fs, ledger, after)
			if wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func (f recoveryNoLinkFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (f recoveryReadlinkErrorFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

type recoveryRemoveErrorFS struct{ afero.Fs }

func (f recoveryRemoveErrorFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (f recoveryRemoveErrorFS) Remove(string) error { return os.ErrPermission }

func TestPublicationRecoveryRetainedOutputStaysRetryable(t *testing.T) {
	for _, weak := range []bool{false, true} {
		t.Run(fmt.Sprint("weak=", weak), func(t *testing.T) {
			db, cleanup := setupTestDB(t)
			defer cleanup()
			repo := database.NewBatchFileOperationRepository(db)
			native := afero.NewOsFs()
			path := filepath.Join(t.TempDir(), "output")
			require.NoError(t, afero.WriteFile(native, path, []byte("owned"), 0600))
			info, err := native.Stat(path)
			require.NoError(t, err)
			dev, ino, strong := fsutil.BoundObjectIdentity(native, path, info)
			require.True(t, strong)
			proof := models.DeleteEntry{Path: path, RecoveryPublication: true, RecoveryUnproven: weak, IdentityStrong: true, IdentityPinned: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}
			ledger := models.GeneratedFilesJSON{PrimaryResident: true, PlannedDeletes: []models.DeleteEntry{proof}}
			op := &models.BatchFileOperation{BatchJobID: "recovery-status", MovieID: "ABC-123", OriginalPath: path + ".primary", OperationType: models.OperationTypeCopy, RevertStatus: models.RevertStatusFailed, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
			require.NoError(t, repo.Create(t.Context(), op))
			r := NewReverter(recoveryRemoveErrorFS{native}, repo)
			result, err := r.revertFile(t.Context(), op)
			require.NoError(t, err)
			require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
			require.FileExists(t, path)
			row, err := repo.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusFailed, row.RevertStatus)
			require.NoError(t, native.Remove(path))
			result, err = NewReverter(native, repo).revertFile(t.Context(), op)
			require.NoError(t, err)
			require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
		})
	}
}
