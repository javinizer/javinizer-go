package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		// Wave-38/44: the only unlink targets the take-aside's fresh
		// terminal name (a .dlq..vac. sibling) — neither the destination
		// name nor the scratch name is ever removed by pathname.
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
	// restoreSourceIdentity exposes dev/inode only on the POSIX Stat_t
	// targets (restore_source_identity_other.go answers not-OK elsewhere):
	// on Windows the SAME-SHAPE different-inode leg degrades to the shape
	// binding alone, so the refusal is asserted only where a kernel identity
	// exists (the w12/k4-style windows-keyed expectation).
	if runtime.GOOS == "windows" {
		require.True(t, destPlaceholderMatchesClaim(otherSameTime, claim),
			"windows exposes no dev/ino — the shape binding alone answers")
	} else {
		require.False(t, destPlaceholderMatchesClaim(otherSameTime, claim),
			"same shape, different inode — dev/inode binding refuses")
	}

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

// w37RemoveFailFs wedges the ONE bound unlink (wave-38/43: the take-aside's
// held scratch name; wave-44: the scratch name itself is never path-removed
// — the bound unlink vacates the proven object onto a FRESH claimed
// terminal name and unlinks only that). The double LEARNS the scratch name
// from the first O_EXCL quarantine claim (the ".vac." claim is ignored),
// then arms the SECOND scratch→".vac." rename target — the FIRST is the
// conditional take's internal reservation vacate, the second is the bound
// unlink's terminal vacate — and fails only the armed terminal name's
// Removes. The take's own claim-bound housekeeping rides through.
type w37RemoveFailFs struct {
	afero.Fs
	victim   string
	err      error
	name     string // learned scratch name
	vacates  int    // scratch→".vac." renames observed
	terminal string // armed terminal name (the second vacate's target)
}

func (f *w37RemoveFailFs) Rename(oldname, newname string) error {
	if f.name != "" && oldname == f.name && strings.Contains(newname, ".vac.") {
		f.vacates++
		if f.vacates == 2 {
			f.terminal = newname
		}
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *w37RemoveFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.name == "" && flag&os.O_EXCL != 0 && strings.Contains(name, f.victim) && !strings.Contains(name, ".vac.") {
		f.name = name
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w37RemoveFailFs) Remove(name string) error {
	if f.terminal != "" && name == f.terminal {
		return f.err
	}
	return f.Fs.Remove(name)
}

// w37xFailReader wedges the quarantine-name entropy draw.
type w37xFailReader struct{ err error }

func (r *w37xFailReader) Read([]byte) (int, error) { return 0, r.err }

// w37xScratchSwapFs replays a foreign object claiming the scratch name
// between the take-aside move and the bound unlink. Wave-43: the take
// inspects the scratch name through the conditional handoff (reservation
// re-proof, publish classification, post-move re-proof, unlink binding),
// with the internal ".vac." housekeeping riding separate names, so the
// double LEARNS the scratch name from the first O_EXCL quarantine claim,
// arms once the publish rename lands there (oldname carries no suffix),
// and the swap lands at the SECOND post-arm scratch lookup (the first is
// the post-move re-proof, the second is the unlink's binding re-derivation).
type w37xScratchSwapFs struct {
	afero.Fs
	scratch    string
	moved      bool
	afterMoves int
}

func (f *w37xScratchSwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.scratch == "" && flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) && !strings.Contains(name, ".vac.") {
		f.scratch = name
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w37xScratchSwapFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && newname == f.scratch && !strings.Contains(oldname, rollbackQuarantineSuffix) {
		f.moved = true
	}
	return err
}

func (f *w37xScratchSwapFs) Stat(name string) (os.FileInfo, error) {
	if f.moved && name == f.scratch {
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
