package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfflineNameSyncQueueStorageFailuresPropagateAndRollback(t *testing.T) {
	for _, fault := range []syncStorageFault{
		{name: "job admission count", operation: "submit", callback: "row", sql: "SELECT DISTINCT t.job_id"},
		{name: "task admission count", operation: "submit", callback: "row", sql: "SELECT t.id FROM actress_sync_tasks"},
		{name: "due job lookup", operation: "due", callback: "query", table: "actress_sync_jobs"},
		{name: "history cursor lookup", operation: "history", callback: "query", table: "actress_sync_jobs"},
		{name: "history page lookup", operation: "history", callback: "query", table: "actress_sync_jobs", nth: 2},
		{name: "request replay lookup", operation: "replay", callback: "query", table: "actress_sync_jobs"},
		{name: "request replay receipt lookup", operation: "replay", callback: "query", table: "actress_sync_tasks"},
		{name: "problem receipt lookup", operation: "read", callback: "query", table: "actress_sync_task_problems"},
		{name: "preclaim write admission", operation: "preclaim", callback: "raw", sql: "UPDATE actress_sync_jobs SET cancelled"},
		{name: "preclaim job read", operation: "preclaim", callback: "query", table: "actress_sync_jobs"},
		{name: "preclaim claimable task read", operation: "preclaim", callback: "query", table: "actress_sync_tasks"},
		{name: "expired preclaim lease release", operation: "preclaim-expired", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "preclaim previous problem read", operation: "preclaim", callback: "query", table: "actress_sync_task_problems"},
		{name: "postclaim lease release", operation: "postclaim", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "postclaim previous problem read", operation: "postclaim", callback: "query", table: "actress_sync_task_problems"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			var job *ActressSyncJob
			var lease *ActressSyncLease
			var err error
			if fault.operation != "submit" {
				job, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "queue-storage", review, snapshot, now)
				require.NoError(t, err)
			}
			if fault.operation == "postclaim" || fault.operation == "preclaim-expired" {
				lease, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "queue-storage-worker", now, time.Minute)
				require.NoError(t, err)
			}
			if fault.operation == "preclaim-expired" {
				now = now.Add(time.Minute)
			}
			before := previewDatabaseState(t, db)
			injected, remove := injectSyncStorageFault(t, db, fault)
			var result any
			switch fault.operation {
			case "submit":
				result, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "queue-storage", review, snapshot, now)
			case "due":
				result, err = repo.DueOfflineNameSyncJobs(t.Context(), now, "", 1)
			case "history":
				result, err = repo.ListOfflineNameSyncJobs(t.Context(), job.ID, 1)
			case "replay":
				result, err = repo.OfflineNameSyncJobByRequestKey(t.Context(), "queue-storage")
			case "read":
				result, err = repo.OfflineNameSyncJob(t.Context(), job.ID)
			case "preclaim", "preclaim-expired":
				err = repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, now)
			case "postclaim":
				err = repo.RecordOfflineNameSyncLeaseProblem(t.Context(), *lease, ActressSyncProblemApplyFailed, now)
			}
			remove()
			require.ErrorIs(t, err, injected)
			require.Nil(t, result)
			require.Equal(t, before, previewDatabaseState(t, db))
		})
	}
}
