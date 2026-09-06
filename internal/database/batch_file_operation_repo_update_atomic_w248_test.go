package database

// Regression pins for PR #248 codex P2 (F-update): the prune fence on Update
// must be WRITE-ATOMIC, exactly like the Create/CreateBatch conditional
// insert. The pre-fix shape was probe-then-Save: the ensureJobWritable status
// read ran as a separate statement, a retention claim landing BETWEEN it and
// GORM Save flipped zero-rows into Save's INSERT fallback AFTER the sweep had
// snapshotted/backed-up/deleted the row (orphaned ledger row under a deleted
// job — SQLite FK enforcement is off per the repository DSN), and Save could
// claim success for a row the already-snapshotted sweep deleted a moment
// later. The single-statement UPDATE ... WHERE NOT EXISTS (jobs pruning)
// leaves zero window; zero affected rows is classified on committed state —
// ErrJobPruning for the fence, ErrNotFound for a genuinely missing row — and
// the Save insert fallback can never resurrect a swept row.

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBFOW248_Update_SaveParityOnLiveJob pins the happy path against a live
// job: every non-key column persists verbatim (Save-parity full-column bind),
// updated_at stamps forward and assigns back onto the caller's struct exactly
// like GORM Save's AutoUpdateTime callback did, and created_at is untouched.
func TestBFOW248_Update_SaveParityOnLiveJob(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, NewJobRepository(db).Create(context.Background(), &models.Job{
		ID: "w248-upd-live", Status: models.JobStatusRunning, Files: "[]", StartedAt: time.Now().UTC(),
	}))
	op := &models.BatchFileOperation{BatchJobID: "w248-upd-live", OriginalPath: "/upd/src.mp4", NewPath: "/upd/dst.mp4"}
	require.NoError(t, repo.Create(context.Background(), op))
	require.NotZero(t, op.ID)
	createdAt := op.CreatedAt

	revertedAt := time.Now().UTC().Add(-time.Minute)
	op.NewPath = "/upd/dst-renamed.mp4"
	op.RevertStatus = models.RevertStatusReverted
	op.RevertedAt = &revertedAt
	op.NFOSnapshot = "<movie/>"
	op.GeneratedFiles = `{"replacements":[]}`
	before := time.Now().UTC()
	require.NoError(t, repo.Update(context.Background(), op))
	assert.WithinDuration(t, before, op.UpdatedAt, 5*time.Second, "Save-parity: updated_at assigns back onto the caller struct")

	got, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, "/upd/dst-renamed.mp4", got.NewPath)
	assert.Equal(t, models.RevertStatusReverted, got.RevertStatus)
	require.NotNil(t, got.RevertedAt)
	assert.WithinDuration(t, revertedAt, *got.RevertedAt, time.Microsecond)
	assert.Equal(t, "<movie/>", got.NFOSnapshot)
	assert.Equal(t, `{"replacements":[]}`, got.GeneratedFiles)
	assert.WithinDuration(t, createdAt, got.CreatedAt, time.Microsecond, "update must not restamp created_at")
	assert.WithinDuration(t, before, got.UpdatedAt, 5*time.Second, "update stamps updated_at like Save")
}

// TestBFOW248_Update_PruningJobRejectedStatically pins the base fence: with
// the retention claim already committed, Update is refused with ErrJobPruning
// and the row is left untouched; a live job's update is unaffected.
func TestBFOW248_Update_PruningJobRejectedStatically(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-upd-pruned")
	op := &models.BatchFileOperation{BatchJobID: "w248-upd-pruned", OriginalPath: "/u/src.mp4", NewPath: "/u/dst.mp4"}
	require.NoError(t, repo.Create(context.Background(), op))
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-upd-pruned").Error)

	op.NewPath = "/u/dst-late.mp4"
	err := repo.Update(context.Background(), op)
	require.ErrorIs(t, err, ErrJobPruning)

	got, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, "/u/dst.mp4", got.NewPath, "a fenced update must never touch the row")
	assert.Equal(t, int64(1), countOpsForJob(t, db, "w248-upd-pruned"))
}

// TestBFOW248_Update_ProbeRunsUnderWriteLock is the force-ordered zero-window
// probe for Update, mirroring TestBFOW248_Create_ProbeRunsUnderWriteLock:
// hold the SQLite write lock on a second connection, start Update against a
// LIVE job (its single statement needs the writer lock, so it cannot land
// early), then commit the retention claim from the lock holder. The blocked
// competitor is observed portably: on Linux/macOS the statement waits on the
// busy timeout, while Windows shared-cache table locks bypass the busy
// handler and fail it fast with the SQLITE_BUSY/SQLITE_LOCKED class (the
// retryOnLocked budget then expires before release) — both legal blocked
// observations, since nothing was committed. The released (or re-issued)
// Update statement evaluates its WHERE NOT EXISTS probe against the
// now-committed pruning status — under the pre-fix probe-then-Save shape the
// pre-check had already read "live" and the write would have landed on the
// pruning job.
func TestBFOW248_Update_ProbeRunsUnderWriteLock(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-upd-race")

	ctx := context.Background()
	op := &models.BatchFileOperation{BatchJobID: "w248-upd-race", OriginalPath: "/ur/src.mp4", NewPath: "/ur/dst.mp4"}
	require.NoError(t, repo.Create(ctx, op))

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	holder, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()

	_, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	committed := false
	defer func() {
		if !committed {
			_, _ = holder.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	done := make(chan error, 1)
	go func() {
		raced := *op
		raced.NewPath = "/ur/dst-late.mp4"
		done <- repo.Update(ctx, &raced)
	}()

	// The update must NOT land while the writer lock is held: it is gated on
	// the lock from its first (only) statement — including the probe. A legal
	// blocked observation is either "still waiting after 250ms" or an
	// SQLITE_BUSY/SQLITE_LOCKED-class error (a write that committed nothing);
	// illegal is any non-lock outcome during the held window — success would
	// be the both-committed interleave this probe exists to kill.
	blockedEarly := false
	select {
	case earlyErr := <-done:
		require.True(t, isLocked(earlyErr), "Update returned a non-lock outcome while the writer lock was held — the probe is not inside the write path: %v", earlyErr)
		blockedEarly = true
	case <-time.After(250 * time.Millisecond):
	}

	// Commit the sweep claim from the lock holder (mirrors
	// DeleteOrganizedOlderThan's status flip) and release the lock.
	_, err = holder.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-upd-race")
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)
	committed = true

	if blockedEarly {
		// The fast-failed competitor committed nothing; pin the same fence
		// directly: an update issued now evaluates its probe against the
		// committed claim and must be rejected.
		raced := *op
		raced.NewPath = "/ur/dst-late.mp4"
		err = repo.Update(ctx, &raced)
		require.ErrorIs(t, err, ErrJobPruning, "an update evaluated after the committed claim must be fenced")
	} else {
		updateErr := <-done
		require.ErrorIs(t, updateErr, ErrJobPruning, "the probe must observe the claim committed while it waited for the lock")
	}

	got, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	assert.Equal(t, "/ur/dst.mp4", got.NewPath, "the fenced update must never land")
	assert.Equal(t, int64(1), countOpsForJob(t, db, "w248-upd-race"))
}

// TestBFOW248_Update_SweptRowNeverResurrects is the P2 orphan-kill pin,
// force-ordered: the lock holder commits the retention claim AND the
// follow-up row delete while Update is gated on the writer lock
// (DeleteOrganizedOlderThan's claim-then-delete sequence compressed into one
// hold). The pre-fix shape reported the probe as clean and GORM Save's INSERT
// fallback re-created the swept row under a pruning job — an orphaned ledger
// row no sweep snapshot ever saw. The atomic shape reports ErrJobPruning and
// writes nothing.
func TestBFOW248_Update_SweptRowNeverResurrects(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-upd-swept")

	ctx := context.Background()
	op := &models.BatchFileOperation{BatchJobID: "w248-upd-swept", OriginalPath: "/us/src.mp4", NewPath: "/us/dst.mp4"}
	require.NoError(t, repo.Create(ctx, op))

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	holder, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()

	_, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	committed := false
	defer func() {
		if !committed {
			_, _ = holder.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	done := make(chan error, 1)
	go func() {
		stale := *op
		done <- repo.Update(ctx, &stale)
	}()

	blockedEarly := false
	select {
	case earlyErr := <-done:
		require.True(t, isLocked(earlyErr), "Update returned a non-lock outcome while the writer lock was held: %v", earlyErr)
		blockedEarly = true
	case <-time.After(250 * time.Millisecond):
	}

	// Claim + delete commit atomically from the holder's perspective (the
	// test observes both only after COMMIT — the fix's classification probes
	// the committed pruning status; the pre-fix insert fallback would land
	// here).
	_, err = holder.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-upd-swept")
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, "DELETE FROM batch_file_operations WHERE id = ?", op.ID)
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)
	committed = true

	if blockedEarly {
		stale := *op
		err = repo.Update(ctx, &stale)
		require.ErrorIs(t, err, ErrJobPruning, "swept row under a claimed job: fence dominates the missing-row report")
	} else {
		updateErr := <-done
		require.ErrorIs(t, updateErr, ErrJobPruning, "the statement-time probe must fence the resurrecting write")
	}
	assert.Zero(t, countOpsForJob(t, db, "w248-upd-swept"), "the swept row must never be resurrected by an insert fallback")
	_, findErr := repo.FindByID(ctx, op.ID)
	require.ErrorIs(t, findErr, ErrNotFound)
}

// TestBFOW248_Update_CommittedStateRidesSweepSnapshot pins the OTHER ordering
// with the REAL sweep: an update committed before the retention claim is
// necessarily observed by the claim's op snapshot (status flip + snapshot
// commit inside one transaction), so the cleanup hook sees the UPDATED row
// and the follow-up delete removes it — never a stale-snapshot cleanup
// against a row whose real state moved.
func TestBFOW248_Update_CommittedStateRidesSweepSnapshot(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	jobRepo := seedOrganizedJob(t, db, "w248-upd-snap")

	op := &models.BatchFileOperation{BatchJobID: "w248-upd-snap", OriginalPath: "/usp/src.mp4", NewPath: "/usp/dst.mp4", OperationType: models.OperationTypeMove}
	require.NoError(t, repo.Create(context.Background(), op))
	op.NewPath = "/usp/dst-final.mp4"
	require.NoError(t, repo.Update(context.Background(), op))

	snapshotted := map[uint]string{}
	jobRepo.SetOrganizedJobPruneHook(func(_ context.Context, ops []models.BatchFileOperation) error {
		for _, o := range ops {
			snapshotted[o.ID] = o.NewPath
		}
		return nil
	})

	require.NoError(t, jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	assert.Equal(t, "/usp/dst-final.mp4", snapshotted[op.ID], "a committed pre-claim update MUST ride the sweep snapshot")
	assert.Zero(t, countOpsForJob(t, db, "w248-upd-snap"), "the sweep deletes the snapshotted row")
}

// TestBFOW248_Update_AfterJobDeletedReportsNotFound pins the decided contract
// for a genuinely missing operation row (the Save insert fallback is out of
// the racy path): once the sweep deleted job AND rows, a caller holding a
// stale snapshot gets ErrNotFound — never a resurrection insert under a
// job id nothing owns. The empty/never-persisted job id keeps the
// pre-existing allow semantics at the fence and still reports ErrNotFound.
func TestBFOW248_Update_AfterJobDeletedReportsNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	jobRepo := seedOrganizedJob(t, db, "w248-upd-gone")

	op := &models.BatchFileOperation{BatchJobID: "w248-upd-gone", OriginalPath: "/ug/src.mp4", NewPath: "/ug/dst.mp4", OperationType: models.OperationTypeMove}
	require.NoError(t, repo.Create(context.Background(), op))
	require.NoError(t, jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	_, jerr := jobRepo.FindByID(context.Background(), "w248-upd-gone")
	require.ErrorIs(t, jerr, ErrNotFound, "fixture sanity: the depleted job row is deleted by the sweep")

	stale := *op // the caller's pre-sweep snapshot
	err := repo.Update(context.Background(), &stale)
	require.ErrorIs(t, err, ErrNotFound, "an update for a swept row on a deleted job reports the missing row, not the fence (and never silently resurrects)")
	assert.Zero(t, countOpsForJob(t, db, "w248-upd-gone"), "no orphaned ledger row lands under the deleted job")

	// Never-existed row on a never-persisted job id: same missing-row report
	// (the fence's missing-job allow semantics are unchanged).
	ghost := &models.BatchFileOperation{ID: 424242, BatchJobID: "w248-upd-ghost", OriginalPath: "/gh/src.mp4", NewPath: "/gh/dst.mp4"}
	require.ErrorIs(t, repo.Update(context.Background(), ghost), ErrNotFound)
	assert.Zero(t, countOpsForJob(t, db, "w248-upd-ghost"))

	// Empty job id (pre-existing allow semantics) with a missing row.
	lone := &models.BatchFileOperation{ID: 434343, OriginalPath: "/ln/src.mp4", NewPath: "/ln/dst.mp4"}
	require.ErrorIs(t, repo.Update(context.Background(), lone), ErrNotFound)
}

// TestBFOW248_Update_StatementErrorSurfaced pins the non-fence failure arm: a
// schema breakage surfaces as an update error and never masquerades as the
// prune fence or a missing row.
func TestBFOW248_Update_StatementErrorSurfaced(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)

	err := repo.Update(context.Background(), &models.BatchFileOperation{ID: 1, BatchJobID: "w248-upd-broken", OriginalPath: "/b/src.mp4", NewPath: "/b/dst.mp4"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update batch file operation")
	assert.NotErrorIs(t, err, ErrJobPruning, "a statement failure must not masquerade as the prune fence")
	assert.NotErrorIs(t, err, ErrNotFound, "a statement failure must not masquerade as a missing row")
}
