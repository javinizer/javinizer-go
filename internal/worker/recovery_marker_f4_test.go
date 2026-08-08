package worker

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPersistence struct {
	noopJobPersistence
	persisted []*BatchJob
}

func (r *recordingPersistence) PersistJob(j *BatchJob) error {
	r.persisted = append(r.persisted, j)
	return nil
}

// audit F4: a terminal row must never reload with a phase marker — no
// goroutine survives the restart to clear it, so edits would 409 forever.
func TestReconstructBatchJobClearsTerminalPhaseMarker(t *testing.T) {
	row, err := jobpersist.Encode(jobpersist.Snapshot{
		ID:           "f4-job",
		Status:       models.JobStatusCancelled,
		CurrentPhase: "apply",
		Files:        []string{"/f/a.mp4"},
	})
	require.NoError(t, err)
	rec := &recordingPersistence{}
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), persistence: rec}
	job := jq.reconstructBatchJob(row)
	require.NotNil(t, job)
	assert.Equal(t, "", job.lifecycle.CurrentPhase(), "stale marker cleared on reconstruct")
	require.Len(t, rec.persisted, 1, "cleared row re-persisted once")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
}

// audit F4: a wedged persister must not stop the marker clear — the in-memory
// row is already sane; only the re-persist failed.
func TestReconstructBatchJobMarkerClearPersistFailure(t *testing.T) {
	row, err := jobpersist.Encode(jobpersist.Snapshot{
		ID:           "f4-pfail",
		Status:       models.JobStatusCancelled,
		CurrentPhase: "apply",
		Files:        []string{"/f/a.mp4"},
	})
	require.NoError(t, err)
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), persistence: failPersistence{}}
	job := jq.reconstructBatchJob(row)
	require.NotNil(t, job)
	assert.Equal(t, "", job.lifecycle.CurrentPhase(), "marker cleared even when the re-persist fails")
}

type failPersistence struct {
	noopJobPersistence
}

func (failPersistence) PersistJob(*BatchJob) error { return errors.New("db wedged") }

// Non-terminal rows keep their marker (the drain-then-clear contract).
func TestReconstructBatchJobKeepsRunningPhaseMarker(t *testing.T) {
	row, err := jobpersist.Encode(jobpersist.Snapshot{
		ID:           "f4-running",
		Status:       models.JobStatusRunning,
		CurrentPhase: "apply",
		Files:        []string{"/f/a.mp4"},
	})
	require.NoError(t, err)
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}
	job := jq.reconstructBatchJob(row)
	require.NotNil(t, job)
	assert.Equal(t, "apply", job.lifecycle.CurrentPhase(), "running rows keep the marker (orphan recovery owns them next)")
}

// audit F4: orphan recovery flips Running→Failed AND clears the marker before
// persisting — the phase goroutine died with the old process.
func TestRecoverOrphanedJobsClearsPhaseMarker(t *testing.T) {
	rec := &recordingPersistence{}
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), persistence: rec}
	job := &BatchJob{
		ID:        models.MustJobID("orphan-1"),
		lifecycle: &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})},
	}
	job.lifecycle.SetCurrentPhase("apply")
	jq.jobs[job.ID] = job
	jq.recoverOrphanedJobs()
	assert.Equal(t, models.JobStatusFailed, job.lifecycle.GetJobStatus())
	assert.Equal(t, "", job.lifecycle.CurrentPhase(), "marker cleared before persist")
	require.Len(t, rec.persisted, 1)
}
