package downloader

// POSTER-WRITE-HARDENING wave-44 (codex P2, PR#215 finding F1 — the
// downloader twin of history's replacement_backup_quarantine fix): honor
// completed quarantine publishes in moveVerifiedRollbackBackupToQuarantine.
// A handoff publish error carrying fsutil.ErrPublishCompleted means the
// verified rollback backup LANDED at the quarantine name — the pre-wave-44
// leg (placeholder restore onto the consumed name + early return before the
// hold was constructed) left journal entries armed against absent/foreign
// journaled names while the OWNED bytes sat stranded under .dlq. The leg
// now treats the quarantine as INSTALLED (the wave-21 owned-name rule):
// no placeholder restore, the post-move verification + hold construction
// run unchanged, and a substituted post-reverify object keeps the existing
// conservative refusal.
//
// Test matrix mirrors history's wave-44 file leg for leg.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// w44RollbackPublishCompletedFs replays the hard-link fallback's
// completed-with-residue publish against the rollback quarantine handoff:
// the publish rename (journaled backup → the LEARNED reservation name)
// lands for real, the optional onPublish hook fires (the substitution
// replay), and the scripted ErrPublishCompleted-carrying error rides up.
type w44RollbackPublishCompletedFs struct {
	afero.Fs
	quar      string // learned reservation name (claim 1)
	claims    int
	onPublish func()
}

func (f *w44RollbackPublishCompletedFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) && !strings.Contains(name, ".vac.") {
		f.claims++
		if f.claims == 1 {
			f.quar = name
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w44RollbackPublishCompletedFs) Rename(oldname, newname string) error {
	if newname == f.quar && !strings.Contains(oldname, rollbackQuarantineSuffix) {
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		if f.onPublish != nil {
			f.onPublish()
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged source no longer names the just-linked object — staged name left untouched: %w: %w",
			oldname, newname, fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	}
	return f.Fs.Rename(oldname, newname)
}

// The headline wave-44 leg: the completed publish installs the quarantine —
// the hold is constructed, the verified unlink consumes the record, and
// nothing stays armed against the journaled name.
func TestRollbackBackupQuarantineW44_PublishCompletedInstallsQuarantine(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44dh/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	fs := &w44RollbackPublishCompletedFs{Fs: base}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w44 unit")
	require.NoError(t, err,
		"ErrPublishCompleted means the quarantine is INSTALLED: the hold is constructed instead of arming the entry against an absent/foreign name")
	require.NotNil(t, hold)
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the verified object moved off the journaled name")
	require.Equal(t, "old", string(readW31(t, base, fs.quar)),
		"the installed quarantine names the verified bytes")
	require.NoError(t, hold.removeVerified())
	require.Empty(t, w32RollbackQuarNames(t, base, "/w44dh"),
		"no .dlq. residue: the installed quarantine was consumed by the verified unlink and the taken placeholder dropped claim-bound")
	require.Contains(t, logs.String(), "treating the quarantine as INSTALLED",
		"the completed-with-residue leg logged its owned-name posture")
}

// The completed publish is not a blanket success: a substitution racing the
// post-move reverify keeps the conservative refusal — the foreign plant
// rides back onto the journaled name NO-REPLACE, byte-intact, entry live.
func TestRollbackBackupQuarantineW44_PublishCompletedSubstitutedReverifyStillRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44ds/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	plant := []byte("foreign substitution at the quarantine name")
	fs := &w44RollbackPublishCompletedFs{Fs: base}
	fs.onPublish = func() {
		require.NoError(t, base.Remove(fs.quar))
		require.NoError(t, afero.WriteFile(base, fs.quar, plant, 0o600))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w44 unit")
	require.Nil(t, hold)
	require.ErrorContains(t, err, "refused", "the substituted quarantined object keeps the proven-foreign refusal path")
	require.NotErrorIs(t, err, errRollbackQuarantineRestoreFailed,
		"the journaled name was free — the plant rode back onto it no-replace")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, backup),
		"the foreign substitution is preserved byte-intact at the journaled name — never unlinked")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w44ds"), "no .dlq. residue after the refusal rewind")
}

// The refusal classes stay disjoint from the completed leg: a plain publish
// wedge (nothing installed) keeps the pre-wave-44 failure shape — the
// placeholder restores and releases, the entry stays live, and the error
// never reports the completed class.
func TestRollbackBackupQuarantineW44_PlainPublishWedgeStillFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44df/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	wedge := errors.New("w44 plain publish wedge")
	fs := &w42RollbackHandoffFs{Fs: base, publishErr: wedge}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w44 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, wedge)
	require.False(t, fsutil.PublishCompleted(err), "nothing installed — the completed class stays disjoint")
	require.Equal(t, "old", string(readW31(t, base, backup)), "the journaled backup never moved")
	require.Equal(t, 2, fs.claims, "reservation + taken claims both ran before the publish leg")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w44df"),
		"the placeholder restored and released identity-bound — no .dlq. residue")
}
