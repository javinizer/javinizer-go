package downloader

// POSTER-WRITE-HARDENING wave-37 (codex P2, PR#215) — the reserved-backup
// handoff must close BOTH the overwrite window and the cleanup-unlink window
// on the identity-bound rename leg (every platform without a renameat2-style
// exchange; the Linux atomic legs live in backup_handoff_linux_w37_test.go).
//
// Test matrix (identity-bound leg — MemMapFs never exchanges):
//   - foreign plant landing between the claim-loop verify and the handoff's
//     syscall-adjacent re-verify → typed collision refusal, destination and
//     plant byte-intact, nothing journaled (the finding's headline, degrade
//     leg (i));
//   - handoff rename failure with the reservation swapped to a FOREIGN
//     occupant → cleanup refuses the unlink (leg (ii)), foreign bytes
//     survive, warn logged;
//   - handoff rename failure with the reservation still ours → the claimed
//     placeholder is released;
//   - handoff rename failure with the reservation vanished on its own → the
//     cleanup completed itself, no foreign object endangered.

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

// w37PlantOnNthBackupLstatFs replays the foreign writer landing between the
// claim-loop reservation verify (2nd backup-name Lstat) and the handoff's
// syscall-adjacent re-verify (3rd backup-name Lstat): on the nth matching
// lookup the placeholder is replaced by foreign bytes, then the lookup is
// answered truthfully through the wrapped filesystem.
type w37PlantOnNthBackupLstatFs struct {
	afero.Fs
	t      *testing.T
	n      int
	plant  []byte
	calls  int
	victim string
}

func (f *w37PlantOnNthBackupLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.Contains(filepath.ToSlash(name), backupSuffixForDest+".") {
		f.calls++
		if f.calls == f.n {
			f.victim = name
			require.NoError(f.t, f.Fs.Remove(name))
			require.NoError(f.t, afero.WriteFile(f.Fs, name, f.plant, 0o600))
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Leg (i): the plant landing inside the last verify→rename interposition is
// caught by the handoff's syscall-adjacent re-derivation — a typed collision
// refusal, the destination untouched, the foreign occupant never renamed
// over and never unlinked.
func TestInstallOverwritingW37_PlantBetweenVerifiesRefusedCollision(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W37-PLANT"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w37PlantOnNthBackupLstatFs{Fs: base, t: t, n: 3, plant: []byte("foreign occupant")}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w37-plant", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision,
		"a reservation proven foreign at syscall adjacency is the typed collision class")
	require.Contains(t, err.Error(), "failed to set aside existing bytes")
	require.True(t, replaced)
	require.Equal(t, 3, fs.calls, "free-check + claim-loop verify + handoff re-verify")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Equal(t, "foreign occupant", string(mustReadDownloaderW7(t, base, fs.victim)),
		"the plant was neither renamed over nor unlinked")
	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, staged)), "staged file retained")
	require.Empty(t, recorder.get(), "a refused handoff never arms the ledger")
}

// w37RenameFailFs fails the dest→backup rename with a scripted error,
// optionally replaying a foreign reservation-swap (or reservation removal)
// at the exact rename instant — the window the wave-37 bound cleanup closes.
type w37RenameFailFs struct {
	afero.Fs
	t     *testing.T
	err   error
	at    func(newname string)
	fired bool
}

func (f *w37RenameFailFs) Rename(oldname, newname string) error {
	if strings.Contains(filepath.ToSlash(newname), backupSuffixForDest+".") &&
		!strings.Contains(filepath.ToSlash(oldname), backupSuffixForDest+".") {
		f.fired = true
		if f.at != nil {
			f.at(newname)
		}
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

func newW37ArmedPair(t *testing.T, tag string) (base afero.Fs, dir, dest, staged string) {
	t.Helper()
	base = afero.NewMemMapFs()
	dir = "/out/" + tag
	dest = filepath.Join(dir, "poster.jpg")
	staged = filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	return base, dir, dest, staged
}

// Leg (ii) foreign: the rename failed AND a foreign occupant was planted at
// the reservation name inside the verify→rename window — the failure cleanup
// must refuse the unlink and leave the occupant byte-intact.
func TestInstallOverwritingW37_RenameFailureCleanupKeepsForeign(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, _, dest, staged := newW37ArmedPair(t, "W37-KEEPFOREIGN")

	sentinel := errors.New("w37 rename wedged")
	var victim string
	fs := &w37RenameFailFs{
		Fs:  base,
		t:   t,
		err: sentinel,
		at: func(newname string) {
			victim = newname
			// Wave-38: the no-replace dest publish now targets a FREED name
			// (the placeholder is taken aside first) — the plant claims it
			// unconditionally.
			_ = base.Remove(newname)
			require.NoError(t, afero.WriteFile(base, newname, []byte("foreign occupant"), 0o600))
		},
	}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w37-keepforeign", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel, "the handoff failure surfaces with the set-aside error class")
	require.Contains(t, err.Error(), "failed to set aside existing bytes")
	require.True(t, fs.fired, "the scripted rename failure ran")
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Equal(t, "foreign occupant", string(mustReadDownloaderW7(t, base, victim)),
		"cleanup never unlinks a foreign occupant — byte-intact at the reservation name")
	require.Empty(t, recorder.get(), "a failed set-aside never arms the ledger")
}

// Leg (ii) own: the rename failed and the reservation is still provably OUR
// claimed placeholder — the cleanup unlinks it so a retry never climbs past
// (or worse, journals) a placeholder.
func TestInstallOverwritingW37_RenameFailureReleasesOwnPlaceholder(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dir, dest, staged := newW37ArmedPair(t, "W37-RELEASEOWN")

	sentinel := errors.New("w37 rename wedged")
	fs := &w37RenameFailFs{Fs: base, t: t, err: sentinel}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w37-releaseown", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), backupSuffixForDest+".",
			"the still-ours placeholder was released by the bound cleanup")
	}
	require.Empty(t, recorder.get())
}

// Leg (ii) vanished: the rename failed and the reservation vanished on its
// own — the cleanup completed itself; no foreign object was ever at risk and
// nothing is removed by name.
func TestInstallOverwritingW37_RenameFailureVanishedReservationSilent(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dir, dest, staged := newW37ArmedPair(t, "W37-VANISHED")

	sentinel := errors.New("w37 rename wedged")
	fs := &w37RenameFailFs{
		Fs:  base,
		t:   t,
		err: sentinel,
		at: func(newname string) {
			// Wave-38: the reservation was already taken aside; nothing else
			// lives at the name at this instant.
			_ = base.Remove(newname)
		},
	}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w37-vanished", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), backupSuffixForDest+".", "the vanished reservation needed no cleanup")
	}
}
