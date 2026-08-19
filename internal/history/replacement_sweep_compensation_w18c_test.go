package history

// POSTER-WRITE-HARDENING codex PR#215 round 18 (P2) — "keep the restored
// destination when the re-arm fails": when the sweep's journal-consumption
// update fails after the crash-window restore removed the backup, and the
// compensating re-arm ALSO fails (a foreign writer occupying the backup
// name — typed fsutil.ErrPublishCollision / ErrPublishNoReplaceUnsupported —
// or any other cause), the compensation used to REMOVE the restored
// destination anyway: the only remaining copy of the pre-crash bytes was lost
// and the journal was left armed against the foreign occupant (a later
// restore would install the foreign bytes over the destination and delete
// the occupant). The restore undo now runs ONLY after a succeeded re-arm; a
// failed re-arm retains the destination, persists the durable RestorePending
// marker (a retry runs cleanup + consumption, never a restore from the
// occupied path), warns with both causes, and leaves the occupant untouched.
// retryPendingRemoval's consumption-failure leg follows the same contract.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w18TxFailRepo fails the chosen 1-based journal transaction calls, mimicking
// the consumption-commit failure (and, for the deeper legs, the restore-
// pending marker persistence failure) that drives the re-arm compensation.
type w18TxFailRepo struct {
	*p3OpRepo
	calls int
	fail  map[int]error
}

func (m *w18TxFailRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	m.calls++
	if err, ok := m.fail[m.calls]; ok {
		return err
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// w18MalformedCallRepo fails chosen journal transaction calls outright and
// serves a corrupted ledger at another, composing the consumption failure
// with the marker merge's unparseable-journal leg.
type w18MalformedCallRepo struct {
	*p3OpRepo
	calls   int
	brokeAt int
	fail    map[int]error
}

func (m *w18MalformedCallRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	m.calls++
	if err, ok := m.fail[m.calls]; ok {
		return err
	}
	if m.calls == m.brokeAt {
		_, _, err := fn(&models.BatchFileOperation{ID: id, GeneratedFiles: "{\"replacements\":broken"})
		return err
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// (a) consumption failure + re-arm collision: the restored destination — the
// only remaining copy of the pre-crash bytes — is RETAINED, the durable
// RestorePending marker is persisted, the foreign occupant at the backup name
// is untouched, and the consume-side cleanup is deferred to a retry that the
// durable marker alone can drive.
func TestReplacementSweepCompensationW18C_ConsumeFailRearmCollisionRetainsDest(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18C-COLLIDE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	op := journalRow(t, baseRepo, "job-w18c", "W18C-COLLIDE", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w18c consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a failed consumption + collided re-arm heals nothing")
	require.True(t, fs.fired, "the injected foreign claim raced the re-arm publish")

	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"the restored destination is retained — it is the only remaining copy")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the foreign occupant at the backup name is untouched")
	w16NoStagedRestoreLeftovers(t, base, dir)
	for _, name := range w15DirListing(t, base, dir) {
		require.NotContains(t, name, rearmStagingSuffix+".", "the staged re-arm copy is cleaned up (saw %q)", name)
	}

	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the journal entry is NOT consumed — cleanup is deferred")
	require.True(t, gf.Replacements[0].RestorePending, "the durable recovery marker is persisted")

	out := logs.String()
	require.Contains(t, out, "consumption failed")
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, "restored destination retained")
	require.Contains(t, out, "marked restore-pending")

	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the destination busy marker is released")

	// The deferred recovery needs NO same-process state: a fresh sweeper,
	// driven only by the durable RestorePending marker, finishes the
	// consumption — and (wave-19) never restores FROM nor removes the
	// occupied path: the rearm-refused kind routes the retry to a journal-only
	// consumption that leaves the foreign occupant untouched.
	repo.fail = nil
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"the retained destination survives the retry byte-for-byte")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"wave-19: the rearm-refused retry never removes the foreign occupant — the backup name is unowned")
	row, err = baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "the deferred consumption completes on the retry")
}

// (b) control: consumption failure with a SUCCEEDED re-arm undoes the
// restored destination exactly as before — the armed, backup-present retry
// posture was re-established first.
func TestReplacementSweepCompensationW18C_ConsumeFailRearmSuccessUndoesRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18C-CONTROL"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	op := journalRow(t, baseRepo, "job-w18c", "W18C-CONTROL", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w18c consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	healed, err := NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist, "re-arm succeeded — the restore is undone exactly as before")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "the re-armed backup is intact for the retry")

	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the entry stays armed")
	require.False(t, gf.Replacements[0].RestorePending, "no recovery marker is needed once the re-arm succeeded")
	require.Contains(t, logs.String(), "restore undone, will retry")

	repo.fail = nil
	healed, err = NewReplacementSweeper(base, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// (d) RestorePending-marking failure ON TOP of consumption failure + re-arm
// collision: nothing is destroyed — the restored destination and the foreign
// occupant both survive, and this process's pending-removal fallback still
// drives the recovery once persistence heals.
func TestReplacementSweepCompensationW18C_ConsumeFailRearmCollisionMarkerFailureKeepsEverything(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18C-TRIPLE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	op := journalRow(t, baseRepo, "job-w18c", "W18C-TRIPLE", dest, backup, 1, models.RevertStatusApplied)

	consumeErr := errors.New("w18c consumption transaction wedged")
	repo := &w18MalformedCallRepo{p3OpRepo: baseRepo, brokeAt: 3, fail: map[int]error{2: consumeErr}}
	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.True(t, fs.fired, "the injected foreign claim raced the re-arm publish")

	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"marker failure on top must not destroy the restored destination")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the foreign occupant survives the triple failure")

	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.False(t, gf.Replacements[0].RestorePending, "the broken marker merge persisted nothing")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, "restore-pending persistence failed")
	require.Contains(t, out, "restored destination retained")
	require.True(t, sweeper.hasPendingRemoval(sweepSlash(backup)),
		"this process's pending-removal fallback carries the recovery")

	// Heal persistence; the SAME sweeper completes the deferred cleanup — the
	// in-process fallback carries the rearm-refused kind (wave-19), so the
	// retry never restores from NOR removes the occupied path.
	repo.fail = nil
	repo.brokeAt = 0
	healed, err = sweeper.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"wave-19: the fallback-authorized rearm-refused retry also stays off the unowned backup name")
	row, err = baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

// retryPendingRemoval's consumption failure composes the same way: the
// collided re-arm leaves the occupant untouched and the destination retains
// the restored bytes, while the already-durable RestorePending marker needs
// no rewrite (a no-change marker merge).
func TestReplacementSweepCompensationW18C_RetryPendingRearmCollisionKeepsPendingMarker(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18C-RPR-COLLIDE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := &models.BatchFileOperation{
		BatchJobID: "job-w18c", MovieID: "W18C-RPR-COLLIDE", OriginalPath: "/src/w18c-rpr.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, baseRepo.Create(ctx, op))

	consumeErr := errors.New("w18c pending cleanup consumption wedged")
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: consumeErr}}
	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(fs, repo)
	require.False(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.True(t, fs.fired, "the re-arm raced the foreign claim")

	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the restored destination is retained")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)), "the occupant is untouched")

	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)),
		"the durable marker survives the no-change marker merge")
	require.True(t, sweeper.hasPendingRemoval(sweepSlash(backup)))

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, "marked restore-pending")

	// Recovery on a healed repository: the wave-19 collision upgrade marked
	// the entry rearm-refused, so the consumption completes with the
	// destination bytes intact and the occupant never removed.
	repo.fail = nil
	require.True(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the refused re-arm upgraded the marker to rearm-refused: the retry runs no backup-path operation")
	row, err = baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

// The fallback-authorized variant: the journal entry carries NO durable
// marker yet (authorization came from this process's pending-removal
// fallback), and the marker merge fails on top of the consumption failure +
// collided re-arm — still nothing is destroyed.
func TestReplacementSweepCompensationW18C_RetryPendingRearmCollisionMarkerFailureRetainsAll(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18C-RPR-TRIPLE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := &models.BatchFileOperation{
		BatchJobID: "job-w18c", MovieID: "W18C-RPR-TRIPLE", OriginalPath: "/src/w18c-rpr-t.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, baseRepo.Create(ctx, op))

	consumeErr := errors.New("w18c pending cleanup consumption wedged")
	repo := &w18MalformedCallRepo{p3OpRepo: baseRepo, brokeAt: 3, fail: map[int]error{2: consumeErr}}
	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	sweeper := NewReplacementSweeper(fs, repo)
	// Authorization comes from the in-process fallback only, exactly like the
	// fall-forward state a crashed marker merge leaves behind.
	sweeper.rememberPendingRemoval(sweepSlash(backup))

	require.False(t, sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.True(t, fs.fired, "the re-arm raced the foreign claim")

	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the restored destination is retained")
	require.Equal(t, "foreign-bytes", string(mustRead2(t, base, backup)),
		"the foreign occupant survives the triple failure")

	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.False(t, gf.Replacements[0].RestorePending, "the broken marker merge persisted nothing")
	require.True(t, sweeper.hasPendingRemoval(sweepSlash(backup)),
		"this process's pending-removal fallback carries the recovery")

	out := logs.String()
	require.Contains(t, out, consumeErr.Error())
	require.Contains(t, out, fsutil.ErrPublishCollision.Error())
	require.Contains(t, out, "restore-pending persistence failed")
	require.Contains(t, out, "restored destination retained")
}
