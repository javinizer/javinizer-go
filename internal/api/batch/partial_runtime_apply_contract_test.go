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
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

// Manual partial wiring supports scan-only workflows, not full Apply. This is
// not a claim that production bootstrap admits a runtime missing repositories.
func TestPartialScanRuntimeRefusesApplyWithoutMutatingCompletedJob(t *testing.T) {
	owned := t.TempDir()
	source := filepath.Join(owned, ".mp4")
	require.NoError(t, os.WriteFile(source, []byte("owned source"), 0600))
	// Snapshot handle identity before later operations; Windows path Stat is lazy.
	sourceHandle, err := os.Open(source)
	require.NoError(t, err)
	sourceInfo, statErr := sourceHandle.Stat()
	closeErr := sourceHandle.Close()
	require.NoError(t, statErr)
	require.NoError(t, closeErr)
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(owned, "work")
	deps := testkit.CreateTestDeps(t, cfg, "")
	healthy := testkit.GetTestRuntime(deps)
	snap := healthy.Snapshot()
	wf, err := snap.BatchWorkflow("partial-runtime-admission")
	require.NoError(t, err)
	factory := snap.BatchJobFactory()
	require.NotNil(t, factory)
	job := factory.CreateJob([]string{source}, worker.BatchJobOptions{WF: wf})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, job.StartScrape(ctx, []string{source}, worker.ScrapePhaseConfig{}))
	settled := make(chan struct{})
	var scrapeErr error
	go func() { scrapeErr = job.Wait(); close(settled) }()
	t.Cleanup(func() { cancel(); <-settled })
	select {
	case <-settled:
		require.NoError(t, scrapeErr)
	case <-ctx.Done():
		t.Fatal("real scrape did not settle")
	}
	// A real completed batch may contain per-file scrape failures. No status or
	// result maps are manufactured, and recovery need not write any media files.
	require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
	results := job.GetResults()
	require.Len(t, results, 1)
	require.Equal(t, models.JobStatusFailed, results[0].Status)
	require.Contains(t, results[0].Error, "empty MovieID")

	// Borrow the same real store/DB while the testkit owner remains alive. Give
	// the partial runtime a distinct CoreDeps/config container; do not mutate the
	// live healthy runtime or transfer ownership/close its borrowed dependencies.
	partialRepos := deps.Repos
	partialRepos.MovieRepo = nil
	partialDeps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{DB: deps.CoreDeps.DB, ScraperRegistry: deps.CoreDeps.GetRegistry()},
		Repos:    partialRepos, JobStore: deps.JobStore,
	}
	partial := core.NewAPIRuntime(partialDeps)
	partial.SetConfig(cfg.Clone())
	partial.EnsureRuntime()
	t.Cleanup(func() {
		partial.Shutdown()
		partial.GetRuntime().Shutdown()
		require.True(t, partial.WaitBackgroundTasks(5*time.Second))
	})
	scanOnly, err := partial.Snapshot().ScanOnlyWorkflow()
	require.NoError(t, err)
	require.NotNil(t, scanOnly)
	require.NotNil(t, partial.Snapshot().BatchJobFactory())
	before := job.GetStatus()
	durableBefore, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)

	requestApply := func(rt *core.APIRuntime) *httptest.ResponseRecorder {
		t.Helper()
		router := gin.New()
		batch.RegisterRoutes(router.Group("/api/v1"), rt)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/batch/"+job.GetID()+"/update", strings.NewReader(`{"skip_nfo":true,"skip_download":true}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	rejected := requestApply(partial)
	require.Equal(t, http.StatusInternalServerError, rejected.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rejected.Body.Bytes(), &body))
	require.Equal(t, map[string]any{"error": "Failed to create workflow: workflow: scraper must not be nil for full workflow"}, body)
	require.True(t, partial.WaitBackgroundTasks(time.Second), "refusal must not admit a background apply phase")
	require.Equal(t, before, job.GetStatus())
	require.Equal(t, results, job.GetResults())
	durableAfter, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	require.Equal(t, durableBefore, durableAfter)
	for _, table := range []string{"movies", "batch_file_operations"} {
		var count int64
		require.NoError(t, deps.CoreDeps.DB.DB.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
	assertSource := func() {
		t.Helper()
		current, err := os.Stat(source)
		require.NoError(t, err)
		require.True(t, os.SameFile(sourceInfo, current))
		contents, err := os.ReadFile(source)
		require.NoError(t, err)
		require.Equal(t, "owned source", string(contents))
		require.NoFileExists(t, filepath.Join(owned, ".nfo"))
	}
	assertSource()

	// The fully wired runtime can admit the same actual job. Its failed scrape
	// row is intentionally skipped by normal Apply; this proves recovery of
	// admission, not successful media organization for absent movie metadata.
	var applySettled chan struct{}
	t.Cleanup(func() {
		healthy.Shutdown()
		if applySettled != nil {
			<-applySettled
		} else {
			_ = job.Wait()
		}
		require.True(t, healthy.WaitBackgroundTasks(5*time.Second))
	})
	recovered := requestApply(healthy)
	applySettled = make(chan struct{})
	var applyErr error
	go func() { applyErr = job.Wait(); close(applySettled) }()
	require.Equal(t, http.StatusOK, recovered.Code, recovered.Body.String())
	select {
	case <-applySettled:
		require.NoError(t, applyErr)
	case <-time.After(10 * time.Second):
		t.Fatal("recovered apply did not settle")
	}
	require.True(t, healthy.WaitBackgroundTasks(5*time.Second))
	require.Greater(t, job.GetStatus().ApplyGeneration, before.ApplyGeneration)
	require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
	assertSource()
}
