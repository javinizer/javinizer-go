package downloader

// POSTER-WRITE-HARDENING wave-36 (codex local review round 6, PR#215 finding
// F1) — keep the backup reservation's IDENTITY bound through the
// claim→move handoff.
//
// claimOverwriteBackupPath used to create the 0-byte reservation, close the
// handle, and hand the bare NAME to the dest→backup rename window: a foreign
// writer renaming the reservation away and planting its own object at the
// backup name then had its bytes silently displaced by the replacing rename
// (destroyed before any journal entry named it). The claim now captures the
// reservation's identity from the open handle, and installOverwriting
// re-derives it immediately before the move
// (overwriteBackupReservationStillOurs): a mismatch is the typed collision
// class and the claim CLIMBS to a fresh backup name, never touching the
// foreign occupant.
//
// Test matrix:
//   - placeholder swapped between claim and move → climb, foreign bytes
//     intact, journal points at the fresh name (the finding's headline);
//   - every claim displaced → bounded refusal, collision class, no ledger
//     arm, destination and occupancy untouched;
//   - helper unit legs: vanished reservation (indeterminate), scripted
//     dev/inode mismatch on real files (POSIX), the claimed placeholder
//     itself → ours;
//   - the claim's reservation-stat failure leg drops the placeholder and
//     fails the claim closed.

import (
	"context"
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

// w36SwapOnCloseFile replays the foreign writer landing inside the
// claim→move window: immediately after the reservation handle closes (the
// exact instant claimOverwriteBackupPath returns control), the placeholder
// is removed and a FOREIGN file with real bytes is planted at the same name.
type w36SwapOnCloseFile struct {
	afero.File
	fs    afero.Fs
	name  string
	plant []byte
}

func (f w36SwapOnCloseFile) Close() error {
	err := f.File.Close()
	if err != nil {
		return err
	}
	_ = f.fs.Remove(f.name)
	return afero.WriteFile(f.fs, f.name, f.plant, 0o600)
}

// w36ReservationSwapFs wraps reservation creates of backup-candidate names:
// up to swaps claims hand back swap-on-close files (-1 = every claim).
type w36ReservationSwapFs struct {
	afero.Fs
	swaps   int
	swapped []string
	plant   []byte
}

func (f *w36ReservationSwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && f.swaps != 0 && flag&os.O_EXCL != 0 && strings.Contains(filepath.ToSlash(name), backupSuffixForDest+".") {
		f.swapped = append(f.swapped, name)
		if f.swaps > 0 {
			f.swaps--
		}
		return w36SwapOnCloseFile{File: file, fs: f.Fs, name: name, plant: f.plant}, nil
	}
	return file, err
}

// The finding's headline: a foreign writer displacing the reservation inside
// the claim→move window is REFUSED that occupancy — the install climbs to a
// fresh backup name, the foreign bytes survive byte-intact at the displaced
// name, and the journal points at the fresh own-name.
func TestInstallOverwritingW36_ReservationSwapClimbsForeignSurvives(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W36-SWAP"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w36ReservationSwapFs{Fs: base, swaps: 1, plant: []byte("foreign reservation occupant")}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w36-swap", recorder: recorder,
	})
	require.NoError(t, err, "the displaced claim climbs to a fresh name — the install itself proceeds")
	require.False(t, skipped)
	require.True(t, replaced)
	require.Len(t, fs.swapped, 1, "exactly one claim was displaced")
	require.Equal(t, "foreign reservation occupant", string(mustReadDownloaderW7(t, base, fs.swapped[0])),
		"the foreign occupant at the displaced name is byte-intact — never displaced by OUR move")
	records := recorder.get()
	require.Len(t, records, 1)
	require.NotEqual(t, fs.swapped[0], records[0].backupPath, "the journal points at the FRESH name")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, records[0].backupPath)),
		"the previous destination bytes were set aside on the fresh name")
	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)))
}

// Every claim displaced → the bounded loop refuses with the typed collision
// class: the destination is untouched, nothing is journaled, and the busiest
// foreign occupant keeps its bytes.
func TestInstallOverwritingW36_ReservationSwapExhaustionRefusesCollision(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W36-SWAPALL"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w36ReservationSwapFs{Fs: base, swaps: -1, plant: []byte("f")}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w36-swapall", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision,
		"a reservation proven foreign is the collision class, climbed to exhaustion")
	require.True(t, replaced)
	require.Len(t, fs.swapped, backupNameClaimTries, "the bound is the claim bound")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Empty(t, recorder.get(), "a refused set-aside never arms the ledger")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the busy marker released on the way out")
}

// Helper unit legs: a vanished reservation is indeterminate; the claimed
// placeholder itself verifies on every platform.
func TestOverwriteBackupReservationW36_LookasideLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w36h", 0o755))
	placeholder := "/w36h/reservation"
	require.NoError(t, afero.WriteFile(base, placeholder, nil, 0o600))
	info, err := base.Stat(placeholder)
	require.NoError(t, err)

	require.NoError(t, overwriteBackupReservationStillOurs(base, placeholder, info),
		"the untouched claimed placeholder verifies")

	err = overwriteBackupReservationStillOurs(&w36LstatFailOnMissingFs{Fs: base, victim: placeholder}, placeholder, info)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect backup reservation")
	err = overwriteBackupReservationStillOurs(base, "/w36h/never-existed", info)
	require.Error(t, err, "a vanished reservation never verifies (ENOENT is indeterminate here)")
}

// w36LstatFailOnMissingFs forces the reservation lookup error leg.
type w36LstatFailOnMissingFs struct {
	afero.Fs
	victim string
}

func (f *w36LstatFailOnMissingFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		return nil, false, errors.New("w36 reservation lstat wedged")
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// POSIX-only identity leg: a same-size (0-byte), same-mtime substitute at the
// reservation name is refused via dev/inode — two simultaneously-existing
// real files guarantee the inode comparison cannot collapse.
func TestOverwriteBackupReservationW36_DevInodeMismatchRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity is POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	claimed := filepath.Join(tmp, "claimed")
	foreign := filepath.Join(tmp, "foreign")
	require.NoError(t, os.WriteFile(claimed, nil, 0o600))
	require.NoError(t, os.WriteFile(foreign, nil, 0o600))
	claimInfo, err := os.Lstat(claimed)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(foreign, claimInfo.ModTime(), claimInfo.ModTime()))

	// The swap: remove the claim and move the foreign object onto the name —
	// identical size (0) and mtime, distinct inode by construction.
	require.NoError(t, os.Remove(claimed))
	require.NoError(t, os.Rename(foreign, claimed))

	err = overwriteBackupReservationStillOurs(base, claimed, claimInfo)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision)
	require.Contains(t, err.Error(), "dev/inode mismatch")
}

// The claim's reservation-Stat failure wedge: the unknown-state placeholder
// is dropped and the claim fails closed (no move ever runs).
func TestInstallOverwritingW36_ClaimStatFailureDropsPlaceholder(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W36-STATFAIL"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	sentinel := errors.New("w36 reservation stat wedged")
	fs := &w36StatFailOpenFs{Fs: base, err: sentinel}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w36-statfail", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "stat backup reservation")
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Empty(t, recorder.get(), "a failed claim never arms the ledger")
	// Wave-62 (codex P2): with the placeholder's identity unprovable, the
	// name might ALREADY be foreign — it is RETAINED for manual cleanup
	// rather than unlinked by pathname.
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	kept := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), backupSuffixForDest+".") {
			kept++
		}
	}
	require.Equal(t, 1, kept, "the unproven reservation is retained for manual cleanup, never unlinked blind")
}

// w36StatFailOpenFs fails the Stat of every freshly-claimed backup
// reservation (the claim's post-create identity capture).
type w36StatFailOpenFs struct {
	afero.Fs
	err error
}

type w36StatFailFile struct {
	afero.File
	err error
}

func (f w36StatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

func (f *w36StatFailOpenFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && flag&os.O_EXCL != 0 && strings.Contains(filepath.ToSlash(name), backupSuffixForDest+".") {
		return w36StatFailFile{File: file, err: f.err}, nil
	}
	return file, err
}
