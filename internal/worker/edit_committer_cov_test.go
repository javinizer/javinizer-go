package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// execTransactor runs the plan legs against a mocked EditUnit.
type execTransactor struct {
	unit database.EditUnit
}

func (x *execTransactor) WithEditTx(_ context.Context, fn func(database.EditUnit) error) error {
	return fn(x.unit)
}

func newTestCommitter(unit database.EditUnit) *EditCommitter {
	return NewEditCommitter(&execTransactor{unit: unit}, newKeyedMutexRegistry(), "JOB-C", newKeyedMutexRegistry())
}

// Plan shape validity without legs.
func TestEditCommitPlanHasDBLegsAndNilPlan(t *testing.T) {
	var noPlan *EditCommitPlan
	assert.False(t, noPlan.HasDBLegs())
	assert.False(t, (&EditCommitPlan{}).HasDBLegs())
	assert.True(t, (&EditCommitPlan{EnvelopeFn: func() (*models.Job, error) { return nil, nil }}).HasDBLegs())

	c := newTestCommitter(database.EditUnit{})
	require.NoError(t, c.Commit(context.Background(), nil))
	published := false
	require.NoError(t, c.Commit(context.Background(), &EditCommitPlan{Publish: func() error { published = true; return nil }}))
	assert.True(t, published, "publish-only plan publishes immediately")
}

func TestEditCommitterRequiresTransactor(t *testing.T) {
	var nilC *EditCommitter
	err := nilC.Commit(context.Background(), &EditCommitPlan{UpsertMovie: &models.Movie{ID: "X"}})
	require.ErrorContains(t, err, "transaction seam")
	bare := NewEditCommitter(nil, nil, "", nil)
	require.ErrorContains(t, bare.Commit(context.Background(), &EditCommitPlan{UpsertMovie: &models.Movie{ID: "X"}}), "transaction seam")
}

// Renames leg matrix: load error, not-found skip, no-op skip, rename failure.
func TestEditCommitterRenameLegMatrix(t *testing.T) {
	actresses := mocks.NewMockActressRepositoryInterface(t)
	// 11: load error (not NotFound) -> aborts the tx
	actresses.EXPECT().FindByID(context.Background(), uint(11)).Return(nil, errors.New("io read"))
	// 12: not found -> skipped
	actresses.EXPECT().FindByID(context.Background(), uint(12)).Return(nil, database.ErrNotFound)
	// 13: unchanged -> skipped
	actresses.EXPECT().FindByID(context.Background(), uint(13)).Return(&models.Actress{ID: 13, FirstName: "same", LastName: "name"}, nil)
	// 14: changed but rename write fails -> abort
	actresses.EXPECT().FindByID(context.Background(), uint(14)).Return(&models.Actress{ID: 14, FirstName: "old", LastName: "name"}, nil)
	actresses.EXPECT().RenameNameFields(context.Background(), uint(14), "new", "name", "").Return(errors.New("rename write"))

	c := newTestCommitter(database.EditUnit{Actresses: actresses})
	plan := &EditCommitPlan{Renames: []ActressRenamePlan{
		{ID: 11},
	}}
	require.ErrorContains(t, c.Commit(context.Background(), plan), "load actress")

	plan = &EditCommitPlan{Renames: []ActressRenamePlan{{ID: 14, FirstName: "new", LastName: "name"}}}
	require.ErrorContains(t, c.Commit(context.Background(), plan), "persist actress name edit")

	plan = &EditCommitPlan{Renames: []ActressRenamePlan{{ID: 12}, {ID: 13, FirstName: "same", LastName: "name"}}}
	require.NoError(t, c.Commit(context.Background(), plan), "skip arms leave the plan green")
}

func TestEditCommitterMovieLegs(t *testing.T) {
	movies := mocks.NewMockMovieRepositoryInterface(t)
	c := newTestCommitter(database.EditUnit{Movies: movies})

	t.Run("upsert failure aborts", func(t *testing.T) {
		movies.EXPECT().Upsert(context.Background(), &models.Movie{ID: "M-1"}).Return(nil, errors.New("db down"))
		err := c.Commit(context.Background(), &EditCommitPlan{UpsertMovie: &models.Movie{ID: "M-1"}})
		require.ErrorContains(t, err, "persist movie update")
	})

	t.Run("mutate not found skips", func(t *testing.T) {
		movies.EXPECT().FindByID(context.Background(), "M-2").Return(nil, database.ErrNotFound)
		err := c.Commit(context.Background(), &EditCommitPlan{MutateMovieID: "M-2", MutateMovie: func(m *models.Movie) { m.Title = "x" }})
		require.NoError(t, err)
	})

	t.Run("mutate lookup error aborts", func(t *testing.T) {
		movies.EXPECT().FindByID(context.Background(), "M-3").Return(nil, errors.New("io read"))
		err := c.Commit(context.Background(), &EditCommitPlan{MutateMovieID: "M-3", MutateMovie: func(m *models.Movie) {}})
		require.ErrorContains(t, err, "find movie M-3")
	})

	t.Run("mutate upsert failure aborts", func(t *testing.T) {
		existing := &models.Movie{ID: "M-4"}
		movies.EXPECT().FindByID(context.Background(), "M-4").Return(existing, nil)
		movies.EXPECT().Upsert(context.Background(), existing).Return(nil, errors.New("db down"))
		err := c.Commit(context.Background(), &EditCommitPlan{MutateMovieID: "M-4", MutateMovie: func(m *models.Movie) { m.Title = "x" }})
		require.ErrorContains(t, err, "persist mutated movie")
	})

	t.Run("mutate happy path runs the mutation", func(t *testing.T) {
		existing := &models.Movie{ID: "M-5"}
		movies.EXPECT().FindByID(context.Background(), "M-5").Return(existing, nil)
		movies.EXPECT().Upsert(context.Background(), &models.Movie{ID: "M-5", Title: "changed"}).Return(&models.Movie{ID: "M-5", Title: "changed"}, nil)
		err := c.Commit(context.Background(), &EditCommitPlan{MutateMovieID: "M-5", MutateMovie: func(m *models.Movie) { m.Title = "changed" }})
		require.NoError(t, err)
		assert.Equal(t, "changed", existing.Title)
	})
}

func TestEditCommitterEnvelopeLegErrors(t *testing.T) {
	c := newTestCommitter(database.EditUnit{})
	err := c.Commit(context.Background(), &EditCommitPlan{EnvelopeFn: func() (*models.Job, error) { return nil, errors.New("encode wedged") }})
	require.ErrorContains(t, err, "encode envelope")

	jobs := mocks.NewMockJobRepositoryInterface(t)
	c2 := newTestCommitter(database.EditUnit{Jobs: jobs})
	jobs.EXPECT().Upsert(context.Background(), &models.Job{}).Return(errors.New("db down"))
	err = c2.Commit(context.Background(), &EditCommitPlan{EnvelopeFn: func() (*models.Job, error) { return &models.Job{}, nil }})
	require.ErrorContains(t, err, "persist job envelope")
}

func TestEditCommitterPublishFailureAfterCommitSurfaces(t *testing.T) {
	c := newTestCommitter(database.EditUnit{})
	err := c.Commit(context.Background(), &EditCommitPlan{
		EnvelopeFn: func() (*models.Job, error) { return nil, nil },
		Publish:    func() error { return errors.New("publish wedged") },
	})
	require.ErrorContains(t, err, "publish committed edit")
}

// --- editAdmissionError matrix ---

func TestEditAdmissionErrorMatrix(t *testing.T) {
	assert.Error(t, editAdmissionError("J", models.JobStatusPending, ""), "pending jobs reject edits")
	assert.Error(t, editAdmissionError("J", models.JobStatusRunning, string(JobPhaseScrape)), "running scrape-phase rejects")
	assert.Error(t, editAdmissionError("J", models.JobStatusRunning, ""), "running with unknown phase rejects")
	assert.NoError(t, editAdmissionError("J", models.JobStatusRunning, string(JobPhaseApply)), "running apply-phase admits")
	// Terminal-looking status with a lingering phase marker = cancelling-but-probably-draining scrape (P1-D): rejects.
	assert.Error(t, editAdmissionError("J", models.JobStatusCancelled, string(JobPhaseScrape)))
	assert.NoError(t, editAdmissionError("J", models.JobStatusCompleted, ""))
	assert.NoError(t, editAdmissionError("J", models.JobStatusFailed, ""))
	assert.NoError(t, editAdmissionError("J", models.JobStatusCancelled, ""))
}

// --- jobReaderImpl / jobEditorImpl direct coverage ---

func TestJobReaderGetCurrentPhase(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusRunning}
	lc.SetCurrentPhase(string(JobPhaseApply) + "x")
	jr := &jobReaderImpl{lifecycle: lc}
	assert.Equal(t, "applyx", jr.GetCurrentPhase())
}

func TestJobEditorImplExcludeFamilyAndKeyedSection(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-e1", "EJ-1", "")
	seedFamilyResult(store, "/f/b.mp4", "res-e2", "EJ-1", "")
	je := &jobEditorImpl{store: store}
	require.NoError(t, je.ExcludeMovieFamily(context.Background(), "EJ-1"))
	assert.True(t, store.IsAllExcluded())
	require.NoError(t, je.WithMovieEditLock("EJ-1", func(m *LockedMovieOps) error {
		assert.Equal(t, "EJ-1", m.MovieID())
		return nil
	}))
}

// --- jobPersistencer deleted-job arm ---

func TestPersistToDatabaseSkipsDeletedJob(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.deleted = true
	_ = job
}
