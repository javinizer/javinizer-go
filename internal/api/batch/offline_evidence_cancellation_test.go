package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOfflineEvidenceCancelledBatchRequestsDoNotMutate(t *testing.T) {
	for _, route := range []string{"scrape", "single rescrape", "bulk rescrape"} {
		t.Run(route, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "EVIDENCE-001.mp4")
			require.NoError(t, os.WriteFile(file, []byte("fixture"), 0600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = t.TempDir()
			cfg.API.Security.AllowedDirectories = []string{dir}
			cfg.Scrapers.Priority = []string{"stub-no-poster"}
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)
			source := &evidenceBatchSource{}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			router := gin.New()
			path := "/batch/scrape"
			body := map[string]any{"files": []string{file}, "selected_scrapers": []string{source.Name()}, "show_offline_evidence": true}
			router.POST(path, batchScrape(rt))
			var jobID string
			var before *models.Job
			var beforeResult *resultstore.MovieResult
			if route != "scrape" {
				job := createJobWithWF(deps, cfg, []string{file})
				jobID = job.GetID()
				setJobResult(job, file, &resultstore.MovieResult{ResultID: "stable", Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "EVIDENCE-001"}, Movie: &models.Movie{ID: "EVIDENCE-001", Title: "committed"}})
				require.NoError(t, deps.JobStore.PersistJobByID(jobID))
				beforeResult, _, _ = job.Results().GetFileResultByResultID("stable")
				var err error
				before, err = deps.Repos.JobRepo.FindByID(t.Context(), jobID)
				require.NoError(t, err)
				if route == "single rescrape" {
					router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(rt))
					path = "/batch/" + jobID + "/results/stable/rescrape"
				} else {
					router.POST("/batch/:id/rescrape", batchRescrapeMovies(rt))
					path = "/batch/" + jobID + "/rescrape"
					body["movie_ids"] = []string{"EVIDENCE-001"}
				}
			}
			payload, err := json.Marshal(body)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Zero(t, source.calls.Load())
			require.True(t, rt.WaitBackgroundTasks(time.Second), "both evidence admission leases must be released")
			if route == "scrape" {
				var count int64
				require.NoError(t, deps.Repos.DB.Model(&models.Job{}).Count(&count).Error)
				require.Zero(t, count)
			} else {
				job, ok := deps.JobStore.GetBatchJob(jobID)
				require.True(t, ok)
				afterResult, _, found := job.GetFileResultByResultID("stable")
				require.True(t, found)
				require.Equal(t, beforeResult, afterResult, "no result, revision, or metadata changes")
				after, err := deps.Repos.JobRepo.FindByID(t.Context(), jobID)
				require.NoError(t, err)
				require.Equal(t, before, after, "cancelled request must not persist an envelope")
				// Delete needs an exclusive lease; success proves the handler released
				// its shared rescrape admission rather than pinning the job forever.
				require.NoError(t, deps.JobStore.DeleteJob(jobID))
			}
		})
	}
}

func TestOfflineEvidenceBulkRescrapeEnvelopeSaveFailureIsNotHTTPSuccess(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = t.TempDir()
	cfg.Scrapers.Priority = []string{"stub-no-poster"}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	source := &evidenceBatchSource{}
	deps.CoreDeps.GetRegistry().RegisterInstance(source)
	file := filepath.Join(t.TempDir(), "EVIDENCE-001.mp4")
	job := createJobWithWF(deps, cfg, []string{file})
	setJobResult(job, file, &resultstore.MovieResult{ResultID: "stable", Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "EVIDENCE-001"}, Movie: &models.Movie{ID: "EVIDENCE-001", Title: "committed"}})
	require.NoError(t, deps.JobStore.PersistJobByID(job.GetID()))
	before, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	var failedSaves atomic.Int32
	const callback = "evidence_bulk_envelope_save_failure"
	require.NoError(t, deps.Repos.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if _, savingJob := tx.Statement.Dest.(*models.Job); tx.Statement.Table == "jobs" && savingJob {
			failedSaves.Add(1)
			_ = tx.AddError(errors.New("fixture envelope save unavailable"))
		}
	}))
	defer func() { _ = deps.Repos.DB.Callback().Update().Remove(callback) }()
	router := gin.New()
	router.POST("/batch/:id/rescrape", batchRescrapeMovies(rt))
	request := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/rescrape", bytes.NewBufferString(`{"movie_ids":["EVIDENCE-001"],"selected_scrapers":["stub-no-poster"],"show_offline_evidence":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "bulk rescrape envelope persist failed")
	require.Contains(t, response.Body.String(), "fixture envelope save unavailable")
	require.NotContains(t, response.Body.String(), `"succeeded"`)
	require.Equal(t, int32(1), source.calls.Load(), "failure must be after actual scrape work")
	require.Positive(t, failedSaves.Load())
	after, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	require.Equal(t, before, after, "failed save must roll back the durable envelope and generation")
	require.True(t, rt.WaitBackgroundTasks(time.Second))
	require.NoError(t, deps.Repos.DB.Callback().Update().Remove(callback))
	require.NoError(t, deps.JobStore.DeleteJob(job.GetID()), "failed handler must release its rescrape lease")
}

func TestOfflineEvidenceShutdownJoinsFailedCancellationSave(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "EVIDENCE-001.mp4")
	require.NoError(t, os.WriteFile(file, []byte("fixture"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = t.TempDir()
	cfg.API.Security.AllowedDirectories = []string{dir}
	cfg.Scrapers.Priority = []string{"stub-no-poster"}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	source := &evidenceBatchSource{}
	deps.CoreDeps.GetRegistry().RegisterInstance(source)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	var blocked atomic.Bool
	const createCallback = "evidence_shutdown_create_barrier"
	require.NoError(t, deps.Repos.DB.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "jobs" && blocked.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
	}))
	defer func() { _ = deps.Repos.DB.Callback().Create().Remove(createCallback) }()
	var failedSaves atomic.Int32
	const saveCallback = "evidence_shutdown_cancel_save_failure"
	require.NoError(t, deps.Repos.DB.Callback().Update().Before("gorm:update").Register(saveCallback, func(tx *gorm.DB) {
		if _, savingJob := tx.Statement.Dest.(*models.Job); tx.Statement.Table == "jobs" && savingJob {
			failedSaves.Add(1)
			_ = tx.AddError(errors.New("fixture cancellation save unavailable"))
		}
	}))
	defer func() { _ = deps.Repos.DB.Callback().Update().Remove(saveCallback) }()
	router := gin.New()
	router.POST("/batch/scrape", batchScrape(rt))
	payload, err := json.Marshal(map[string]any{"files": []string{file}, "selected_scrapers": []string{source.Name()}, "show_offline_evidence": true})
	require.NoError(t, err)
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	request := httptest.NewRequest(http.MethodPost, "/batch/scrape", bytes.NewReader(payload)).WithContext(requestCtx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	served, shutdown := make(chan struct{}), make(chan struct{})
	var shutdownOnce sync.Once
	startShutdown := func() { shutdownOnce.Do(func() { go func() { rt.Shutdown(); close(shutdown) }() }) }
	// Registered after the callback removals so cleanup always joins every
	// admitted operation before GORM callbacks or the temporary DB go away.
	defer func() {
		cancelRequest()
		release()
		startShutdown()
		for _, done := range []<-chan struct{}{served, shutdown} {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("admitted operation did not join during cleanup")
			}
		}
	}()
	go func() { router.ServeHTTP(response, request); close(served) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job creation did not enter barrier")
	}
	startShutdown()
	select {
	case <-rt.ServerCtx().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel")
	}
	select {
	case <-shutdown:
		t.Fatal("shutdown escaped admitted creation")
	default:
	}
	release()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not join")
	}
	select {
	case <-shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join failed persistence")
	}
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "cancel admitted job")
	require.Contains(t, response.Body.String(), "fixture cancellation save unavailable")
	require.NotContains(t, response.Body.String(), `"job_id"`)
	require.Zero(t, source.calls.Load())
	require.Positive(t, failedSaves.Load())
	var jobs []models.Job
	require.NoError(t, deps.Repos.DB.Find(&jobs).Error)
	require.Len(t, jobs, 1)
	require.Equal(t, models.JobStatusPending, jobs[0].Status, "failed cancellation save cannot claim a durable transition")
	job, ok := deps.JobStore.GetBatchJob(jobs[0].ID)
	require.True(t, ok)
	require.Equal(t, models.JobStatusCancelled, job.GetStatus().Status)
	require.True(t, rt.WaitBackgroundTasks(time.Second))
}
