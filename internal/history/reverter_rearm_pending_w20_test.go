package history

// POSTER-WRITE-HARDENING codex PR#215 wave-20 (P2) — "persist pending state
// after every failed re-arm": before this wave only the PublishRefusal
// classes disarmed the entry (waves 18+19). A consumption failure followed by
// a NON-refusal re-arm failure (staging open/write outage, post-publish
// metadata failure) left the entry ARMED against a backup name the failed
// re-arm never reclaimed — every later explicit revert failed statting the
// absent name forever, and sweeps saw an ordinary armed row with a present
// destination (nothing to repair). Wave-20 disarms into the durable
// RestorePending marker for EVERY re-arm failure class, routing on
// backup-name ownership via rearmPendingKind:
//   - refusal classes (occupied name / no-replace-unsupported volume)
//     → rearm_refused (unchanged from wave 19);
//   - failures BEFORE any publish completed (re-arm source open, staging
//     open/write/close, failed publish) leave the name unproven/absent
//     → rearm_refused (journal-only retries);
//   - failures AFTER the staged copy definitely published (post-publish
//     Chmod/Chtimes fix-ups, wrapping errRearmAfterPublish; fsutil's
//     ErrPublishCompleted link-fallback leg) leave THIS operation's bytes at
//     the name → clean (the pending retry reaps the owned name).
//
// The explicit-reverter legs live here; the wave-18/19 refusal pins stay in
// reverter_rearm_collision_w18c_test.go (c1/c3) and
// reverter_pending_kind_w19_test.go.

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

// The classifier itself: every failure class resolves to exactly the kind its
// backup-name ownership state demands, and the two "completed" signals stay
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
	require.Equal(t, models.RestorePendingKindClean,
		rearmPendingKind(fmt.Errorf("%w: re-arm chmod wedged", errRearmAfterPublish)),
		"a post-publish metadata failure leaves THIS operation's bytes at the name — clean")
	require.Equal(t, models.RestorePendingKindClean,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w", fsutil.ErrPublishCompleted)),
		"fsutil's completed-despite-error publish leaves the staged copy at the name — clean")

	// Disjointness with the wave-19 refusal classifier: the two
	// ownership-positive signals classify clean, everything else refused.
	verify := map[error]bool{
		fmt.Errorf("%w: meta wedged", errRearmAfterPublish):              false,
		fmt.Errorf("install: %w", fsutil.ErrPublishCompleted):            false,
		fmt.Errorf("install: %w", fsutil.ErrPublishCollision):            true,
		fmt.Errorf("install: %w", fsutil.ErrPublishNoReplaceUnsupported): true,
		errors.New("staging write wedged"):                               false,
		fmt.Errorf("no-replace publish /s -> /d: staged cleanup failed"): false,
	}
	for err, occupied := range verify {
		require.Equal(t, occupied, rearmOccupiedClass(err), "refusal classifier disjointness for %v", err)
		if occupied {
			require.Equal(t, models.RestorePendingKindRearmRefused, rearmPendingKind(err))
		}
	}
}

// w20RearmWriteFailFs wedges the re-arm's STAGING WRITE leg: the temp open
// itself succeeds, but the first Write into a ".tmp-" staging name fails —
// the publish is never attempted, so the backup name stays absent (unproven).
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
	if strings.Contains(filepath.Base(name), ".tmp-") {
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
		require.NotContains(t, name, ".tmp-", "the staged re-arm copy is cleaned up (saw %q)", name)
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

// Explicit revert: consumption failure + POST-PUBLISH re-arm failure (the
// staged copy was published but the Chmod metadata fix-up wedged) → the
// backup name carries THIS operation's own bytes, so the entry marks
// pending(CLEAN) — the healed retry stats/removes the owned name and
// consumes, exactly like the wave-9 clean-pending flow.
func TestReverterRearmPendingW20_PostPublishFailureMarksCleanPendingAndRetryReapsOwnedBackup(t *testing.T) {
	fixture := newP3Fixture()
	normalizing := &pathNormalizingChmodFs{Fs: fixture.fs}
	fixture.fs = normalizing
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-POSTPUB", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"
	require.NoError(t, normalizing.Chmod(backup, 0o600), "restrictive original bits exercise the fix-up leg")

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w17aChmodFailFs{Fs: normalizing, failPath: backup}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	ctx := context.Background()
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restored destination is retained")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, backup),
		"the publish COMPLETED before the metadata wedge — the name carries this operation's own bytes")

	row, ferr := fixture.repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending, "the post-publish failure disarms too")
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
	restored, err = NewReverter(fs, repo).restoreReplacementJournal(ctx, &fresh)
	require.NoError(t, err, "the clean-pending retry completes — no wedge on the stat leg")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the certified destination is never re-restored")
	_, serr := fixture.fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the clean retry reaps the owned backup name")
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements, "the deferred consumption completes")
}

// The CHTIMES post-publish leg classifies the same way as the Chmod leg.
func TestReverterRearmPendingW20_PostPublishChtimesFailureAlsoClean(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w20", "W20-CTIMES", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w20 consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w17aChtimesFailFs{Fs: fixture.fs, failPath: backup}

	ctx := context.Background()
	_, err := NewReverter(fs, repo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, consumeErr)
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, backup), "the publish completed before Chtimes")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindClean, gf.Replacements[0].PendingKind(),
		"the Chtimes fix-up failure is post-publish too — clean kind")
	require.Equal(t, "", gf.Replacements[0].RestorePendingKind)
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

// Sweep mirror — restoreAndConsume's consumption failure + POST-PUBLISH
// re-arm failure (Chmod wedged) persists the CLEAN kind: the crash-window
// restore landed, the re-arm re-published the operation's own bytes at the
// backup name, and only the metadata fix-up failed. The healed sweep reaps
// the owned name and consumes.
func TestReplacementSweepPendingW20_RestoreAndConsumePostPublishRearmFailurePersistsClean(t *testing.T) {
	base := afero.NewMemMapFs()
	normalizing := &pathNormalizingChmodFs{Fs: base}
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
	fs := &w17aChmodFailFs{Fs: normalizing, failPath: backup}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the failed consumption defers the heal")

	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the crash-window restore landed")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the re-arm PUBLISHED before the Chmod wedge — the name carries owned bytes")

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
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the clean-kind retry reaps the owned name")
	require.Empty(t, w19Journal(t, baseRepo, op.ID).Replacements)
}
