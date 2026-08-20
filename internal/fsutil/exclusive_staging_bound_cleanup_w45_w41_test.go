package fsutil

// POSTER-WRITE-HARDENING wave-45 (codex P2, PR#215 finding F3) — the failed
// staging cleanup is bound to the opened inode: the restoreStagingMode
// failure leg used to close the handle and Remove the staged NAME, so a
// staged name renamed away and re-occupied by a substitute inside the
// close→remove window had the SUBSTITUTE unlinked. The OsFs leg now fstats
// the handle, Lstats the staged name (both with the handle still open), and
// unlinks only on os.SameFile; virtual filesystems keep the close+remove
// fallback (this file's leg); the !windows file pins the OsFs binding.

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w45ChmodFailFs fails only Chmod so the strict mode-restoration cleanup path
// in CreateExclusiveStagingFile is reachable on a virtual filesystem (the w7b
// double lives in a !windows-tagged file and cannot be reused here).
type w45ChmodFailFs struct {
	afero.Fs
}

func (f w45ChmodFailFs) Chmod(_ string, _ os.FileMode) error {
	return errors.New("injected w45 chmod failure")
}

// Virtual fallback leg: no kernel identity channel exists, so the failure leg
// keeps the plain close+remove against the stored spelling — the failed
// staging attempt must not leak the staged name.
func TestCreateExclusiveStagingFileW45_VirtualFsCleansUp(t *testing.T) {
	fs := w45ChmodFailFs{Fs: afero.NewMemMapFs()}
	require.NoError(t, fs.MkdirAll("/out", 0o755))

	staged, file, err := CreateExclusiveStagingFile(fs, "/out/poster.jpg", ".rstr", 1, 0o666)
	require.Error(t, err)
	require.ErrorContains(t, err, "apply exclusive staging mode")
	require.Empty(t, staged)
	require.Nil(t, file)

	exists, derr := afero.Exists(fs, "/out/poster.jpg.rstr.1")
	require.NoError(t, derr)
	require.False(t, exists, "the failed staging attempt removes its own staged name")
}
