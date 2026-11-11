package batch

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// A shared rescrape lease excludes phase starts and deletion, but does not
// exclude cancellation. Cancel the real pending job at that scheduling boundary
// without replacing admission results or manufacturing a lifecycle snapshot.
type cancelAfterRescrapeAdmissionStore struct {
	worker.JobStoreInterface
	admissions int
	releases   int
}

func (s *cancelAfterRescrapeAdmissionStore) AcquireRescrapeAccess(id string) (worker.ControlledJob, func(), error) {
	job, release, err := s.JobStoreInterface.AcquireRescrapeAccess(id)
	if err != nil {
		return job, release, err
	}
	s.admissions++
	job.Cancel()
	return job, func() { s.releases++; release() }, nil
}

func TestRescrapeRejectsPendingJobCancelledAfterAdmission(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CANCEL-001.mp4")
	require.NoError(t, os.WriteFile(file, []byte("original source"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = t.TempDir()
	cfg.API.Security.AllowedDirectories = []string{dir}
	cfg.Scrapers.Priority = []string{"stub-no-poster"}
	deps := createTestDeps(t, cfg, "")
	source := &evidenceBatchSource{}
	deps.CoreDeps.GetRegistry().RegisterInstance(source)
	job := createJobWithWF(deps, cfg, []string{file})
	require.Equal(t, models.JobStatusPending, job.GetStatus().Status)
	setJobResult(job, file, &resultstore.MovieResult{
		ResultID: "stable", Status: models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "CANCEL-001"},
		Movie:         &models.Movie{ID: "CANCEL-001", Title: "original result"},
	})
	require.NoError(t, deps.JobStore.PersistJobByID(job.GetID()))
	beforeResult, _, found := job.Results().GetFileResultByResultID("stable")
	require.True(t, found)
	beforeRow, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	store := &cancelAfterRescrapeAdmissionStore{JobStoreInterface: deps.JobStore}
	ownedDeps := *deps
	ownedDeps.JobStore = store
	rt := core.NewAPIRuntime(&ownedDeps)
	rt.SetConfig(cfg)
	t.Cleanup(rt.Shutdown)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(rt))
	request := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/stable/rescrape", strings.NewReader(`{"selected_scrapers":["stub-no-poster"],"show_offline_evidence":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.JSONEq(t, `{"error":"Cannot rescrape cancelled job"}`, response.Body.String())
	require.Equal(t, 1, store.admissions)
	require.Equal(t, 1, store.releases)
	require.Equal(t, models.JobStatusCancelled, job.GetStatus().Status)
	require.Zero(t, source.calls.Load(), "cancellation must be checked before scraping")
	afterResult, _, found := job.Results().GetFileResultByResultID("stable")
	require.True(t, found)
	require.Equal(t, beforeResult, afterResult)
	afterRow, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	require.Equal(t, beforeRow, afterRow, "rejected rescrape must not persist an envelope")
	contents, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "original source", string(contents))
	require.True(t, rt.WaitBackgroundTasks(time.Second), "evidence admission must be released")
	require.NoError(t, deps.JobStore.DeleteJob(job.GetID()), "shared lease must be released")
}
