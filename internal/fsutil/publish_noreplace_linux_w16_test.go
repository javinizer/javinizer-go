//go:build linux

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (coverage) — the Linux
// renameat2 leg's degrade (ENOSYS / EINVAL / EOPNOTSUPP → hard-link publish)
// and default-error legs cannot be produced by the CI host's kernel on
// demand, so they dispatch through the renameNoReplaceKernel seam here with
// stubbed kernel responses. The degrade cases run the REAL hard-link
// fallback afterwards, so these tests also cover publishNoReplaceFallback on
// the coverage-uploading Linux job (Darwin runs it as the OsFs dispatch).

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func stubRenameNoReplaceKernelW16(t *testing.T, err error) {
	t.Helper()
	prev := renameNoReplaceKernel
	renameNoReplaceKernel = func(string, string) error { return err }
	t.Cleanup(func() { renameNoReplaceKernel = prev })
}

// Kernels/filesystems that cannot express RENAME_NOREPLACE degrade to the
// hard-link publish — which then runs FOR REAL and lands the publish.
func TestPublishNoReplaceLinuxW16_UnsupportedFlagDegradesToFallback(t *testing.T) {
	for _, errno := range []error{syscall.ENOSYS, syscall.EINVAL, syscall.EOPNOTSUPP} {
		t.Run(errno.Error(), func(t *testing.T) {
			stubRenameNoReplaceKernelW16(t, errno)
			dir := t.TempDir()
			src := filepath.Join(dir, "staged.tmp")
			dst := filepath.Join(dir, "poster.jpg")
			require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

			require.NoError(t, publishNoReplaceOSFS(src, dst), "degrade into the hard-link publish")
			got, err := os.ReadFile(dst)
			require.NoError(t, err)
			require.Equal(t, "staged", string(got))
			_, err = os.Stat(src)
			require.ErrorIs(t, err, os.ErrNotExist, "the fallback consumed the staged name")
		})
	}
}

// An occupied destination through the degrade leg still collides via the
// fallback's link(2) EEXIST — never a clobber.
func TestPublishNoReplaceLinuxW16_DegradedPublishStillRefusesOccupied(t *testing.T) {
	stubRenameNoReplaceKernelW16(t, syscall.ENOSYS)
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("racer"), 0o644))

	err := publishNoReplaceOSFS(src, dst)
	require.ErrorIs(t, err, ErrPublishCollision)
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "racer", string(got))
}

// Any other renameat2 failure surfaces wrapped, classified neither as a
// collision nor as a degrade signal.
func TestPublishNoReplaceLinuxW16_GenericKernelErrorSurfaces(t *testing.T) {
	stubRenameNoReplaceKernelW16(t, syscall.EPERM)
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	err := publishNoReplaceOSFS(src, dst)
	require.ErrorIs(t, err, syscall.EPERM)
	require.NotErrorIs(t, err, ErrPublishCollision)
	require.Contains(t, err.Error(), "no-replace renameat2")
	_, statErr := os.Stat(dst)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing was published")
}
