//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215
// findings R2+R3+R5):
//
//   - R2: the recorded-plant displacement unlink used to swallow its Remove
//     error and fall into the republish loop; it refused typed from wave-32
//     on, and wave-38 (codex P2, PR#215 finding F1) retired the displacement
//     entirely: NOTHING recorded post-publish is ever unlinked — the
//     post-publish mismatch occupant (plant or legitimate gap file,
//     indistinguishable) is preserved with a typed foreign-occupant refusal,
//     and recovery proceeds only when the binding re-lookup finds the
//     destination free again (the vanish leg). The windows-leg twin compiles
//     only on Windows (its own waves pin the shape there).
//   - R5 (history — retired by r12, codex P2 "keep deferred timestamps
//     bound to the published inode"): the ENOSYS legs used to re-prove the
//     published name around a name-based deferred Chtimes. r12 refuses the
//     pathname fallback entirely: the ENOSYS leg completes the verified
//     publish with the times SKIPPED and runs NO post-reverify destination
//     glimpse at all, so the foreign-occupant / indeterminate refusal
//     family those legs fed has no producer left on the times leg (the
//     completed-classification + no-foreign-stamp pins live in the wave-60
//     companion file).

import (
	"os"
	"path/filepath"
	"testing"

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
// SECOND lookup (the wave-38 vanish-check binding) runs fn before answering.
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
		return info, err
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prev })
}

// R2 (i) re-shaped by wave-38 (finding F1): between the detection and the
// binding re-lookup the recorded plant is replaced BY A DIRECTORY (a
// non-empty, un-unlinkable occupant) — the refusal is typed through the
// foreign-occupant class, NOTHING is ever removed by this leg whatever the
// occupant's shape, and recovery never proceeds over it.
func TestPublishStagedBoundW32POSIX_PlantReplacedByDirectoryPreserved(t *testing.T) {
	b := newW32PlantBundle(t)
	b.scriptLstats(t, func(name string) {
		// The detection→binding window, replayed: the real path no longer
		// holds the plant at all — a non-empty directory took its place.
		require.NoError(t, os.Remove(b.dest))
		require.NoError(t, os.Mkdir(b.dest, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(b.dest, "foreign"), []byte("x"), 0o644))
	})

	err := PublishStagedBoundInfo_(t, b)
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	entries, derr := os.ReadDir(b.dest)
	require.NoError(t, derr, "the directory occupant is never touched — no republish proceeds over it")
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

// R2 (ii): the recorded plant vanished between the detection and the
// binding re-lookup — nothing stands at dest, so the loop proceeds to
// restage from the handle and republishes the genuine bytes into proven
// absence (the wave-30 vanish leg, unchanged by wave-38).
func TestPublishStagedBoundW32POSIX_PlantVanishedAtUnlinkRestages(t *testing.T) {
	b := newW32PlantBundle(t)
	b.scriptLstats(t, func(name string) {
		require.NoError(t, os.Remove(b.dest), "the plant vanished in the detection→binding window")
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
