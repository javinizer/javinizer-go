package batch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReviewRegressionListRepositoryErrorIsPrivate(t *testing.T) {
	const detail = "database /private/library.sqlite: query SELECT private_movie"
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.On("List", mock.Anything).Return([]models.Job(nil), errors.New(detail)).Once()
	rt := core.NewAPIRuntime(newTestAPIDeps(t, repo, nil))
	router := gin.New()
	router.GET("/jobs", listBatchJobs(rt))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), detail)
	require.JSONEq(t, `{"error":"failed to list jobs"}`, rec.Body.String())
}

func TestReviewRegressionPanicDoesNotLoseQueuedMovies(t *testing.T) {
	ids := make([]string, bulkRescrapeWorkers+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("TEST-%03d", i)
	}
	job := &safetyRegressionJob{run: func(context.Context, worker.RescrapeCmd) (*worker.RescrapeResult, error) { panic("worker fault") }}
	var progress []float64
	results, recovery := bulkRescrapePool(t.Context(), job, ids, &contracts.BatchRescrapeRequest{}, safetyRegressionFactory{}, func(_ string, _ *contracts.BulkRescrapeMovieResult, p float64) { progress = append(progress, p) })
	require.Empty(t, recovery)
	require.Len(t, results, len(ids), "every accepted movie must receive a terminal outcome, even after worker panic")
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.MovieID)
		require.Equal(t, models.RescrapeStatusFailed, result.Status)
	}
	require.ElementsMatch(t, ids, got)
	require.NotEmpty(t, progress)
	require.Equal(t, float64(100), progress[len(progress)-1])
}

func TestReviewRegressionPanicPayloadIsPrivate(t *testing.T) {
	const detail = "panic at /private/library/movie.nfo with private query"
	job := &safetyRegressionJob{run: func(context.Context, worker.RescrapeCmd) (*worker.RescrapeResult, error) { panic(detail) }}
	results, _ := bulkRescrapePool(t.Context(), job, []string{"TEST-001"}, &contracts.BatchRescrapeRequest{}, safetyRegressionFactory{}, nil)
	require.Len(t, results, 1)
	require.Equal(t, models.RescrapeStatusFailed, results[0].Status)
	// The HTTP handler serializes these results unchanged in BulkRescrapeResponse.
	encoded, err := json.Marshal(contracts.BulkRescrapeResponse{Results: results, Failed: 1})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), detail)
	require.Equal(t, "Rescrape failed unexpectedly", results[0].Error)
}

// These helpers deliberately do not depend on the separate coverage-only PR.
type safetyRegressionJob struct {
	worker.BatchJobInterface
	run func(context.Context, worker.RescrapeCmd) (*worker.RescrapeResult, error)
}

func (j *safetyRegressionJob) Rescrape(ctx context.Context, cmd worker.RescrapeCmd) (*worker.RescrapeResult, error) {
	return j.run(ctx, cmd)
}

type safetyRegressionFactory struct {
	worker.BatchJobFactoryInterface
}

func (safetyRegressionFactory) NewRescrapeCmd(movieID, filePath, manual string, scrapers []string, force bool, merge workflow.MergeOptions) worker.RescrapeCmd {
	return worker.RescrapeCmd{MovieID: movieID, FilePath: filePath, ManualSearchInput: manual, SelectedScrapers: scrapers, Force: force, Merge: merge}
}

func TestReviewRegressionPanicPreservesLaterSuccessAndRecovery(t *testing.T) {
	ids := make([]string, 0, bulkRescrapeWorkers+2)
	for i := 0; i < bulkRescrapeWorkers; i++ {
		ids = append(ids, fmt.Sprintf("PANIC-%03d", i))
	}
	ids = append(ids, "SUCCESS-001", "SUCCESS-002")
	var finalized atomic.Int32
	first := worker.NewRescapeRecoveryHandle(func() { finalized.Add(1) })
	second := worker.NewRescapeRecoveryHandle(func() { finalized.Add(1) })
	job := &safetyRegressionJob{run: func(_ context.Context, cmd worker.RescrapeCmd) (*worker.RescrapeResult, error) {
		if strings.HasPrefix(cmd.MovieID, "PANIC-") {
			panic("worker fault")
		}
		handle := first
		if cmd.MovieID == "SUCCESS-002" {
			handle = second
		}
		return &worker.RescrapeResult{Status: models.RescrapeStatusSuccess, Movie: &models.Movie{ID: cmd.MovieID}, PosterRecovery: handle}, nil
	}}
	results, recovery := bulkRescrapePool(t.Context(), job, ids, &contracts.BatchRescrapeRequest{}, safetyRegressionFactory{}, nil)
	require.Len(t, results, len(ids))
	gotSuccess := []string{}
	for _, result := range results {
		if strings.HasPrefix(result.MovieID, "SUCCESS-") {
			require.Equal(t, models.RescrapeStatusSuccess, result.Status)
			require.NotNil(t, result.Movie)
			require.Equal(t, result.MovieID, result.Movie.ID)
			require.Empty(t, result.Error)
			gotSuccess = append(gotSuccess, result.MovieID)
		} else {
			require.Equal(t, models.RescrapeStatusFailed, result.Status)
		}
	}
	require.ElementsMatch(t, []string{"SUCCESS-001", "SUCCESS-002"}, gotSuccess)
	require.ElementsMatch(t, []*worker.RescrapeRecoveryHandle{first, second}, recovery)
	require.Zero(t, finalized.Load(), "pool must not finalize recovery before durable envelope persistence")
	for _, handle := range recovery {
		handle.Finalize()
	}
	require.Equal(t, int32(2), finalized.Load())
}
