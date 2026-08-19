//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 findings
// L1/L2) — PublishStagedBoundInfo hands the post-publish-VERIFIED
// destination identity back to restore/rollback callers so they can
// revalidate the destination against exactly the object the publish landed
// BEFORE deleting their source backup or consuming the journal. These legs
// pin the POSIX legs: the reverify stat, the ENOSYS deferred-times relookup
// (the returned identity must carry the applied times), and the relookup
// failure degrading to no-identity (never a stale-mtime false refusal).

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The happy path: the returned FileInfo IS the published destination's own
// stat (SameFile-bound to the staged inode) with the fd-applied times.
func TestPublishStagedBoundInfoW31POSIX_ReturnsPublishedDestIdentity(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	when := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)

	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: when, Mtime: when,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	require.NotNil(t, info, "a proven publish reports the destination identity")

	current, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, os.SameFile(info, current), "the returned identity is the post-publish destination object itself")
	require.Equal(t, int64(len("genuine staged bytes")), info.Size())
	require.True(t, info.ModTime().Equal(current.ModTime()), "fd-applied times are part of the returned identity")
	require.False(t, info.IsDir())
}

// ENOSYS fd-times (solaris/aix posture): the deferred Chtimes lands on the
// published name, and the returned identity is the FRESH relookup so its
// mtime carries the applied times — a stale reverify stat would false-refuse
// every caller revalidation on those platforms.
func TestPublishStagedBoundInfoW31POSIX_ENOSYSDeferredTimesFreshIdentity(t *testing.T) {
	prevTimes := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevTimes })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	when := time.Date(2019, 7, 8, 9, 10, 11, 0, time.UTC)

	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: when, Mtime: when,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	require.NotNil(t, info)

	current, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, info.ModTime().Equal(current.ModTime()),
		"the returned identity carries the DEFERRED applied mtime, not the pre-Chtimes reverify snapshot")
	require.True(t, current.ModTime().Equal(when), "the deferred leg actually landed the requested times")
}

// The deferred-times relookup failing keeps the proven publish but reports
// NO identity (nil, nil) — the caller's documented residual posture instead
// of an identity with a stale mtime.
func TestPublishStagedBoundInfoW31POSIX_ENOSYSRelookFailureYieldsNoIdentity(t *testing.T) {
	prevTimes := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevTimes })

	sentinel := errors.New("w31 relookup wedged")
	prevLstat := publishStagedBoundDestLstat
	calls := 0
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return os.Lstat(name) // the post-publish reverify: must succeed
		}
		return nil, sentinel // the deferred-times relookup: wedged
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLstat })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: time.Now(), Mtime: time.Now(),
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err, "a wedged relookup never turns a proven publish into an error")
	require.Nil(t, info, "no provable identity — callers keep their documented residual posture")
	require.Equal(t, 2, calls)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got), "the publish itself landed")
}

// The classic wrapper stays the pure-error contract and discards the
// identity exactly like the pre-wave-31 shape.
func TestPublishStagedBoundInfoW31_ClassicWrapperDiscardsIdentity(t *testing.T) {
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
}
