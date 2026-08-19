package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — "disarm the ledger when
// the downloader re-arm is refused": after a rollback consumed the backup,
// ReleaseReplacement failure triggers a backup re-arm; when that re-arm is
// REFUSED with the occupied-name classes (fsutil.PublishRefusal — a foreign
// writer owns the name, or the volume cannot express a no-replace publish),
// the journal entry used to stay ARMED against the unowned name — a later
// revert would copy the foreign bytes over the restored destination and then
// delete the occupant. The entry is now durably marked RestorePending with
// the rearm-refused kind (finding 1's state machine consumes it WITHOUT any
// backup-path operation). A plain re-arm failure keeps the wave-18 warn-only
// armed posture (the name is simply absent).

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
	"github.com/javinizer/javinizer-go/internal/models"
)

// w19RearmStageRaceFs claims the RE-ARM publish target with foreign bytes
// when the re-arm's staged copy is created: the OpenFile of a ".dlrstr.N"
// staging name derives its publish target; when that target is a backup name
// (…​.dlbak.…) rather than the destination, the fs claims it. The no-replace
// publish then collides with the typed fsutil.ErrPublishCollision through the
// same classify->publish window the w16 tests exercise. Rollback staging
// (target == dest) passes untouched.
type w19RearmStageRaceFs struct {
	afero.Fs
	dest    string
	foreign []byte
	fired   bool
}

func (f *w19RearmStageRaceFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && !f.fired && strings.Contains(filepath.Base(name), ".dlrstr.") {
		target := strings.Split(name, ".dlrstr.")[0]
		if filepath.Clean(target) != filepath.Clean(f.dest) && strings.Contains(filepath.Base(target), backupSuffixForDest) {
			f.fired = true
			if wErr := afero.WriteFile(f.Fs, target, f.foreign, 0o600); wErr != nil {
				return nil, wErr
			}
		}
	}
	return file, err
}

// w19MarkFailLedger fails release AND the wave-19 pending mark (the marker
// persistence outage on top of the release outage).
type w19MarkFailLedger struct {
	*covW14BReleaseFailingLedger
	markErr error
	marks   int
}

func (l *w19MarkFailLedger) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	l.marks++
	return l.markErr
}

func w19SetupReplace(t *testing.T, fs afero.Fs, dir string) (dest, staged string) {
	t.Helper()
	dest = filepath.Join(dir, "poster.jpg")
	staged = filepath.Join(dir, "poster.tmp")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("original bytes"), 0o640))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new bytes"), 0o644))
	return dest, staged
}

// Replace-failure leg: release fails, the re-arm collides with a foreign
// claimant at the vacated backup name (Stat-occupied timing) — the entry
// leaves the armed state: it is marked restore-pending with the rearm-refused
// kind while the occupant and the rolled-back destination stay byte-exact.
func TestInstallOverwritingW19_ReplaceLegRearmRefusalMarksPending(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W19-RELEG"
	dest, staged := w19SetupReplace(t, base, dir)

	fs := &w16RearmRaceFs{Fs: base, dest: dest, foreign: []byte("foreign bytes"), rollback: true}
	recorder := &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w19-releg", recorder: recorder})
	require.ErrorIs(t, err, errW16StagedInstall, "the rollback error stays the surfaced failure")
	require.Equal(t, 1, recorder.releases)
	require.True(t, fs.fired, "the injected foreign claim raced the re-arm")

	records := recorder.get()
	require.Len(t, records, 1, "the entry is NOT released — it converts instead of staying armed")
	backup := records[0].backupPath

	pendings := recorder.getPendings()
	require.Len(t, pendings, 1, "the re-arm refusal marks the entry restore-pending (rearm-refused kind)")
	require.Equal(t, dest, pendings[0].replacedPath)
	require.Equal(t, backup, pendings[0].backupPath)

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)), "the rolled-back destination is untouched")
	require.Equal(t, "foreign bytes", string(mustReadDownloaderW7(t, base, backup)), "the foreign occupant is untouched")
	require.Contains(t, logs.String(), "marked restore-pending (rearm-refused)")
}

// Install-confirm leg: confirm fails AND release fails, and the re-arm's
// staged publish collides with a foreign claimant mid-window — same wave-19
// conversion, driven from the confirm-failure rollback chain.
func TestInstallOverwritingW19_ConfirmLegRearmCollisionMarksPending(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W19-CNLEG"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.staged")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("original bytes"), 0o640))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := &w19RearmStageRaceFs{Fs: base, dest: dest, foreign: []byte("window racer bytes")}
	recorder := &confirmAndReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, err: errors.New("w19 outage")}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w19-cnleg", recorder: recorder})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm")
	require.True(t, fs.fired, "the foreign claim landed inside the re-arm copy→publish window")

	records := recorder.get()
	require.Len(t, records, 1, "the entry converts instead of staying armed")
	backup := records[0].backupPath

	pendings := recorder.getPendings()
	require.Len(t, pendings, 1)
	require.Equal(t, dest, pendings[0].replacedPath)
	require.Equal(t, backup, pendings[0].backupPath)

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)), "the rollback restored the destination")
	require.Equal(t, "window racer bytes", string(mustReadDownloaderW7(t, base, backup)), "the occupant is untouched")
	require.Contains(t, logs.String(), "marked restore-pending (rearm-refused)")
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlrstr.", "the staged re-arm copy is cleaned up on collision")
	}
}

// Marker persistence failing ON TOP of the refusal: the entry stays armed
// as the last resort, every byte survives, and both causes reach the log —
// matching the wave-18 history compensation's triple-failure contract.
func TestInstallOverwritingW19_MarkFailureOnTopKeepsArmedPosture(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W19-MARKFAIL"
	dest, staged := w19SetupReplace(t, base, dir)

	fs := &w16RearmRaceFs{Fs: base, dest: dest, foreign: []byte("foreign bytes"), rollback: true}
	markErr := errors.New("w19 marker persistence wedged")
	recorder := &w19MarkFailLedger{
		covW14BReleaseFailingLedger: &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr},
		markErr:                     markErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w19-markfail", recorder: recorder})
	require.ErrorIs(t, err, errW16StagedInstall)
	require.Equal(t, 1, recorder.marks, "the mark was attempted")

	records := recorder.get()
	require.Len(t, records, 1, "the entry stays armed when the mark cannot persist")
	require.Empty(t, recorder.getPendings(), "the failed mark recorded nothing")
	backup := records[0].backupPath

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)))
	require.Equal(t, "foreign bytes", string(mustReadDownloaderW7(t, base, backup)), "the occupant survives the triple failure")
	out := logs.String()
	require.Contains(t, out, markErr.Error(), "the marker failure reaches the log")
	require.Contains(t, out, "journal entry stays armed")
	require.Contains(t, out, "restore-pending marking failed")
}

// A NON-class (plain) re-arm failure — SUPERSEDED by wave-21 (codex P2
// PR#215): it used to keep the wave-18 warn-only armed posture, but that
// left the entry ARMED against an ABSENT backup name — every later explicit
// revert wedged statting the absent source forever, and sweeps saw an
// ordinary armed row with a present destination (nothing to repair).
// Wave-21 disarms EVERY re-arm failure class; the plain pre-publish staging
// failure leaves the name absent, so it takes the rearm-refused kind exactly
// like the refusal classes. (The kind-routing pins live in
// install_overwrite_rearm_pending_w21_test.go.)
func TestInstallOverwritingW19_PlainRearmFailureMarksRearmRefusedPending(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W19-PLAIN"
	dest, staged := w19SetupReplace(t, base, dir)

	fs := &covW14BRearmFailureFS{Fs: base, dest: dest} // plain ".dlrstr." staging wedge
	recorder := &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w19-plain", recorder: recorder})
	require.ErrorIs(t, err, covW14BInstallErr)

	records := recorder.get()
	require.Len(t, records, 1, "the entry stays journaled — cleanup is deferred by the pending marker")
	pendings := recorder.getPendings()
	require.Len(t, pendings, 1, "wave-21: a plain (pre-publish) re-arm failure writes a pending marker")
	require.Equal(t, dest, pendings[0].replacedPath)
	require.Equal(t, records[0].backupPath, pendings[0].backupPath)
	require.Equal(t, models.RestorePendingKindRearmRefused, pendings[0].kind,
		"the name is absent (unproven) — journal-only retries, like the refusal classes")
	_, statErr := base.Stat(records[0].backupPath)
	require.Error(t, statErr, "the failed re-arm published nothing — the name stays absent")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)))
	require.Contains(t, logs.String(), "marked restore-pending (rearm-refused)")
}
