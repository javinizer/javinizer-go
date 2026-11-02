package batch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

// A real per-file scrape failure may retain its stable result identity without
// acquiring a movie-family identity. Preview must distinguish these two lookups.
func TestFailedRealScrapePreviewReportsMissingMovieFamily(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".mp4")
	require.NoError(t, os.WriteFile(file, []byte("unchanged preview input"), 0600))
	original, err := os.Stat(file)
	require.NoError(t, err)
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(dir, "work")
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	snap := rt.Snapshot()
	factory := snap.BatchJobFactory()
	require.NotNil(t, factory)
	wf, err := snap.BatchWorkflow("failed-preview-family")
	require.NoError(t, err)
	job := factory.CreateJob([]string{file}, worker.BatchJobOptions{WF: wf})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, job.StartScrape(ctx, []string{file}, worker.ScrapePhaseConfig{}))
	settled := make(chan struct{})
	var phaseErr error
	go func() { phaseErr = job.Wait(); close(settled) }()
	t.Cleanup(func() { cancel(); <-settled })
	select {
	case <-settled:
		require.NoError(t, phaseErr)
	case <-ctx.Done():
		t.Fatal("real scrape did not settle")
	}
	results := job.GetResults()
	require.Len(t, results, 1)
	failed := results[0]
	require.Equal(t, models.JobStatusFailed, failed.Status)
	require.Contains(t, failed.Error, "empty MovieID")
	require.NotEmpty(t, failed.ResultID)
	require.Empty(t, failed.FileMatchInfo.MovieID)
	require.Nil(t, failed.Movie)
	resolved, source, found := job.GetFileResultByResultID(failed.ResultID)
	require.True(t, found)
	require.Equal(t, file, source)
	require.Equal(t, failed, *resolved)
	require.Empty(t, job.GetMovieResultsForMovieID(""))
	before := job.GetStatus()
	movie, files, resolveErr := batch.ResolvePreviewData(t.Context(), deps, job.GetID(), failed.ResultID, contracts.OrganizePreviewRequest{})
	require.Nil(t, movie)
	require.Nil(t, files)
	require.NotNil(t, resolveErr)
	require.Equal(t, http.StatusNotFound, resolveErr.Status)
	require.Equal(t, "Movie  not found in job", resolveErr.Error())
	router := gin.New()
	batch.RegisterRoutes(router.Group("/api/v1"), rt)
	for _, tc := range []struct{ id, want string }{
		{failed.ResultID, "Movie  not found in job"},
		{"unknown-result", "Result unknown-result not found in job"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/batch/"+job.GetID()+"/results/"+tc.id+"/preview", strings.NewReader(`{"operation_mode":"metadata-artwork"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, map[string]any{"error": tc.want}, body)
	}
	require.Equal(t, before, job.GetStatus())
	require.Equal(t, results, job.GetResults())
	current, err := os.Stat(file)
	require.NoError(t, err)
	require.True(t, os.SameFile(original, current))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "unchanged preview input", string(data))
}
