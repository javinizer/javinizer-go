//go:build linux

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

// The Linux kernel leg of the link-object vacate: replayed kernel responses
// through the shared renameat2 seam pin the refusal classes (never the
// link(2) fallback — that primitive may follow a symlink source).
func TestVacateLinkObjectOsFsLinux_KernelResponseClasses(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "movie.mkv")
	terminal := filepath.Join(root, "terminal")
	target := filepath.Join(root, "real.mkv")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	require.NoError(t, os.Symlink(target, name))

	t.Run("eexist maps to the publish collision class", func(t *testing.T) {
		stubRenameNoReplaceKernelW16(t, syscall.EEXIST)
		err := vacateLinkObjectOsFs(name, terminal)
		require.ErrorIs(t, err, ErrPublishCollision)
	})
	t.Run("unsupported classes refuse typed, never the link(2) fallback", func(t *testing.T) {
		for _, errno := range []error{syscall.ENOSYS, syscall.EINVAL, syscall.EOPNOTSUPP} {
			stubRenameNoReplaceKernelW16(t, errno)
			err := vacateLinkObjectOsFs(name, terminal)
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported, "%v must refuse, not fall back to link(2)", errno)
		}
	})
	t.Run("enoent keeps the not-exist class for the caller's vanished mapping", func(t *testing.T) {
		stubRenameNoReplaceKernelW16(t, syscall.ENOENT)
		err := vacateLinkObjectOsFs(name, terminal)
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})
	t.Run("any other kernel failure wraps", func(t *testing.T) {
		stubRenameNoReplaceKernelW16(t, syscall.EIO)
		err := vacateLinkObjectOsFs(name, terminal)
		require.ErrorIs(t, err, syscall.EIO)
		require.NotErrorIs(t, err, ErrPublishNoReplaceUnsupported)
	})
	// The kernel seam restores per-subtest; the name is still intact.
	assert.Equal(t, target, func() string { got, _ := os.Readlink(name); return got }(), "no replay ever touched the name")
}
