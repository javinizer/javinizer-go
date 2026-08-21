//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 findings
// L1/L2) — PublishStagedBoundInfo hands the post-publish-VERIFIED
// destination identity back to restore/rollback callers so they can
// revalidate the destination against exactly the object the publish landed
// BEFORE deleting their source backup or consuming the journal. These legs
// pin the POSIX legs: the reverify stat, and (r12) the ENOSYS leg
// completing the verified publish with the times SKIPPED — no deferred
// landing, no post-reverify relookup, no handed-back identity.

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

// ENOSYS fd-times (solaris/aix posture) — r12: the pre-r12 deferred
// Chtimes onto the published name is REFUSED entirely, so there is no
// fresh relookup to hand back at all: the verified publish surfaces the
// completed classification with the times skipped (nil identity — the
// caller's wave-31 revalidation rides the completed-publish discipline),
// and dest keeps the staged inode's own times.
func TestPublishStagedBoundInfoW31POSIX_ENOSYSLegSkipsTimesNoIdentity(t *testing.T) {
	prevTimes := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevTimes })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	preInfo, serr := fh.Stat()
	require.NoError(t, serr)
	when := time.Date(2019, 7, 8, 9, 10, 11, 0, time.UTC)

	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: when, Mtime: when,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCompleted,
		"times skipped on the ENOSYS leg classify as the completed publish, never a staging failure")
	require.ErrorIs(t, err, syscall.ENOSYS)
	require.NotNil(t, info, "wave-61 (codex P2): the verified publish result IS handed back")

	current, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, current.ModTime().Equal(preInfo.ModTime()),
		"dest keeps the staged inode's own mtime — the requested times are skipped, never stamped")
	require.False(t, current.ModTime().Equal(when))
}

// r12: the ENOSYS leg runs NO destination lookup past the post-publish
// reverify — wedge EVERY second lookup and prove it is unreachable (the
// pre-r12 pre/post-Chtimes glimpses and the wave-60 error-step re-
// derivation are gone, so the indeterminate-relookup refusal class has no
// producer left on this leg).
func TestPublishStagedBoundInfoW31POSIX_ENOSYSLegNeverRelookupsDest(t *testing.T) {
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
		return nil, sentinel // any second glimpse of the published name
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
	require.ErrorIs(t, err, ErrPublishCompleted,
		"the completed classification needs no second lookup — it stands from the reverify instant")
	require.NotErrorIs(t, err, sentinel, "the wedged second lookup is never reached")
	require.NotErrorIs(t, err, ErrPublishStagedIdentityIndeterminate,
		"the indeterminate-refusal class has no producer on this leg anymore")
	require.NotNil(t, info, "wave-61 — handed back so restore converges")
	require.Equal(t, 1, calls)
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
