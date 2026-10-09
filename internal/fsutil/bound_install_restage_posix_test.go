//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6qKyWN): when the OS publish restages the bytes
// into a fresh O_EXCL name (a plant consumed the staged name mid-publish),
// the returned identity's strong key must name the PUBLISHED object — pairing
// the initial staging handle's key with the new inode would make the bound
// observation reject our own install.
func TestCopyInstallRestageBindsPublishedInode(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, os.WriteFile(src, []byte("admitted"), 0o644))

	old := publishStagedBoundDestLstat
	t.Cleanup(func() { publishStagedBoundDestLstat = old })
	removed := false
	publishStagedBoundDestLstat = func(dest string) (os.FileInfo, error) {
		t.Helper()
		if !removed {
			removed = true
			require.NoError(t, os.Remove(dest), "the plant consumed the published name")
			return nil, os.ErrNotExist // looking gone at reverify → restage+republish on a fresh inode
		}
		return old(dest)
	}

	identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, admitAll)
	require.NoError(t, err)
	require.True(t, removed, "the restage leg ran")

	dstat, derr := os.Lstat(dst)
	require.NoError(t, derr)
	sys, ok := dstat.Sys().(*syscall.Stat_t)
	require.True(t, ok, "an OsFs stat carries the kernel pair")
	dev, ino, strongOK := identity.strongIdentity()
	require.True(t, strongOK, "the install identity carries a strong pair")
	assert.Equal(t, dev, uint64(sys.Dev), "the strong key names the published inode, not the abandoned initial staging one")
	assert.Equal(t, ino, sys.Ino)
}
