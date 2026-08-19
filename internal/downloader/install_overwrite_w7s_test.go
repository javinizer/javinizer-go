package downloader

// POSTER-WRITE-HARDENING codex PR#215 (P2): existence must be classified
// with Lstat semantics. os.Stat follows symlinks, so a DANGLING symlink at
// the destination reported ENOENT and took the create path — replacing the
// link object with no ledger entry and bypassing symlink refusal entirely.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestInstallOverwritingW7S_DanglingSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	dest := filepath.Join(root, "poster.jpg")
	staged := filepath.Join(root, "poster.tmp")
	danglingTarget := filepath.Join(root, "gone.bin")
	require.NoError(t, os.WriteFile(staged, []byte("new"), 0o644))
	if err := os.Symlink(danglingTarget, dest); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, afero.NewOsFs(), &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7s-dangling", recorder: recorder})
	require.NoError(t, err, "symlink refusal keeps the established skip+warn style")
	require.True(t, skipped, "a dangling symlink must refuse, not fall into the create path")
	require.True(t, replaced)

	info, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "destination must remain the symlink object")
	target, rerr := os.Readlink(dest)
	require.NoError(t, rerr)
	require.Equal(t, danglingTarget, target, "the link itself is untouched")
	stagedBytes, serr := os.ReadFile(staged)
	require.NoError(t, serr)
	require.Equal(t, "new", string(stagedBytes), "staged bytes were not swapped over the link")
	require.Empty(t, recorder.get(), "a refused install journals nothing")
	_, markerErr := os.Lstat(dest + fsutil.ReplacementBusySuffix)
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the held marker is released on refusal")
}

func TestInstallOverwritingW7S_LiveSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	realPath := filepath.Join(root, "real.jpg")
	dest := filepath.Join(root, "poster.jpg")
	staged := filepath.Join(root, "poster.tmp")
	require.NoError(t, os.WriteFile(realPath, []byte("real"), 0o644))
	require.NoError(t, os.WriteFile(staged, []byte("new"), 0o644))
	if err := os.Symlink(realPath, dest); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, afero.NewOsFs(), &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7s-live", recorder: recorder})
	require.NoError(t, err)
	require.True(t, skipped)
	require.True(t, replaced)

	info, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link object is preserved")
	body, rerr := os.ReadFile(realPath)
	require.NoError(t, rerr)
	require.Equal(t, "real", string(body), "target bytes are untouched")
	require.Empty(t, recorder.get())
	_, markerErr := os.Lstat(dest + fsutil.ReplacementBusySuffix)
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// Lstat ENOENT stays a create: the staged bytes install directly, no ledger
// arm is required, and the marker is released on the way out.
func TestInstallOverwritingW7S_AbsentDestCreates(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W7S-CREATE/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7s-create", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.False(t, replaced)
	require.Equal(t, "new", string(mustReadDownloaderW7(t, fs, dest)))
	require.Empty(t, recorder.get())
	_, markerErr := fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// A filesystem whose Lstat succeeds but yields no file information cannot be
// classified — fail closed with an error, never guess a create or a replace.
func TestInstallOverwritingW7S_NilLstatInfoFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W7S-NIL-LSTAT/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, &w25NilLstatFs{Fs: base}, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7s-nil-lstat", recorder: recorder})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no file information")
	require.False(t, skipped)
	require.False(t, replaced)
	require.Equal(t, "old", string(mustReadDownloaderW7(t, base, dest)), "destination bytes are intact")
	require.Empty(t, recorder.get())
	// Wave-38 (finding F4): on this pathological filesystem even the release
	// cannot PROVE which object a name addresses (every Lstat answers nil
	// info) — the take-aside release refuses closed and NEVER deletes by
	// pathname, so the marker survives for the stale rules to arbitrate.
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.NoError(t, markerErr, "release refuses on an unprovable filesystem rather than deleting by pathname")
}
