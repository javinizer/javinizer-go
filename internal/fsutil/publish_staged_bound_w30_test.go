package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-30 (P1) — PublishStagedBound's
// host-agnostic legs: happy paths on the real OsFs (no attacker) and the
// virtual-filesystem passthrough (the pre-wave-30 CloseStaged + publish tail
// that wrapper/MemMap filesystems keep). Attack legs live in the !_windows
// companion; the windows-tagged leg compiles only on Windows hosts and is
// pinned by codecov.yml like the other platform files.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w30Stage writes genuine bytes into an exclusive staging file for dest and
// returns the open read/write handle. The ordinal starts deterministic so
// tests can glob leftovers.
func w30Stage(t *testing.T, fs afero.Fs, dest, suffix string, mode os.FileMode) (string, afero.File) {
	t.Helper()
	staged, fh, err := CreateExclusiveStagingFile(fs, dest, suffix, 3, mode)
	require.NoError(t, err)
	_, err = fh.Write([]byte("genuine staged bytes"))
	require.NoError(t, err)
	return staged, fh
}

func w30Ordinal(at uint64) func() uint64 {
	current := at
	return func() uint64 {
		current++
		return current
	}
}

// No-replace publish into an absent destination: verify+publish+reverify
// agree — the staged inode lands at dest, the staged name is gone.
func TestPublishStagedBoundW30_HappyPathNoReplace(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: false,
		Suffix:     ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
	_, lerr := os.Lstat(staged)
	require.ErrorIs(t, lerr, os.ErrNotExist, "the staged name was consumed by the publish")
}

// Replace semantics over an occupied destination: the pre-existing bytes are
// deliberately displaced; identity proven the same way.
func TestPublishStagedBoundW30_HappyPathReplaceOverOccupied(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(dest, []byte("current bytes"), 0o644))
	staged, fh := w30Stage(t, fs, dest, ".dlrstr", 0o600)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: ReplaceFile, NoReplace: false,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: false,
		Suffix:     ".dlrstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// With times requested, the published destination carries the exact atime /
// mtime the caller staged through the open handle (platforms without an
// fd-scoped primitive — the ENOSYS leg — SKIP the times instead: r12 pins
// that shape plus the completed classification in the posix tests).
func TestPublishStagedBoundW30_HappyPathTimesLand(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	at := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	mt := time.Date(2002, 3, 4, 5, 6, 7, 0, time.UTC)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Atime: at, Mtime: mt, ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	info, serr := os.Stat(dest)
	require.NoError(t, serr)
	require.WithinDuration(t, mt, info.ModTime(), 2*time.Second,
		"the staged mtime survives the bound publish")
}

// Virtual filesystems keep the pre-wave-30 tail (CloseStaged + publish): the
// mem handle re-stamps at close and the times land by name after it.
func TestPublishStagedBoundW30_VirtualFsPassthrough(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/W30", 0o755))
	dest := "/out/W30/poster.jpg"
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	at := time.Date(2003, 4, 5, 6, 7, 8, 0, time.UTC)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Atime: at, Mtime: at, ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
	info, serr := fs.Stat(dest)
	require.NoError(t, serr)
	require.WithinDuration(t, at, info.ModTime(), 2*time.Second,
		"the virtual leg still lands the staged times on the published name")
	_, lerr := fs.Stat(staged)
	require.ErrorIs(t, lerr, os.ErrNotExist)
}

// A publish error passes through VERBATIM so every caller classifier (the
// wave-15/17/20/29 sentinels behind PublishRefusal / PublishCompleted) keeps
// working through the callers' wraps.
func TestPublishStagedBoundW30_PublishErrorPassthrough(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/W30P", 0o755))
	dest := "/out/W30P/poster.jpg"
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	sentinel := os.ErrPermission
	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(afero.Fs, string, string) error { return sentinel },
		Staged:  staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
		ApplyTimes: false,
	})
	require.ErrorIs(t, err, os.ErrPermission)
	require.NotErrorIs(t, err, ErrPublishStagedIdentityBreak,
		"a publish error is never reclassified as an identity break")
	_, lerr := fs.Stat(staged)
	require.NoError(t, lerr, "nothing was consumed — the caller's cleanup still sees the staged name")
}
