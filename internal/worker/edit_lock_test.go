package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedOneMovie(t *testing.T, s *JobStore, filePath, movieID string) *BatchJob {
	t.Helper()
	job := s.CreateJobBatch([]string{filePath})
	job.results.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Poster: models.PosterState{PosterURL: "orig.jpg"}},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	return job
}

// TestWithMovieEditLock_LockedCoresCannotReenter — a handler-style callback
// invoking TWO locked cores completes exactly once under a single
// acquisition: cores are exposed only via LockedMovieOps which carries no
// re-locking surface, so composition cannot self-deadlock
// (poster-write-hardening tasks: WithMovieEditLock callback contract).
func TestWithMovieEditLock_LockedCoresCannotReenter(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-001")
	pe := job.posterEditor

	calls := 0
	errc := pe.WithMovieEditLock("AAA-001", func(m *LockedMovieOps) error {
		calls++
		// Handler-style composition: save first, crop second, one acquisition.
		if err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "AAA-001", Title: "T"}); err != nil {
			return err
		}
		return m.UpdatePosterCrop("crop1.jpg", nil, false)
	})
	require.NoError(t, errc)
	assert.Equal(t, 1, calls, "callback must run exactly once")
	res, _ := job.results.GetMovieResult("/v/a.mp4")
	assert.Equal(t, "crop1.jpg", res.Movie.Poster.CroppedPosterURL)
	assert.Equal(t, "T", res.Movie.Title)
}

// TestJobEditLock_SerializesSaveAgainstPosterSourceChange — a held family
// op (gated pause inside an AtomicUpdateFileResult publication on the lock
// section) makes a concurrent UpdatePosterFromURL wait; final state is
// last-op-wins, never interleaved geometry resurrection.
func TestJobEditLock_SerializesSaveAgainstPosterSourceChange(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-002")
	pe := job.posterEditor

	inLock := make(chan struct{})
	releaseLock := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- pe.WithMovieEditLock("AAA-002", func(m *LockedMovieOps) error {
			close(inLock)
			<-releaseLock
			return m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "AAA-002", Title: "Saved", Poster: models.PosterState{PosterURL: "orig.jpg"}})
		})
	}()
	<-inLock

	urlDone := make(chan error, 1)
	go func() {
		urlDone <- pe.UpdatePosterFromURL(context.Background(), "AAA-002", "https://x/p.jpg", "https://x/c.jpg")
	}()

	select {
	case err := <-urlDone:
		t.Fatalf("UpdatePosterFromURL completed while family key held (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseLock)
	require.NoError(t, <-done)
	require.NoError(t, <-urlDone)

	res, _ := job.results.GetMovieResult("/v/a.mp4")
	assert.Equal(t, "https://x/p.jpg", res.Movie.Poster.PosterURL, "from-URL op landed last under the key")
}

// TestPersist_EncodeFailure_SurfacedLikeRepoError — an envelope encode
// (marshal) failure rides the same typed error channel as a repo upsert
// failure (D2/D4 pivot), instead of vanishing into persist_error state only.
func TestPersist_EncodeFailure_SurfacedLikeRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-003")

	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, errors.New("forced marshal failure") }
	defer func() { jobpersist.MarshalFn = orig }()

	err := store.PersistJob(job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode job")
}

// TestJobStorePersist_ParallelEdits_EnvelopeNeverRegresses — two concurrent
// edits to DIFFERENT movies of the same job: the final committed envelope
// contains BOTH even when the first edit's publish is gated past the second
// edit's publication (envelope lock + candidate-merged snapshot at commit).
func TestJobStorePersist_ParallelEdits_EnvelopeNeverRegresses(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := store.CreateJobBatch([]string{"/v/a.mp4", "/v/b.mp4"})
	job.results.UpdateFileResult("/v/a.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/v/a.mp4", MovieID: "A-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "A-1", Title: "a"},
	})
	job.results.UpdateFileResult("/v/b.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/v/b.mp4", MovieID: "B-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "B-1", Title: "b"},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	require.NoError(t, store.PersistJobByID(job.ID.String()))

	var wg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	run := func(movieID, title string) {
		defer wg.Done()
		if err := job.posterEditor.UpdateMovieFamily(context.Background(), movieID, "", &models.Movie{ID: movieID, Title: title}, FamilySaveOptions{}); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go run("A-1", "a-edit")
		go run("B-1", "b-edit")
	}
	wg.Wait()
	require.Empty(t, errs)

	row, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	require.NotNil(t, row)
	env := row.Results
	assert.Contains(t, env, "a-edit", "final envelope must contain edit A")
	assert.Contains(t, env, "b-edit", "final envelope must contain edit B")
}

// TestDeleteJob_InFlightPersistDoesNotResurrect — a persist that began
// before DeleteJob committed must not re-insert the jobs row (tombstone
// re-check inside the envelope lock; delete side serializes row-delete +
// tombstone-mark under the same lock).
func TestDeleteJob_InFlightPersistDoesNotResurrect(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-004")
	require.NoError(t, store.PersistJobByID(job.ID.String()))

	// Pin the envelope lock in the test goroutine: the racing persist blocks
	// on it; DeleteJob's [row-delete + tombstone] section ALSO blocks on it,
	// proving the serialization that prevents resurrection.
	envRelease := store.envLocks.Acquire(job.ID.String())
	persistDone := make(chan error, 1)
	deleteDone := make(chan error, 1)
	go func() { persistDone <- store.PersistJobByID(job.ID.String()) }()
	go func() { deleteDone <- store.DeleteJob(job.ID.String()) }()
	select {
	case err := <-persistDone:
		t.Fatalf("persist should be blocked on the envelope lock, got %v", err)
	case err := <-deleteDone:
		t.Fatalf("delete should be blocked on the envelope lock, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	envRelease() // winner order is implementation-defined; either way no resurrect
	require.NoError(t, <-deleteDone)
	persistErr := <-persistDone
	require.True(t, persistErr == nil || errors.Is(persistErr, ErrJobGone), "persist must land before delete or skip via tombstone, got %v", persistErr)

	row, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	if err == nil && row != nil {
		t.Fatalf("deleted job row resurrected by racing persist")
	}
	require.True(t, err == nil || database.IsNotFound(err), "unexpected find error: %v", err)
	// Tombstone surfaces typed gone.
	assert.ErrorIs(t, store.PersistJobByID(job.ID.String()), ErrJobGone)
}

// TestDeleteJob_WaitsForActiveEdits_DrainBarrier — DeleteJob takes the
// exclusive admission lease: an in-flight edit (gated inside the family key
// under a shared lease) completes fully before the delete proceeds,
// never half-applied against unregistered state.
func TestDeleteJob_WaitsForActiveEdits_DrainBarrier(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-005")

	inEdit := make(chan struct{})
	finishEdit := make(chan struct{})
	editDone := make(chan error, 1)
	go func() {
		editDone <- func() error {
			_, release, err := store.AcquireEditAccess(job.ID.String())
			if err != nil {
				return err
			}
			defer release()
			close(inEdit)
			<-finishEdit
			err2 := job.posterEditor.UpdateMovieFamily(context.Background(), "AAA-005", "", &models.Movie{ID: "AAA-005", Title: "edited"}, FamilySaveOptions{})
			return err2
		}()
	}()
	<-inEdit

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- store.DeleteJob(job.ID.String()) }()
	select {
	case err := <-deleteDone:
		t.Fatalf("DeleteJob returned while an edit lease was held: %v", err)
	case <-time.After(120 * time.Millisecond):
	}
	close(finishEdit)
	require.NoError(t, <-editDone)
	require.NoError(t, <-deleteDone)
	_, ok := store.GetJob(job.ID.String())
	assert.False(t, ok)
}

// TestDeleteJob_DBFailureKeepsJobUsable — a failed DB row delete leaves the
// job registered, un-cancelled, and editable (D3 order: no lifecycle
// mutation precedes a confirmed row delete).
func TestDeleteJob_DBFailureKeepsJobUsable(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil) // nil jobRepo
	store.persistence = &alwaysFailDeletePersistence{}
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-006")

	err := store.DeleteJob(job.ID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database deletion failed")
	_, ok := store.GetJob(job.ID.String())
	require.True(t, ok, "job must remain usable after a failed delete")
	assert.False(t, job.Lifecycle().IsDeleted())
	assert.False(t, store.JobGone(job.ID.String()))
	// Still editable: an admitted save succeeds.
	editJob, rel, aerr := store.AcquireEditAccess(job.ID.String())
	require.NoError(t, aerr)
	defer rel()
	require.NoError(t, editJob.UpdateMovieFamily(context.Background(), "AAA-006", "", &models.Movie{ID: "AAA-006", Title: "post-failure edit"}, FamilySaveOptions{}))
}

// TestStore_EditAfterDelete_ReturnsGoneNotFound410 — the tombstone
// registry distinguishes recently-deleted (410) from never-known (404).
func TestStore_EditAfterDelete_ReturnsGoneNotFound410(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-007")
	require.NoError(t, store.DeleteJob(job.ID.String()))

	_, _, err := store.AcquireEditAccess(job.ID.String())
	require.ErrorIs(t, err, ErrJobGone) // 410 mapping

	_, _, err = store.AcquireEditAccess("never-existed")
	require.ErrorIs(t, err, ErrJobNotFound) // 404 mapping
	assert.NotErrorIs(t, store.AcquireEditAccessErrKind("never-existed"), ErrJobGone)
}

// AcquireEditAccessErrKind is a test helper returning just the error.
func (s *JobStore) AcquireEditAccessErrKind(id string) error {
	_, _, err := s.AcquireEditAccess(id)
	return err
}

// TestPhaseStart_AfterDelete_ReturnsGone — StartScrape/StartApply consult the
// admission barrier so a deleted job cannot start a phase (D3).
func TestPhaseStart_AfterDelete_ReturnsGone(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-008")
	// Wire a workflow so StartScrape would otherwise proceed.
	job.controller.setDepsFromConfig(&JobConfig{BatchJobDeps: BatchJobDeps{WF: &stubWorkflow{}, BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: time.Second}}})
	require.NoError(t, store.DeleteJob(job.ID.String()))

	err := job.controller.StartScrape(context.Background(), []string{"/v/a.mp4"}, ScrapePhaseConfig{})
	require.ErrorIs(t, err, ErrJobGone)
}

// TestEditor_EmptyFamilyReturnsTypedError (D10): crop/from-url/PATCH variants
// re-resolve the family inside the lock and surface ErrMovieFamilyEmpty
// (404-mappable) instead of silently succeeding on nothing.
func TestEditor_EmptyFamilyReturnsTypedError(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-009")
	pe := job.posterEditor

	require.ErrorIs(t, pe.UpdatePosterCrop("NOPE-1", "c.jpg", nil, false), ErrMovieFamilyEmpty)
	require.ErrorIs(t, pe.UpdatePosterFromURL(context.Background(), "NOPE-1", "p.jpg", "c.jpg"), ErrMovieFamilyEmpty)
	require.ErrorIs(t, pe.UpdateMovieFamily(context.Background(), "NOPE-1", "", &models.Movie{ID: "NOPE-1"}, FamilySaveOptions{}), ErrMovieFamilyEmpty)
}

// TestUpdateMovie_CandidateThenCompositeTxThenPublish — publication NEVER
// precedes commit: forcing the envelope leg to fail inside the tx leaves the
// movie row, the job row, AND in-memory state untouched (D4 full rollback).
func TestUpdateMovie_CandidateThenCompositeTxThenPublish(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/a.mp4", "AAA-010")
	require.NoError(t, store.PersistJobByID(job.ID.String()))

	envRow, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	require.NotNil(t, envRow)

	// Sabotage the envelope leg: EnvelopeFn executes INSIDE the tx (after the
	// movie upsert leg), so forcing a marshal failure proves full rollback.
	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, errors.New("encode fail") }
	err = job.posterEditor.UpdateMovieFamily(context.Background(), "AAA-010", "", &models.Movie{ID: "AAA-010", Title: "should never persist"}, FamilySaveOptions{})
	require.Error(t, err)
	jobpersist.MarshalFn = orig

	// In-memory untouched.
	res, _ := job.results.GetMovieResult("/v/a.mp4")
	assert.NotEqual(t, "should never persist", res.Movie.Title)
	// DB movie row untouched (was never created).
	mv, err := repos.MovieRepo.FindByID(context.Background(), "AAA-010")
	if err == nil && mv != nil {
		assert.NotEqual(t, "should never persist", mv.Title)
	}
	// Job envelope row untouched.
	envRow2, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	assert.Equal(t, envRow.Results, envRow2.Results)

	// Success side: a clean save commits movie row + envelope atomically and
	// publishes after (success order verified by post-state).
	err = job.posterEditor.UpdateMovieFamily(context.Background(), "AAA-010", "", &models.Movie{ID: "AAA-010", Title: "committed title"}, FamilySaveOptions{})
	require.NoError(t, err)
	res, _ = job.results.GetMovieResult("/v/a.mp4")
	assert.Equal(t, "committed title", res.Movie.Title)
	envRow3, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	assert.Contains(t, envRow3.Results, "committed title")
	mv, err = repos.MovieRepo.FindByID(context.Background(), "AAA-010")
	require.NoError(t, err)
	require.NotNil(t, mv)
	assert.Equal(t, "committed title", mv.Title)
}

// alwaysFailDeletePersistence is a JobPersistencer whose DeleteJobFromDB
// always fails, used to prove delete-failure leaves the job usable.
type alwaysFailDeletePersistence struct{ noopJobPersistence }

func (alwaysFailDeletePersistence) DeleteJobFromDB(id string) error {
	return errors.New("database deletion failed: forced")
}

// --- Council-review blind-spot regressions (B1/B2/B3/C4) ---

// TestReconstructedJob_EditUsesCompositeTx (B1): jobs reconstructed from the
// DB (restart path) MUST re-attach the composite-tx edit seam. Without it the
// editor fell back to publish-only legacy behavior and the envelope silently
// never persisted (handlers no longer run post-op persists).
func TestReconstructedJob_EditUsesCompositeTx(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	storeA := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	jobA := seedOneMovie(t, storeA, "/v/r.mp4", "REC-001")
	require.NoError(t, storeA.PersistJobByID(jobA.ID.String()))

	// Simulate restart: a second store over the same DB reconstructs via
	// loadFromDatabase — with no LiveDeps attachment for the reconstructed
	// instance unless attachEditDeps ran.
	storeB := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	jobB, ok := storeB.GetBatchJob(jobA.ID.String())
	require.True(t, ok, "job must reconstruct from DB")

	require.NoError(t, jobB.UpdateMovieFamily(ctx, "REC-001", "", &models.Movie{ID: "REC-001", Title: "after restart"}, FamilySaveOptions{}))
	row, err := repos.JobRepo.FindByID(ctx, jobA.ID.String())
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Contains(t, row.Results, "after restart", "envelope tx must persist edits on reconstructed jobs")
	mv, err := repos.MovieRepo.FindByID(ctx, "REC-001")
	require.NoError(t, err)
	require.NotNil(t, mv)
	assert.Equal(t, "after restart", mv.Title, "composite tx must persist movie row on reconstructed jobs")
}

// TestDeleteJob_RunningJobFailsFast_NotDrainBlocked (B2): DeleteJob on a
// Running job must return promptly with the running-error even while a
// shared admission lease is held (the phase case). The exclusive drain must
// NOT wait out a whole phase.
func TestDeleteJob_RunningJobFailsFast_NotDrainBlocked(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/r2.mp4", "REC-002")
	job.Controller().SetJobStatus(models.JobStatusRunning)
	leaseHold, err := job.admission.AdmitShared() // simulate an in-flight phase lease
	require.NoError(t, err)
	defer leaseHold()

	done := make(chan error, 1)
	go func() { done <- store.DeleteJob(job.ID.String()) }()
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete running job")
	case <-time.After(2 * time.Second):
		t.Fatal("DeleteJob on a Running job must fail fast, not drain-wait the phase")
	}
}

// TestExcludeAll_PersistsCancelledStatus (B3): when the exclusion
// auto-cancels the only-file job on the composite-tx path, the durable row
// must carry the cancelled status — otherwise recoverOrphanedJobs resurrects
// the job as failed after restart.
func TestExcludeAll_PersistsCancelledStatus(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/r3.mp4", "REC-003")
	job.Controller().SetJobStatus(models.JobStatusPending)
	require.NoError(t, store.PersistJobByID(job.ID.String()))

	require.NoError(t, job.posterEditor.ExcludeMovieFamily(context.Background(), "REC-003"))
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())

	row, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, models.JobStatusCancelled, row.Status, "durable row must reflect the auto-cancellation")
}

// TestCompositeTx_RenamesLegRollsBackOnEnvelopeFailure (C4): a seeded
// existing actress rename inside the tx must roll back when the envelope leg
// fails — no partial rename committed mid-flight.
func TestCompositeTx_RenamesLegRollsBackOnEnvelopeFailure(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	seeded := seedNamedActress(t, repos.ActressRepo, "Yui", "", "波多野結衣")

	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil,
		WithActressRepo(repos.ActressRepo), WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/r4.mp4", "REC-004")
	require.NoError(t, store.PersistJobByID(job.ID.String()))

	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, errors.New("forced envelope encode failure") }
	err := job.posterEditor.UpdateMovieFamily(context.Background(), "REC-004", "", &models.Movie{
		ID: "REC-004",
		Actresses: []models.Actress{
			{ID: seeded.ID, FirstName: "Yui-Edited", JapaneseName: "波多野結衣"},
		},
	}, FamilySaveOptions{})
	jobpersist.MarshalFn = orig
	require.Error(t, err)

	got, err := repos.ActressRepo.FindByID(context.Background(), seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Yui", got.FirstName, "rename leg must roll back with the composite tx")
}

// gatedPersistencer delays the envelope UPSERT inside the envelope lock when
// armed — tests arm it for exactly the one racing persist call (job creation
// itself persists and must not trip the latch).
type gatedPersistencer struct {
	inner    JobPersistencer
	armed    atomic.Bool
	enterUps chan<- struct{}
	release  <-chan struct{}
}

// Pointer receivers: the store holds the same instance the test arms (a
// value copy would make armed.Store land on the test's private replica).
func (g *gatedPersistencer) PersistJob(j *BatchJob) error {
	if g.armed.Load() {
		if g.enterUps != nil {
			close(g.enterUps) // signals: inside envelope lock, about to upsert
			<-g.release
		}
	}
	return g.inner.PersistJob(j)
}
func (g *gatedPersistencer) PersistJobByID(id string) error {
	return g.inner.PersistJobByID(id)
}
func (g *gatedPersistencer) DeleteJobFromDB(id string) error { return g.inner.DeleteJobFromDB(id) }
func (g *gatedPersistencer) LoadJobs(ctx context.Context) ([]models.Job, error) {
	return g.inner.LoadJobs(ctx)
}
func (g *gatedPersistencer) UpsertJob(j *models.Job) error { return g.inner.UpsertJob(j) }

// TestDeleteJob_InFlightPersistDoesNotResurrect_PersistWins: persist is
// deterministically pinned INSIDE the envelope lock, delete waits on the
// same lock; the delete must still win finally (row gone, no resurrect).
func TestDeleteJob_InFlightPersistDoesNotResurrect_PersistWins(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	enterUps := make(chan struct{})
	releaseUps := make(chan struct{})
	gated := &gatedPersistencer{
		inner:    NewDBJobPersistence(repos.JobRepo),
		enterUps: enterUps,
		release:  releaseUps,
	}
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithPersistence(gated))
	job := seedOneMovie(t, store, "/v/r5.mp4", "REC-005")
	gated.armed.Store(true)

	persistDone := make(chan error, 1)
	go func() { persistDone <- store.PersistJobByID(job.ID.String()) }()
	<-enterUps // persister is inside the envelope lock now

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- store.DeleteJob(job.ID.String()) }()
	select {
	case err := <-deleteDone:
		t.Fatalf("delete must wait for the in-flight persist: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseUps)
	require.NoError(t, <-persistDone)
	require.NoError(t, <-deleteDone)

	row, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	if err == nil && row != nil {
		t.Fatalf("row resurrected: delete landed before upsert without tombstone guard")
	}
}

// TestDeleteJob_InFlightPersistDoesNotResurrect_DeleteWins: delete first,
// then a tombstone-guarded persist — the upsert must skip and the row must
// stay gone.
func TestDeleteJob_InFlightPersistDoesNotResurrect_DeleteWins(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil)
	job := seedOneMovie(t, store, "/v/r6.mp4", "REC-006")
	require.NoError(t, store.PersistJobByID(job.ID.String()))
	require.NoError(t, store.DeleteJob(job.ID.String()))

	err := store.PersistJobByID(job.ID.String())
	require.ErrorIs(t, err, ErrJobGone, "tombstone must surface a typed gone")

	row, ferr := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	if ferr == nil && row != nil {
		t.Fatalf("deleted row resurrected by post-delete persist")
	}
}
