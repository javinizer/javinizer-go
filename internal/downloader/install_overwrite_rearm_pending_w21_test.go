package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-21 (P2) — "disarm entries after
// non-refusal re-arm failures": waves 19/20 disarmed only the
// fsutil.PublishRefusal-class rollback re-arm failures; a NON-refusal
// PRE-publish failure (re-arm staging open/write) left the journal ARMED
// against an ABSENT backupPath — every explicit revert thereafter wedged at
// the backup source stat forever, and sweeps saw an ordinary armed row with
// a present destination (nothing to repair). ReleaseReplacement's
// compensation now mirrors history's wave-20 trichotomy through the SHARED
// fsutil classifiers (rollbackRearmPendingKind — never duplicated):
//
//   - fsutil.PublishRefusal (occupied name / no-replace-unsupported volume)
//     → rearm_refused pending (unchanged from wave 19 — the w19 pins live in
//     install_overwrite_pending_w19_test.go);
//   - fsutil.PublishCompleted / post-publish demonstrable (the publish
//     installed this operation's own bytes at the backup name, then its
//     cleanup+rollback reported failure) → clean pending: the retry reaps
//     the owned name and consumes;
//   - anything else pre-publish (staging open/write/close, failed publish)
//     leaves the name absent → rearm_refused pending.
//
// The finding's coverage: a wedged re-arm staging open marks the entry
// rearm_refused pending with the destination intact and foreign bytes
// untouched; a publish-completed failure marks clean pending.

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

// The shared-classifier trichotomy itself: the completed signal alone maps
// to the clean owned-name kind; refusal and every plain pre-publish failure
// map to rearm-refused.
func TestRollbackRearmPendingKindW21_Trichotomy(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("re-arm backup /b refused: name occupied: %w", fsutil.ErrPublishCollision),
		fmt.Errorf("no-replace publish /a -> /b: %w: %w", fsutil.ErrPublishNoReplaceUnsupported, errors.New("link EPERM")),
		errors.New("re-arm staging open wedged"),
		fmt.Errorf("stat backup for re-arm: %w", os.ErrNotExist),
		errors.New("w21 re-arm staging write wedged"),
		fmt.Errorf("no-replace publish /a -> /b: %w: %w", fsutil.ErrPublishNoReplaceLinkFailed, errors.New("link EMLINK")),
		fmt.Errorf("stage rollback identity: %w", fsutil.ErrStagedIdentityMismatch),
	} {
		require.Equal(t, models.RestorePendingKindRearmRefused, rollbackRearmPendingKind(err),
			"refusal, wave-29 fail-closed link failures, identity mismatches, and pre-publish classes take the unowned-name kind: %v", err)
	}
	require.Equal(t, models.RestorePendingKindClean,
		rollbackRearmPendingKind(fmt.Errorf("swap rollback: no-replace publish /s -> /b: staged cleanup failed: %w", fsutil.ErrPublishCompleted)),
		"the shared fsutil.PublishCompleted class owns the backup name")
	// Agreement with history's classifier contract: the downloader's kind
	// equals fsutil.PublishCompleted-driven routing on every leg.
	for _, err := range []error{
		fsutil.ErrPublishCompleted, fsutil.ErrPublishCollision, fsutil.ErrPublishNoReplaceUnsupported,
		fsutil.ErrPublishNoReplaceLinkFailed, fsutil.ErrStagedIdentityMismatch, errors.New("plain"),
	} {
		require.Equal(t, fsutil.PublishCompleted(err),
			rollbackRearmPendingKind(err) == models.RestorePendingKindClean,
			"clean ⇔ PublishCompleted for %v", err)
	}
}

// w21RearmPublishCompletedFs fails the staged-install rename (driving the
// rollback chain, the covW14B posture) AND makes the re-arm's no-replace
// publish SUCCEED (the staged copy is renamed onto the backup name through
// the real filesystem) while reporting fsutil.ErrPublishCompleted — the
// POSIX hard-link fallback's "cleanup failed, rollback also failed" leg
// replayed at the fs seam on a virtual filesystem. The re-arm publish is
// keyed by its staged source (".dlrstr.") renaming INTO a ".dlbak." backup
// name; the set-aside handoff (dest → backup placeholder) renames from the
// destination itself and never trips the wedge.
type w21RearmPublishCompletedFs struct {
	afero.Fs
	dest  string
	fired bool
}

func (f *w21RearmPublishCompletedFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(oldname, ".tmp") {
		return covW14BInstallErr
	}
	if !f.fired && strings.Contains(filepath.Base(oldname), ".dlrstr.") && strings.Contains(filepath.Base(newname), backupSuffixForDest) {
		f.fired = true
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w", oldname, newname, fsutil.ErrPublishCompleted)
	}
	return f.Fs.Rename(oldname, newname)
}

// REPLACE leg: the publish-completed re-arm failure marks the entry
// CLEAN-pending (the backup name carries this operation's own bytes), the
// rolled-back destination is intact, and the owned backup survives for the
// pending retry to reap.
func TestInstallOverwritingW21_ReplaceLegPublishCompletedMarksCleanPending(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W21-PUBDONE"
	dest, staged := w19SetupReplace(t, base, dir)

	recorder := &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr}
	fs := &w21RearmPublishCompletedFs{Fs: base, dest: dest}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w21-pubdone", recorder: recorder})
	require.ErrorIs(t, err, covW14BInstallErr, "the rollback error stays the surfaced failure")
	require.True(t, fs.fired, "the publish-completed wedge fired on the re-arm's publish")

	records := recorder.get()
	require.Len(t, records, 1, "the entry stays journaled — it converts instead of staying armed")
	backup := records[0].backupPath

	pendings := recorder.getPendings()
	require.Len(t, pendings, 1, "the publish-completed re-arm failure disarms the entry")
	require.Equal(t, dest, pendings[0].replacedPath)
	require.Equal(t, backup, pendings[0].backupPath)
	require.Equal(t, models.RestorePendingKindClean, pendings[0].kind,
		"fsutil.PublishCompleted proves the name carries this operation's own bytes")

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)), "the rolled-back destination is intact")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, backup)),
		"the publish DID complete — the owned backup sits at the name for the clean retry to reap")
	require.Contains(t, logs.String(), "marked restore-pending (clean)")
}

// w21SecondStagingFailFs wedges ONLY the re-arm's staging open: the FIRST
// exclusive ".dlrstr." staging open (the install-confirm rollback restore's
// staged copy) passes; the second (the re-arm's) fails — a NON-refusal,
// pre-publish failure leaving the backup name absent.
type w21SecondStagingFailFs struct {
	afero.Fs
	opens int
}

func (f *w21SecondStagingFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(filepath.Base(name), ".dlrstr.") && flag&os.O_CREATE != 0 {
		f.opens++
		if f.opens > 1 {
			return nil, errors.New("w21 re-arm staging open wedged")
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// CONFIRM leg mirror: confirm fails, the rollback restore succeeds, the
// release retract fails, and the re-arm's staging OPEN wedges — the entry
// converts to rearm_refused pending, the destination stays the restored
// original bytes, and nothing touches any foreign path (there is none).
func TestInstallOverwritingW21_ConfirmLegStagingOpenFailureMarksRearmRefusedPending(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W21-CNPLAIN"
	dest, staged := w19SetupReplace(t, base, dir)

	fs := &w21SecondStagingFailFs{Fs: base}
	recorder := &confirmAndReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, err: errors.New("w21 confirm outage")}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w21-cnplain", recorder: recorder})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm")
	require.Equal(t, 2, fs.opens, "rollback restore staged once, re-arm staging wedged second")

	records := recorder.get()
	require.Len(t, records, 1, "the entry converts instead of staying armed against an absent name")
	backup := records[0].backupPath

	pendings := recorder.getPendings()
	require.Len(t, pendings, 1)
	require.Equal(t, dest, pendings[0].replacedPath)
	require.Equal(t, backup, pendings[0].backupPath)
	require.Equal(t, models.RestorePendingKindRearmRefused, pendings[0].kind,
		"a pre-publish staging failure leaves the name absent — journal-only retries")

	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)), "the rollback restored the destination")
	_, statErr := base.Stat(backup)
	require.Error(t, statErr, "the wedged re-arm published nothing")
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlrstr.", "no staged residue")
	}
	require.Contains(t, logs.String(), "marked restore-pending (rearm-refused)")
}

// Marker persistence failing ON TOP of a plain (non-refusal) re-arm failure:
// the entry stays armed as the last resort — every byte survives, both
// causes reach the log (the wave-19 triple-failure contract, generalized).
func TestInstallOverwritingW21_PlainFailureMarkFailureOnTopKeepsArmedResidue(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W21-PLAIN-TRIPLE"
	dest, staged := w19SetupReplace(t, base, dir)

	fs := &covW14BRearmFailureFS{Fs: base, dest: dest} // plain ".dlrstr." staging-open wedge
	markErr := errors.New("w21 marker persistence wedged")
	recorder := &w19MarkFailLedger{
		covW14BReleaseFailingLedger: &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr},
		markErr:                     markErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w21-plain-triple", recorder: recorder})
	require.ErrorIs(t, err, covW14BInstallErr)
	require.Equal(t, 1, recorder.marks, "the mark was attempted with the classified kind")

	records := recorder.get()
	require.Len(t, records, 1, "the entry stays armed when the mark cannot persist")
	require.Empty(t, recorder.getPendings(), "the failed mark recorded nothing")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)))

	out := logs.String()
	require.Contains(t, out, markErr.Error(), "the marker failure reaches the log")
	require.Contains(t, out, "journal entry stays armed")
	require.Contains(t, out, "restore-pending marking failed")
	require.Contains(t, out, "(pending kind rearm-refused)",
		"the intended kind is logged on the last-resort armed posture")
}
