package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The decorator pauses a successful, real admission without changing its result
// or releasing its lease. Shutdown may legitimately begin at this scheduling
// boundary: the first evidence admission does not keep admission open forever.
type shutdownBoundaryStore struct {
	worker.JobStoreInterface
	entered  chan struct{}
	resume   chan struct{}
	releases atomic.Int32
}

func (s *shutdownBoundaryStore) AcquireRescrapeAccess(id string) (worker.ControlledJob, func(), error) {
	job, release, err := s.JobStoreInterface.AcquireRescrapeAccess(id)
	if err != nil {
		return job, release, err
	}
	close(s.entered)
	<-s.resume
	return job, func() {
		s.releases.Add(1)
		release()
	}, nil
}

func TestOfflineEvidenceShutdownBetweenRescrapeAdmissions(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		name := "single"
		if bulk {
			name = "bulk"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "EVIDENCE-001.mp4")
			require.NoError(t, os.WriteFile(file, []byte("owned source"), 0600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = t.TempDir()
			cfg.API.Security.AllowedDirectories = []string{dir}
			cfg.Scrapers.Priority = []string{"stub-no-poster"}
			deps := createTestDeps(t, cfg, "")
			source := &evidenceBatchSource{}
			deps.CoreDeps.GetRegistry().RegisterInstance(source)
			job := createJobWithWF(deps, cfg, []string{file})
			setJobStatus(job, models.JobStatusCompleted)
			setJobResult(job, file, &resultstore.MovieResult{ResultID: "stable", Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "EVIDENCE-001"}, Movie: &models.Movie{ID: "EVIDENCE-001", Title: "committed"}})
			require.NoError(t, deps.JobStore.PersistJobByID(job.GetID()))
			beforeResult, _, found := job.Results().GetFileResultByResultID("stable")
			require.True(t, found)
			before, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
			require.NoError(t, err)

			store := &shutdownBoundaryStore{JobStoreInterface: deps.JobStore, entered: make(chan struct{}), resume: make(chan struct{})}
			// Do not mutate an existing runtime's dependency graph. This request
			// owns a separate runtime, built with the decorated store from birth.
			ownedDeps := *deps
			ownedDeps.JobStore = store
			rt := core.NewAPIRuntime(&ownedDeps)
			rt.SetConfig(cfg)
			served, shutdown := make(chan struct{}), make(chan struct{})
			var resumeOnce, shutdownOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(store.resume) }) }
			startShutdown := func() { shutdownOnce.Do(func() { go func() { rt.Shutdown(); close(shutdown) }() }) }
			wait := func(ch <-chan struct{}, what string) bool {
				t.Helper()
				select {
				case <-ch:
					return true
				case <-time.After(5 * time.Second):
					t.Errorf("timed out waiting for %s", what)
					return false
				}
			}

			var writes atomic.Int32
			observeWrite := func(tx *gorm.DB) {
				if tx.Statement.Table == "jobs" {
					writes.Add(1)
				}
			}
			const callback = "shutdown_boundary_observe_job_write"
			require.NoError(t, deps.Repos.DB.Callback().Create().Before("gorm:create").Register(callback, observeWrite))
			t.Cleanup(func() { _ = deps.Repos.DB.Callback().Create().Remove(callback) })
			require.NoError(t, deps.Repos.DB.Callback().Update().Before("gorm:update").Register(callback, observeWrite))
			t.Cleanup(func() { _ = deps.Repos.DB.Callback().Update().Remove(callback) })

			router := gin.New()
			path := "/batch/" + job.GetID() + "/results/stable/rescrape"
			body := map[string]any{"selected_scrapers": []string{source.Name()}, "show_offline_evidence": true}
			if bulk {
				path = "/batch/" + job.GetID() + "/rescrape"
				body["movie_ids"] = []string{"EVIDENCE-001"}
				router.POST("/batch/:id/rescrape", batchRescrapeMovies(rt))
			} else {
				router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(rt))
			}
			payload, err := json.Marshal(body)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			// Registered before starting any owned goroutine, and before callbacks
			// are removed: even a failed assertion releases the barrier and joins.
			t.Cleanup(func() {
				resume()
				startShutdown()
				wait(served, "handler cleanup")
				wait(shutdown, "runtime cleanup")
			})
			go func() { defer close(served); router.ServeHTTP(response, request) }()
			if !wait(store.entered, "real rescrape admission") {
				return
			}
			startShutdown()
			if !wait(rt.ServerCtx().Done(), "shutdown cancellation") {
				return
			}
			select {
			case <-shutdown:
				t.Fatal("shutdown returned while the handler still owns evidence admission")
			default:
			}
			resume()
			if !wait(served, "handler response") || !wait(shutdown, "shutdown join") {
				return
			}
			require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			var failure map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &failure))
			require.Equal(t, map[string]any{"error": core.ErrOfflineEvidenceShuttingDown.Error()}, failure, "only the nested admission refusal is returned, without success payload")
			require.Zero(t, source.calls.Load(), "shutdown must precede any scrape")
			require.Zero(t, writes.Load(), "neither job creation nor envelope persistence is admitted")
			require.Equal(t, int32(1), store.releases.Load(), "handler releases the real shared lease exactly once")
			require.True(t, rt.WaitBackgroundTasks(time.Second), "all evidence admissions are released")
			afterResult, _, found := job.Results().GetFileResultByResultID("stable")
			require.True(t, found)
			require.Equal(t, beforeResult, afterResult)
			after, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Len(t, deps.JobStore.ListJobs(), 1, "no replacement job was created")
			contents, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "owned source", string(contents))
			require.NoError(t, deps.JobStore.DeleteJob(job.GetID()), "exclusive deletion proves shared admission was released")
		})
	}
}
