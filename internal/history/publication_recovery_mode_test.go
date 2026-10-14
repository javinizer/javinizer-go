package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type recoveryModeEXDEVFS struct {
	afero.Fs
	target  string
	crossed int
}

func (f *recoveryModeEXDEVFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f *recoveryModeEXDEVFS) Rename(a, b string) error {
	if b == f.target && strings.Contains(a, ".vac.") {
		f.crossed++
		return syscall.EXDEV
	}
	return f.Fs.Rename(a, b)
}

func TestPublicationRecoveryMoveModeAfterReopen(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0444, 0751} {
		for _, scenario := range []string{"stored", "missing", "invalid"} {
			t.Run(fmt.Sprintf("%04o/%s", mode, scenario), func(t *testing.T) {
				db, cleanup := setupTestDB(t)
				defer cleanup()
				var databases []struct{ Name, File string }
				require.NoError(t, db.Raw("PRAGMA database_list").Scan(&databases).Error)
				dsn := ""
				for _, row := range databases {
					if row.Name == "main" {
						dsn = row.File
					}
				}
				require.NotEmpty(t, dsn)
				native := afero.NewOsFs()
				root := t.TempDir()
				published, original := filepath.Join(root, "published.srt"), filepath.Join(root, "incoming.srt")
				require.NoError(t, afero.WriteFile(native, published, []byte("private subtitle"), 0600))
				require.NoError(t, native.Chmod(published, mode))
				t.Cleanup(func() { _ = native.Chmod(published, 0600); _ = native.Chmod(original, 0600) })
				info, err := native.Stat(published)
				require.NoError(t, err)
				dev, ino, strong := fsutil.BoundObjectIdentity(native, published, info)
				require.True(t, strong)
				proof := models.DeleteEntry{Path: published, RecoveryPublication: true, IdentityStrong: true, IdentityPinned: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}
				ledger := models.GeneratedFilesJSON{PrimaryResident: true, MoveBack: []models.FileMove{{OriginalPath: original, NewPath: published, Proof: &proof}}}
				// Inject the persisted wire field so this regression also builds against the
				// pre-fix model, which silently discarded the installed permission mode.
				raw := models.MarshalLedgerJSON(ledger)
				if scenario != "missing" {
					savedMode := uint32(info.Mode().Perm())
					if scenario == "invalid" {
						savedMode = 01000
					}
					raw = strings.Replace(raw, `"identity_strong":true`, fmt.Sprintf(`"identity_strong":true,"identity_mode":%d`, savedMode), 1)
				}
				repo := database.NewBatchFileOperationRepository(db)
				op := &models.BatchFileOperation{BatchJobID: "recovery-mode", MovieID: "ABC-123", OriginalPath: filepath.Join(root, "resident.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusFailed, GeneratedFiles: raw}
				require.NoError(t, repo.Create(t.Context(), op))
				require.NoError(t, db.Close())
				fresh, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, err)
				defer fresh.Close()
				reopened := database.NewBatchFileOperationRepository(fresh)
				fs := &recoveryModeEXDEVFS{Fs: native, target: original}
				result, err := NewReverter(fs, reopened).RevertBatch(t.Context(), "recovery-mode")
				require.NoError(t, err)
				if scenario != "stored" {
					require.Zero(t, result.Succeeded)
					require.Equal(t, 1, result.Failed)
					require.FileExists(t, published)
					_, statErr := native.Stat(original)
					require.True(t, os.IsNotExist(statErr))
					saved, findErr := reopened.FindByID(t.Context(), op.ID)
					require.NoError(t, findErr)
					require.Equal(t, models.RevertStatusFailed, saved.RevertStatus)
					return
				}
				require.Equal(t, 1, result.Succeeded)
				require.Zero(t, result.Failed)
				require.Positive(t, fs.crossed)
				restored, err := native.Stat(original)
				require.NoError(t, err)
				require.Equal(t, info.Mode().Perm(), restored.Mode().Perm(), "preserve actual supported native permission bits across restart and EXDEV")
				_, err = native.Stat(published)
				require.True(t, os.IsNotExist(err))
			})
		}
	}
}

type recoveryModeReadFS struct{ *recoveryModeEXDEVFS }
type recoveryModeReadFile struct {
	afero.File
	fired bool
}

func (f *recoveryModeReadFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	if n > 0 && !f.fired {
		f.fired = true
		if modeErr := os.Chmod(f.Name(), 0444); modeErr != nil {
			return n, modeErr
		}
	}
	return n, err
}
func (f *recoveryModeReadFS) Open(path string) (afero.File, error) {
	file, err := f.Fs.Open(path)
	if err == nil && strings.Contains(path, ".vac.") {
		return &recoveryModeReadFile{File: file}, nil
	}
	return file, err
}

func TestPublicationRecoveryRejectsMidstreamModeDrift(t *testing.T) {
	native := afero.NewOsFs()
	root := t.TempDir()
	source, target := filepath.Join(root, "published"), filepath.Join(root, "original")
	require.NoError(t, afero.WriteFile(native, source, []byte("private subtitle"), 0600))
	require.NoError(t, native.Chmod(source, 0644))
	t.Cleanup(func() { _ = native.Chmod(source, 0600); _ = native.Chmod(target, 0600) })
	info, err := native.Stat(source)
	require.NoError(t, err)
	dev, ino, strong := fsutil.BoundObjectIdentity(native, source, info)
	require.True(t, strong)
	mode := uint32(info.Mode().Perm())
	proof := &models.DeleteEntry{IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix(), IdentityMode: &mode}
	fs := &recoveryModeReadFS{&recoveryModeEXDEVFS{Fs: native, target: target}}
	err = recoverPublicationMove(fs, models.FileMove{NewPath: source, OriginalPath: target, Proof: proof})
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign)
	require.FileExists(t, source)
	_, err = native.Stat(target)
	require.True(t, os.IsNotExist(err), "refuse before publishing stale permissions")
	body, err := afero.ReadFile(native, source)
	require.NoError(t, err)
	require.Equal(t, "private subtitle", string(body))
	changed, err := native.Stat(source)
	require.NoError(t, err)
	require.NotEqual(t, info.Mode().Perm(), changed.Mode().Perm())
}
