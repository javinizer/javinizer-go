package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// W37x: releaseClaimedReservation's proven-ours arm must actually release the
// placeholder (covers the Remove at the top leg of the switch).
func TestReleaseClaimedReservationW37X_ReleasesProvenPlaceholder(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")

	// Claim exactly as the production claim does.
	f, err := fs.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	claim, err := fs.Stat(backup)
	require.NoError(t, err)

	releaseClaimedReservation(fs, backup, claim)

	_, err = os.Lstat(backup)
	require.True(t, os.IsNotExist(err), "proven placeholder released")
}

// W37x: vanished reservation is a silent no-op (no error surface, no log).
func TestReleaseClaimedReservationW37X_VanishedIsNoOp(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "gone.dlbak.0123456789abcdef")
	releaseClaimedReservation(fs, backup, nil)
}

// W37x: foreign occupant is never removed.
func TestReleaseClaimedReservationW37X_ForeignOccupantPreserved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(backup, []byte("foreign-bytes"), 0o644))

	releaseClaimedReservation(fs, backup, nil)

	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "foreign-bytes", string(got), "foreign occupant byte-intact")
}

// Wave-38 (finding F3): releaseExchangedPlaceholder's take-aside legs, moved
// untagged with the function so every host exercises them — the exchange leg

func TestReleaseExchangedPlaceholderW37_Legs(t *testing.T) {
	claimOf := func(t *testing.T, fsys afero.Fs, path string) os.FileInfo {
		t.Helper()
		require.NoError(t, afero.WriteFile(fsys, path, nil, 0o600))
		info, err := fsys.Stat(path)
		require.NoError(t, err)
		return info
	}

	t.Run("vanished completes itself", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/claim")
		require.NoError(t, releaseExchangedPlaceholder(base, "/never-existed", claim))
	})

	t.Run("indeterminate lookup refuses", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/claim")
		sentinel := errors.New("w37 lstat wedged")
		fsys := &w37LstatFailFs{Fs: base, victim: "/dest", err: sentinel}
		err := releaseExchangedPlaceholder(fsys, "/dest", claim)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("foreign occupant kept", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/claim")
		require.NoError(t, afero.WriteFile(base, "/dest", []byte("foreign"), 0o600))
		err := releaseExchangedPlaceholder(base, "/dest", claim)
		require.ErrorIs(t, err, fsutil.ErrPublishCollision)
		require.Equal(t, "foreign", string(mustReadDownloaderW7(t, base, "/dest")), "foreign bytes intact")
	})

	t.Run("unlink failure restores the verified placeholder to dest", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/dest")
		sentinel := errors.New("w37 remove wedged")
		// Wave-38: the only unlink targets the take-aside SCRATCH name
		// (a .dlq. sibling) — the destination name is never removed by
		// pathname.
		fsys := &w37RemoveFailFs{Fs: base, victim: rollbackQuarantineSuffix, err: sentinel}
		err := releaseExchangedPlaceholder(fsys, "/dest", claim)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "remove take-aside exchanged placeholder")
		info, serr := base.Stat("/dest")
		require.NoError(t, serr, "the wedge compensation restored the placeholder to dest")
		require.Zero(t, info.Size(), "restored placeholder is the 0-byte reservation")
		entries, rerr := afero.ReadDir(base, "/")
		require.NoError(t, rerr)
		for _, e := range entries {
			require.False(t, strings.Contains(e.Name(), rollbackQuarantineSuffix),
				"a successful restore leaves no scratch litter")
		}
	})

	t.Run("scratch swap under the unlink refuses and preserves the foreign object", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/dest")
		fsys := &w37xScratchSwapFs{Fs: base}
		err := releaseExchangedPlaceholder(fsys, "/dest", claim)
		require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign,
			"the swapped scratch occupant is never unlinked")
		require.Equal(t, "w37x foreign scratch", string(mustReadDownloaderW7(t, base, "/dest")),
			"the refusal's no-replace restore re-homes the occupant onto the free source name — byte-intact")
		entries, rerr := afero.ReadDir(base, "/")
		require.NoError(t, rerr)
		for _, e := range entries {
			require.NotContains(t, e.Name(), rollbackQuarantineSuffix, "no scratch litter after the restore")
		}
	})

	t.Run("scratch claim failure refuses with the placeholder intact", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/dest")
		sentinel := errors.New("w37x entropy wedged")
		prev := rollbackQuarantineRandReader
		rollbackQuarantineRandReader = &w37xFailReader{err: sentinel}
		t.Cleanup(func() { rollbackQuarantineRandReader = prev })

		err := releaseExchangedPlaceholder(base, "/dest", claim)
		require.ErrorIs(t, err, sentinel)
		info, serr := base.Stat("/dest")
		require.NoError(t, serr, "the placeholder is untouched when no scratch could be reserved")
		require.Zero(t, info.Size())
	})

	t.Run("racer swapped dest pre-take — moved object restored byte-intact", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/dest")
		fsys := &w37xDestSwapOnTakeFs{Fs: base, dest: "/dest"}
		err := releaseExchangedPlaceholder(fsys, "/dest", claim)
		require.ErrorIs(t, err, fsutil.ErrPublishCollision,
			"the take-aside proof caught the mid-take swap")
		require.Equal(t, "racer pre-take swap", string(mustReadDownloaderW7(t, base, "/dest")),
			"the racer's object took dest back via the no-replace restore — never removed")
	})

	t.Run("verified placeholder removed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		claim := claimOf(t, base, "/dest")
		require.NoError(t, releaseExchangedPlaceholder(base, "/dest", claim))
		_, err := base.Stat("/dest")
		require.True(t, os.IsNotExist(err))
	})
}

// destPlaceholderMatchesClaim unit legs — the pure shape/identity binding,
// host-testable on every CI depot since wave-38 moved the function untagged.
func TestDestPlaceholderMatchesClaimW37_Legs(t *testing.T) {
	tmp := t.TempDir()
	mk := func(name string, data []byte) os.FileInfo {
		p := filepath.Join(tmp, name)
		require.NoError(t, os.WriteFile(p, data, 0o600))
		info, err := os.Lstat(p)
		require.NoError(t, err)
		return info
	}

	claim := mk("claim", nil)
	require.True(t, destPlaceholderMatchesClaim(claim, claim), "the placeholder itself matches")
	require.False(t, destPlaceholderMatchesClaim(nil, claim), "a nil occupant never matches")

	mk("other", nil)
	require.NoError(t, os.Chtimes(filepath.Join(tmp, "other"), claim.ModTime(), claim.ModTime()))
	otherSameTime, err := os.Lstat(filepath.Join(tmp, "other"))
	require.NoError(t, err)
	require.False(t, destPlaceholderMatchesClaim(otherSameTime, claim),
		"same shape, different inode — dev/inode binding refuses")

	mk("mutated", nil)
	require.NoError(t, os.Chtimes(filepath.Join(tmp, "mutated"), claim.ModTime().Add(123), claim.ModTime().Add(123)))
	mutatedInfo, err := os.Lstat(filepath.Join(tmp, "mutated"))
	require.NoError(t, err)
	require.False(t, destPlaceholderMatchesClaim(mutatedInfo, claim), "mtime divergence refuses")

	nonEmpty := mk("nonempty", []byte("x"))
	require.False(t, destPlaceholderMatchesClaim(nonEmpty, claim), "non-empty refuses")

	link := filepath.Join(tmp, "link")
	require.NoError(t, os.Symlink(filepath.Join(tmp, "claim"), link))
	linkInfo, err := os.Lstat(link)
	require.NoError(t, err)
	require.False(t, destPlaceholderMatchesClaim(linkInfo, claim), "a symlink refuses")

	// Identity unavailable on BOTH sides (MemMap-shaped stats): the shape
	// binding alone carries the answer.
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/a", nil, 0o600))
	memInfo, err := base.Stat("/a")
	require.NoError(t, err)
	require.True(t, destPlaceholderMatchesClaim(memInfo, memInfo),
		"no dev/inode exposed on either side — shape match answers")

	// Claim identity exposed, occupant identity not: the dev/inode block is
	// skipped and the shape binding answers.
	require.NoError(t, afero.WriteFile(base, "/b", nil, 0o600))
	curMem, err := base.Stat("/b")
	require.NoError(t, err)
	require.NoError(t, base.Chtimes("/b", claim.ModTime(), claim.ModTime()))
	curMem, err = base.Stat("/b")
	require.NoError(t, err)
	require.True(t, destPlaceholderMatchesClaim(curMem, claim),
		"claim dev/ino with unexposed occupant identity degrades to the shape binding")
}

// w37LstatFailFs forces an indeterminate lookup for a single victim name.
type w37LstatFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w37LstatFailFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		return nil, false, f.err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// w37RemoveFailFs forces removal failures for names carrying the victim
// substring (wave-38: the take-aside scratch name).
type w37RemoveFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w37RemoveFailFs) Remove(name string) error {
	if strings.Contains(name, f.victim) {
		return f.err
	}
	return f.Fs.Remove(name)
}

// w37xFailReader wedges the quarantine-name entropy draw.
type w37xFailReader struct{ err error }

func (r *w37xFailReader) Read([]byte) (int, error) { return 0, r.err }

// w37xScratchSwapFs replays a foreign object claiming the scratch name
// between the take-aside move and the bound unlink: only after the move
// landed does the Stat hook arm, and the swap lands at the SECOND post-move
// scratch lookup (the first is the post-move re-proof, the second is the
// unlink's binding re-derivation).
type w37xScratchSwapFs struct {
	afero.Fs
	moved      bool
	afterMoves int
}

func (f *w37xScratchSwapFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.moved = true
	}
	return err
}

func (f *w37xScratchSwapFs) Stat(name string) (os.FileInfo, error) {
	if f.moved && strings.Contains(name, rollbackQuarantineSuffix) {
		f.afterMoves++
		if f.afterMoves == 2 {
			if err := f.Fs.Remove(name); err == nil {
				if werr := afero.WriteFile(f.Fs, name, []byte("w37x foreign scratch"), 0o600); werr != nil {
					return nil, werr
				}
			}
		}
	}
	return f.Fs.Stat(name)
}

// w37xDestSwapOnTakeFs replays a racer replacing the exchange-parked
// placeholder at dest between the classification and the take-aside move:
// the pre-move reservation re-proof and the take both ride Rename, so the
// hook lands the swap immediately before relocating it.
type w37xDestSwapOnTakeFs struct {
	afero.Fs
	dest string
	done bool
}

func (f *w37xDestSwapOnTakeFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.dest && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.done = true
		// The swap lands first: fresh (non-zero) content at dest, then the
		// take moves THAT object onto the scratch — the proof must refuse.
		_ = f.Fs.Remove(f.dest)
		if err := afero.WriteFile(f.Fs, f.dest, []byte("racer pre-take swap"), 0o600); err != nil {
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}
