package worker

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Cancelling a job must abort in-flight task contexts and remember the
// request, so tasks completing afterwards settle as cancelled instead of
// mutating data and lingering on an expiring lease.
func TestCancelJobCancelsRunningTask(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB:          db,
		ActressRepo: database.NewActressRepository(db),
		MovieRepo:   database.NewMovieRepository(db),
	})

	job := models.ActressSyncJob{ID: "job-cancel", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)

	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()
	manager.taskMu.Lock()
	manager.runningTasks["task-1"] = []trackedSyncTask{{jobID: job.ID, cancel: taskCancel}}
	manager.taskMu.Unlock()

	require.NoError(t, manager.CancelJob(job.ID))
	require.ErrorIs(t, taskCtx.Err(), context.Canceled)
	require.True(t, manager.isTaskCancelled("task-1"))

	var stored models.ActressSyncJob
	require.NoError(t, db.First(&stored, "id = ?", job.ID).Error)
	require.True(t, stored.CancelRequested)

	// untrack tolerates stale runs: a retry re-registered under the same task
	// ID must keep its entry (and its live context).
	manager.taskMu.Lock()
	newerCtx, newerCancel := context.WithCancel(context.Background())
	manager.runningTasks["task-1"] = []trackedSyncTask{{jobID: job.ID, cancel: newerCancel, run: 2}}
	manager.taskMu.Unlock()
	manager.untrackTask("task-1", 1)
	require.NoError(t, newerCtx.Err())
	manager.taskMu.Lock()
	runs, ok := manager.runningTasks["task-1"]
	manager.taskMu.Unlock()
	require.True(t, ok)
	require.Len(t, runs, 1)
	require.Equal(t, uint64(2), runs[0].run)
	newerCancel()
	manager.untrackTask("task-1", 2)
	manager.untrackTask("unknown", 3)

	// Multiple runs sharing one task ID: each keeps its own cancel entry.
	manager.taskMu.Lock()
	a := context.Background()
	_, c1 := context.WithCancel(a)
	_, c2 := context.WithCancel(a)
	manager.runningTasks["two"] = []trackedSyncTask{{jobID: "j", cancel: c1, run: 1}, {jobID: "j", cancel: c2, cancelled: true, run: 2}}
	manager.taskMu.Unlock()
	require.True(t, manager.isTaskCancelled("two"), "any cancelled run means the task is cancelled")
	c1()
	c2()
	manager.untrackTask("two", 1)
	require.True(t, manager.isTaskCancelled("two"), "second run remains tracked")
	manager.untrackTask("two", 2)
	require.False(t, manager.isTaskCancelled("two"))
	manager.untrackTask("two", 99)
}

// Stale selections from merge-deleted actresses must not reject the whole job.
func TestCreateJobSkipsMergedAwayActresses(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	repo := database.NewActressRepository(db)
	valid := &models.Actress{JapaneseName: "still here"}
	require.NoError(t, repo.Create(context.Background(), valid))
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: repo, MovieRepo: database.NewMovieRepository(db)})

	job, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{valid.ID, 999999}})
	require.NoError(t, err)
	tasks, err := manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskPending, tasks[0].Status)

	_, _, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{999998, 999999}})
	require.Error(t, err)
}

// CountTasks mirrors the task lists: view=active counts running, view=
// diagnostics counts the diagnostic set.
// The dispatch recovery ticker must actually recover stale leases: plant an
// expired lease on a running task and watch the manager hand it back.
func TestDispatchRecoveryTickReclaimsExpiredLease(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db)})
	manager.recoveryInterval = 15 * time.Millisecond

	job := models.ActressSyncJob{ID: "job-recover", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	actress := &models.Actress{JapaneseName: "占位"}
	require.NoError(t, database.NewActressRepository(db).Create(context.Background(), actress))
	expired := time.Now().UTC().Add(-time.Minute)
	task := models.ActressSyncTask{
		ID: "task-stale", JobID: job.ID, Label: "l", DedupeKey: "actress:" + strconv.FormatUint(uint64(actress.ID), 10),
		Status: models.ActressSyncTaskRunning, Stage: "running",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &actress.ID, LeaseOwner: "dead-owner", LeaseToken: "tok", LeaseExpiresAt: &expired,
	}
	require.NoError(t, db.Create(&task).Error)

	manager.Start()
	t.Cleanup(manager.Stop)
	require.Eventually(t, func() bool {
		var stored models.ActressSyncTask
		return db.First(&stored, "id = ?", "task-stale").Error == nil && stored.LeaseOwner == ""
	}, 2*time.Second, 10*time.Millisecond, "recovery tick must reclaim stale leases")
}

// When a job-cancelled task cannot settle its completion, that must not panic.
func TestRunTaskSettleWarnsOnCompletionError(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db)})

	job := models.ActressSyncJob{ID: "job-warn", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	actress := &models.Actress{JapaneseName: "settle-fail"}
	require.NoError(t, database.NewActressRepository(db).Create(context.Background(), actress))

	taskCtx, cancel := context.WithCancel(context.Background())
	task := &models.ActressSyncTask{
		ID: "task-settle-warn", JobID: job.ID, Label: "l", DedupeKey: "actress:" + strconv.FormatUint(uint64(actress.ID), 10),
		Status: models.ActressSyncTaskRunning, Stage: "running",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &actress.ID, LeaseToken: "missing-row-in-db", LeaseExpiresAt: nil,
	}
	manager.taskMu.Lock()
	manager.runningTasks[task.ID] = []trackedSyncTask{{jobID: job.ID, cancel: cancel, cancelled: true, run: 1}}
	manager.taskMu.Unlock()
	cancel()

	manager.wg.Add(1)
	manager.runTaskWithContext(taskCtx, task, time.Second, nil, nil)
}

func TestManagerCountTasks(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db)})

	job := models.ActressSyncJob{ID: "job-count", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	leaseUntil := time.Now().UTC().Add(time.Minute)
	mk := func(id, status, stage, outcome, warning string) models.ActressSyncTask {
		return models.ActressSyncTask{
			ID: id, JobID: job.ID, Label: id, DedupeKey: id, Status: status, Stage: stage,
			Messages: []string{}, UpdatedFields: []string{}, Warning: warning,
		}
	}
	running := mk("t-run", models.ActressSyncTaskRunning, "running", "", "")
	running.LeaseToken = "tok"
	running.LeaseExpiresAt = &leaseUntil
	done := time.Now()
	success := mk("t-ok", models.ActressSyncTaskCompleted, "completed", "updated", "")
	success.CompletedAt = &done
	flagged := mk("t-warn", models.ActressSyncTaskCompleted, "completed", "updated", "slow")
	flagged.CompletedAt = &done
	for _, task := range []*models.ActressSyncTask{&running, &success, &flagged} {
		require.NoError(t, db.Create(task).Error)
	}

	total, err := manager.CountTasks(job.ID, "")
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	active, err := manager.CountTasks(job.ID, "active")
	require.NoError(t, err)
	require.Equal(t, int64(1), active)
	diag, err := manager.CountTasks(job.ID, "diagnostics")
	require.NoError(t, err)
	require.Equal(t, int64(1), diag, "warning-bearing tasks only")
}
