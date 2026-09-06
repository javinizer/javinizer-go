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

// countOpsForJob returns the number of COMMITTED operation rows for jobID.
//
// Busy-timeout conformance: the fixture connection already carries prod's
// busy handling — newDatabaseTestDB goes through New, and normalizeSQLiteDSN
// appends the same _busy_timeout=5000 to the shared-cache memory DSN that
// file DSNs get. Shared-cache TABLE locks are the one conflict class SQLite
// never routes through the busy handler: a reader/writer racing a held table
// write lock gets SQLITE_LOCKED ("database table is locked") INSTANTLY, so
// busy_timeout has nothing to wait on (Windows scheduling surfaces this
// readily; Linux rarely). Every prod writer absorbs exactly this class via
// retryOnLocked/isLocked; this helper, whose polling callers race the race
// hammer's creators, mirrors that seam with a deadline-bounded retry instead
// of treating a legal blocked observation as fatal. Any non-lock failure
// (schema bugs, real corruption) still aborts the test immediately.
func countOpsForJob(t *testing.T, db *DB, jobID string) int64 {
	t.Helper()
	const lockRetryDeadline = 15 * time.Second
	deadline := time.Now().Add(lockRetryDeadline)
	var lastErr error
	for attempts := 0; ; attempts++ {
		var count int64
		err := db.DB.Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", jobID).Count(&count).Error
		if err == nil {
			return count
		}
		require.True(t, isLocked(err), "count batch_file_operations for %s: %v", jobID, err)
		lastErr = err
		if time.Now().After(deadline) {
			t.Fatalf("batch_file_operations count stayed table-locked past %s (%d attempts): %v", lockRetryDeadline, attempts+1, lastErr)
		}
		time.Sleep(2 * time.Millisecond)
	}
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
// a LIVE job (its single statement needs the writer lock, so it cannot insert
// early), then commit the retention claim from the lock holder. The blocked
// competitor is observed portably: on Linux/macOS the statement waits on the
// busy timeout, while Windows shared-cache table locks bypass the busy
// handler and fail it fast with the SQLITE_BUSY/SQLITE_LOCKED class (the
// retryOnLocked budget then expires before release) — both legal blocked
// observations, since nothing was committed. The released (or re-issued)
// Create statement evaluates its WHERE NOT EXISTS probe against the
// now-committed pruning status — under the old pre-check shape the check had
// already read "live" and the insert would have landed on the pruned job.
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

	// The create must NOT insert while the writer lock is held: it is gated on
	// the lock from its first (only) statement — including the probe. A legal
	// blocked observation is either "still waiting after 250ms" or an
	// SQLITE_BUSY/SQLITE_LOCKED-class error (a write that committed nothing);
	// illegal is any non-lock outcome during the held window — success would
	// be the both-committed interleave this probe exists to kill.
	blockedEarly := false
	select {
	case earlyErr := <-done:
		require.True(t, isLocked(earlyErr), "Create returned a non-lock outcome while the writer lock was held — the probe is not inside the write path: %v", earlyErr)
		blockedEarly = true
	case <-time.After(250 * time.Millisecond):
	}

	// Commit the sweep claim from the lock holder (mirrors
	// DeleteOrganizedOlderThan's status flip) and release the lock.
	_, err = holder.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, "w248-race")
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)
	committed = true

	if blockedEarly {
		// The fast-failed competitor committed nothing; pin the same fence
		// directly: a create issued now evaluates its probe against the
		// committed claim and must be rejected.
		err = repo.Create(ctx, &models.BatchFileOperation{BatchJobID: "w248-race", OriginalPath: "/race/after.mp4", NewPath: "/race/dst.mp4"})
		require.ErrorIs(t, err, ErrJobPruning, "a create evaluated after the committed claim must be fenced")
	} else {
		createErr := <-done
		require.ErrorIs(t, createErr, ErrJobPruning, "the probe must observe the claim committed while it waited for the lock")
	}
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
//   - no create reports anything but success, ErrJobPruning, or an exhausted
//     transient-lock retry (a legal blocked observation on the Windows
//     fail-fast table-lock path — nothing was committed, so it can never
//     orphan a row; anything else is the illegal class);
//   - once the claim is committed (observable inside the cleanup hook), EVERY
//     subsequent create/batch is refused — proven deterministically by direct
//     main-thread probes rather than worker timing, so no late-but-successful
//     insert can orphan a ledger row;
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
	lockBlocked := 0
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
					// The first terminal error ends this worker's loop; assertions
					// run on the main goroutine after wg.Wait. Legal terminal
					// outcomes: the prune fence, or an exhausted transient-lock
					// retry (Windows shared-cache table locks fail fast with no
					// busy-handler cover, so a contended create can burn its
					// retryOnLocked budget — a blocked write that committed
					// nothing). Any other error is the illegal class.
					mu.Lock()
					switch {
					case errors.Is(err, ErrJobPruning):
						rejections++
					case isLocked(err):
						lockBlocked++
					default:
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
	mu.Lock()
	assert.Empty(t, unexpectedErrs, "only the prune fence or an exhausted transient-table-lock retry may refuse a create: %v", unexpectedErrs)
	fenceHits := rejections
	blockedWrites := lockBlocked
	mu.Unlock()
	// Seat-level breadth is calibrated, not loose: every creator exits on
	// exactly one legal terminal observation, and the fence is proved
	// deterministically by the five direct probes above — the both-committed
	// interleave this test exists to kill cannot hide behind a blocked write,
	// because a blocked write committed nothing while snapshot disjointness
	// and the zero-survivor assert below run over only COMMITTED state.
	assert.Equal(t, 4, fenceHits+blockedWrites, "every creator exits on exactly one terminal observation")
	assert.Greater(t, fenceHits, 0, "at least one creator observed the fence directly")

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
