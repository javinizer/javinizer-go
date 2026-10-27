package batch

import (
	"bytes"
	"context"
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
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOfflineEvidenceBatchClosedAdmissionBeforeDependencies(t *testing.T) {
	rt := core.NewAPIRuntime(nil)
	rt.Shutdown()
	for _, route := range []struct {
		path, body string
		handler    gin.HandlerFunc
	}{
		{"/batch/scrape", `{"files":["/never/read.mp4"],"show_offline_evidence":true}`, batchScrape(rt)},
		{"/batch/job/results/result/rescrape", `{"selected_scrapers":["r18dev"],"show_offline_evidence":true}`, rescrapeBatchMovie(rt)},
		{"/batch/job/rescrape", `{"movie_ids":["TEST-001"],"selected_scrapers":["r18dev"],"show_offline_evidence":true}`, batchRescrapeMovies(rt)},
	} {
		t.Run(route.path, func(t *testing.T) {
			router := gin.New()
			router.POST(route.path, route.handler)
			request := httptest.NewRequest(http.MethodPost, route.path, bytes.NewBufferString(route.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
		})
	}
	_, err := StartScrapeUseCase(t.Context(), rt, StartScrapeInput{ShowOfflineEvidence: true})
	require.ErrorIs(t, err, core.ErrOfflineEvidenceShuttingDown)
}

type evidenceBatchSource struct {
	noPosterStubScraper
	calls atomic.Int32
}

func (s *evidenceBatchSource) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.calls.Add(1)
	return s.noPosterStubScraper.Search(ctx, id)
}

func TestOfflineEvidenceBatchAdmissionOwnsCreateJobUntilShutdownJoin(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "EVIDENCE-001.mp4")
	require.NoError(t, os.WriteFile(file, []byte("fixture"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{dir}
	cfg.Scrapers.Priority = []string{"stub-no-poster"}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	source := &evidenceBatchSource{}
	deps.CoreDeps.GetRegistry().RegisterInstance(source)
	entered, resume := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	require.NoError(t, deps.Repos.DB.Callback().Create().Before("gorm:create").Register("evidence_create_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "jobs" && blocked.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
	}))
	router := gin.New()
	router.POST("/batch/scrape", batchScrape(rt))
	body, err := json.Marshal(map[string]any{"files": []string{file}, "selected_scrapers": []string{"stub-no-poster"}, "show_offline_evidence": true})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/batch/scrape", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	served := make(chan struct{})
	go func() { router.ServeHTTP(response, request); close(served) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job persistence barrier not entered")
	}
	shutdown := make(chan struct{})
	go func() { rt.Shutdown(); close(shutdown) }()
	select {
	case <-rt.ServerCtx().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel")
	}
	select {
	case <-shutdown:
		t.Fatal("shutdown escaped admitted DB work")
	default:
	}
	release()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("handler failed to join")
	}
	select {
	case <-shutdown:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown failed to join")
	}
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, source.calls.Load())
	var jobs []models.Job
	require.NoError(t, deps.Repos.DB.Find(&jobs).Error)
	require.Len(t, jobs, 1)
	require.Equal(t, models.JobStatusCancelled, jobs[0].Status)
}

func TestOfflineEvidenceAPIConvertersOwnHistoricalReport(t *testing.T) {
	report := &scrapeevidence.Report{Outcome: scrapeevidence.Available, SnapshotDigest: "historical", AdmittedAt: time.Now().UTC(), Observations: []scrapeevidence.Observation{{Source: "r18dev", SourceID: "007", Aliases: []string{"Whole alias"}}}}
	prov := &resultstore.ProvenanceData{OfflineEvidence: report}
	result := &resultstore.MovieResult{ResultID: "stable", Revision: 4, Movie: &models.Movie{ID: "TEST-001"}}
	full := movieResultToResponse(result, prov)
	slim := movieResultToSlimResponse(result, prov)
	full.OfflineEvidence.Observations[0].Aliases[0] = "changed"
	require.Equal(t, "Whole alias", slim.OfflineEvidence.Observations[0].Aliases[0])
	require.Equal(t, "Whole alias", report.Observations[0].Aliases[0])
	require.Equal(t, uint64(4), full.Revision)
	require.Nil(t, movieResultToResponse(result, nil).OfflineEvidence)
	require.Nil(t, movieResultToSlimResponse(result, nil).OfflineEvidence)
}
