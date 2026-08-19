package history

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — sweep mirror of the
// pending-kind state machine (the explicit-revert legs live in
// reverter_pending_kind_w19_test.go):
//   - retryPendingRemoval routes on the entry's restore-pending KIND: the
//     rearm-refused kind consumes the journal entry WITHOUT any backup-path
//     operation (a removal would delete a foreign occupant; an existence
//     check would wedge on an absent name), while the legacy clean kind keeps
//     its remove-then-consume behavior;
//   - a refused re-arm upgrade marks the durable marker AND the in-process
//     fallback rearm-refused (one-way, never a downgrade);
//   - the dest-absent restore leg never auto-restores FROM an unowned name:
//     a rearm-refused pending entry with a missing destination is retained
//     untouched for manual recovery.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w19PendingSweepRow journals one replacement entry carrying the given
// restore-pending kind ("" = clear, no marker) for sweep tests.
func w19PendingSweepRow(t *testing.T, repo *p3OpRepo, movieID, dest, backup string, pending bool, pendingKind string) *models.BatchFileOperation {
	t.Helper()
	entry := models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: 1}
	if pending {
		require.True(t, entry.SetRestorePending(pendingKind))
	}
	op := &models.BatchFileOperation{
		BatchJobID: "job-w19k-sweep", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{entry},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

// Full drive: crash-window restore + consumption failure + collided re-arm
// persists the durable marker WITH the rearm-refused kind — the payload the
// retry routes on.
func TestReplacementSweepPendingKindW19_ConsumeFailRearmCollisionPersistsKind(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-DRIVE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	op := journalRow(t, baseRepo, "job-w19k-sweep", "W19K-DRIVE", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w19k consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.True(t, fs.fired)

	gf := w19Journal(t, baseRepo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].RestorePendingKind,
		"the refused re-arm persists the rearm-refused kind, not the bare wave-18 bit")
	row, ferr := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	require.Contains(t, row.GeneratedFiles, `"restore_pending":true,"restore_pending_kind":"rearm_refused"`)

	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)))

	// Retry on a healed repo: fresh sweeper, durable kind only — journal-only
	// consumption, occupant untouched, destination byte-exact.
	repo.fail = nil
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the retry never removes the occupant")
	require.Empty(t, w19Journal(t, baseRepo, op.ID).Replacements)
}

// retryPendingRemoval on a durable rearm-refused entry: the foreign occupant
// is never statted-as-source nor removed; the entry consumes journal-only.
func TestReplacementSweepPendingKindW19_RetryPendingRearmRefusedOccupantConsumesWithoutPathOps(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-OCC"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	op := w19PendingSweepRow(t, repo, "W19K-RPR-OCC", dest, backup, true, models.RestorePendingKindRearmRefused)

	sweeper := NewReplacementSweeper(base, repo)
	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the occupant is never removed")
	require.Empty(t, w19Journal(t, repo, op.ID).Replacements, "the entry consumed")
}

// Absent backup name + rearm-refused pending (no-replace-unsupported volume):
// the consumption completes — pre-wave-19's remove gate tolerated the missing
// name but the sweep-retry contract is now explicitly path-free.
func TestReplacementSweepPendingKindW19_RetryPendingAbsentBackupConsumes(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-ABS"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	op := w19PendingSweepRow(t, repo, "W19K-RPR-ABS", dest, backup, true, models.RestorePendingKindRearmRefused)

	_, statErr := base.Stat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the backup name is absent")

	sweeper := NewReplacementSweeper(base, repo)
	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Empty(t, w19Journal(t, repo, op.ID).Replacements)
}

// Legacy clean-pending mirror: the operation's OWN backup is still removed
// before the entry consumes — the wave-18 behavior is unchanged for the
// clean kind.
func TestReplacementSweepPendingKindW19_RetryPendingLegacyCleanRemovesOwnedBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-LEGACY"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := w19PendingSweepRow(t, repo, "W19K-RPR-LEGACY", dest, backup, true, models.RestorePendingKindClean)

	sweeper := NewReplacementSweeper(base, repo)
	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, err := base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist, "the clean-kind retry removes the owned backup")
	require.Empty(t, w19Journal(t, repo, op.ID).Replacements)
}

// Fallback-kind routing: no durable marker at all, but THIS process watched
// the re-arm refusal (in-process rearm-refused fallback) — the authorized
// retry still stays off the unowned name.
func TestReplacementSweepPendingKindW19_RetryPendingFallbackRearmRefusedStaysOffOccupant(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-FBK"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	// Armed (no durable marker): authorization comes from the fallback below.
	op := w19PendingSweepRow(t, repo, "W19K-RPR-FBK", dest, backup, false, "")

	sweeper := NewReplacementSweeper(base, repo)
	sweeper.rememberPendingRemovalKind(sweepSlash(backup), models.RestorePendingKindRearmRefused)

	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the in-process rearm-refused fallback also forbids the removal path")
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Empty(t, w19Journal(t, repo, op.ID).Replacements)
}

// Kind dominance: a durable LEGACY-clean marker plus an in-process
// rearm-refused memory still routes rearm-refused — the fresher refusal
// evidence wins over the committed (stale) clean posture.
func TestReplacementSweepPendingKindW19_FallbackRearmRefusedDominatesDurableClean(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-DOM"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	op := w19PendingSweepRow(t, repo, "W19K-RPR-DOM", dest, backup, true, models.RestorePendingKindClean)

	sweeper := NewReplacementSweeper(base, repo)
	sweeper.rememberPendingRemovalKind(sweepSlash(backup), models.RestorePendingKindRearmRefused)

	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the in-process refusal memory dominates the stale clean marker")
	require.Empty(t, w19Journal(t, repo, op.ID).Replacements)
}

// Consumption failure inside the rearm-refused leg: no re-arm is attempted
// (nothing was removed — no staging residue), the durable and in-process
// rearm-refused markers are re-persisted, and the tenant bytes survive.
func TestReplacementSweepPendingKindW19_RetryPendingRearmRefusedConsumptionFailureKeepsKinds(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-CFAIL"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	op := w19PendingSweepRow(t, baseRepo, "W19K-RPR-CFAIL", dest, backup, true, models.RestorePendingKindRearmRefused)

	consumeErr := errors.New("w19k pending consumption wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(base, repo)
	require.False(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, 3, repo.calls, "auth read + failed consumption + marker re-persist, nothing else")

	gf := w19Journal(t, baseRepo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"the durable rearm-refused marker survives")
	fallbackKind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, fallbackKind,
		"the in-process fallback remembers the rearm-refused kind too")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, "rearm-refused pending consumption failed")
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the occupant is never touched")
	entries, rdErr := afero.ReadDir(base, dir)
	require.NoError(t, rdErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), rearmStagingSuffix+".", "no staged re-arm copy: nothing was removed, so no compensation ran")
	}
}

// The marker re-persist failing ON TOP of the rearm-refused consumption
// failure: the entry stays journaled with its durable kind, the in-process
// fallback remembers rearm-refused, both causes reach the log, and no byte
// moves anywhere.
func TestReplacementSweepPendingKindW19_RetryPendingRearmRefusedConsumptionFailureMarkerFailureKeepsAll(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-RPR-TRIPLE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	op := w19PendingSweepRow(t, baseRepo, "W19K-RPR-TRIPLE", dest, backup, true, models.RestorePendingKindRearmRefused)

	consumeErr := errors.New("w19k pending consumption wedged")
	// call 1: auth scan (succeeds); call 2: consumption consumes (fails);
	// call 3: the marker re-persist reads a BROKEN journal (fails the merge).
	repo := &w18MalformedCallRepo{p3OpRepo: baseRepo, brokeAt: 3, fail: map[int]error{2: consumeErr}}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(base, repo)
	require.False(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, 3, repo.calls, "auth read + failed consumption + failed marker re-persist")

	gf := w19Journal(t, baseRepo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"the durable marker survives the failed re-persist")
	fallbackKind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, fallbackKind)

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, "restore-pending persistence failed")
	require.Contains(t, out, "unowned backup name untouched")
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the occupant survives the triple failure")
}

// The dest-absent restore leg never auto-restores FROM an unowned name: a
// rearm-refused pending entry whose certified destination is gone keeps the
// journal record, the occupant, and the (absent) destination untouched.
func TestReplacementSweepPendingKindW19_DestAbsentRearmRefusedRetainedUntouched(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-NODEST"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "foreign-bytes", time.Hour)
	op := w19PendingSweepRow(t, repo, "W19K-NODEST", dest, backup, true, models.RestorePendingKindRearmRefused)

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "nothing heals: the only candidate is an unowned name")

	require.Contains(t, logs.String(), "rearm-refused restore-pending")
	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "no restore happens FROM the occupant")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the occupant is untouched")
	gf := w19Journal(t, repo, op.ID)
	require.Len(t, gf.Replacements, 1, "the journal record is retained for manual recovery")
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind())
}

// A consumed entry whose in-process fallback still remembers a rearm-refused
// kind: the !targetFound leg must not delete the possibly-foreign occupant —
// the consumption proved the journal only, never the path's ownership.
func TestReplacementSweepPendingKindW19_ConsumedEntryKeepsRearmRefusedOccupant(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W19K-GONE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o644))
	// The entry is already CONSUMED (consumed-meanwhile state).
	op := &models.BatchFileOperation{
		BatchJobID: "job-w19k-sweep", MovieID: "W19K-GONE", OriginalPath: "/src/w19k-gone.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{dir}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	sweeper := NewReplacementSweeper(base, repo)
	sweeper.rememberPendingRemovalKind(sweepSlash(backup), models.RestorePendingKindRearmRefused)

	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the consumed-journal racing-sweeper never deletes a name the fallback calls unowned")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)), "the moot fallback is forgotten")
}

// journalEntryPendingKind itself: malformed journals read as no-kind, a
// missing entry reads as no-kind, and the normalized kind round-trips.
func TestReplacementSweepPendingKindW19_JournalEntryPendingKindLegs(t *testing.T) {
	backup := "/out/W19K-HELP/p.jpg.dlbak." + p3HexA
	row := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Backup: backup, RestorePending: true, RestorePendingKind: models.RestorePendingKindRearmRefused}},
	})}
	require.Equal(t, models.RestorePendingKindRearmRefused, journalEntryPendingKind(row, sweepSlash(backup)))
	require.Equal(t, "", journalEntryPendingKind(row, sweepSlash("/out/W19K-HELP/other.dlbak."+p3HexB)))
	require.Equal(t, "", journalEntryPendingKind(&models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}, sweepSlash(backup)))

	legacy := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Backup: backup, RestorePending: true}},
	})}
	require.Equal(t, models.RestorePendingKindClean, journalEntryPendingKind(legacy, sweepSlash(backup)), "legacy pending defaults to clean")
	require.Equal(t, "", journalEntryPendingKind(&models.BatchFileOperation{GeneratedFiles: ""}, sweepSlash(backup)))
}

// Kind memory discipline: a remembered rearm-refused kind is never
// downgraded by a later clean-kind memory for the same key.
func TestReplacementSweepPendingKindW19_RememberedKindNeverDowngrades(t *testing.T) {
	sweeper := &ReplacementSweeper{}
	sweeper.rememberPendingRemovalKind("/w19k-mem", models.RestorePendingKindRearmRefused)
	sweeper.rememberPendingRemoval("/w19k-mem")
	kind, ok := sweeper.pendingRemovalKind("/w19k-mem")
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind, "clean memory must not downgrade the refusal memory")

	sweeper.rememberPendingRemoval("/w19k-mem-clean")
	kind, ok = sweeper.pendingRemovalKind("/w19k-mem-clean")
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindClean, kind)
	require.True(t, sweeper.hasPendingRemoval("/w19k-mem-clean"))
	sweeper.forgetPendingRemoval("/w19k-mem-clean")
	require.False(t, sweeper.hasPendingRemoval("/w19k-mem-clean"))
}
