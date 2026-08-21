//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-30 (P1) — PublishStagedBound's
// POSIX attack legs: the verify→publish window from the wave-29 posture is
// exercised by wedging the publish function itself (the deterministic
// mid-window instant — exactly what a racing directory writer would do),
// and the bound loop must either republish the genuine bytes or refuse
// typed with nothing consumed.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// plantAtStaged performs the directory writer's move: rename the staged name
// away and plant foreign bytes (or a symlink) on it.
func w30SwapPlant(t *testing.T, staged string) {
	t.Helper()
	require.NoError(t, os.Rename(staged, staged+".w30-away"))
	require.NoError(t, os.WriteFile(staged, []byte("foreign window plant"), 0o644))
}

// The wave-29 refusal preserved: a name swapped BEFORE the helper's own
// verify is refused typed, the plant untouched, no publish attempted.
func TestPublishStagedBoundW30POSIX_PreVerifyPlantRefusesUntouched(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	require.NoError(t, os.Rename(staged, staged+".w30-away"))
	require.NoError(t, os.Symlink(victim, staged))

	published := 0
	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(f afero.Fs, src, dst string) error { published++; return PublishNoReplace(f, src, dst) },
		Staged:  staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
		ApplyTimes: false,
	})
	require.ErrorIs(t, err, ErrPublishStagedVerify)
	require.ErrorIs(t, err, ErrStagedIdentityMismatch)
	require.Zero(t, published, "no publish ever ran against the foreign name")
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the foreign plant is never removed by the refusal")
	_, derr := os.Lstat(dest)
	require.ErrorIs(t, derr, os.ErrNotExist)
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away), "the staged inode survives under the attacker's name")
}

// An outright-missing staged name is the same pre-publish refusal class but
// WITHOUT the mismatch sentinel: the lookup failure stays unwrap-reachable.
func TestPublishStagedBoundW30POSIX_PreVerifyMissingNameRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	require.NoError(t, os.Rename(staged, staged+".w30-away"))

	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true, Publish: PublishNoReplace,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedVerify)
	require.ErrorIs(t, err, os.ErrNotExist, "the name lookup failure stays reachable")
	require.NotErrorIs(t, err, ErrStagedIdentityMismatch)
}

// A closed staging handle fails the proof's own fstat — the failure is
// neither the mismatch sentinel nor a name lookup, and nothing is touched.
func TestVerifyStagedIdentityW30_ClosedHandleStatFails(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh := w30Stage(t, fs, filepath.Join(dir, "poster.jpg"), ".rstr", 0o640)
	require.NoError(t, fh.Close())

	err := VerifyStagedIdentity(fs, staged, fh)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStagedIdentityMismatch)
	require.Contains(t, err.Error(), "staged identity proof")
}

// THE wave-30 finding's window: the attack lands BETWEEN verify and publish.
// The plant gets installed at dest by the no-replace publish and the
// post-publish reverify proves the occupant is not ours. Wave-38 (codex P2,
// PR#215 finding F1): the occupant is NEVER deleted here — the kernel proved
// dest free at the rename instant, so what the publish installed and what a
// legitimate writer created in the publish→reverify gap are
// indistinguishable; both are preserved byte-intact with the typed
// ErrPublishStagedForeignOccupant refusal, the genuine inode survives on the
// handle (for its owner under the attacker's name), and nothing is consumed.
func TestPublishStagedBoundW30POSIX_MismatchPreservesOccupantTypedRefusal(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"the post-publish occupant is preserved, never displaced")
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.Equal(t, 1, attacked, "no republish ever runs over unverified bytes")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the plant survives byte-intact at dest — the finding's lost-write stays closed")
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away),
		"the genuine inode stays reachable under the attacker's name until unlinked by its owner")
}

// Same window, replace-semantics publish over an occupied dest: the plant is
// never displaced by deletion — the republish puts the genuine bytes OVER
// whatever dest holds, which is exactly what a replace publish means.
func TestPublishStagedBoundW30POSIX_MismatchRepublishesGenuineReplace(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(dest, []byte("current bytes"), 0o644))
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return ReplaceFile(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: false,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// A PRE-publish obstacle: the plant claims dest before the first publish
// attempt, so the no-replace publish collides. Wave-49 (codex P2, PR#215):
// the collision is a REFUSAL surfaced verbatim, not a displace-then-retry —
// the racer WON the no-replace race and its bytes prevail byte-intact; the
// collided publish never consumed the staged name, so the genuine bytes are
// retained for the caller's wave-15 reclassification legs.
func TestPublishStagedBoundW30POSIX_PrePublishCollisionRefusesVerbatim(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacks := 0
	wedge := func(f afero.Fs, src, dst string) error {
		// A plant claims dest BEFORE the publish attempt: the no-replace
		// publish collides, and the winner's bytes must prevail.
		require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		attacks++
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCollision,
		"wave-49: the collision winner's race is the verbatim no-replace refusal — never displaced")
	require.NotErrorIs(t, err, ErrPublishStagedExhausted)
	require.NotErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.Equal(t, 1, attacks, "no displace-then-retry: exactly one publish attempt ran")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the collision winner's bytes survive at dest byte-intact")
	got, rerr = os.ReadFile(staged)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the collided publish never consumed the staged name — genuine bytes retained for the caller")
}

// The published destination VANISHING between publish and reverify is the
// same recovery with nothing to displace.
func TestPublishStagedBoundW30POSIX_DestVanishedAfterPublish(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			perr := PublishNoReplace(f, src, dst)
			require.NoError(t, perr)
			require.NoError(t, os.Remove(dst), "the racer unlinks the just-published name")
			return nil
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// An indeterminate post-publish destination lookup refuses typed WITHOUT
// removing anything (nothing is proven about the name). The denial is
// replayed through the publishStagedBoundDestLstat seam (wave-24 fix,
// codex PR#215): the pre-wave-24 test denied the directory with chmod 000,
// which silently SUCCEEDS for uid 0 — on root CI hosts the lookup answered,
// the refusal leg never ran, and the test failed/left the leg uncovered.
// The seam fires deterministically under root and non-root alike.
func TestPublishStagedBoundW30POSIX_ReverifyIndeterminateRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	lookupDenied := errors.New("post-publish destination lookup indeterminate")
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			// The racer made the destination state unprovable between the
			// publish and the reverify.
			return nil, lookupDenied
		}
		return prevLookup(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLookup })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.ErrorIs(t, err, lookupDenied, "the lookup failure stays unwrap-reachable")
	require.NotErrorIs(t, err, ErrPublishStagedExhausted)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the genuine publish landed — the refusal only withholds the proof; nothing foreign was planted and nothing was removed")
	_, lerr := os.Lstat(staged)
	require.ErrorIs(t, lerr, os.ErrNotExist,
		"the publish consumed the staged name before the lookup turned indeterminate — the refusal removes nothing else")
}

// r12 (codex P2 — "keep deferred timestamps bound to the published
// inode"): ENOSYS from the fd times seam (the staging_times_unixother.go
// posture on platforms without an fd-scoped wrapper) no longer defers the
// times onto the PUBLISHED name — the check→apply window could chase a
// planted substitute — so the verified publish completes with the times
// SKIPPED under wave-60's completed classification, and dest keeps the
// staged inode's own times.
func TestPublishStagedBoundW30POSIX_TimesENOSYSSkippedCompletedNoTimesLand(t *testing.T) {
	prev := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prev })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	preInfo, perr := fh.Stat()
	require.NoError(t, perr)
	mt := time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Atime: mt, Mtime: mt, ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCompleted,
		"the verified publish is completed — the skipped times never demote it to a staging failure")
	require.ErrorIs(t, err, syscall.ENOSYS, "the platform answer stays unwrap-reachable")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
	info, serr := os.Stat(dest)
	require.NoError(t, serr)
	require.True(t, info.ModTime().Equal(preInfo.ModTime()),
		"the published destination keeps the staged inode's own times — no pathname Chtimes lands")
	require.False(t, info.ModTime().Equal(mt),
		"the requested times were skipped, not stamped onto the published name")
}

// A hard fd-times failure is the pre-wave-30 *StagingTimesError class: the
// publish never runs, the handle is closed, the staged name is left for the
// caller's cleanup.
func TestPublishStagedBoundW30POSIX_TimesErrorRefusesBeforePublish(t *testing.T) {
	prev := stagedHandleChtimes
	stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error { return syscall.EPERM }
	t.Cleanup(func() { stagedHandleChtimes = prev })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	published := 0
	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(f afero.Fs, src, dst string) error { published++; return PublishNoReplace(f, src, dst) },
		Staged:  staged, Handle: fh, Dest: dest,
		Atime: time.Now(), Mtime: time.Now(), ApplyTimes: true,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	var timesErr *StagingTimesError
	require.ErrorAs(t, err, &timesErr)
	require.Zero(t, published)
	_, lerr := os.Lstat(staged)
	require.NoError(t, lerr, "the (proven-ours) staged name waits for the caller's cleanup")
	_, derr := os.Lstat(dest)
	require.ErrorIs(t, derr, os.ErrNotExist)
}

// The publish's own error passes through verbatim on the POSIX leg too.
func TestPublishStagedBoundW30POSIX_PublishErrorPassthrough(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	sentinel := fmt.Errorf("disk full: %w", syscall.ENOSPC)

	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: true,
		Publish: func(afero.Fs, string, string) error { return sentinel },
		Staged:  staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
		ApplyTimes: false,
	})
	require.ErrorIs(t, err, sentinel)
	_, lerr := os.Lstat(staged)
	require.NoError(t, lerr, "nothing was consumed — caller cleanup sees the staged name")
}

// If the fresh O_EXCL staging name can never be claimed mid-recovery, the
// refusal stays typed (identity-break): the caller retains the backup.
func TestPublishStagedBoundW30POSIX_RestageNamesExhausted(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	// Occupy the whole restage ordinal space the recovery would climb.
	for i := 4; i < 4+64; i++ {
		require.NoError(t, os.WriteFile(dest+".rstr."+strconv.FormatUint(uint64(i), 16), []byte("x"), 0o644))
	}
	vanished := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if vanished == 0 {
			vanished++
			if err := PublishNoReplace(f, src, dst); err != nil {
				return err
			}
			// The racer unlinks the just-published name before the reverify:
			// the wave-38-safe vanish leg restages from the handle.
			require.NoError(t, os.Remove(dst))
			return nil
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(3),
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.True(t, strings.Contains(err.Error(), "re-stage substituted staged file"))
}

// A mid-restaging copy failure (seam replay) refuses typed and removes the
// half-written fresh staged name.
func TestPublishStagedBoundW30POSIX_RestreamFailure(t *testing.T) {
	prev := publishStagedBoundRestream
	publishStagedBoundRestream = func(src afero.File, dst io.Writer) error { return errors.New("replayed restream failure") }
	t.Cleanup(func() { publishStagedBoundRestream = prev })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	vanished := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if vanished == 0 {
			vanished++
			if err := PublishNoReplace(f, src, dst); err != nil {
				return err
			}
			require.NoError(t, os.Remove(dst), "the racer unlinks the just-published name before the reverify")
			return nil
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.Contains(t, err.Error(), "replayed restream failure")
	_, derr := os.Lstat(dest)
	require.ErrorIs(t, derr, os.ErrNotExist, "the vanish leg leaves dest free — nothing of ours ever occupies it")
}
