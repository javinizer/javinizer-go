package database

// Regression pins for PR #248 codex P1 (F1): the prune fence on
// Create/CreateBatch must be WRITE-ATOMIC. The pre-check shape let a
// retention sweep flip the owning job to "pruning" BETWEEN the status probe
// and the insert, so the operation row survived cleanup-hook snapshotting and
// then got pruned while its apply proceeded with mutations and NO durable
// revert ledger. The single-statement INSERT ... SELECT ... WHERE NOT EXISTS
// (jobs pruning) leaves zero window: the claim observes every committed row,
// and every post-claim create is rejected.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedOrganizedJob inserts an organized jobs row old enough for retention and
// returns its repository handle.
func seedOrganizedJob(t *testing.T, db *DB, jobID string) *JobRepository {
	t.Helper()
	jobRepo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, jobRepo.Create(context.Background(), &models.Job{
		ID:          jobID,
		Status:      models.JobStatusOrganized,
		Files:       "[]",
		StartedAt:   organizedAt,
		OrganizedAt: &organizedAt,
	}))
	return jobRepo
}

func countOpsForJob(t *testing.T, db *DB, jobID string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.DB.Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", jobID).Count(&count).Error)
	return count
}

// TestBFOW248_Create_PruningJobRejectedStatically pins the base fence: with
// the claim already committed, Create is refused with ErrJobPruning and no
// row lands; a live job's create is untouched.
func TestBFOW248_Create_PruningJobRejectedStatically(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-pruned")
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-pruned").Error)

	op := &models.BatchFileOperation{BatchJobID: "w248-pruned", OriginalPath: "/prune/src.mp4", NewPath: "/prune/dst.mp4"}
	err := repo.Create(context.Background(), op)
	require.ErrorIs(t, err, ErrJobPruning)
	assert.Zero(t, op.ID, "a fenced create must never mint a row id")
	assert.Zero(t, countOpsForJob(t, db, "w248-pruned"))

	// A live job (and a never-persisted job id, the pre-existing allow
	// semantics) still insert.
	live := &models.BatchFileOperation{BatchJobID: "w248-live", OriginalPath: "/live/src.mp4", NewPath: "/live/dst.mp4"}
	require.NoError(t, repo.Create(context.Background(), live))
	assert.NotZero(t, live.ID)
}

// TestBFOW248_Create_ProbeRunsUnderWriteLock is the force-ordered zero-window
// probe: hold the SQLite write lock on a second connection, start Create for
// a LIVE job (it must block — its single statement needs the writer lock and
// cannot complete early), then commit the retention claim from the lock
// holder. The released Create statement evaluates its WHERE NOT EXISTS probe
// against the now-committed pruning status — under the old pre-check shape
// the check had already read "live" and the insert would have landed on the
// pruned job.
func TestBFOW248_Create_ProbeRunsUnderWriteLock(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-race")

	ctx := context.Background()
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
		done <- repo.Create(ctx, &models.BatchFileOperation{BatchJobID: "w248-race", OriginalPath: "/race/src.mp4", NewPath: "/race/dst.mp4"})
	}()

	// The create must NOT complete while the writer lock is held: it is gated
	// on the lock from its first (only) statement — including the probe.
	select {
	case earlyErr := <-done:
		t.Fatalf("Create returned while the writer lock was held — the probe is not inside the write path: %v", earlyErr)
	case <-time.After(250 * time.Millisecond):
	}

	// Commit the sweep claim from the lock holder (mirrors
	// DeleteOrganizedOlderThan's status flip) and release the lock.
	_, err = holder.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-race")
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)
	committed = true

	createErr := <-done
	require.ErrorIs(t, createErr, ErrJobPruning, "the probe must observe the claim committed while it waited for the lock")
	assert.Zero(t, countOpsForJob(t, db, "w248-race"))
}

// TestBFOW248_Create_CommittedRowIncludedInSweepSnapshot pins the OTHER
// ordering: a create committed before the claim is necessarily part of the
// sweep's op snapshot (it commits before the claim can land), so the cleanup
// hook sees it and the follow-up delete removes it — never an orphaned live
// ledger row.
func TestBFOW248_Create_CommittedRowIncludedInSweepSnapshot(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	jobRepo := seedOrganizedJob(t, db, "w248-snap")

	op := &models.BatchFileOperation{BatchJobID: "w248-snap", OriginalPath: "/snap/src.mp4", NewPath: "/snap/dst.mp4", OperationType: models.OperationTypeMove}
	require.NoError(t, repo.Create(context.Background(), op))
	require.NotZero(t, op.ID)

	snapshotted := map[uint]bool{}
	jobRepo.SetOrganizedJobPruneHook(func(_ context.Context, ops []models.BatchFileOperation) error {
		for _, o := range ops {
			snapshotted[o.ID] = true
		}
		return nil
	})

	require.NoError(t, jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	assert.True(t, snapshotted[op.ID], "a row committed before the claim must ride the sweep snapshot")
	assert.Zero(t, countOpsForJob(t, db, "w248-snap"), "the sweep deletes the snapshotted row")
}

// TestBFOW248_CreateBatch_PruneFenceRollsBackWholeBatch pins per-statement
// fencing inside the batch transaction: a claim committed against ANY target
// job rejects the batch atomically — no partially journaled batch (a split
// batch would corrupt revert bookkeeping).
func TestBFOW248_CreateBatch_PruneFenceRollsBackWholeBatch(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	seedOrganizedJob(t, db, "w248-batch-live")
	seedOrganizedJob(t, db, "w248-batch-pruned")
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-batch-pruned").Error)

	ops := []*models.BatchFileOperation{
		{BatchJobID: "w248-batch-live", OriginalPath: "/b/1.mp4", NewPath: "/d/1.mp4"},
		{BatchJobID: "w248-batch-pruned", OriginalPath: "/b/2.mp4", NewPath: "/d/2.mp4"},
	}
	err := repo.CreateBatch(context.Background(), ops)
	require.ErrorIs(t, err, ErrJobPruning)
	// ops[0].ID may carry its in-memory RETURNING id from before the
	// rollback (same as the old GORM path); only COMMITTED state matters.
	assert.Zero(t, countOpsForJob(t, db, "w248-batch-live"), "the unfenced sibling row must roll back too")
	assert.Zero(t, countOpsForJob(t, db, "w248-batch-pruned"))
}

// TestBFOW248_CreateBatch_StatementErrorRollsBack pins the non-fence failure
// arm of the batch write path (and the empty-batch no-DB-touch early return).
func TestBFOW248_CreateBatch_StatementErrorRollsBack(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)

	// Empty batch: a no-op contract that must not touch the (broken) schema.
	require.NoError(t, repo.CreateBatch(context.Background(), nil))

	err := repo.CreateBatch(context.Background(), []*models.BatchFileOperation{
		{BatchJobID: "w248-broken", OriginalPath: "/b/1.mp4", NewPath: "/d/1.mp4"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create batch file operation")
	assert.NotErrorIs(t, err, ErrJobPruning, "a statement failure must not masquerade as the prune fence")
}

// TestBFOW248_CreateBatch_DefaultsParity pins the schema-default parity of
// the hand-bound insert: zero-valued enum/status fields land as the schema
// defaults exactly like the GORM Create they replace, stamped timestamps bind
// and report back, and caller-preset values persist verbatim.
func TestBFOW248_CreateBatch_DefaultsParity(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	presetCreated := time.Date(2023, 3, 4, 5, 6, 7, 0, time.UTC)
	presetUpdated := presetCreated.Add(time.Hour)
	bare := &models.BatchFileOperation{BatchJobID: "w248-parity", OriginalPath: "/p/bare.mp4", NewPath: "/q/bare.mp4"}
	full := &models.BatchFileOperation{
		BatchJobID:     "w248-parity",
		MovieID:        "PAR-001",
		OriginalPath:   "/p/full.mp4",
		NewPath:        "/q/full.mp4",
		OperationType:  models.OperationTypeCopy,
		RevertStatus:   models.RevertStatusNoOp,
		InPlaceRenamed: true,
		CreatedAt:      presetCreated,
		UpdatedAt:      presetUpdated,
	}
	require.NoError(t, repo.CreateBatch(context.Background(), []*models.BatchFileOperation{bare, full}))
	require.NotZero(t, bare.ID)
	require.NotZero(t, full.ID)

	// Assign-back parity with the old GORM create callbacks.
	assert.Equal(t, models.OperationTypeMove, bare.OperationType)
	assert.Equal(t, models.RevertStatusApplied, bare.RevertStatus)
	assert.False(t, bare.CreatedAt.IsZero(), "create stamps created_at on the record")
	assert.False(t, bare.UpdatedAt.IsZero())

	gotBare, err := repo.FindByID(context.Background(), bare.ID)
	require.NoError(t, err)
	assert.Equal(t, models.OperationTypeMove, gotBare.OperationType)
	assert.Equal(t, models.RevertStatusApplied, gotBare.RevertStatus)
	assert.False(t, gotBare.CreatedAt.IsZero())

	gotFull, err := repo.FindByID(context.Background(), full.ID)
	require.NoError(t, err)
	assert.Equal(t, models.OperationTypeCopy, gotFull.OperationType)
	assert.Equal(t, models.RevertStatusNoOp, gotFull.RevertStatus)
	assert.True(t, gotFull.InPlaceRenamed)
	assert.WithinDuration(t, presetCreated, gotFull.CreatedAt, time.Microsecond)
	assert.WithinDuration(t, presetUpdated, gotFull.UpdatedAt, time.Microsecond)
}

// TestBFOW248_Create_RaceSweepClosesWindow is the race-gate hammer: creators
// loop against a job while the retention sweep claims it. Invariants:
//
//   - no create reports anything but success or ErrJobPruning;
//   - once the claim is committed (observable inside the cleanup hook), EVERY
//     subsequent create/batch is refused — there is no late-but-successful
//     insert that would orphan a ledger row;
//   - every row created successfully pre-claim appears in the sweep snapshot
//     and is deleted by the sweep (zero rows survive).
func TestBFOW248_Create_RaceSweepClosesWindow(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	jobRepo := seedOrganizedJob(t, db, "w248-hammer")

	var mu sync.Mutex
	successIDs := map[uint]bool{}
	snapshotIDs := map[uint]bool{}
	rejections := 0
	var unexpectedErrs []error

	claimCommitted := make(chan struct{})
	releaseHook := make(chan struct{})
	jobRepo.SetOrganizedJobPruneHook(func(_ context.Context, ops []models.BatchFileOperation) error {
		mu.Lock()
		for _, o := range ops {
			snapshotIDs[o.ID] = true
		}
		mu.Unlock()
		close(claimCommitted)
		<-releaseHook
		return nil
	})

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; ; i++ {
				op := &models.BatchFileOperation{
					BatchJobID:   "w248-hammer",
					OriginalPath: fmt.Sprintf("/hammer/%d/%d.mp4", worker, i),
					NewPath:      "/hammer/dst.mp4",
				}
				err := repo.Create(context.Background(), op)
				if err != nil {
					// The ONLY admissible rejection is the prune fence; the
					// first rejection ends this worker's loop. Assertions run
					// on the main goroutine after wg.Wait.
					mu.Lock()
					if errors.Is(err, ErrJobPruning) {
						rejections++
					} else {
						unexpectedErrs = append(unexpectedErrs, err)
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				successIDs[op.ID] = true
				mu.Unlock()
				// Yield between inserts so the sweep claim can interleave: shared
				// cache + memory journal surfaces table-lock conflicts immediately
				// (no busy-handler cover), and a back-to-back write loop would
				// starve the claim forever under -race.
				time.Sleep(time.Millisecond)
			}
		}(w)
	}

	// Wait until at least one creator row is committed, THEN claim+snapshot:
	// the cleanup hook fires only for a non-empty op snapshot, so a fixed
	// sleep cannot gate this deterministically under -race slowdowns. The hook
	// blocks inside the sweep so the committed-claim window can be probed.
	waitDeadline := time.Now().Add(10 * time.Second)
	for countOpsForJob(t, db, "w248-hammer") == 0 {
		if time.Now().After(waitDeadline) {
			t.Fatal("creators never journaled a row before the claim")
		}
		time.Sleep(2 * time.Millisecond)
	}
	// Claim+snapshot may need several attempts under writer pressure: the
	// retention claim rides NO retryOnLocked seam of its own, and transient
	// table-lock conflicts surface immediately on this shared-cache fixture —
	// retry the sweep (the same retryOnLocked seam the write paths use) until
	// its hook fires. A nil-error finish without the hook would mean the claim
	// landed on an empty snapshot, contradicting the ≥1-row gate above.
	sweepDone := make(chan error, 1)
	claimed := false
	for attempt := 0; attempt < 30 && !claimed; attempt++ {
		go func() {
			sweepDone <- jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
		}()
		select {
		case <-claimCommitted:
			claimed = true
		case sweepErr := <-sweepDone:
			if sweepErr == nil {
				t.Fatal("sweep claim landed but the cleanup hook never fired (empty snapshot despite committed rows)")
			}
			time.Sleep(10 * time.Millisecond)
		case <-time.After(30 * time.Second):
			t.Fatal("sweep cleanup hook never fired — the claim must eventually interleave with creator writes")
		}
	}
	if !claimed {
		t.Fatal("sweep claim could not land within the attempt budget")
	}

	// Once the claim is committed, the window is closed: every late create is
	// refused. Between each probe, creators above hit their first rejection
	// and stop, so wg converges while the hook still holds the sweep.
	for i := 0; i < 4; i++ {
		err := repo.Create(context.Background(), &models.BatchFileOperation{BatchJobID: "w248-hammer", OriginalPath: fmt.Sprintf("/hammer/late/%d.mp4", i), NewPath: "/hammer/dst.mp4"})
		require.ErrorIs(t, err, ErrJobPruning, "create after the committed claim must be fenced")
	}
	err := repo.CreateBatch(context.Background(), []*models.BatchFileOperation{
		{BatchJobID: "w248-hammer", OriginalPath: "/hammer/late-batch/1.mp4", NewPath: "/hammer/dst.mp4"},
		{BatchJobID: "w248-hammer", OriginalPath: "/hammer/late-batch/2.mp4", NewPath: "/hammer/dst.mp4"},
	})
	require.ErrorIs(t, err, ErrJobPruning, "create-batch after the committed claim must be fenced")

	wg.Wait()
	assert.Empty(t, unexpectedErrs, "the prune fence is the ONLY admissible create rejection: %v", unexpectedErrs)
	assert.Equal(t, 4, rejections, "every creator eventually hit the fence")

	close(releaseHook)
	require.NoError(t, <-sweepDone)

	mu.Lock()
	defer mu.Unlock()
	for id := range successIDs {
		assert.True(t, snapshotIDs[id], "a successful pre-claim create MUST ride the sweep snapshot (orphan ledger → silent revert loss)")
	}
	assert.Equal(t, len(successIDs), len(snapshotIDs))
	assert.Greater(t, len(successIDs), 0, "the hammer must have journaled rows before the claim (fixture sanity)")
	assert.Zero(t, countOpsForJob(t, db, "w248-hammer"), "the sweep deletes exactly the snapshotted rows; no op row survives")
	_, findErr := jobRepo.FindByID(context.Background(), "w248-hammer")
	assert.ErrorIs(t, findErr, ErrNotFound, "the depleted job row is deleted by the sweep")
}
