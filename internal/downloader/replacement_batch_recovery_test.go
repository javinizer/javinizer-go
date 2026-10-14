package downloader

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestReplacementBatchRecoveryOwnership(t *testing.T) {
	for _, kind := range []string{"owned delete", "owned move", "foreign same bytes", "uninstalled", "replaced", "absent", "weak virtual", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			path := filepath.Join(root, "output")
			require.NoError(t, afero.WriteFile(fs, path, []byte("bytes"), 0600))
			info, err := fs.Stat(path)
			require.NoError(t, err)
			leg := &replacementBatchLeg{destination: path, installed: true, installedID: fsutil.NewObservedBoundInstallIdentity(fs, path, info)}
			var base afero.Fs = fs
			switch kind {
			case "owned move":
				leg.rollbackOrigin = filepath.Join(root, "original")
			case "foreign same bytes":
				require.NoError(t, fs.Rename(path, path+".saved"))
				require.NoError(t, afero.WriteFile(fs, path, []byte("bytes"), 0600))
				require.NoError(t, fs.Chtimes(path, info.ModTime(), info.ModTime()))
			case "uninstalled":
				leg.installed = false
			case "replaced":
				leg.installed = false
				leg.replaced = true
			case "absent":
				require.NoError(t, fs.Remove(path))
			case "weak virtual":
				base = afero.NewMemMapFs()
				require.NoError(t, afero.WriteFile(base, path, []byte("bytes"), 0600))
				info, err = base.Stat(path)
				require.NoError(t, err)
				leg.installedID = fsutil.NewWeakBoundInstallIdentity(info)
			case "symlink":
				require.NoError(t, fs.Remove(path))
				if err := os.Symlink("source", path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				info, err = os.Lstat(path)
				require.NoError(t, err)
				leg.installedID = fsutil.NewBoundInstallLinkIdentity(info, "source")
			}
			batch := &ReplacementBatch{fs: base, legs: []*replacementBatchLeg{leg}, released: []string{filepath.Join(root, "released")}}
			recovery := batch.RecoverySnapshot()
			require.Len(t, recovery.Managed, 2)
			if kind == "foreign same bytes" || kind == "uninstalled" {
				require.False(t, recovery.Unresolved)
				require.Empty(t, recovery.Deletes)
				require.Empty(t, recovery.Moves)
				return
			}
			require.True(t, recovery.Unresolved)
			if kind == "owned move" {
				require.Len(t, recovery.Moves, 1)
				require.True(t, recovery.Moves[0].Proof.IdentityStrong)
			} else if kind == "replaced" {
				require.Empty(t, recovery.Deletes)
			} else {
				require.Len(t, recovery.Deletes, 1)
				if kind == "symlink" || kind == "weak virtual" {
					require.True(t, recovery.Deletes[0].RecoveryUnproven)
				}
			}
		})
	}
}
func TestReplacementBatchRecoveryReleasedUninstalled(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out", 0755))
	batch, err := NewReplacementBatch(fs, "", nil)
	require.NoError(t, err)
	_, err = batch.BeforePublish(context.Background(), "/out/pending", false)
	require.NoError(t, err)
	require.NoError(t, batch.ReleaseUninstalled("/out/pending"))
	snapshot := batch.RecoverySnapshot()
	require.Equal(t, []string{"/out/pending"}, snapshot.Managed)
	require.False(t, snapshot.Unresolved)
	require.Empty(t, snapshot.Deletes)
}

type recoveryArmDeniedFS struct{ afero.Fs }

func (f recoveryArmDeniedFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f recoveryArmDeniedFS) OpenFile(p string, flags int, mode os.FileMode) (afero.File, error) {
	if flags&os.O_CREATE != 0 {
		return nil, os.ErrPermission
	}
	return f.Fs.OpenFile(p, flags, mode)
}
func TestReplacementBatchRecoveryFailedArm(t *testing.T) {
	for _, kind := range []string{"move", "delete", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			native := afero.NewOsFs()
			path := filepath.Join(t.TempDir(), "output")
			require.NoError(t, afero.WriteFile(native, path, []byte("owned"), 0600))
			info, err := native.Stat(path)
			require.NoError(t, err)
			id := fsutil.NewObservedBoundInstallIdentity(native, path, info)
			var fs afero.Fs = recoveryArmDeniedFS{native}
			origin := path + ".original"
			if kind == "delete" {
				origin = ""
			}
			if kind == "foreign" {
				require.NoError(t, native.Rename(path, path+".saved"))
				require.NoError(t, afero.WriteFile(native, path, []byte("owned"), 0600))
				fs = native
			}
			batch, err := NewReplacementBatch(fs, "", nil)
			require.NoError(t, err)
			require.Error(t, batch.SetRollbackOriginBound(path, origin, id))
			snapshot := batch.RecoverySnapshot()
			require.Contains(t, snapshot.Managed, path)
			if kind == "foreign" {
				require.False(t, snapshot.Unresolved)
				require.Empty(t, snapshot.Moves)
				require.Empty(t, snapshot.Deletes)
			} else if kind == "move" {
				require.True(t, snapshot.Unresolved)
				require.Len(t, snapshot.Moves, 1)
				require.True(t, snapshot.Moves[0].Proof.IdentityStrong)
			} else {
				require.True(t, snapshot.Unresolved)
				require.Len(t, snapshot.Deletes, 1)
				require.True(t, snapshot.Deletes[0].IdentityStrong)
			}
		})
	}
}
