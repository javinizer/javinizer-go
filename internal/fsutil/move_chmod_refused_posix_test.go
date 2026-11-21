//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
)

// Issue #367: on ACL-managed datasets (ZFS aclmode=restricted, NFS squash)
// the staging file's mode differs from the request and chmod is refused.
// Moving media must still succeed with the filesystem-assigned permissions
// and leave no staging residue behind.
func TestMediaStaging_ChmodRefusedStillSucceeds(t *testing.T) {
	cases := map[string]func(src, dst string) error{
		"CopyFileFs":        func(s, d string) error { return CopyFileFs(afero.NewOsFs(), s, d) },
		"CopyFileNoReplace": func(s, d string) error { return CopyFileNoReplace(afero.NewOsFs(), s, d) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			called := false
			prev := stagedHandleChmod
			stagedHandleChmod = func(*os.File, os.FileMode) error {
				called = true
				return &os.PathError{Op: "chmod", Path: "staged", Err: syscall.EPERM}
			}
			t.Cleanup(func() { stagedHandleChmod = prev })

			// Force a mode mismatch independent of the host umask: the real
			// process umask is pinned to 0o077 (O_CREATE yields at most
			// 0o600) while the cached config umask of 0 makes staging
			// request 0o666, so a chmod is attempted (and refused).
			prevProc := syscall.Umask(0o077)
			t.Cleanup(func() { syscall.Umask(prevProc) })
			prevMask := config.UmaskValue()
			config.StoreUmask(0)
			t.Cleanup(func() { config.StoreUmask(prevMask) })

			dir := t.TempDir()
			src := filepath.Join(dir, "src.mp4")
			dst := filepath.Join(dir, "dst.mp4")
			require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))

			require.NoError(t, run(src, dst))
			require.True(t, called, "chmod must have been attempted, otherwise the test proves nothing")

			got, err := os.ReadFile(dst)
			require.NoError(t, err)
			require.Equal(t, "video", string(got))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			for _, e := range entries {
				require.Contains(t, []string{"src.mp4", "dst.mp4"}, e.Name(), "no staging residue expected")
			}
		})
	}
}
