//go:build darwin

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubRenameExclLinkKernel(t *testing.T, err error) {
	t.Helper()
	prev := renameExclLinkKernel
	renameExclLinkKernel = func(string, string) error { return err }
	t.Cleanup(func() { renameExclLinkKernel = prev })
}

// The Darwin kernel leg of the link-object vacate: replayed renameatx_np
// responses pin the refusal classes (never a link(2) degrade — that
// primitive may follow a symlink source).
func TestVacateLinkObjectOsFsDarwin_KernelResponseClasses(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "movie.mkv")
	terminal := filepath.Join(root, "terminal")
	target := filepath.Join(root, "real.mkv")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	require.NoError(t, os.Symlink(target, name))

	t.Run("eexist maps to the publish collision class", func(t *testing.T) {
		stubRenameExclLinkKernel(t, syscall.EEXIST)
		err := vacateLinkObjectOsFs(name, terminal)
		require.ErrorIs(t, err, ErrPublishCollision)
	})
	t.Run("unsupported classes refuse typed, never a follow-capable degrade", func(t *testing.T) {
		for _, errno := range []error{syscall.ENOSYS, syscall.ENOTSUP, syscall.EOPNOTSUPP} {
			stubRenameExclLinkKernel(t, errno)
			err := vacateLinkObjectOsFs(name, terminal)
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported, "%v must refuse, not degrade", errno)
		}
	})
	t.Run("enoent keeps the not-exist class for the caller's vanished mapping", func(t *testing.T) {
		stubRenameExclLinkKernel(t, syscall.ENOENT)
		err := vacateLinkObjectOsFs(name, terminal)
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})
	t.Run("any other kernel failure wraps", func(t *testing.T) {
		stubRenameExclLinkKernel(t, syscall.EIO)
		err := vacateLinkObjectOsFs(name, terminal)
		require.ErrorIs(t, err, syscall.EIO)
		require.NotErrorIs(t, err, ErrPublishNoReplaceUnsupported)
	})
	got, readErr := os.Readlink(name)
	require.NoError(t, readErr)
	assert.Equal(t, target, got, "no replay ever touched the name")
}
