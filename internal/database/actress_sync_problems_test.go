package database

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOfflineNameSyncProblemLeaseReleaseFencesEveryOldWorker(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "problem-lease", review, snapshot, now)
	require.NoError(t, err)
	first, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "first-worker", now, time.Minute)
	require.NoError(t, err)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *first, nil, now)
	require.ErrorIs(t, err, ErrActressSyncStale)
	wrongOwner := *first
	wrongOwner.Owner = "not-the-owner"
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), wrongOwner, ActressSyncProblemApplyFailed, now), ErrActressSyncLease)
	require.NoError(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *first, ActressSyncProblemApplyFailed, now))
	stored, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", stored.Tasks[0].Status)
	require.Equal(t, first.Generation, stored.Tasks[0].LeaseGeneration)
	require.Empty(t, stored.Tasks[0].LeaseOwner)
	require.Zero(t, stored.Tasks[0].LeaseUntilMS)
	require.Equal(t, ActressSyncProblemApplyFailed, stored.Tasks[0].ProblemCode)
	require.Equal(t, 1, stored.Tasks[0].ProblemAttempts)
	require.Equal(t, now.Add(5*time.Second).UnixMilli(), stored.Tasks[0].RetryAfterMS)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *first, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncLease, "released lease is invalid even before its former expiry")
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *first, ActressSyncProblemSnapshotMissing, now), ErrActressSyncLease)
	_, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "too-early", now.Add(5*time.Second-time.Millisecond), time.Minute)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	second, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "second-worker", now.Add(5*time.Second), time.Minute)
	require.NoError(t, err)
	require.Greater(t, second.Generation, first.Generation)
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *first, ActressSyncProblemSnapshotMissing, now.Add(5*time.Second)), ErrActressSyncLease)
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *second, ActressSyncProblemSnapshotMissing, now.Add(65*time.Second)), ErrActressSyncLease, "expiry is exclusive for error recording too")
	third, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "third-worker", now.Add(65*time.Second), time.Minute)
	require.NoError(t, err)
	require.Greater(t, third.Generation, second.Generation)
	require.NoError(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *third, ActressSyncProblemApplyFailed, now.Add(65*time.Second)))
	stored, err = repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Tasks[0].ProblemAttempts)
	require.Equal(t, now.Add(75*time.Second).UnixMilli(), stored.Tasks[0].RetryAfterMS)
	fourth, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "fourth-worker", now.Add(75*time.Second), time.Minute)
	require.NoError(t, err)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *fourth, snapshot, now.Add(75*time.Second))
	require.NoError(t, err)
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *fourth, ActressSyncProblemApplyFailed, now.Add(75*time.Second)), ErrActressSyncLease)
	stored, err = repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", stored.Tasks[0].Status)
	require.Empty(t, stored.Tasks[0].ProblemCode)
	require.Zero(t, stored.Tasks[0].ProblemAttempts)
	require.Zero(t, stored.Tasks[0].RetryAfterMS)
	var history actressSyncTaskProblem
	require.NoError(t, db.Where("task_id = ?", first.TaskID).Take(&history).Error)
	require.Equal(t, 2, history.Attempts, "terminal receipts hide, but need not destroy, diagnostic history")
}

func TestOfflineNameSyncProblemBeforeClaimNeverOverwritesLiveLease(t *testing.T) {
	_, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 3)
	batch := reviews[0]
	batch.Proposals = append(append([]ActressSyncProposal(nil), batch.Proposals...), reviews[1].Proposals...)
	batch.Proposals = append(batch.Proposals, reviews[2].Proposals...)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "mixed-lease-job", batch, snapshot, now)
	require.NoError(t, err)
	live, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "live-worker", now, time.Minute)
	require.NoError(t, err)
	require.NoError(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, now))
	stored, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, live.TaskID, stored.Tasks[0].ID)
	require.Equal(t, "leased", stored.Tasks[0].Status)
	require.Empty(t, stored.Tasks[0].ProblemCode)
	for _, task := range stored.Tasks[1:] {
		require.Equal(t, "pending", task.Status)
		require.Equal(t, ActressSyncProblemSnapshotMissing, task.ProblemCode)
		require.Equal(t, 1, task.ProblemAttempts)
	}
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotInvalid, now), ErrActressSyncLease, "a stale discovery result cannot extend an existing backoff")
	_, err = repo.ApplyOfflineNameSync(t.Context(), *live, snapshot, now)
	require.NoError(t, err, "preclaim failures cannot fence another worker's valid lease")
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
	stored, err = repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, stored.Cancelled)
	require.Equal(t, "applied", stored.Tasks[0].Status)
	for _, task := range stored.Tasks[1:] {
		require.Equal(t, "cancelled", task.Status)
		require.Empty(t, task.ProblemCode)
		require.Zero(t, task.RetryAfterMS)
		require.Zero(t, task.ProblemAttempts)
	}
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, now.Add(time.Hour)), ErrActressSyncLease)
}

func TestOfflineNameSyncProblemBackoffCannotStarveLaterTask(t *testing.T) {
	_, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 2)
	batch := reviews[0]
	batch.Proposals = append(append([]ActressSyncProposal(nil), batch.Proposals...), reviews[1].Proposals...)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "per-task-backoff", batch, snapshot, now)
	require.NoError(t, err)
	first, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "first", now, time.Minute)
	require.NoError(t, err)
	require.NoError(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *first, ActressSyncProblemApplyFailed, now))
	second, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "second", now, time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, first.TaskID, second.TaskID)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *second, snapshot, now)
	require.NoError(t, err)
}

func TestOfflineNameSyncProblemBackoffIsPersistentlyBoundedAndReadOnlyForIdentity(t *testing.T) {
	_, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "bounded-backoff", review, snapshot, now)
	require.NoError(t, err)
	actorBefore, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
	require.NoError(t, err)
	at := now
	for attempt := 1; attempt <= 20; attempt++ {
		require.NoError(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, at))
		stored, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
		require.NoError(t, err)
		task := stored.Tasks[0]
		require.Equal(t, min(attempt, 16), task.ProblemAttempts)
		delay := time.Duration(task.RetryAfterMS-at.UnixMilli()) * time.Millisecond
		require.GreaterOrEqual(t, delay, 5*time.Second)
		require.LessOrEqual(t, delay, 5*time.Minute)
		if attempt >= 7 {
			require.Equal(t, 5*time.Minute, delay)
		}
		require.Equal(t, "pending", task.Status)
		require.Empty(t, task.Reason, "operational errors are not terminal receipt reasons")
		at = time.UnixMilli(task.RetryAfterMS).UTC()
	}
	actorAfter, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
	require.NoError(t, err)
	require.Equal(t, actorBefore, actorAfter)
	stored, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, snapshot.Digest(), stored.SnapshotDigest)
	require.Equal(t, snapshot.Digest(), stored.Tasks[0].SnapshotDigest, "blocked evidence is never substituted")
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
}

func TestOfflineNameSyncProblemRejectsUnsafeCodesAndInvalidInputs(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "problem-inputs", review, snapshot, now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	for _, code := range []string{"", "permission denied: /private/user/path", strings.Repeat("x", 4096)} {
		require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, code, now), ErrActressSyncConflict)
		require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *lease, code, now), ErrActressSyncConflict)
	}
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), "", ActressSyncProblemSnapshotMissing, now), ErrActressSyncConflict)
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, time.Time{}), ErrActressSyncConflict)
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), ActressSyncLease{}, ActressSyncProblemApplyFailed, now), ErrActressSyncLease)
	require.ErrorIs(t, repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *lease, ActressSyncProblemApplyFailed, time.Time{}), ErrActressSyncLease)
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), "name-"+strings.Repeat("0", 64), ActressSyncProblemSnapshotMissing, now), gorm.ErrRecordNotFound)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestOfflineNameSyncProblemStorageFailureRollsBackLeaseAndDiagnostics(t *testing.T) {
	for _, operation := range []string{"preclaim", "postclaim"} {
		t.Run(operation, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "problem-fault", review, snapshot, now)
			require.NoError(t, err)
			var lease *ActressSyncLease
			if operation == "postclaim" {
				lease, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "failed-worker", now, time.Minute)
				require.NoError(t, err)
			}
			before := previewDatabaseState(t, db)
			injected, remove := injectSyncStorageFault(t, db, syncStorageFault{name: "persist operational problem", callback: "create", table: "actress_sync_task_problems", after: true})
			if operation == "postclaim" {
				err = repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *lease, ActressSyncProblemApplyFailed, now)
			} else {
				err = repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, now)
			}
			remove()
			require.ErrorIs(t, err, injected)
			require.Equal(t, before, previewDatabaseState(t, db))
			var count int64
			require.NoError(t, db.Model(&actressSyncTaskProblem{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestOfflineNameSyncQueueMigrationRollbackRetiresBlockedGrants(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "problem-migration", review, snapshot, now)
	require.NoError(t, err)
	require.NoError(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotInvalid, now))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 26)
	require.NoError(t, err)
	stored, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "invalidated", stored.Tasks[0].Status)
	require.Equal(t, "migration_rollback", stored.Tasks[0].Reason)
	require.Empty(t, stored.Tasks[0].ProblemCode)
	var problem actressSyncTaskProblem
	require.NoError(t, db.Where("task_id = ?", stored.Tasks[0].ID).Take(&problem).Error)
	require.Equal(t, ActressSyncProblemSnapshotInvalid, problem.ProblemCode)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	due, err := repo.DueOfflineNameSyncJobs(t.Context(), now.Add(time.Hour), "", 0)
	require.NoError(t, err)
	require.Empty(t, due)
	_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "rollback-no-regrant", review, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncProtected)
	duplicate, err := repo.OfflineNameSyncJobByRequestKey(t.Context(), "problem-migration")
	require.NoError(t, err)
	require.Equal(t, job.RequestHash, duplicate.RequestHash)
}
