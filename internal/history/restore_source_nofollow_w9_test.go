package history

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-9 (codex P2 follow-up): the restore source open moved behind the
// restoreOpenReplacementSource seam so the Windows build can install its
// reparse-point handle open at init (restore_source_nofollow_windows.go).
// These tests pin the seam contract on any host: copyRestoreBytes opens the
// backup through the installed opener and propagates its refusal unchanged.

func TestW9CopyRestoreBytesOpensThroughInstalledSeam(t *testing.T) {
	fs := afero.NewMemMapFs()
	backup := "/bk/poster.jpg.dlbak"
	dest := "/lib/poster.jpg"
	require.NoError(t, fs.MkdirAll("/bk", 0o755))
	require.NoError(t, fs.MkdirAll("/lib", 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("orig"), 0o600))

	var gotFs afero.Fs
	var gotBackup string
	prev := restoreOpenReplacementSource
	restoreOpenReplacementSource = func(fsys afero.Fs, path string) (afero.File, error) {
		gotFs, gotBackup = fsys, path
		return openRestoreSourceNoFollow(fsys, path)
	}
	t.Cleanup(func() { restoreOpenReplacementSource = prev })

	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	require.Equal(t, backup, gotBackup, "copyRestoreBytes opens through the installed seam")
	require.Same(t, fs, gotFs, "the caller's filesystem is threaded to the opener")

	data, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, "orig", string(data), "bytes land through the seam-opened handle")
}

func TestW9CopyRestoreBytesSeamRefusalPropagates(t *testing.T) {
	fs := afero.NewMemMapFs()
	backup := "/bk/poster.jpg.dlbak"
	require.NoError(t, fs.MkdirAll("/bk", 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("orig"), 0o600))

	prev := restoreOpenReplacementSource
	restoreOpenReplacementSource = func(afero.Fs, string) (afero.File, error) {
		return nil, refuseRestoreSource(backup, "backup became a symlink before open")
	}
	t.Cleanup(func() { restoreOpenReplacementSource = prev })

	err := copyRestoreBytes(fs, backup, "/lib/poster.jpg")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRestoreSourceRefused),
		"the opener's refusal keeps its classification through copyRestoreBytes, got %v", err)
}

// TestW9OpenRestoreSourceNoFollowDefault passes the platform's (inert on this
// host's MemMapFs) no-follow flag through afero — the seam's default value.
func TestW9OpenRestoreSourceNoFollowDefault(t *testing.T) {
	fs := afero.NewMemMapFs()
	backup := "/bk/poster.jpg.dlbak"
	require.NoError(t, fs.MkdirAll("/bk", 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("orig"), 0o600))

	f, err := openRestoreSourceNoFollow(fs, backup)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = openRestoreSourceNoFollow(fs, "/bk/absent.dlbak")
	require.True(t, errors.Is(err, os.ErrNotExist), "plain open errors surface unchanged, got %v", err)
}
