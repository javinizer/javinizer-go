package history

// POSTER-WRITE-HARDENING codex PR#215 wave-20 (P2), refined by wave-21 —
// "persist pending state after every failed re-arm": before this wave only
// the PublishRefusal classes disarmed the entry (waves 18+19). A consumption
// failure followed by a NON-refusal re-arm failure left the entry ARMED
// against a backup name the failed re-arm never reclaimed — every later
// explicit revert failed statting the absent name forever, and sweeps saw an
// ordinary armed row with a present destination (nothing to repair). Wave-20
// disarms into the durable RestorePending marker for EVERY re-arm failure
// class, routing on backup-name ownership via rearmPendingKind:
//   - refusal classes (occupied name / no-replace-unsupported volume)
//     → rearm_refused (unchanged from wave 19);
//   - failures BEFORE any publish completed (re-arm source open, staging
//     open/write/close, failed publish, and — wave-21, codex P1 — the
//     PRE-publish metadata fix-ups on the exclusively-staged inode: mode at
//     the O_EXCL create, Chtimes/ownership on the staged name) leave the
//     name unproven/absent → rearm_refused (journal-only retries);
//   - failures AFTER the staged copy definitely PUBLISHED — exactly fsutil's
//     ErrPublishCompleted link-fallback leg now, since wave-21 moved every
//     metadata fix-up pre-publish, deleting wave-20's post-publish
//     Chmod/Chtimes-on-the-published-name legs (and their errRearmAfterPublish
//     wrapper) entirely — leave THIS operation's bytes at the name → clean
//     (the pending retry reaps the owned name).
//
// The explicit-reverter legs live here; the wave-18/19 refusal pins stay in
// reverter_rearm_collision_w18c_test.go (c1/c3) and
// reverter_pending_kind_w19_test.go. The wave-21 staged-inode ordering pins
// live in replacement_rearm_staged_w21_test.go.

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
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The classifier itself: every failure class resolves to exactly the kind
// its backup-name ownership state demands, and the only "completed" signal
// (fsutil.PublishCompleted, shared with the downloader — wave-21) stays
// disjoint from the refusal classifier.
func TestRearmPendingKindW20_ClassifierLegs(t *testing.T) {
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(errors.New("re-arm temp open wedged")),
		"a pre-publish staging outage proves nothing about the name — refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm source /d: %w", os.ErrNotExist)),
		"a re-arm SOURCE open failure (destination unreadable) publishes nothing — refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w", fsutil.ErrPublishCollision)),
		"the occupied-name refusal stays refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w", fsutil.ErrPublishNoReplaceUnsupported)),
		"the volume-cannot-publish refusal stays refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w", fsutil.ErrPublishNoReplaceLinkFailed)),
		"wave-29: a fail-closed hard-link error (EMLINK & friends) never installed anything — refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm stage backup identity /b: %w", fsutil.ErrStagedIdentityMismatch)),
		"wave-29: a swapped staged name proves nothing about the name — refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm stage backup times /b: %w", errors.New("re-arm chtimes wedged"))),
		"wave-21: a PRE-publish metadata failure (staged-name Chtimes) publishes nothing — refused")
	require.Equal(t, models.RestorePendingKindClean,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w", fsutil.ErrPublishCompleted)),
		"fsutil's completed-despite-error publish leaves the staged copy at the name — clean")

	// Disjointness with the wave-19 refusal classifier: only the refusal
	// classes classify occupied; the completed class is never a refusal, and
	// plain failures are neither.
	verify := map[error]bool{
		fmt.Errorf("install: %w", fsutil.ErrPublishCompleted):            false,
		fmt.Errorf("install: %w", fsutil.ErrPublishCollision):            true,
		fmt.Errorf("install: %w", fsutil.ErrPublishNoReplaceUnsupported): true,
		fmt.Errorf("install: %w", fsutil.ErrPublishNoReplaceLinkFailed):  false,
		fmt.Errorf("identity: %w", fsutil.ErrStagedIdentityMismatch):     false,
		errors.New("staging write wedged"):                               false,
		fmt.Errorf("no-replace publish /s -> /d: staged cleanup failed"): false,
	}
	for err, occupied := range verify {
		require.Equal(t, occupied, rearmOccupiedClass(err), "refusal classifier disjointness for %v", err)
		if occupied {
			require.Equal(t, models.RestorePendingKindRearmRefused, rearmPendingKind(err))
		}
	}
	// The shared publish-completed classifier agrees with the local kind
	// mapping on every entry above.
	for err := range verify {
		require.Equal(t, fsutil.PublishCompleted(err),
			rearmPendingKind(err) == models.RestorePendingKindClean,
			"clean kind ⇔ fsutil.PublishCompleted for %v", err)
	}
}

// w20RearmWriteFailFs wedges the re-arm's STAGING WRITE leg: the exclusive
// staging open itself succeeds, but the first Write into the staged
// `<backup>.dlrarm.<hex>` name fails — the publish is never attempted, so
// the backup name stays absent (unproven).
type w20RearmWriteFailFs struct {
	afero.Fs
	fired bool
}

type w20FailWriteFile struct {
	afero.File
	owner *w20RearmWriteFailFs
}

func (f *w20FailWriteFile) Write([]byte) (int, error) {
	f.owner.fired = true
	return 0, errors.New("re-arm staging write wedged")
}

func (f *w20RearmWriteFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	base, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, rearmStagingSuffix+".") {
		return &w20FailWriteFile{File: base, owner: f}, nil
	}
	return base, nil
}

// Explicit revert: consumption failure + staging-WRITE re-arm failure
// (publish never attempted, name absent) → entry marked
// pending(rearm_refused), destination retained, and the healed retry consumes
// journal-only. Mirrors the wave-18c staging-OPEN pin — both pre-publish
// classes share the absent-name posture but must cover their own legs.
func TestReverterRearmPendingW20_StagingWriteFailureMarksRearmRefusedAndHeals(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-WRITE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w20RearmWriteFailFs{Fs: fixture.fs}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr, "the consumption failure surfaces exactly like the neighboring legs")
	require.True(t, restored[dest])
	require.True(t, fs.fired, "the write wedge hit the re-arm's staging leg")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "no publish was attempted — the backup name is absent")
	for _, name := range w15DirListing(t, fixture.fs, filepath.Dir(dest)) {
		require.NotContains(t, name, rearmStagingSuffix+".", "the staged re-arm copy is cleaned up (saw %q)", name)
	}

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled — cleanup is deferred")
	require.True(t, gf.Replacements[0].RestorePending,
		"wave-20: a NON-refusal re-arm failure disarms the entry too")
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"a failure before any publish leaves the name unproven/absent — rearm_refused")
	require.Equal(t, 2, repo.calls, "consumption attempt + marker persistence, nothing else")
	require.Contains(t, logs.String(), "re-arm failed")
	require.Contains(t, logs.String(), "marked restore-pending")

	// Heal persistence; the rearm-refused retry runs NO backup-path operation
	// (the write wedge stays armed and is never needed).
	repo.fail = nil
	retryRow, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	fresh := *retryRow
	restored, err = NewReverter(fs, repo).restoreReplacementJournal(ctx, &fresh)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the certified destination stays byte-for-byte")
	_, serr = fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "nothing re-published at the unowned name")
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements, "journal-only consumption completes")
}

// Explicit revert: consumption failure + PUBLISH-COMPLETED re-arm failure
// (the staged copy PUBLISHED at the backup name but the publish reports
// fsutil.ErrPublishCompleted — the hard-link fallback's
// cleanup-with-failed-rollback leg; wave-21: this is the ONLY post-publish
// failure class left now that every metadata fix-up runs pre-publish on the
// staged inode) → the backup name carries THIS operation's own bytes, so the
// entry marks pending(CLEAN) — the healed retry stats/removes the owned
// name and consumes, exactly like the wave-9 clean-pending flow.
func TestReverterRearmPendingW20_PublishCompletedMarksCleanPendingAndRetryReapsOwnedBackup(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-PUBDONE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	published := false
	w21RearmPublishCompletedSeam(t, &published)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fixture.fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.True(t, published, "the publish completed BEFORE the compensating error")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, backup),
		"the publish COMPLETED — the name carries this operation's own bytes")

	row, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending, "the completed-despite-error failure disarms too")
	require.Equal(t, models.RestorePendingKindClean, gf.Replacements[0].PendingKind(),
		"the name provably holds owned bytes — the clean kind reaps it")
	require.Equal(t, "", gf.Replacements[0].RestorePendingKind,
		"the clean kind stays unwritten (legacy blob parity)")
	require.Contains(t, logs.String(), "re-arm failed")
	require.Contains(t, logs.String(), "marked restore-pending (clean)")

	// The healed retry runs the clean-kind leg: the owned backup is statted,
	// never copied (destination already certified), removed, then consumed.
	repo.fail = nil
	retryRow, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	fresh := *retryRow
	restored, err = NewReverter(fixture.fs, repo).restoreReplacementJournal(ctx, &fresh)
	require.NoError(t, err, "the clean-pending retry completes — no wedge on the stat leg")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the certified destination is never re-restored")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the clean retry reaps the owned backup name")
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements, "the deferred consumption completes")
}

// The wave-21 PRE-publish Chtimes leg (the metadata fix-up on the staged
// inode) classifies opposite to wave-20's published-name leg: the publish is
// never attempted, the name stays absent, and the entry marks
// pending(REARM-REFUSED) — journal-only retries.
func TestReverterRearmPendingW20_PrePublishChtimesFailureMarksRearmRefused(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-CTIMES", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w17aChtimesFailFs{Fs: fixture.fs}

	ctx := context.Background()
	_, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist,
		"wave-21: a pre-publish Chtimes failure never publishes — the name stays absent")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"a pre-publish metadata failure leaves the name unproven/absent — rearm_refused")
}

// Triple failure, non-refusal class: marker persistence fails ON TOP of the
// consumption failure and the pre-publish re-arm failure. Nothing could be
// persisted — the entry stays armed (the only residue the finding 2 fix
// cannot prevent) — the restored destination is retained, the log carries
// both causes, and the refusal-only "occupant untouched" phrasing is absent.
func TestReverterRearmPendingW20_PlainFailureMarkerFailureOnTopKeepsArmedResidue(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-TRIPLE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w20 consumption transaction wedged")
	markerErr := errors.New("w20 marker transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr, 2: markerErr}}
	fs := &w8RearmFailFs{Fs: fixture.fs}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr, "the returned error carries the consumption failure, like neighboring legs")
	require.NotErrorIs(t, err, markerErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "no publish happened")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1, "the entry stays journaled")
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker merge persisted nothing — entry stays armed")
	require.Equal(t, 2, repo.calls, "consumption attempt + marker attempt, nothing else")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, markerErr.Error())
	require.Contains(t, out, "restore-pending persistence failed")
	require.Contains(t, out, "entry stays armed")
	require.NotContains(t, out, "backup occupant untouched", "that phrasing is refusal-class only")
}

// Sweep mirror — retryPendingRemoval's consumption failure + pre-publish
// re-arm failure now persists the REARM-REFUSED kind (pre-wave-20 kept
// clean): this sweep's own removal leg tolerates the absent name, but an
// explicit revert reading a clean-pending entry against an absent backup
// names a source stat that would fail forever.
func TestReplacementSweepPendingW20_RetryPendingPlainRearmFailurePersistsRearmRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W20-RPR"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := w19PendingSweepRow(t, baseRepo, "W20-RPR", dest, backup, true, models.RestorePendingKindClean)

	consumeErr := errors.New("w20 pending cleanup consumption wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	fs := &w8RearmFailFs{Fs: base}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(fs, repo)
	require.False(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))

	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the restored destination is retained")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the failed re-arm published nothing — the name is absent")

	gf := w19Journal(t, baseRepo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"wave-20: a pre-publish re-arm failure upgrades the durable marker to rearm-refused")
	fallbackKind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, fallbackKind,
		"the in-process fallback records the same kind")
	require.Contains(t, logs.String(), "restore-pending")

	// Healed retry consumes journal-only (durably refused kind), dest intact.
	repo.fail = nil
	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Empty(t, w19Journal(t, baseRepo, op.ID).Replacements)
}

// Sweep mirror — restoreAndConsume's consumption failure + PUBLISH-COMPLETED
// re-arm failure (fsutil.ErrPublishCompleted — wave-21: the only surviving
// post-publish class) persists the CLEAN kind: the crash-window restore
// landed, the re-arm re-published the operation's own bytes at the backup
// name, and only the publish cleanup/rollback leg reported failure. The
// healed sweep reaps the owned name and consumes.
func TestReplacementSweepPendingW20_RestoreAndConsumePublishCompletedPersistsClean(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W20-RAC"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", 1)
	op := journalRow(t, baseRepo, "job-w20-sweep", "W20-RAC", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	published := false
	w21RearmPublishCompletedSeam(t, &published)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the failed consumption defers the heal")

	require.True(t, published, "the re-arm publish completed before the compensating error")
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the crash-window restore landed")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the re-arm PUBLISHED — the name carries owned bytes")

	gf := w19Journal(t, baseRepo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindClean, gf.Replacements[0].PendingKind(),
		"a failure after the definite publish keeps the clean kind")
	require.Equal(t, "", gf.Replacements[0].RestorePendingKind, "clean stays serialized-unwritten")
	require.Contains(t, logs.String(), "marked restore-pending")

	// Healed: a fresh sweeper (durable marker only) removes the owned backup
	// through the clean pending leg and consumes the entry.
	repo.fail = nil
	healed, err = NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the clean-kind retry reaps the owned name")
	require.Empty(t, w19Journal(t, baseRepo, op.ID).Replacements)
}
