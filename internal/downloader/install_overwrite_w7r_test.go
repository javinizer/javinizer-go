package downloader

// POSTER-WRITE-HARDENING codex PR#215 (P2): claimOverwriteBackupPath must
// atomically RESERVE an observed-free backup name with O_CREATE|O_EXCL
// before os.Rename(dest → backup), or a foreign writer can occupy the name
// in the Lstat-to-Rename window and POSIX rename silently overwrites its
// bytes before the journal ever sees them.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w7rWindowOccupierFs simulates a foreign claimant landing in the
// Lstat-to-OpenFile window: the first O_CREATE|O_EXCL attempt against a
// mapped candidate finds the name observed-free by Lstat yet already
// reserved by the time the exclusive create runs.
type w7rWindowOccupierFs struct {
	afero.Fs
	occupied map[string]string
}

func (f *w7rWindowOccupierFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_CREATE != 0 && flag&os.O_EXCL != 0 {
		clean := filepath.Clean(name)
		if foreign, ok := f.occupied[clean]; ok {
			delete(f.occupied, clean)
			if err := afero.WriteFile(f.Fs, name, []byte(foreign), 0o600); err != nil {
				return nil, err
			}
			return nil, os.ErrExist
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func w7rSetupFixture(t *testing.T, dir string) (afero.Fs, string, string, *armedTestLedger) {
	t.Helper()
	base := afero.NewMemMapFs()
	dest := filepath.Join(filepath.FromSlash(dir), "poster.jpg")
	staged := filepath.Join(filepath.FromSlash(dir), "poster.tmp")
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	return base, dest, staged, &armedTestLedger{}
}

// A candidate ALREADY occupied when Lstat runs — including a 0-byte
// reservation placeholder left by a foreign claimant — is treated as
// occupied and the claim climbs, never reclaiming or overwriting it.
func TestInstallOverwritingW7R_PreexistingReservationClimbsAndIsNotReclaimed(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dest, staged, recorder := w7rSetupFixture(t, "/out/W7R-PRE")
	first := backupCandidateW22(dest, "w7r-pre", 1)
	second := backupCandidateW22(dest, "w7r-pre", 2)
	third := backupCandidateW22(dest, "w7r-pre", 3)
	require.NoError(t, afero.WriteFile(base, first, nil, 0o600)) // foreign reservation placeholder
	require.NoError(t, afero.WriteFile(base, second, []byte("foreign-2"), 0o600))

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7r-pre", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)))
	require.Len(t, recorder.get(), 1)
	require.Equal(t, third, recorder.get()[0].backupPath, "the claim climbs past occupied names")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, third)), "the set-aside lands at the claimed name")
	reservation, readErr := afero.ReadFile(base, first)
	require.NoError(t, readErr)
	require.Empty(t, reservation, "a pre-existing 0-byte reservation is never reclaimed or filled")
	require.Equal(t, "foreign-2", string(mustReadDownloaderW7(t, base, second)), "occupied candidates are never overwritten")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// A candidate observed-free by Lstat but reserved by the time the exclusive
// create runs (the window the reservation exists to close) is detected via
// O_EXCL and the claim climbs — the foreign reservation survives untouched.
func TestInstallOverwritingW7R_WindowCollisionClimbsPastForeignReserve(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W7R-WINDOW"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	first := backupCandidateW22(dest, "w7r-window", 1)
	second := backupCandidateW22(dest, "w7r-window", 2)
	third := backupCandidateW22(dest, "w7r-window", 3)
	wrapped := &w7rWindowOccupierFs{Fs: base, occupied: map[string]string{
		filepath.Clean(first):  "foreign-1",
		filepath.Clean(second): "foreign-2",
	}}

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, wrapped, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7r-window", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)

	require.Empty(t, wrapped.occupied, "both window collisions were injected")
	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)))
	require.Len(t, recorder.get(), 1)
	require.Equal(t, third, recorder.get()[0].backupPath, "the claim climbs past window collisions")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, third)))
	require.Equal(t, "foreign-1", string(mustReadDownloaderW7(t, base, first)), "the racer's reservation is never overwritten")
	require.Equal(t, "foreign-2", string(mustReadDownloaderW7(t, base, second)))
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// A non-collision reservation failure (permissions, wedges) fails the claim
// closed: no journal write, destination intact, marker released.
func TestInstallOverwritingW7R_ReserveFailureFailsClosed(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dest, staged, recorder := w7rSetupFixture(t, "/out/W7R-RESERVE-ERR")
	sentinel := errors.New("w7r reservation create wedged")
	wrapped := &w7rReserveFailFs{Fs: base, err: sentinel}
	d := NewDownloader(nil, wrapped, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7r-reserve-err", recorder: recorder})
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "reserve backup candidate")
	require.Contains(t, err.Error(), "failed to claim backup path")
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination bytes are intact")
	require.Empty(t, recorder.get(), "nothing journals")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "marker released on the failure path")
}

// A reservation whose Close fails is dropped (never renamed over), the claim
// reports the close failure, and no placeholder residue remains to block a
// later claim.
func TestInstallOverwritingW7R_ReservationCloseFailureDropsPlaceholder(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dest, staged, recorder := w7rSetupFixture(t, "/out/W7R-CLOSE-ERR")
	sentinel := errors.New("w7r reservation close wedged")
	wrapped := &w7rCloseFailFs{Fs: base, err: sentinel}
	d := NewDownloader(nil, wrapped, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w7r-close-err", recorder: recorder})
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "close backup reservation")
	require.False(t, skipped)
	require.True(t, replaced)
	_, statErr := base.Stat(backupCandidateW22(dest, "w7r-close-err", 1))
	require.ErrorIs(t, statErr, os.ErrNotExist, "the failed reservation placeholder is dropped")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))
	require.Empty(t, recorder.get())
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

type w7rReserveFailFs struct {
	afero.Fs
	err error
}

func (f *w7rReserveFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, backupSuffixForDest+".") {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w7rCloseFailFs struct {
	afero.Fs
	err error
}

func (f *w7rCloseFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&os.O_EXCL != 0 && strings.Contains(name, backupSuffixForDest+".") {
		return covW1BCloseErrorFile{File: file, err: f.err}, nil
	}
	return file, nil
}
