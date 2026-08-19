package history

// POSTER-WRITE-HARDENING codex PR#215 round 18 (P2) — "refuse retries after a
// collided backup re-arm": when the explicit revert's journal consumption
// fails AND rearmReplacementBackup reports the occupied-name classes (a
// foreign writer claimed the backup name mid-window —
// fsutil.ErrPublishCollision — or the volume cannot express a no-replace
// publish at all — fsutil.ErrPublishNoReplaceUnsupported), the journal entry
// used to stay ARMED against the occupant: a retry would copy the foreign
// bytes over the restored destination and then DELETE the occupant. The entry
// is now durably marked RestorePending instead — the marker certifies the
// destination bytes are in place, so the restore leg skips the copy and every
// retry runs only cleanup + consumption. The armed-against-a-foreign-file
// state is unreachable.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// (c1) explicit-revert legs: consumption failure + re-arm collision marks the
// entry RestorePending, retains the restored destination, and leaves the
// occupant untouched — and the healed retry never restores FROM the occupied
// path.
func TestReverterRearmCollisionW18C_MarksRestorePendingAndNeverRestoresOccupant(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w18c", "W18C-REVERT", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w18c consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w15BackupRaceFs{Fs: fixture.fs, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr, "the consumption failure surfaces exactly like the neighboring legs")
	require.Contains(t, err.Error(), "journal consumption failed")
	require.True(t, restored[dest], "the restore leg landed before the consumption failure")
	require.True(t, fs.fired, "the injected foreign claim raced the re-arm publish")

	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup), "the foreign occupant is untouched")

	row, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled — cleanup is deferred")
	require.True(t, gf.Replacements[0].RestorePending,
		"the armed-against-foreign state is impossible: the entry is marked restore-pending")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, "restored destination retained")
	require.Contains(t, out, "marked restore-pending")
	absoluteBackup, aerr := filepath.Abs(backup)
	require.NoError(t, aerr)
	requireLogPathContains(t, out, absoluteBackup)

	for _, name := range w15DirListing(t, fixture.fs, filepath.Dir(dest)) {
		require.False(t, strings.Contains(name, rearmStagingSuffix+"."), "no staged re-arm residue (saw %q)", name)
	}
	_, markerErr := fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the destination busy marker is released")

	// The RETRY must never treat the occupied path as a restore source AND
	// (wave-19) must never REMOVE it either: the durable marker carries the
	// rearm-refused kind, so the retry runs NO backup-path operation — the
	// marker-certified destination is kept byte-for-byte, the foreign
	// occupant stays untouched, and the entry is consumed journal-only.
	repo.fail = nil
	retryRow, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	fresh := *retryRow
	restored, err = NewReverter(fs, repo).restoreReplacementJournal(ctx, &fresh)
	require.NoError(t, err)
	require.True(t, restored[dest], "the marker-certified destination stays in the delete-subtraction set")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest),
		"the retry never copies the foreign occupant over the marker-certified destination")
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup),
		"wave-19: the rearm-refused retry never removes the foreign occupant — the backup name is unowned")
	row, ferr = fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Empty(t, gf.Replacements, "the deferred consumption completes")
}

// (c2) — SUPERSEDED by wave-20 (codex P2, PR#215): a NON-class re-arm
// failure used to keep the armed posture — plain warn, no marker write —
// which left the entry armed against an ABSENT backup name: every later
// explicit revert failed at the backup source stat forever, and sweeps saw
// an ordinary armed row with a present destination (nothing to repair).
// Wave-20 marks the entry restore-pending(rearm-refused) for every
// pre-publish re-arm failure class; the dedicated wave-20 pins live in
// reverter_rearm_pending_w20_test.go (classifier unit, staging-open AND
// staging-write wedges, post-publish clean kind, marker-failure-on-top).
// What stays true here (and this regression now pins): the failed staging
// open published nothing — the backup name is EMPTY, never occupied — and
// the healed retry consumes journal-only.
func TestReverterRearmCollisionW18C_PlainRearmFailureMarksRearmRefusedPendingAndHeals(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w18c", "W18C-PLAIN-ERR", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w18c consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w8RearmFailFs{Fs: fixture.fs}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist,
		"the failed re-arm published nothing — the backup name is empty, never occupied")

	row, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1)
	// Wave-20: the entry is disarmed — it can no longer wedge the retry at the
	// absent backup's source stat. A pre-publish staging failure leaves the
	// name absent (unproven), so the marker carries the rearm-refused kind.
	require.True(t, gf.Replacements[0].RestorePending, "every re-arm failure class disarms the entry now")
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"a pre-publish (staging-open) failure leaves the name absent — rearm-refused")
	require.Equal(t, 2, repo.calls, "consumption attempt + marker persistence")
	require.Contains(t, logs.String(), "consumption failed")
	require.Contains(t, logs.String(), "re-arm failed")
	require.Contains(t, logs.String(), "marked restore-pending")

	// The healed retry consumes JOURNAL-ONLY: no stat, copy, or removal ever
	// runs against the absent backup name (the wedge fs stays armed).
	repo.fail = nil
	retryRow, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	fresh := *retryRow
	restored, err = NewReverter(fs, repo).restoreReplacementJournal(ctx, &fresh)
	require.NoError(t, err, "the rearm-refused pending retry needs no backup-path operation")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the marker-certified destination is kept byte-for-byte")
	_, serr = fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "nothing was re-published at the unowned name")
	row, ferr = fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Empty(t, gf.Replacements, "the deferred consumption completes")
}

// (c3) RestorePending-marking failure on top: the occupant and the restored
// destination both survive, and the returned error carries the consumption
// failure (not the marker failure) — consistent with the neighboring
// cleanup-marker legs, which also log the marker outcome without returning it.
func TestReverterRearmCollisionW18C_MarkerFailureOnTopKeepsEverything(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w18c", "W18C-TRIPLE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w18c consumption transaction wedged")
	markerErr := errors.New("w18c marker transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr, 2: markerErr}}
	fs := &w15BackupRaceFs{Fs: fixture.fs, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr, "the returned state carries the consumption failure, like the neighboring legs")
	require.NotErrorIs(t, err, markerErr, "the marker outcome is logged via the logger seam, not returned")
	require.True(t, restored[dest])
	require.True(t, fs.fired)

	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup), "the foreign occupant is untouched")

	row, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled")
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker merge persisted nothing")
	require.Equal(t, 2, repo.calls, "consumption attempt + marker attempt, nothing else")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, markerErr.Error())
	require.Contains(t, out, "restore-pending persistence failed")
	require.Contains(t, out, "backup occupant untouched")
}

// The re-arm refusal classifier itself: both typed occupied-name classes (and
// their wrapped forms) classify; anything else keeps the plain warn-only
// posture.
func TestRearmOccupiedClassW18C_TypedClasses(t *testing.T) {
	require.True(t, rearmOccupiedClass(fmt.Errorf("re-arm install backup /x: %w", fsutil.ErrPublishCollision)))
	require.True(t, rearmOccupiedClass(fmt.Errorf("no-replace publish /a -> /b: %w: %w",
		fsutil.ErrPublishNoReplaceUnsupported, errors.New("link EPERM"))))
	require.False(t, rearmOccupiedClass(errors.New("re-arm temp open wedged")))
}
