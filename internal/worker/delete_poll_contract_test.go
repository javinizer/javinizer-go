package worker_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func deletePollJob(t *testing.T) (*worker.JobStore, worker.BatchJobInterface) {
	t.Helper()
	store := worker.NewInMemoryJobStore()
	job := store.CreateJob([]string{"movie.mp4"})
	job.SetJobStatus(models.JobStatusCompleted)
	return store, job
}

// Waiting in the synctest bubble establishes that DeleteJob is parked on its
// real polling timer and the admission caller is parked on the real condition
// variable. No scheduler sleeps, barrier mutation or internal hooks are needed.
func TestDeletePollDrainsEditorBeforeRejectingQueuedAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, job := deletePollJob(t)
		_, release, err := store.AcquireEditAccess(job.GetID())
		require.NoError(t, err)
		defer func() {
			if release != nil {
				release()
			}
		}()
		deleted := make(chan error, 1)
		go func() { deleted <- store.DeleteJob(job.GetID()) }()
		synctest.Wait()
		select {
		case err := <-deleted:
			t.Fatalf("delete crossed the held edit lease: %v", err)
		default:
		}

		admitted := make(chan error, 1)
		go func() {
			_, release, err := store.AcquireEditAccess(job.GetID())
			if release != nil {
				release()
			}
			admitted <- err
		}()
		synctest.Wait()
		select {
		case err := <-admitted:
			t.Fatalf("queued delete allowed a later edit admission: %v", err)
		default:
		}
		require.Equal(t, models.JobStatusCompleted, job.GetJobStatus(), "drain must not cancel an active editor")
		release()
		release = nil
		require.NoError(t, <-deleted)
		require.ErrorIs(t, <-admitted, worker.ErrJobGone)
		require.True(t, store.JobGone(job.GetID()))
		require.ErrorIs(t, store.PersistJobByID(job.GetID()), worker.ErrJobGone, "a deleted job must not be resurrected")
		require.ErrorIs(t, store.DeleteJob(job.GetID()), worker.ErrJobGone)
	})
}

type deletePollUnusedWorkflow struct{ workflow.WorkflowInterface }

func TestDeletePollRunningPhaseRejectionReleasesIntentForCancelAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, job := deletePollJob(t)
		job.SetWorkflow(&deletePollUnusedWorkflow{})
		_, release, err := store.AcquireEditAccess(job.GetID())
		require.NoError(t, err)
		defer func() {
			if release != nil {
				release()
			}
		}()
		deleted := make(chan error, 1)
		go func() { deleted <- store.DeleteJob(job.GetID()) }()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		started := make(chan error, 1)
		go func() { started <- job.StartApply(ctx, worker.ApplyPhaseConfig{}) }()
		synctest.Wait()
		require.Equal(t, models.JobStatusRunning, job.GetJobStatus())
		require.EqualError(t, <-deleted, "cannot delete running job")
		require.False(t, store.JobGone(job.GetID()))
		cancel()
		require.NoError(t, <-started, "cancelled queued phase must never invoke the workflow")
		require.ErrorContains(t, job.Wait(), "cancelled")
		require.Empty(t, job.GetCurrentPhase())
		// The original edit is still held. A fresh shared admission proves both
		// the rejected delete and cancelled phase deregistered their writer intent.
		_, secondRelease, err := store.AcquireEditAccess(job.GetID())
		require.NoError(t, err)
		secondRelease()
		release()
		release = nil
		require.NoError(t, store.DeleteJob(job.GetID()))
		require.True(t, store.JobGone(job.GetID()))
	})
}

func TestDeletePollTimeoutRetainsJobAndAllowsRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, job := deletePollJob(t)
		_, release, err := store.AcquireEditAccess(job.GetID())
		require.NoError(t, err)
		defer func() {
			if release != nil {
				release()
			}
		}()
		// Synctest advances the production deadline while this lease remains held;
		// the test neither shortens the timeout nor alters admission internals.
		started := time.Now()
		require.ErrorContains(t, store.DeleteJob(job.GetID()), "delete timed out waiting for an in-flight operation")
		require.GreaterOrEqual(t, time.Since(started), 5*time.Second, "the production drain budget must not be shortened")
		require.LessOrEqual(t, time.Since(started), 5*time.Second+25*time.Millisecond, "the polling loop must stop at the first tick after its deadline")
		require.False(t, store.JobGone(job.GetID()))
		require.Equal(t, models.JobStatusCompleted, job.GetJobStatus())
		_, secondRelease, err := store.AcquireEditAccess(job.GetID())
		require.NoError(t, err, "timed-out writer must not strand future edits")
		secondRelease()
		release()
		release = nil
		require.NoError(t, store.DeleteJob(job.GetID()))
		require.True(t, store.JobGone(job.GetID()))
	})
}
