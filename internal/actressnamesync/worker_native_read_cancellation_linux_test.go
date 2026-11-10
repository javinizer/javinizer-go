package actressnamesync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

// Observe the serial worker without changing repository results. A cancelled
// context would also reject a mistaken write in SQLite, so durable-state checks
// alone would not catch an attempted corruption report or lease acquisition.
type historicalReadShutdownRepository struct {
	Repository
	claims, problems int // Read only after Run has joined.
}

func (r *historicalReadShutdownRepository) ClaimOfflineNameSync(ctx context.Context, jobID, owner string, now time.Time, duration time.Duration) (*database.ActressSyncLease, error) {
	r.claims++
	return r.Repository.ClaimOfflineNameSync(ctx, jobID, owner, now, duration)
}

func (r *historicalReadShutdownRepository) RecordOfflineNameSyncJobProblem(ctx context.Context, jobID, code string, now time.Time) error {
	r.problems++
	return r.Repository.RecordOfflineNameSyncJobProblem(ctx, jobID, code, now)
}

func TestWorkerShutdownDuringHistoricalArtifactReadPreservesPendingJob(t *testing.T) {
	f := newServiceFixture(t)
	admission, _ := f.service(t, f.repo)
	admission.initialize()
	_, job := f.confirm(t, admission)
	oldPath := filepath.Join(f.directory, f.snapshot.Digest()+".ndjson")
	next := f.publish(t, "更新名")
	manifestBefore, err := os.ReadFile(filepath.Join(f.directory, "current.json"))
	require.NoError(t, err)
	before := reloadDatabaseState(t, f.db)
	actorBefore, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)

	// A restarted worker initializes from B, but the durable job only authorizes
	// A. Lease A before Run starts; the native lease-break event proves the
	// worker reached its historical read, rather than startup or queue discovery.
	shutdownRepo := &historicalReadShutdownRepository{Repository: f.repo}
	worker, cancel := f.service(t, shutdownRepo)
	var workerDone chan struct{}
	t.Cleanup(func() {
		cancel()
		if workerDone != nil {
			awaitServiceSignal(t, workerDone)
		}
	})
	events, releaseLease := startNativeArtifactReadLease(t, oldPath)
	workerDone = make(chan struct{})
	go func() { worker.Run(); close(workerDone) }()
	awaitServiceSignal(t, worker.Ready())
	info, err := worker.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, next.Digest(), info.ArtifactDigest)
	require.Equal(t, "reader blocked", awaitServiceSignal(t, events))
	cancel()
	releaseLease()
	awaitServiceSignal(t, workerDone)

	// Shutdown must neither acquire a lease nor classify a valid historical
	// artifact as corrupt. Compare every durable table, including diagnostics.
	require.Zero(t, shutdownRepo.claims)
	require.Zero(t, shutdownRepo.problems)
	require.Equal(t, before, reloadDatabaseState(t, f.db))
	pending, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.False(t, pending.Cancelled)
	require.Len(t, pending.Tasks, 1)
	require.Equal(t, "pending", pending.Tasks[0].Status)
	require.Zero(t, pending.Tasks[0].LeaseGeneration)
	require.Zero(t, pending.Tasks[0].ProblemAttempts)
	require.Empty(t, pending.Tasks[0].ProblemCode)

	// Reopen the real SQLite catalog and start a fresh Run worker. No in-memory
	// wake, cached historical snapshot, or new confirmation may be needed.
	f.reopen(t)
	observer := &observedApplyRepository{Repository: f.repo, applied: make(chan applyResult, 1)}
	restarted, stopContext := f.service(t, observer)
	stop := startService(t, restarted, stopContext)
	applied := awaitServiceSignal(t, observer.applied)
	require.NoError(t, applied.err)
	require.NotNil(t, applied.task)
	require.Equal(t, "applied", applied.task.Status)
	stop()
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Len(t, durable.Tasks, 1)
	task := durable.Tasks[0]
	require.Equal(t, "applied", task.Status)
	require.Equal(t, f.snapshot.Digest(), task.SnapshotDigest)
	require.Equal(t, "旧名", task.BeforeName)
	require.Equal(t, "新名", task.AfterName, "never substitute the current B generation")
	require.EqualValues(t, 1, task.LeaseGeneration)
	require.Empty(t, task.ProblemCode)
	require.Zero(t, task.ProblemAttempts)
	actorAfter, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", actorAfter.JapaneseName)
	require.Equal(t, actorBefore.ID, actorAfter.ID)
	require.Equal(t, actorBefore.DMMID, actorAfter.DMMID)
	require.Equal(t, actorBefore.FirstName, actorAfter.FirstName)
	require.Equal(t, actorBefore.LastName, actorAfter.LastName)
	require.Equal(t, actorBefore.Aliases, actorAfter.Aliases)
	require.Equal(t, actorBefore.ThumbURL, actorAfter.ThumbURL)
	requireServiceCounts(t, f.db, 1, 1)
	manifestAfter, err := os.ReadFile(filepath.Join(f.directory, "current.json"))
	require.NoError(t, err)
	require.Equal(t, manifestBefore, manifestAfter)
	artifactAfter, err := os.ReadFile(oldPath)
	require.NoError(t, err)
	require.Equal(t, f.snapshot.Bytes(), artifactAfter)
}
