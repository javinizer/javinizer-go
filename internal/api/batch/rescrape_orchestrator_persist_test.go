package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// failingEnvelopePersist implements the orchestrator's JobPersistencer seam
// with an always-failing upsert, pinning that a persist failure is now
// SURFACED on the orchestrator result (PersistErr) — with the committed
// per-file results retained — instead of warn-only. The poster cache was
// already rolled back (PosterCacheRollback) so a restart cannot resurrect
// pre-rescrape state against the rescraped image.
type failingEnvelopePersist struct {
	err   error
	calls int
}

func (f *failingEnvelopePersist) PersistJobByID(string) error { f.calls++; return f.err }

// stubWfFactory resolves any job to a fixed (nil-ok) workflow.
type stubWfFactory struct{ err error }

func (s stubWfFactory) GetBatchWorkflow(string) (workflow.WorkflowInterface, error) {
	return nil, s.err
}

// stubRescrapeCmdFactory satisfies BatchJobFactoryInterface for the orch tests;
// only NewRescrapeCmd is exercised.
type stubRescrapeCmdFactory struct {
	worker.BatchJobFactoryInterface
}

func (stubRescrapeCmdFactory) NewRescrapeCmd(movieID, filePath, manualSearchInput string, selectedScrapers []string, force bool, mergeOpts workflow.MergeOptions) worker.RescrapeCmd {
	return worker.RescrapeCmd{
		MovieID:           movieID,
		FilePath:          filePath,
		ManualSearchInput: manualSearchInput,
		SelectedScrapers:  selectedScrapers,
		Force:             force,
		Merge:             mergeOpts,
	}
}

func TestRescrapeOrchestrator_Rescrape_PersistFailureSurfacesAndRollsBack(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	rollbackCalls := 0
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{
			Status: models.RescrapeStatusSuccess,
			PosterCacheRollback: func() error {
				rollbackCalls++
				return nil
			},
		}, nil)

	persist := &failingEnvelopePersist{err: errors.New("job repository unavailable")}
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		Persist:   persist,
		ServerCtx: context.Background(),
	})

	result, err := orch.Rescrape(context.Background(), "job-1", "MOV-1", "/f.mp4", &contracts.BatchRescrapeRequest{})
	require.NoError(t, err, "the rescrape itself committed — the envelope persist is a separate failure")
	require.NotNil(t, result)
	require.Error(t, result.PersistErr,
		"a committed-but-unpersisted rescrape must surface the persist failure, not ack it")
	assert.Contains(t, result.PersistErr.Error(), "job repository unavailable")
	assert.Equal(t, 1, persist.calls, "the orchestrator must attempt the persist")
	assert.Equal(t, 1, rollbackCalls, "the poster cache must be rolled back to the pre-rescrape assets")
	require.NotNil(t, result.RescrapeResult)
	assert.Equal(t, models.RescrapeStatusSuccess, result.RescrapeResult.Status,
		"the committed rescrape result is retained even though the persist failed")
	assert.Equal(t, "job-1", result.JobID)
}

func TestRescrapeOrchestrator_Rescrape_PersistFailureRollbackFailureSurfaced(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{
			Status: models.RescrapeStatusSuccess,
			PosterCacheRollback: func() error {
				return errors.New("restore exploded")
			},
		}, nil)

	persist := &failingEnvelopePersist{err: errors.New("job repository unavailable")}
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		Persist:   persist,
		ServerCtx: context.Background(),
	})

	result, err := orch.Rescrape(context.Background(), "job-1", "MOV-1", "/f.mp4", &contracts.BatchRescrapeRequest{})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Error(t, result.PersistErr)
	// The persist error stays primary; the rollback failure is surfaced
	// alongside it, not swallowed (parity with the PATCH/override paths).
	assert.Contains(t, result.PersistErr.Error(), "job repository unavailable")
	assert.Contains(t, result.PersistErr.Error(), "poster rollback failed: restore exploded")
}

func TestRescrapeOrchestrator_BulkRescrape_PersistFailureSurfacesAndRollsBack(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	rollbackCalls := 0
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{
			Status: models.RescrapeStatusSuccess,
			PosterCacheRollback: func() error {
				rollbackCalls++
				return nil
			},
		}, nil)
	mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{})

	persist := &failingEnvelopePersist{err: errors.New("job repository unavailable")}
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		Persist:   persist,
		ServerCtx: context.Background(),
	})

	result, err := orch.BulkRescrape(context.Background(), "job-1", []string{"MOV-1"}, &contracts.BatchRescrapeRequest{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, persist.calls)
	assert.Equal(t, 1, result.Succeeded, "the committed rescrape stays counted")
	require.Error(t, result.PersistErr, "the bulk persist failure must surface on the result")
	assert.Contains(t, result.PersistErr.Error(), "job repository unavailable")
	assert.Equal(t, 1, rollbackCalls, "every successful rescrape's poster cache is rolled back")
	require.Len(t, result.Results, 1, "per-file results are retained alongside the persist failure")
	assert.Equal(t, models.RescrapeStatusSuccess, result.Results[0].Status)
}

// TestPrepareAndLaunchApply_StartApplyError_PersistFailureLogged pins
// execute.go's StartApply-failure path: the failed job start is persisted so
// the failure state survives restarts, and a persist failure there is logged,
// not swallowed.
func TestPrepareAndLaunchApply_StartApplyError_PersistFailureLogged(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)
	// Register a real job under a fixed ID so PersistJobByID resolves to it
	// (persist-by-ID is a store-map lookup; a failed upsert is then recorded
	// on the job's PersistError).
	job := deps.JobStore.CreateJobBatch([]string{"/f.mp4"}, &worker.JobConfig{ID: "job-apply-fail"})

	started := make(chan struct{})
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetID().Return("job-apply-fail").Maybe()
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().StartApply(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, worker.ApplyPhaseConfig) error {
			close(started)
			return errors.New("apply launcher exploded")
		})

	rt := testkit.GetTestRuntime(deps)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/batch/job-apply-fail/organize", nil)
	prepareAndLaunchApply(c, rt, rt.Snapshot(), mockJob, worker.ApplyPhaseConfig{}, "Organization started")

	<-started
	require.Eventually(t, func() bool {
		return job.GetPersistError() != ""
	}, 5*time.Second, 5*time.Millisecond, "the failed StartApply must be persisted; the failed upsert lands on PersistError")
	assert.Contains(t, job.GetPersistError(), "upsert failed")
}

// posterStubScraper is a stub scraper that returns a PosterURL so the rescrape
// phase generates real poster assets (exercising the snapshot/rollback during
// the envelope-persist failure).
type posterStubScraper struct {
	posterURL string
	title     string
}

func (s *posterStubScraper) Name() string { return "stub-persist-poster" }

func (s *posterStubScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")
	return &models.ScraperResult{
		Source:      s.Name(),
		ID:          id,
		ContentID:   id,
		Title:       s.title,
		PosterURL:   s.posterURL,
		ReleaseDate: &releaseDate,
	}, nil
}

func (s *posterStubScraper) GetURL(_ context.Context, id string) (string, error) {
	return "https://example.invalid/" + id, nil
}

func (s *posterStubScraper) IsEnabled() bool { return true }

func (s *posterStubScraper) Close() error { return nil }

func (s *posterStubScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

// TestRescrapeBatchMovie_PersistFailureRestoresPosterCache covers F-B end to
// end at the HTTP layer: GeneratePoster has replaced the cached
// {movieID}-full.jpg/preview, the commit succeeded, but the envelope persist
// then fails. The handler must answer 500 (never ack undurable success), and
// the cached poster assets must be restored to the pre-rescrape bytes so a
// restart cannot resurrect pre-rescrape job state against the rescraped
// image.
func TestRescrapeBatchMovie_PersistFailureRestoresPosterCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)
	allowTestHTTPServerURL(t)

	oldJPEG := posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0x33, A: 0xff})
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x55, A: 0xff})
	newJPEG := posterRefreshJPEG(t, 800, 500, color.RGBA{B: 0x99, A: 0xff})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(newJPEG)
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)
	deps.CoreDeps.GetRegistry().RegisterInstance(&posterStubScraper{posterURL: srv.URL + "/poster.jpg", title: "Persist Poster"})

	// Real poster generator backed by the OS fs (post-chdir "data/temp") with
	// SSRF bypassed for the httptest loopback server.
	gen := poster.NewScrapePosterGenerator(
		poster.NewPosterManager(afero.NewOsFs(), filepath.Join("data", "temp"), srv.Client()).
			WithSSRFCheck(func(string) error { return nil }),
		"", "")

	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	factory, ferr := workflow.NewWorkflowFactory(fc)
	require.NoError(t, ferr)
	wf, ferr := factory.NewWorkflow("")
	require.NoError(t, ferr)

	const movieID = "RBK-901"
	filePath := "/tmp/" + movieID + ".mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath}, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			WF:        wf,
			PosterGen: gen,
			BatchCfg: worker.BatchJobConfig{
				MaxWorkers:      cfg.Performance.MaxWorkers,
				WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
				ScraperPriority: cfg.Scrapers.Priority,
			},
		},
	})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Old Title", Poster: models.PosterState{
			PosterURL:        "https://old.invalid/poster.jpg",
			ShouldCropPoster: false,
		}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	require.NoError(t, os.WriteFile(fullPath, oldJPEG, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-persist-poster"},
		ManualSearchInput: movieID,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a committed-but-unpersisted rescrape must not be acked: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")

	// The committed rescrape stays in memory...
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "Persist Poster", current.Movie.Title, "the commit is not reverted; only the cache rolls back")

	// ...but the cache is restored to the pre-rescrape bytes, keeping
	// restart-reconstructed state and the cached image in agreement.
	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, oldJPEG, full, "-full.jpg must be restored to the pre-rescrape bytes")
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview, "the preview must be restored to the pre-rescrape bytes")

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestBatchRescrapeMovies_PersistFailureReturnsResultsWith500 covers F-B's
// bulk response: the per-file results stay in the response body while the
// persist failure flips the status to 500 with persist_error detail.
func TestBatchRescrapeMovies_PersistFailureReturnsResultsWith500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	job := createJobWithWF(deps, cfg, []string{"/tmp/RBK-902.mp4"})
	setJobResult(job, "/tmp/RBK-902.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/RBK-902.mp4", MovieID: "RBK-902"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "RBK-902", Title: "Old Bulk"},
	})

	router := gin.New()
	router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.BulkRescrapeRequest{
		MovieIDs:         []string{"RBK-902"},
		SelectedScrapers: []string{"stub-no-poster"},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/movies/batch-rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	var resp contracts.BulkRescrapeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.PersistError, "persist", "the persist failure detail must ride along")
	require.Len(t, resp.Results, 1, "the per-file results survive the persist-failure response")
	assert.Equal(t, models.RescrapeStatusSuccess, resp.Results[0].Status)
	assert.Equal(t, 1, resp.Succeeded)
}
