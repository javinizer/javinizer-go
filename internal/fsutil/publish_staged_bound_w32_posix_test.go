//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215
// findings R2+R3+R5):
//
//   - R2: the recorded-plant displacement unlink used to swallow its Remove
//     error and fall into the republish loop; it now refuses typed (a failed
//     displacement must never proceed to a republish over the surviving
//     foreign object). The windows-leg twin compiles only on Windows (its
//     own waves pin the shape there).
//   - R5: the ENOSYS deferred-times legs re-prove the published name against
//     the staged inode around the name-based Chtimes: a foreign occupant or
//     vanished/indeterminate answer skips the times and refuses typed, the
//     post-Chtimes relookup failure is the typed indeterminate refusal
//     (updated w31 test), and a relookup naming a different inode is never
//     handed back as the published identity.

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

// w32PlantBundle stages a genuine file, then runs one publish cycle whose
// publish call replays the directory writer (staged name swapped away, plant
// written onto it) so PublishNoReplace links the PLANT at dest; the returned
// script drives the publishStagedBoundDestLstat seam and records each lookup.
type w32PlantBundle struct {
	dest      string
	staged    string
	fh        afero.File
	plantInfo os.FileInfo
	lookups   int
}

func newW32PlantBundle(t *testing.T) *w32PlantBundle {
	t.Helper()
	fs := afero.NewOsFs()
	dir := t.TempDir()
	b := &w32PlantBundle{dest: filepath.Join(dir, "poster.jpg")}
	b.staged, b.fh = w30Stage(t, fs, b.dest, ".rstr", 0o640)
	return b
}

// publishWedge replays the plant onto the staged name, then runs the real
// no-replace publish (dest provably carries the plant afterwards).
func (b *w32PlantBundle) publishWedge(t *testing.T) func(afero.Fs, string, string) error {
	t.Helper()
	wedged := false
	return func(f afero.Fs, src, dst string) error {
		if !wedged {
			wedged = true
			w30SwapPlant(t, src)
		}
		return PublishNoReplace(f, src, dst)
	}
}

// scriptLstats drives the destination-lookup seam: the first lookup (the
// mismatch detection) catches the real plant and records its identity; the
// SECOND lookup (the removal binding) runs fn before answering.
func (b *w32PlantBundle) scriptLstats(t *testing.T, onSecond func(name string)) {
	t.Helper()
	prev := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		b.lookups++
		if b.lookups == 2 && onSecond != nil {
			onSecond(name)
		}
		info, err := os.Lstat(name)
		if b.lookups == 1 {
			b.plantInfo = info
		}
		if b.lookups == 2 {
			return b.plantInfo, err // replay the recorded plant identity
		}
		return info, err
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prev })
}

// R2 (i): the displacement Remove fails — the recorded plant (verified by
// the binding re-lookup) cannot be unlinked (a non-empty directory occupies
// the real path at that instant). The leg refuses typed instead of wedging
// the delete and republishing over the surviving occupant.
func TestPublishStagedBoundW32POSIX_PlantDisplacementFailureRefuses(t *testing.T) {
	b := newW32PlantBundle(t)
	b.scriptLstats(t, func(name string) {
		// The reverify→unlink window, replayed: the real path no longer
		// holds the plant at all — a non-empty directory took its place.
		require.NoError(t, os.Remove(b.dest))
		require.NoError(t, os.Mkdir(b.dest, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(b.dest, "foreign"), []byte("x"), 0o644))
	})

	err := PublishStagedBoundInfo_(t, b)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.Contains(t, err.Error(), "displacement of the recorded plant")
	entries, derr := os.ReadDir(b.dest)
	require.NoError(t, derr, "a failed displacement never proceeds to a republish — the directory survives")
	require.Len(t, entries, 1)
	require.Equal(t, 2, b.lookups)
}

func PublishStagedBoundInfo_(t *testing.T, b *w32PlantBundle) error {
	t.Helper()
	_, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: b.publishWedge(t), NoReplace: true,
		Staged: b.staged, Handle: b.fh, Dest: b.dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	return err
}

// R2 (ii): the recorded plant vanished between the binding lookup and the
// unlink (Remove answers ENOENT) — the loop proceeds to restage from the
// handle and republishes the genuine bytes.
func TestPublishStagedBoundW32POSIX_PlantVanishedAtUnlinkRestages(t *testing.T) {
	b := newW32PlantBundle(t)
	b.scriptLstats(t, func(name string) {
		require.NoError(t, os.Remove(b.dest), "the plant vanished in the binding→unlink window")
	})

	_, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: b.publishWedge(t), NoReplace: true,
		Staged: b.staged, Handle: b.fh, Dest: b.dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err, "the vanished plant leaves nothing to displace — restage+republish heals")
	got, rerr := os.ReadFile(b.dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// w32EnosysCase forces the deferred-times leg (fd times answer ENOSYS) and
// drives the destination-lookup seam by call number: 1 = post-publish
// reverify (must succeed), 2 = pre-Chtimes ownership re-proof, 3 =
// post-Chtimes identity relookup.
func w32EnosysCase(t *testing.T) (dest, staged string, fh afero.File, foreignInfo os.FileInfo) {
	t.Helper()
	prevTimes := stagedHandleChtimes
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return syscall.ENOSYS }
	t.Cleanup(func() { stagedHandleChtimes = prevTimes })

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest = filepath.Join(dir, "poster.jpg")
	staged, fh = w30Stage(t, fs, dest, ".rstr", 0o640)
	foreign := filepath.Join(dir, "foreign.jpg")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign replacement"), 0o644))
	var err error
	foreignInfo, err = os.Lstat(foreign)
	require.NoError(t, err)
	return dest, staged, fh, foreignInfo
}

func w32EnosysRun(t *testing.T, dest, staged string, fh afero.File, script func(calls int, name string) (os.FileInfo, error)) error {
	t.Helper()
	calls := 0
	prevLstat := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		calls++
		return script(calls, name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLstat })
	_, err := PublishStagedBoundInfo(StagedPublish{
		FS: afero.NewOsFs(), Publish: PublishNoReplace, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		ApplyTimes: true, Atime: time.Now(), Mtime: time.Now(),
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	return err
}

// R5 (a): a foreign occupant claiming the destination inside the
// match→deferred-Chtimes window never gets its times clobbered — the
// pre-times re-proof names it and the leg refuses typed with the times
// skipped.
func TestPublishStagedBoundW32POSIX_ENOSYSDeferredTimesForeignOccupantPreTimes(t *testing.T) {
	dest, staged, fh, foreignInfo := w32EnosysCase(t)
	err := w32EnosysRun(t, dest, staged, fh, func(calls int, name string) (os.FileInfo, error) {
		if calls == 2 {
			return foreignInfo, nil
		}
		return os.Lstat(name)
	})
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, ErrPublishStagedIdentityIndeterminate)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
}

// R5 (b): the destination vanished before the deferred times leg.
func TestPublishStagedBoundW32POSIX_ENOSYSDeferredTimesDestVanishedPreTimes(t *testing.T) {
	dest, staged, fh, _ := w32EnosysCase(t)
	err := w32EnosysRun(t, dest, staged, fh, func(calls int, name string) (os.FileInfo, error) {
		if calls == 2 {
			return nil, os.ErrNotExist
		}
		return os.Lstat(name)
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityIndeterminate)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
}

// R5 (c): an indeterminate answer before the deferred times leg.
func TestPublishStagedBoundW32POSIX_ENOSYSDeferredTimesIndeterminatePreTimes(t *testing.T) {
	dest, staged, fh, _ := w32EnosysCase(t)
	sentinel := errors.New("pre-times lookup wedged")
	err := w32EnosysRun(t, dest, staged, fh, func(calls int, name string) (os.FileInfo, error) {
		if calls == 2 {
			return nil, sentinel
		}
		return os.Lstat(name)
	})
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, ErrPublishStagedIdentityIndeterminate)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
}

// R5 (d): the post-Chtimes identity relookup fails — the typed indeterminate
// refusal, never a nil-identity success.
func TestPublishStagedBoundW32POSIX_ENOSYSFreshRelookFailureRefuses(t *testing.T) {
	dest, staged, fh, _ := w32EnosysCase(t)
	sentinel := errors.New("post-times relookup wedged")
	err := w32EnosysRun(t, dest, staged, fh, func(calls int, name string) (os.FileInfo, error) {
		if calls == 3 {
			return nil, sentinel
		}
		return os.Lstat(name)
	})
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, ErrPublishStagedIdentityIndeterminate)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
}

// R5 (e): the post-Chtimes relookup names a DIFFERENT (foreign) inode — the
// foreign identity is never handed back as the published one.
func TestPublishStagedBoundW32POSIX_ENOSYSFreshRelookForeignRefuses(t *testing.T) {
	dest, staged, fh, foreignInfo := w32EnosysCase(t)
	err := w32EnosysRun(t, dest, staged, fh, func(calls int, name string) (os.FileInfo, error) {
		if calls == 3 {
			return foreignInfo, nil
		}
		return os.Lstat(name)
	})
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
}
