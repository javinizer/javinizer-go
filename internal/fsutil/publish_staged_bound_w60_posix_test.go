//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-60 (P2) + r12 (P2 — "keep
// deferred timestamps bound to the published inode") — the ENOSYS leg
// (AIX/Solaris: stagedHandleChtimes answers ENOSYS because no fd-scoped
// times primitive exists there). Pre-r12 the times were deferred onto the
// PUBLISHED destination name: an ownerNow identity re-proof, the pathname
// Chtimes itself, and wave-60's error-step identity re-derivation all rode
// the published NAME. Even behind the re-proof, the check→utimens window
// let a directory writer land OUR stamp on a substitute's metadata — a
// planted symlink would be chased — and the re-derivation could only
// classify that harm after the fact, never prevent it. r12 refuses the
// pathname fallback ENTIRELY: the identity-verified publish completes with
// NO times applied and NO post-reverify destination operation of any kind.
//
// The wave-60 completed classification (r11) stands: the error carries
// ErrPublishCompleted (destination proven to carry the published bytes —
// callers run their completed-publish discipline), the ENOSYS answer stays
// unwrap-reachable, and it is neither a *StagingTimesError (a pre-publish
// failure class whose cleanup removes the staged name — the successful
// publish already consumed it) nor an identity break.
//
// Coverage posture: the posix leg is gated on the REAL OsFs
// (osStagingHandle), so no afero-level Chtimes recorder can interpose on
// this leg. The "no pathname Chtimes ever runs on dest" pin is therefore
// carried by the two package seams' call recordings (one fd-times attempt,
// exactly ONE destination lookup — the post-publish reverify, i.e. no
// check→apply window exists to wedge) plus the behavioral proof that the
// published object's times stay exactly the staged inode's own (a stamped
// Chtimes, pathname or otherwise, would have moved the mtime).

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

// w60EnosysCase forces the r12 ENOSYS leg: the fd times seam answers ENOSYS
// from a call-recording wedge, and the destination-lookup seam records every
// post-publish dest glimpse (its recording proves the reverify is the ONLY
// name lookup — no pre/post-times glimpse, no error-step re-derivation).
func w60EnosysCase(t *testing.T, dest string) (staged string, fh afero.File, fdCalls *int, lookups *[]string) {
	t.Helper()
	fdCalls = new(int)
	prevFd := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
		(*fdCalls)++
		return syscall.ENOSYS
	}
	t.Cleanup(func() { stagedHandleChtimes = prevFd })

	lookups = new([]string)
	prevLstat := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		*lookups = append(*lookups, name)
		return os.Lstat(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLstat })

	staged, fh = w30Stage(t, afero.NewOsFs(), dest, ".rstr", 0o640)
	return staged, fh, fdCalls, lookups
}

// r12 (a): the ENOSYS leg never touches the published name — the verified
// publish surfaces the completed classification (r11) with NO times
// applied, the destination keeping its staged bytes AND staged times.
func TestPublishStagedBoundW60POSIX_ENOSYSLegSkipsTimesSurfacesCompleted(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	staged, fh, fdCalls, lookups := w60EnosysCase(t, dest)
	preInfo, err := fh.Stat()
	require.NoError(t, err)

	when := time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)
	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: when, Mtime: when,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCompleted,
		"a verified publish with skipped times is wave-60's completed class, not a staging failure")
	require.ErrorIs(t, err, syscall.ENOSYS, "the platform's fd-times answer stays unwrap-reachable")
	var timesErr *StagingTimesError
	require.False(t, errors.As(err, &timesErr),
		"never the pre-publish times arm — its staged-name cleanup would remove a name the publish consumed")
	require.NotErrorIs(t, err, ErrPublishStagedIdentityBreak,
		"the publish was verified — no identity break")
	require.NotNil(t, info,
		"wave-61 — destInfo travels with the completed outcome so reverts converge")

	require.Equal(t, 1, *fdCalls, "exactly one fd-times attempt — the ENOSYS answer itself")
	require.Equal(t, []string{dest}, *lookups,
		"the post-publish reverify is the ONLY dest name lookup: no check→apply window, no deferred relookup, no error-step re-derivation — hence no pathname Chtimes on dest exists to record")

	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the proven publish is never undone by the times refusal")
	cur, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, cur.ModTime().Equal(preInfo.ModTime()),
		"dest keeps the staged inode's own times — no Chtimes (pathname or otherwise) ever landed")
	require.False(t, cur.ModTime().Equal(when), "the requested times were SKIPPED, not stamped")
	_, serr := os.Lstat(staged)
	require.ErrorIs(t, serr, os.ErrNotExist, "the publish consumed the staged name — no litter, nothing to clean")
}

// r12 (b): the finding's own race replayed — a directory writer swaps dest
// for a SYMLINK into a foreign canary in the instant the verified reverify
// returns. Pre-r12 the deferred leg would have re-glimpsed the name and
// any slip would have pathname-stamped the chase target; r12 runs NO dest
// operation past the reverify, so the symlink substitute and the foreign
// metadata it points at stay byte- and stamp-perfect while the completed
// classification (fixed at the reverify instant) still surfaces.
func TestPublishStagedBoundW60POSIX_ENOSYSLegSubstituteAfterReverifyUntouched(t *testing.T) {
	prevFd := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevFd })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	canary := filepath.Join(dir, "canary.jpg")
	require.NoError(t, os.WriteFile(canary, []byte("foreign canary"), 0o644))
	canaryMtime := time.Date(2010, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(canary, canaryMtime, canaryMtime))
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	lookups := 0
	prevLstat := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		lookups++
		info, lerr := os.Lstat(name)
		// The reverify captured OUR inode; the directory writer's symlink
		// substitute lands in the very next instant.
		require.NoError(t, os.Remove(dest))
		require.NoError(t, os.Symlink(canary, dest))
		return info, lerr
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLstat })

	when := time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)
	info, err := PublishStagedBoundInfo(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: when, Mtime: when,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCompleted,
		"the completed classification stands from the verified-publish instant")
	require.ErrorIs(t, err, syscall.ENOSYS)
	require.NotErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"no foreign-occupant arm remains on this leg — nothing is inspected past the reverify")
	require.NotNil(t, info)
	require.Equal(t, 1, lookups, "exactly one dest lookup: no check→apply window can be widened into a stamp")

	linkInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink,
		"the substitute symlink is preserved byte-intact — never followed, never stamped, never removed")
	target, rerr := os.ReadFile(canary)
	require.NoError(t, rerr)
	require.Equal(t, "foreign canary", string(target), "the chase target's bytes are untouched")
	canaryInfo, cerr := os.Lstat(canary)
	require.NoError(t, cerr)
	require.True(t, canaryInfo.ModTime().Equal(canaryMtime),
		"the chase target's METADATA is untouched — the skipped times never landed through the symlink")
	require.False(t, canaryInfo.ModTime().Equal(when))
}
