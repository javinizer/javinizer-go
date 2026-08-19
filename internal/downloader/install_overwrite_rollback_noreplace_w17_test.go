package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-17 (P2) — "refuse occupied
// destinations during rollback": the two rollback legs (revert-ledger record
// failure, staged-install replace failure) used to restore the set-aside
// backup with a bare os.Rename. A foreign file created at the destination
// inside the set-aside → rollback window was silently REPLACED, with no
// backup and no journal entry (POSIX rename overwrites in place). The
// rollback now restores through fsutil.PublishNoReplace: an occupied (or
// no-replace-inexpressible) destination REFUSES the restore — the foreign
// file keeps its bytes, the aside backup is RETAINED in place as the armed
// backup, and the collision surfaces through the existing warn legs while the
// caller's primary error still returns. On MemMapFs the no-replace primitive
// is the virtual classify-then-rename leg, and the wrapped filesystems below
// stand in for the platform primitives exactly like the wave-15/16 races.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

var (
	errW17Record = errors.New("w17 revert-ledger record wedged")
	errW17Swap   = errors.New("w17 staged swap wedged")
)

// w17PlantAtRecordLedger fails RecordReplacement AND plants a foreign file at
// the destination at that moment — the exact set-aside → record → rollback
// window of the record-failure leg: the destination was moved aside before
// the record, and the racer lands before the rollback restore publishes.
// Note it deliberately does NOT journal anything: the record FAILED, so no
// journal entry exists for the retained-as-orphan backup.
type w17PlantAtRecordLedger struct {
	*armedTestLedger
	fs      afero.Fs
	dest    string
	foreign []byte
	err     error
}

func (l *w17PlantAtRecordLedger) RecordReplacement(context.Context, string, string, string, ...models.ReplacementBackupFacts) error {
	_ = afero.WriteFile(l.fs, l.dest, l.foreign, 0o644)
	return l.err
}

func TestInstallOverwritingW17_RecordFailureRollbackCollisionKeepsForeignDest(t *testing.T) {
	logs := w16CaptureLogging(t)
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W17-RECORD-RACE"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("original bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	recorder := &w17PlantAtRecordLedger{
		armedTestLedger: &armedTestLedger{},
		fs:              base,
		dest:            dest,
		foreign:         []byte("foreign bytes"),
		err:             errW17Record,
	}
	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w17-record-race", recorder: recorder})
	require.ErrorIs(t, err, errW17Record, "the caller's primary error still surfaces")
	require.ErrorContains(t, err, "revert-ledger record failed")
	require.ErrorContains(t, err, "rollback restore refused")
	require.ErrorContains(t, err, "no journal entry")
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "foreign bytes", string(mustReadDownloaderW7(t, base, dest)),
		"the foreign destination is never REPLACED or removed by the rollback")
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	var retained []string
	for _, e := range entries {
		if strings.Contains(e.Name(), backupSuffixForDest) {
			retained = append(retained, filepath.Join(dir, e.Name()))
		}
	}
	require.Len(t, retained, 1, "the set-aside backup is RETAINED in place")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, retained[0])),
		"the retained backup keeps the original bytes recoverable")
	require.Empty(t, recorder.get(), "the failed record journals nothing — the sweep's orphan retention owns the kept backup")
	require.Contains(t, logs.String(), "rollback restore of")
	require.Contains(t, logs.String(), "refused")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the busy marker is still released on the refused rollback")
}

// Control for the record-failure leg: with NO racer the no-replace rollback
// restore consumes the backup exactly like the pre-wave-17 rename did.
func TestInstallOverwritingW17_RecordFailureNormalRollbackRestoresDest(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W17-RECORD-CLEAN"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("original bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w17-record-clean", recorder: &covW1BRecorder{recordErr: errW17Record}})
	require.ErrorIs(t, err, errW17Record)
	require.ErrorContains(t, err, "revert-ledger record failed")
	require.NotContains(t, err.Error(), "rollback restore refused")
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)),
		"a clean rollback restores the pre-existing destination bytes")
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), backupSuffixForDest,
			"the restored backup is consumed, nothing stranded")
	}
}

// w17SwapFailPlantFs forces the staged-install ReplaceFile leg (rename FROM a
// staged ".tmp" name onto the destination) and plants a foreign file at the
// destination in the same breath — the racer lands between the ReplaceFile
// failure and the rollback restore's publish.
type w17SwapFailPlantFs struct {
	afero.Fs
	dest    string
	foreign []byte
	fired   bool
}

func (f *w17SwapFailPlantFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(filepath.Base(oldname), ".tmp") {
		if err := afero.WriteFile(f.Fs, f.dest, f.foreign, 0o644); err != nil {
			return err
		}
		f.fired = true
		return errW17Swap
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwritingW17_ReplaceFailureRollbackCollisionKeepsForeignArmed(t *testing.T) {
	logs := w16CaptureLogging(t)
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W17-REPLACE-RACE"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("original bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := &w17SwapFailPlantFs{Fs: base, dest: dest, foreign: []byte("foreign bytes")}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w17-replace-race", recorder: recorder})
	require.ErrorIs(t, err, errW17Swap, "the install failure stays the surfaced error")
	require.ErrorContains(t, err, "failed to replace")
	require.ErrorContains(t, err, "journal entry stays armed against the retained backup")
	require.True(t, fs.fired, "the injected race fired")
	require.False(t, skipped)
	require.True(t, replaced)

	records := recorder.get()
	require.Len(t, records, 1, "the armed entry is kept...")
	require.Empty(t, recorder.released, "...and NOT released — the backup was never consumed")
	require.Equal(t, "foreign bytes", string(mustReadDownloaderW7(t, base, dest)),
		"the foreign destination survives byte-identical")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, records[0].backupPath)),
		"the retained backup keeps the original bytes recoverable at the JOURNALED path — the existing sweep/revert flow consumes it from exactly this state")
	require.Contains(t, logs.String(), "refused")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// w17UnsupportedRestoreFs models a volume whose no-replace publish is
// inexpressible altogether (the wave-17 fsutil.ErrPublishNoReplaceUnsupported
// class): the staged swap fails, and the rollback restore's rename is
// answered with the typed unsupported error — exactly what renameat2+link(2)
// report on a FAT/exFAT destination. The refusal must retain the armed
// backup rather than degrading into replacing semantics.
type w17UnsupportedRestoreFs struct {
	afero.Fs
	dest string
}

func (f *w17UnsupportedRestoreFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		if strings.HasSuffix(filepath.Base(oldname), ".tmp") {
			return errW17Swap
		}
		if strings.Contains(filepath.Base(oldname), backupSuffixForDest) {
			return fmt.Errorf("no-replace publish %s -> %s: %w: operation not permitted", oldname, newname, fsutil.ErrPublishNoReplaceUnsupported)
		}
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwritingW17_ReplaceFailureRollbackUnsupportedRetainsArmedBackup(t *testing.T) {
	logs := w16CaptureLogging(t)
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W17-REPLACE-UNSUPP"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("original bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := &w17UnsupportedRestoreFs{Fs: base, dest: dest}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w17-replace-unsupp", recorder: recorder})
	require.ErrorIs(t, err, errW17Swap)
	require.ErrorContains(t, err, "journal entry stays armed against the retained backup")
	require.False(t, skipped)
	require.True(t, replaced)

	records := recorder.get()
	require.Len(t, records, 1, "the entry stays armed — nothing released on the unsupported refusal")
	require.Empty(t, recorder.released)
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, records[0].backupPath)),
		"the armed backup is retained byte-identical for sweep/revert recovery")
	_, destErr := base.Stat(dest)
	require.ErrorIs(t, destErr, os.ErrNotExist,
		"the unsupported refusal never publishes onto the destination, even absent")
	require.Contains(t, logs.String(), "refused")
	require.Contains(t, logs.String(), fsutil.ErrPublishNoReplaceUnsupported.Error(),
		"the unsupported reason reaches the warn seam verbatim")
}

// The refusal classifier itself: collision AND no-replace-unsupported (each
// anywhere in the wrap chain) are refusal classes; genuine IO failures are not.
func TestRollbackRestoreRefusedW17_ClassifiesRefusalClasses(t *testing.T) {
	require.True(t, rollbackRestoreRefused(fsutil.ErrPublishCollision))
	require.True(t, rollbackRestoreRefused(fmt.Errorf("swap: %w", fsutil.ErrPublishCollision)))
	require.True(t, rollbackRestoreRefused(fsutil.ErrPublishNoReplaceUnsupported))
	require.True(t, rollbackRestoreRefused(fmt.Errorf("no-replace publish: %w: %w", fsutil.ErrPublishNoReplaceUnsupported, os.ErrPermission)))
	require.False(t, rollbackRestoreRefused(errors.New("plain io failure")))
	require.False(t, rollbackRestoreRefused(os.ErrPermission))
}
