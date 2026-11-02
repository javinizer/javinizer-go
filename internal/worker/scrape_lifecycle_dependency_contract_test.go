package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

type lifecycleScrapeWorkflow struct{ workflow.WorkflowInterface }

func (*lifecycleScrapeWorkflow) Scrape(context.Context, scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return &scrape.ScrapeResult{Movie: &models.Movie{ID: "LIFECYCLE-001", Title: "retained metadata"}}, nil, nil
}

type panicLifecycleHistory struct {
	database.HistoryRepositoryInterface
	calls atomic.Int32
}

func (r *panicLifecycleHistory) Create(context.Context, *models.History) error {
	r.calls.Add(1)
	panic("history backend panic")
}

type failedLifecycleMovieRepo struct {
	database.MovieRepositoryInterface
}

func (*failedLifecycleMovieRepo) UpsertWithTranslations(context.Context, *models.Movie, []models.GenreTranslationData, []models.ActressTranslationData) (*models.Movie, error) {
	return nil, errors.New("movie write refused")
}

// Wait joins the phase's cleanup, not merely its terminal status transition.
func waitLifecycleContract(t *testing.T, job worker.ControlledJob) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- job.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("scrape phase did not quiesce after dependency panic")
		return nil
	}
}
func requireLifecycleEventsClosed(t *testing.T, sub worker.JobEventSubscriber) {
	t.Helper()
	for {
		select {
		case _, ok := <-sub.Events():
			if !ok {
				return
			}
		default:
			t.Fatal("Wait returned without closing the scrape event stream")
			return
		}
	}
}

func TestScrapeLifecycleHistoryPanicFailsAndQuiesces(t *testing.T) {
	history := &panicLifecycleHistory{}
	store := worker.NewInMemoryJobStore()
	job := store.CreateJob([]string{"movie.mp4"}, &worker.JobConfig{BatchJobDeps: worker.BatchJobDeps{
		WF: &lifecycleScrapeWorkflow{}, BatchCfg: worker.BatchJobConfig{MaxWorkers: 1}, HistoryRepo: history,
	}})
	sub := job.Subscribe()
	defer sub.Close()
	require.NoError(t, job.StartScrape(t.Context(), []string{"movie.mp4"}, worker.ScrapePhaseConfig{}))
	require.ErrorContains(t, waitLifecycleContract(t, job), "failed")
	require.Equal(t, int32(1), history.calls.Load())
	require.Equal(t, models.JobStatusFailed, job.GetJobStatus())
	require.Empty(t, job.GetCurrentPhase(), "terminal cleanup clears the durable phase marker")
	result, err := job.GetMovieResult("movie.mp4")
	require.NoError(t, err)
	require.Equal(t, "retained metadata", result.Movie.Title, "an audit failure must not discard scraped metadata")
	require.False(t, result.Persisted)
	requireLifecycleEventsClosed(t, sub)
	late := job.Subscribe()
	defer late.Close()
	requireLifecycleEventsClosed(t, late)
	require.ErrorContains(t, job.Wait(), "failed", "repeated wait remains safe after terminal cleanup")
	require.Error(t, job.StartApply(t.Context(), worker.ApplyPhaseConfig{}), "a failed scrape cannot enter apply")
}

func TestScrapeLifecyclePersistFailureCallbackPanicQuiesces(t *testing.T) {
	store := worker.NewInMemoryJobStore()
	job := store.CreateJob([]string{"movie.mp4"}, &worker.JobConfig{BatchJobDeps: worker.BatchJobDeps{
		WF: &lifecycleScrapeWorkflow{}, BatchCfg: worker.BatchJobConfig{MaxWorkers: 1}, MovieRepo: &failedLifecycleMovieRepo{},
	}})
	sub := job.Subscribe()
	defer sub.Close()
	var callbacks atomic.Int32
	require.NoError(t, job.StartScrape(t.Context(), []string{"movie.mp4"}, worker.ScrapePhaseConfig{
		OnFileScrapeFailed: func(string, string, string) { callbacks.Add(1); panic("progress consumer panic") },
	}))
	require.NoError(t, waitLifecycleContract(t, job), "per-file persistence failure does not strand the batch worker")
	require.Equal(t, int32(2), callbacks.Load(), "both the original error and recovered panic are offered to the progress consumer")
	require.Equal(t, models.JobStatusCompleted, job.GetJobStatus())
	require.Empty(t, job.GetCurrentPhase())
	result, err := job.GetMovieResult("movie.mp4")
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, result.Status)
	require.Contains(t, result.Error, "persist panic: progress consumer panic")
	require.False(t, result.Persisted)
	require.Equal(t, "retained metadata", result.Movie.Title)
	requireLifecycleEventsClosed(t, sub)
	require.NoError(t, job.Wait(), "repeated wait remains safe after the persist workers exit")
}
