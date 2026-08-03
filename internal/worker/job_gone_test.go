package worker

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestPersistJobByID_JobGone pins A13: persisting a job ID that is absent
// from the store must fail safe with ErrJobGone instead of no-oping to nil —
// edit handlers would otherwise answer 200 for state never persisted.
func TestPersistJobByID_JobGone(t *testing.T) {
	s := NewJobStore(nil, nil, nil, "", nil, nil)

	err := s.PersistJobByID(models.NewJobID().String())
	require.Error(t, err, "an absent job ID must not no-op to success")
	assert.True(t, errors.Is(err, ErrJobGone), "err: %v", err)
	assert.Contains(t, err.Error(), "job gone")
}

// TestPersistJobByID_PresentJobPersists pins the other arm: a present job
// does not report the gone sentinel.
func TestPersistJobByID_PresentJobPersists(t *testing.T) {
	s := NewJobStore(nil, nil, nil, "", nil, nil)
	job := &BatchJob{
		ID: models.NewJobID(),
		lifecycle: &JobLifecycle{
			Status: models.JobStatusPending,
			done:   make(chan struct{}),
		},
		results: resultstore.NewFromSnapshot(1, []string{"file1.mp4"}, make(map[string]*resultstore.MovieResult), nil, make(map[string]models.FileMatchInfo), make(map[string]bool), 0, 0, 0),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	// Nil job repository: persist degrades to a logged no-op (persistToDatabase) —
	// the point here is only that a LIVE job never reports ErrJobGone.
	err := s.PersistJobByID(job.ID.String())
	assert.NoError(t, err)
}
