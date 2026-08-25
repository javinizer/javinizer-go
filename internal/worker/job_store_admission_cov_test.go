package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
)

// freshStore returns an empty JobStore with no DB seams.
func freshStore(t *testing.T) *JobStore {
	t.Helper()
	return NewJobStore(nil, nil, nil, "", nil, nil)
}

func seedJobLifecycle(t *testing.T, s *JobStore, status models.JobStatus, phase string) *BatchJob {
	t.Helper()
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = status
	job.lifecycle.SetCurrentPhase(phase)
	id := job.ID.String()
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	_ = id
	return job
}

func TestAcquireEditAccessMatrix(t *testing.T) {
	s := freshStore(t)

	t.Run("unknown is not-found", func(t *testing.T) {
		_, _, err := s.AcquireEditAccess("nope")
		require.ErrorIs(t, err, ErrJobNotFound)
	})
	t.Run("tombstoned is gone", func(t *testing.T) {
		s2 := freshStore(t)
		s2.tombstones.Mark("gone-job")
		_, _, err := s2.AcquireEditAccess("gone-job")
		require.ErrorIs(t, err, ErrJobGone)
	})
	t.Run("running scrape phase is busy", func(t *testing.T) {
		s3 := freshStore(t)
		job := seedJobLifecycle(t, s3, models.JobStatusRunning, string(JobPhaseScrape))
		_, _, err := s3.AcquireEditAccess(job.ID.String())
		require.ErrorIs(t, err, ErrEditNotAdmitted)
	})
	t.Run("completed admits", func(t *testing.T) {
		s4 := freshStore(t)
		job := seedJobLifecycle(t, s4, models.JobStatusCompleted, "")
		gotJob, release, err := s4.AcquireEditAccess(job.ID.String())
		require.NoError(t, err)
		require.NotNil(t, gotJob)
		release()
	})
	t.Run("apply phase admits", func(t *testing.T) {
		s5 := freshStore(t)
		job := seedJobLifecycle(t, s5, models.JobStatusRunning, string(JobPhaseApply))
		_, release, err := s5.AcquireEditAccess(job.ID.String())
		require.NoError(t, err)
		release()
	})
	t.Run("deleted lifecycle is gone even without tombstone", func(t *testing.T) {
		s6 := freshStore(t)
		job := seedJobLifecycle(t, s6, models.JobStatusCompleted, "")
		job.lifecycle.SetDeleted(true)
		_, _, err := s6.AcquireEditAccess(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
}

func TestAcquireRescrapeAccessMatrix(t *testing.T) {
	t.Run("pending admits", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusPending, "")
		ctrl, release, err := s.AcquireRescrapeAccess(job.ID.String())
		require.NoError(t, err)
		require.NotNil(t, ctrl)
		release()
	})
	t.Run("running scrape rejected", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusRunning, string(JobPhaseScrape))
		_, _, err := s.AcquireRescrapeAccess(job.ID.String())
		var busy *EditPhaseBusyError
		require.ErrorAs(t, err, &busy)
	})
	// The acquired release is returned on the controlled seam type-assertion
	// failure path; *BatchJob from newBatchJob always satisfies ControlledJob.
	t.Run("unknown is not-found", func(t *testing.T) {
		s := freshStore(t)
		_, _, err := s.AcquireRescrapeAccess("nope")
		require.ErrorIs(t, err, ErrJobNotFound)
	})
}

func TestAcquireExclusionAccessMatrix(t *testing.T) {
	t.Run("running phase rejects exclusion", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusRunning, string(JobPhaseApply))
		_, _, err := s.AcquireExclusionAccess(job.ID.String())
		require.ErrorIs(t, err, ErrEditNotAdmitted)
	})
	t.Run("completed admits", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		gotJob, release, err := s.AcquireExclusionAccess(job.ID.String())
		require.NoError(t, err)
		require.NotNil(t, gotJob)
		release()
	})
}

func TestAcquireSharedLeaseArms(t *testing.T) {
	s := freshStore(t)
	_, err := s.AcquireSharedLease("nope")
	require.ErrorIs(t, err, ErrJobNotFound)
	s.tombstones.Mark("gone")
	_, err = s.AcquireSharedLease("gone")
	require.ErrorIs(t, err, ErrJobGone)
}

func TestJobStoreGoneLookups(t *testing.T) {
	s := freshStore(t)
	_, ok := s.GetBatchJob("nope")
	assert.False(t, ok)
	s.tombstones.Mark("dead")
	_, ok = s.GetBatchJob("dead")
	assert.False(t, ok)
	assert.True(t, s.IsTombstoned("dead"))
	assert.True(t, s.JobGone("dead"))
	assert.False(t, s.JobGone("fresh"))
}

func TestDeleteJobTypedArms(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		s := freshStore(t)
		require.ErrorIs(t, s.DeleteJob("nope"), ErrJobNotFound)
	})
	t.Run("tombstoned", func(t *testing.T) {
		s := freshStore(t)
		s.tombstones.Mark("gone")
		require.ErrorIs(t, s.DeleteJob("gone"), ErrJobGone)
	})
	t.Run("running is untyped busy error", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusRunning, "")
		err := s.DeleteJob(job.ID.String())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete running job")
	})
}

func TestPersistJobSkipsTombstonedAndDeleted(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	jobRepo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil).Maybe()
	s := NewJobStore(jobRepo, nil, nil, "", nil, nil)
	job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
	s.tombstones.Mark(job.ID.String())
	require.ErrorIs(t, s.PersistJob(job), ErrJobGone)
	s.tombstones.Unmark(job.ID.String())
	job.lifecycle.SetDeleted(true)
	require.ErrorIs(t, s.PersistJob(job), ErrJobGone)
}
