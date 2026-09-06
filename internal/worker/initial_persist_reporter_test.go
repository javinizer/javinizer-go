package worker

// Regression pins for #248 codex P2: the create-time jobs-row persist failure
// inside JobStore.createJob (shallow disk-full / locked database after
// bootstrap) keeps its best-effort swallow-and-log semantics for the API path
// — the job is constructed, registered, and returned — while a registered
// WithInitialPersistErrorReporter observes the error so one-shot callers (the
// CLI batch runtime) can turn it into a batch startup failure instead of
// advertising an unqueryable batch id.

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingInitialPersist is a JobPersistencer whose PersistJob always fails —
// injected via the store's documented WithPersistence test seam to mimic the
// post-bootstrap jobs-row upsert failure class (#248 codex P2) without a
// sabotaged database handle.
type failingInitialPersist struct {
	err error
}

func (p *failingInitialPersist) PersistJob(*BatchJob) error                     { return p.err }
func (p *failingInitialPersist) PersistJobByID(string) error                    { return p.err }
func (p *failingInitialPersist) DeleteJobFromDB(string) error                   { return p.err }
func (p *failingInitialPersist) LoadJobs(context.Context) ([]models.Job, error) { return nil, nil }
func (p *failingInitialPersist) UpsertJob(*models.Job) error                    { return p.err }

// TestJobStore_CreateJob_InitialPersistFailure_ReporterHook pins the seam: on
// create-time persist failure the reporter receives the error, AND the
// best-effort in-flight semantics stay intact (job constructed + registered).
func TestJobStore_CreateJob_InitialPersistFailure_ReporterHook(t *testing.T) {
	boom := errors.New("upsert failed: simulated disk full")
	var reported []error
	store := NewInMemoryJobStore(
		WithPersistence(&failingInitialPersist{err: boom}),
		WithInitialPersistErrorReporter(func(err error) { reported = append(reported, err) }),
	)

	job := store.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job, "best-effort semantics intact: the job is still constructed and returned")
	_, ok := store.GetJob(job.ID.String())
	assert.True(t, ok, "the job still registers in the store map (API-path behavior unchanged)")

	require.Len(t, reported, 1, "the reporter fires exactly once for the create-time persist")
	assert.ErrorIs(t, reported[0], boom)
}

// TestJobStore_CreateJob_InitialPersistFailure_NoReporter pins the nil-hook
// branch: without a reporter the store's historical swallow-and-log behavior
// is byte-identical (no panic, job returned) — the API path is undisturbed.
func TestJobStore_CreateJob_InitialPersistFailure_NoReporter(t *testing.T) {
	boom := errors.New("upsert failed: simulated disk full")
	store := NewInMemoryJobStore(WithPersistence(&failingInitialPersist{err: boom}))

	job := store.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job)
}

// TestJobStore_CreateJob_SuccessfulPersist_ReporterSilent pins the good path:
// a successful create-time persist never fires the reporter.
func TestJobStore_CreateJob_SuccessfulPersist_ReporterSilent(t *testing.T) {
	called := false
	store := NewInMemoryJobStore(
		WithInitialPersistErrorReporter(func(error) { called = true }),
	)
	job := store.CreateJobBatch([]string{"file1.mp4"})
	require.NotNil(t, job)
	assert.False(t, called)
}
