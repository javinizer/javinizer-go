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

// TestRescrapeOrchestrator_Rescrape_PersistFailureSurfaced pins P1-1's
// orchestrator half: the envelope persist (and its rollback) ran INSIDE the
// rescrape phase's critical section, so the orchestrator performs NO persist
// and NO rollback of its own — it only surfaces the phase's PersistErr,
// keeping the committed result shape for the caller.
func TestRescrapeOrchestrator_Rescrape_PersistFailureSurfaced(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	persistErr := errors.New("rescrape committed but job state persist failed: job repository unavailable")
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{
			Status:     models.RescrapeStatusSuccess,
			PersistErr: persistErr,
		}, nil)

	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		ServerCtx: context.Background(),
	})

	result, err := orch.Rescrape(context.Background(), "job-1", "MOV-1", "/f.mp4", &contracts.BatchRescrapeRequest{})
	require.NoError(t, err, "the rescrape itself committed — the envelope persist is a separate failure")
	require.NotNil(t, result)
	assert.ErrorIs(t, result.PersistErr, persistErr,
		"the phase's persist failure is surfaced verbatim, not re-run or rolled back here")
	require.NotNil(t, result.RescrapeResult)
	assert.Equal(t, models.RescrapeStatusSuccess, result.RescrapeResult.Status,
		"the committed rescrape result is retained even though the persist failed")
	assert.Equal(t, "job-1", result.JobID)
}

// TestRescrapeOrchestrator_BulkRescrape_PersistFailureSurfacedPerMovie pins
// the bulk aggregation: a movie whose in-phase envelope persist failed
// reports RescrapeStatusFailed (its state reverted to pre-rescrape), and the
// batch-level PersistErr joins the per-movie failures so the handler can
// answer 500 with the detail.
func TestRescrapeOrchestrator_BulkRescrape_PersistFailureSurfacedPerMovie(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, cmd worker.RescrapeCmd) (*worker.RescrapeResult, error) {
			out := &worker.RescrapeResult{Status: models.RescrapeStatusSuccess}
			if cmd.MovieID == "M2" {
				out.PersistErr = errors.New("job repository unavailable")
			}
			return out, nil
		})
	mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{})

	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		ServerCtx: context.Background(),
	})

	result, err := orch.BulkRescrape(context.Background(), "job-1", []string{"M1", "M2"}, &contracts.BatchRescrapeRequest{})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Error(t, result.PersistErr, "the per-movie persist failure must surface on the batch result")
	assert.Contains(t, result.PersistErr.Error(), "job repository unavailable")
	assert.Equal(t, 1, result.Succeeded, "the genuinely persisted movie stays counted")
	assert.Equal(t, 1, result.Failed, "the rolled-back movie counts as failed — its state is pre-rescrape")
	require.Len(t, result.Results, 2, "per-file results are retained")
	byMovie := map[string]models.RescrapeStatus{}
	var persistResultErr string
	for _, r := range result.Results {
		byMovie[r.MovieID] = r.Status
		if r.MovieID == "M2" {
			persistResultErr = r.Error
		}
	}
	assert.Equal(t, models.RescrapeStatusSuccess, byMovie["M1"])
	assert.Equal(t, models.RescrapeStatusFailed, byMovie["M2"],
		"the rolled-back movie reports failure, not a success the envelope contradicts")
	assert.Contains(t, persistResultErr, "job repository unavailable")
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

// TestRescrapeBatchMovie_PersistFailureRestoresPosterCache covers F-B/F1 end
// to end at the HTTP layer: GeneratePoster has replaced the cached
// {movieID}-full.jpg/preview, the commit succeeded, but the envelope persist
// INSIDE the phase's poster-source lock then fails. The handler must answer
// 500 (never ack undurable success), and BOTH the in-memory MovieResult and
// the cached poster assets must be restored to the pre-rescrape state before
// the phase released its locks — so memory, cache, and the unpersisted
// envelope all converge.
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

	// The in-memory result is restored to the pre-rescrape state (F1)...
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "Old Title", current.Movie.Title,
		"the persist-failure state rollback must restore the pre-rescrape MovieResult in memory")
	assert.Equal(t, "https://old.invalid/poster.jpg", current.Movie.Poster.PosterURL)
	assert.Equal(t, movieID, current.Movie.ID, "no rekey here — identity unchanged")

	// ...and the cache is restored to the pre-rescrape bytes, keeping
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
// bulk response: a movie whose in-phase envelope persist failed reports
// FAILED (its state reverted to pre-rescrape under its own locks), the
// batch-level persist error flips the status to 500, and the per-file
// results stay in the response body.
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
	require.Len(t, resp.Results, 1, "the per-file result survives the persist-failure response")
	assert.Equal(t, models.RescrapeStatusFailed, resp.Results[0].Status,
		"the rolled-back movie reports failure — its in-memory state is pre-rescrape")
	assert.Contains(t, resp.Results[0].Error, "persist")
	assert.Equal(t, 0, resp.Succeeded)
	assert.Equal(t, 1, resp.Failed)

	// F1: the in-memory result converges back to the pre-rescrape state as
	// well — not only the cache — so memory matches the unpersisted envelope.
	stored := storedMovieResult(t, job, "RBK-902")
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "Old Bulk", stored.Movie.Title,
		"the bulk persist failure must restore the pre-rescrape MovieResult in memory")
}

// TestRescrapeOrchestrator_Rescrape_ErrorGuards pins the pre-execution
// rejections of the single-movie path: unknown job, workflow resolution
// failure, invalid merge options, and an erroring job.Rescrape all return
// errors without touching persistence.
func TestRescrapeOrchestrator_Rescrape_ErrorGuards(t *testing.T) {
	cfg := &config.Config{}

	t.Run("job not found", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "") // empty store: any job lookup misses
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  deps.JobStore,
			WfFactory: stubWfFactory{},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.Rescrape(context.Background(), "nope", "M", "/f.mp4", &contracts.BatchRescrapeRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("workflow init failure", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		mockJob := workermocks.NewMockBatchJobInterface(t)
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
			WfFactory: stubWfFactory{err: errors.New("wf exploded")},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.Rescrape(context.Background(), "job-1", "M", "/f.mp4", &contracts.BatchRescrapeRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow init failed")
	})

	t.Run("invalid merge options", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything)
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
			WfFactory: stubWfFactory{},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.Rescrape(context.Background(), "job-1", "M", "/f.mp4",
			&contracts.BatchRescrapeRequest{ScalarStrategy: "bogus-strategy"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid merge options")
	})

	t.Run("job rescrape error", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything)
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(nil, errors.New("rescrape exploded"))
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
			WfFactory: stubWfFactory{},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.Rescrape(context.Background(), "job-1", "M", "/f.mp4", &contracts.BatchRescrapeRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rescrape exploded")
	})
}

// TestRescrapeOrchestrator_BulkRescrape_ErrorGuards covers the bulk path's
// pre-execution rejections (unknown job, workflow failure) and the
// nil-ServerCtx / caller-cancellation context plumbing.
func TestRescrapeOrchestrator_BulkRescrape_ErrorGuards(t *testing.T) {
	cfg := &config.Config{}

	t.Run("job not found", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  deps.JobStore,
			WfFactory: stubWfFactory{},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.BulkRescrape(context.Background(), "nope", []string{"M1"}, &contracts.BatchRescrapeRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("workflow init failure", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		mockJob := workermocks.NewMockBatchJobInterface(t)
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
			WfFactory: stubWfFactory{err: errors.New("wf exploded")},
			Factory:   stubRescrapeCmdFactory{},
			ServerCtx: context.Background(),
		})
		_, err := orch.BulkRescrape(context.Background(), "job-1", []string{"M1"}, &contracts.BatchRescrapeRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow init failed")
	})

	t.Run("canceled caller ctx cancels work", func(t *testing.T) {
		deps := createTestDeps(t, cfg, "")
		rescrapeStarted := make(chan struct{})
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything)
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).RunAndReturn(
			// The pool ctx is derived from BOTH the (here nil) ServerCtx and
			// the caller's ctx: once the caller cancels, the ctx this mock
			// receives is done — deterministically exercising the
			// cancellation-watcher branch.
			func(ctx context.Context, _ worker.RescrapeCmd) (*worker.RescrapeResult, error) {
				select {
				case <-rescrapeStarted:
				default:
					close(rescrapeStarted)
				}
				<-ctx.Done()
				return &worker.RescrapeResult{Status: models.RescrapeStatusSuccess}, nil
			})
		mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{})

		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
			WfFactory: stubWfFactory{},
			Factory:   stubRescrapeCmdFactory{},
			// ServerCtx deliberately nil: the orchestrator must fall back to
			// context.Background for the work context base.
		})

		callerCtx, cancel := context.WithCancel(context.Background())
		type outcome struct {
			res *RescrapeResult
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			res, err := orch.BulkRescrape(callerCtx, "job-1", []string{"M1"}, &contracts.BatchRescrapeRequest{})
			done <- outcome{res, err}
		}()

		<-rescrapeStarted // the pool worker is parked inside Rescrape
		cancel()          // the cancellation watcher releases it
		out := <-done
		require.NoError(t, out.err)
		require.NotNil(t, out.res)
		assert.Equal(t, 1, out.res.Succeeded)
		assert.NoError(t, out.res.PersistErr)
	})
}
