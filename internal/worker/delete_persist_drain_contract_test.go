package worker

import (
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type deleteDrainPersistence struct {
	noopJobPersistence
	resume   <-chan struct{}
	persists int
	deletes  int
}

func (p *deleteDrainPersistence) PersistJob(*BatchJob) error {
	p.persists++
	<-p.resume
	return nil
}

func (p *deleteDrainPersistence) DeleteJobFromDB(string) error {
	p.deletes++
	return nil
}

func TestDeleteJobRechecksDeletedLifecycleAfterPersistenceDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resume := make(chan struct{})
		persistence := &deleteDrainPersistence{resume: resume}
		store := NewInMemoryJobStore(WithPersistence(persistence))
		job := seedJobLifecycle(t, store, models.JobStatusCompleted, "")
		flight := store.persistFlightForJob(job)
		defer func() {
			if resume != nil {
				close(resume)
			}
		}()
		persisted := make(chan error, 1)
		go func() { persisted <- store.PersistJob(job) }()
		synctest.Wait()
		require.Equal(t, 1, persistence.persists, "the real store persistence path must reach its backend before deletion")

		deleted := make(chan error, 1)
		go func() { deleted <- store.DeleteJob(job.ID.String()) }()
		synctest.Wait()
		flight.mu.Lock()
		fenced := flight.exclusive
		flight.mu.Unlock()
		require.True(t, fenced, "deletion must be waiting after admission, while persistence drains")
		select {
		case err := <-deleted:
			t.Fatalf("deletion crossed an active persistence flight: %v", err)
		default:
		}

		job.lifecycle.SetDeleted(true)
		close(resume)
		resume = nil
		require.NoError(t, <-persisted)
		require.ErrorIs(t, <-deleted, ErrJobGone)
		require.Zero(t, persistence.deletes, "the late lifecycle change must prevent a second durable deletion")
		require.False(t, store.IsTombstoned(job.ID.String()), "a refused deletion must not publish a tombstone")
		require.Same(t, job, store.jobs[job.ID], "the refusal must leave the registered job untouched")
		require.ErrorIs(t, store.PersistJob(job), ErrJobGone, "subsequent persistence must observe the deleted lifecycle")
		require.Equal(t, 1, persistence.persists, "the stale job must never reach the persistence backend again")
		unfence, err := flight.acquireExclusive(t.Context())
		require.NoError(t, err, "refusal must release the persistence fence")
		unfence()
		release, ok := job.admission.TryAdmitExclusive()
		require.True(t, ok, "refusal must release the admission lease")
		release()
	})
}
