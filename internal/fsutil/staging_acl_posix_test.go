//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func denyStagingChmod(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := stagedHandleChmod
	stagedHandleChmod = func(f *os.File, _ os.FileMode) error {
		*calls++
		return &os.PathError{Op: "chmod", Path: f.Name(), Err: syscall.EPERM}
	}
	t.Cleanup(func() { stagedHandleChmod = old })
	return calls
}

func TestExclusiveStagingACLRejectsOnlyRequiredModeChanges(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	calls := denyStagingChmod(t)
	fs := afero.NewOsFs()
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		t.Run(mode.String(), func(t *testing.T) {
			path, file, err := CreateExclusiveStagingFile(fs, filepath.Join(t.TempDir(), "marker"), ".tmp", 0, mode)
			require.NoError(t, err)
			defer file.Close()
			info, err := file.Stat()
			require.NoError(t, err)
			require.Equal(t, mode, info.Mode().Perm())
			require.NoError(t, VerifyStagedIdentity(fs, path, file))
		})
	}
	require.Zero(t, *calls, "already-correct permissions need no forbidden ACL mutation")

	parent := t.TempDir()
	path, file, err := CreateExclusiveStagingFile(fs, filepath.Join(parent, "media"), ".tmp", 0, 0o666)
	require.ErrorIs(t, err, syscall.EPERM, "umask narrowed permissions must not be silently accepted")
	require.Empty(t, path)
	require.Nil(t, file)
	require.Equal(t, 1, *calls)
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Empty(t, entries, "the failed exclusive staging inode is removed")
}

func TestACLModeProofUsesHandleNotSubstitutedName(t *testing.T) {
	calls := denyStagingChmod(t)
	fs := afero.NewOsFs()
	root := t.TempDir()
	path := filepath.Join(root, "stage")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	require.NoError(t, os.Rename(path, path+".owned"))
	victim := filepath.Join(root, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("foreign bytes"), 0o600))
	require.NoError(t, os.Chmod(victim, 0o644))
	require.NoError(t, os.Symlink(victim, path))

	require.NoError(t, restoreStagingMode(fs, path, file, 0o600))
	require.Zero(t, *calls)
	require.Error(t, VerifyStagedIdentity(fs, path, file), "mode proof never authorizes publishing a substituted name")
	require.ErrorIs(t, restoreStagingMode(fs, path, file, 0o644), syscall.EPERM,
		"the symlink target's matching mode cannot stand in for the handle's mode")
	require.Equal(t, 1, *calls)
	DiscardFailedExclusiveStaging(fs, path, file)
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	body, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "foreign bytes", string(body))
	info, err = os.Stat(victim)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestNativeStagingModeProofRefusesUnknownAndSpecialBits(t *testing.T) {
	fs := afero.NewMemMapFs()
	virtual, err := fs.Create("virtual")
	require.NoError(t, err)
	defer virtual.Close()
	require.False(t, nativeStagingModeMatches(virtual, 0o666), "virtual chmod fault contracts remain unchanged")

	file, err := os.Create(filepath.Join(t.TempDir(), "stage"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	for _, special := range []os.FileMode{os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
		t.Run(special.String(), func(t *testing.T) {
			require.NoError(t, file.Chmod(0o600|special))
			info, err := file.Stat()
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			t.Logf("requested special bit %v; handle mode %v", special, info.Mode())
			if info.Mode()&special == 0 {
				t.Skipf("environment did not retain requested special bit %v", special)
			}
			require.False(t, nativeStagingModeMatches(file, 0o600), "chmod must still clear special bits present on the handle")
		})
	}
	require.NoError(t, file.Chmod(0o600))
	require.True(t, nativeStagingModeMatches(file, 0o600))
	require.NoError(t, file.Close())
	require.False(t, nativeStagingModeMatches(file, 0o600), "an unavailable fstat must not bypass chmod")
}

func TestDurableStagingACLModeProofAfterOwnership(t *testing.T) {
	calls := denyStagingChmod(t)
	fs := afero.NewOsFs()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	require.NoError(t, os.WriteFile(source, []byte("video"), 0o600))
	require.NoError(t, os.Chmod(source, 0o640))
	stamp := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(source, stamp, stamp))
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)
	file, err := os.OpenFile(filepath.Join(root, "stage"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer file.Close()
	require.NoError(t, file.Chmod(0o640))
	require.NoError(t, SyncStagedMetadata(fs, file, sourceInfo))
	require.Zero(t, *calls)
	info, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, sourceInfo.ModTime(), info.ModTime())

	old := restoreFchown
	restoreFchown = func(_ int, _, _ int) error { return file.Chmod(0o600) }
	t.Cleanup(func() { restoreFchown = old })
	require.ErrorIs(t, SyncStagedMetadata(fs, file, sourceInfo), syscall.EPERM,
		"ownership changing permissions still requires successful mode restoration")
	require.Equal(t, 1, *calls)
}
