package history

// POSTER-WRITE-HARDENING wave-44 (codex P2, PR#215 finding F1) — honor
// completed quarantine publishes: when the handoff's no-replace publish
// (fsutil.PublishNoReplace) returns an error carrying
// fsutil.ErrPublishCompleted, the verified object LANDED at the quarantine
// name (the POSIX hard-link fallback's staged cleanup/reverify failed AFTER
// the destination stood and the rollback also failed). The pre-wave-44 leg
// treated it as a full failure: it tried to restore the placeholder onto
// the consumed reservation name (a guaranteed self-collision, joining a
// spurious restore-failure) and returned before the hold was ever
// constructed — leaving journal entries armed against absent/foreign
// journaled names while the OWNED bytes sat stranded under .dlq. The leg
// now classifies fsutil.PublishCompleted FIRST: the quarantine is
// INSTALLED, no placeholder restore runs, and the caller's post-move
// verification + hold construction proceed exactly like the clean publish —
// the post-move reverify still re-binds the name, so a substitution or
// vanish there keeps the existing conservative legs.
//
// Test matrix: the completed leg constructs the hold and the verified
// unlink consumes the record with no .dlq. residue (journal NOT left
// armed); the completed leg with a substituted post-reverify object keeps
// the refusal path (foreign bytes ride back no-replace, entry live).

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

// w44PublishCompletedFs replays the hard-link fallback's completed-with-
// residue publish: the handoff's publish rename (backup → the LEARNED
// reservation name) lands for real, the optional onPublish hook fires
// (the substitution replay), and the scripted ErrPublishCompleted-carrying
// error rides up. The reservation name is learned from the first O_EXCL
// quarantine claim (claim 2 is the take-aside taken name; the transient
// ".vac." claims are ignored) — restored rides (taken → reservation) carry
// the suffix in OLDNAME and pass through.
type w44PublishCompletedFs struct {
	afero.Fs
	quar      string // learned reservation name (claim 1)
	claims    int
	onPublish func()
}

func (f *w44PublishCompletedFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, backupQuarantineSuffix) && !strings.Contains(name, ".vac.") {
		f.claims++
		if f.claims == 1 {
			f.quar = name
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w44PublishCompletedFs) Rename(oldname, newname string) error {
	if newname == f.quar && !strings.Contains(oldname, backupQuarantineSuffix) {
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		if f.onPublish != nil {
			f.onPublish()
		}
		// The wave-33 staged-cleanup refusal shape: the destination link
		// stood, the staged source could not be re-proven, the name stays
		// owned — ErrPublishCompleted joined alongside.
		return fmt.Errorf("no-replace publish %s -> %s: staged source no longer names the just-linked object — staged name left untouched: %w: %w",
			oldname, newname, fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	}
	return f.Fs.Rename(oldname, newname)
}

// The headline wave-44 leg: the completed publish installs the quarantine —
// the caller constructs the hold, the verified unlink consumes the record,
// and nothing stays armed against the journaled name.
func TestRemoveReplacementBackupW44_PublishCompletedInstallsQuarantine(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44h/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	fs := &w44PublishCompletedFs{Fs: base}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w44 unit", nil, nil),
		"ErrPublishCompleted means the quarantine is INSTALLED: the verified gate finishes instead of arming the entry against an absent/foreign name")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the verified object moved off the journaled name")
	require.Empty(t, w26DirQuarNames(t, base, "/w44h"),
		"no .dlq. residue: the installed quarantine was consumed by the verified unlink and the taken placeholder dropped claim-bound")
	require.Contains(t, logs.String(), "treating the quarantine as INSTALLED",
		"the completed-with-residue leg logged its owned-name posture")
}

// The completed publish is not a blanket success: a substitution racing the
// post-move reverify still takes the existing conservative refusal — the
// foreign plant rides back onto the journaled name NO-REPLACE, byte-intact,
// with the entry left live.
func TestRemoveReplacementBackupW44_PublishCompletedSubstitutedReverifyStillRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44s/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	plant := []byte("foreign substitution at the quarantine name")
	fs := &w44PublishCompletedFs{Fs: base}
	fs.onPublish = func() {
		// remove+recreate: a real substitution, never a MemMap live-view
		// truncate (the w35-documented hazard).
		require.NoError(t, base.Remove(fs.quar))
		require.NoError(t, afero.WriteFile(base, fs.quar, plant, 0o600))
	}

	hold, err := quarantineReplacementBackupForRemoval(fs, backup, "w44 unit", nil, nil)
	require.Nil(t, hold)
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused,
		"the substituted quarantined object keeps the proven-foreign refusal path")
	require.NotErrorIs(t, err, errBackupQuarantineRestoreFailed,
		"the journaled name was free — the plant rode back onto it no-replace")
	require.Equal(t, plant, mustRead2(t, base, backup),
		"the foreign substitution is preserved byte-intact at the journaled name — never unlinked")
	require.Empty(t, w26DirQuarNames(t, base, "/w44s"), "no .dlq. residue after the refusal rewind")
}

// The refusal classes stay disjoint from the completed leg: a plain
// publish wedge (nothing installed) keeps the pre-wave-44 failure shape —
// the placeholder restores and releases, the entry stays live, and the
// error never reports the completed class.
func TestRemoveReplacementBackupW44_PlainPublishWedgeStillFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w44f/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	wedge := errors.New("w44 plain publish wedge")
	fs := &w42HandoffFs{Fs: base, publishErr: wedge}

	hold, err := quarantineReplacementBackupForRemoval(fs, backup, "w44 unit", nil, nil)
	require.Nil(t, hold)
	require.ErrorIs(t, err, wedge)
	require.False(t, fsutil.PublishCompleted(err), "nothing installed — the completed class stays disjoint")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "the journaled backup never moved")
	require.Equal(t, 2, fs.claims, "reservation + taken claims both ran before the publish leg")
	require.Empty(t, w26DirQuarNames(t, base, "/w44f"),
		"the placeholder restored and released identity-bound — no .dlq. residue")
}
