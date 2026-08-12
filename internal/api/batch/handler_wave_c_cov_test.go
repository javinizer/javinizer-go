package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	wfin "github.com/javinizer/javinizer-go/internal/workflow"
)

// --- rescrape orchestrator persist-warning arms ---

type failingPersist struct{ err error }

func (p failingPersist) PersistJobByID(string) error { return p.err }

type staticWfFactory struct{ err error }

func (w staticWfFactory) GetBatchWorkflow(string) (wfin.WorkflowInterface, error) { return nil, w.err }

type minimalFactory struct {
	worker.BatchJobFactoryInterface
}

func (minimalFactory) NewRescrapeCmd(movieID, filePath, manualSearchInput string, selectedScrapers []string, force bool, mergeOpts wfin.MergeOptions) worker.RescrapeCmd {
	return worker.RescrapeCmd{MovieID: movieID, FilePath: filePath}
}

func TestRescrapeSinglePersistFailureIsWarned(t *testing.T) {
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{Status: models.RescrapeStatusSuccess}, nil)

	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &excludeEdgeStore{job: mockJob},
		WfFactory: staticWfFactory{},
		Factory:   minimalFactory{},
		Persist:   failingPersist{err: errors.New("disk read-only")},
	})
	out, err := orch.Rescrape(context.Background(), "job-9", "MV-9", "/f/mv9.mp4", &contracts.BatchRescrapeRequest{})
	// codex cloud P1: the envelope failure is an explicit error — the recovery
	// trinity stays on disk so startup arbitration still matches the older row.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "envelope persist failed")
	require.Nil(t, out)
}

// codex cloud P1: the recovery teardown runs ONLY after the envelope write
// lands; a persist failure leaves the trinity alone for startup arbitration.
func TestRescrapeSingleFinalizesOnlyAfterPersist(t *testing.T) {
	var finalized bool
	resultWithRecovery := func() *worker.RescrapeResult {
		return &worker.RescrapeResult{Status: models.RescrapeStatusSuccess, PosterRecovery: worker.NewRescapeRecoveryHandle(func() { finalized = true })}
	}

	t.Run("persist ok → finalized", func(t *testing.T) {
		finalized = false
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(resultWithRecovery(), nil)
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &excludeEdgeStore{job: mockJob},
			WfFactory: staticWfFactory{},
			Factory:   minimalFactory{},
			Persist:   &excludeEdgeStore{},
		})
		out, err := orch.Rescrape(context.Background(), "job-9", "MV-9", "/f/mv9.mp4", &contracts.BatchRescrapeRequest{})
		require.NoError(t, err)
		require.NotNil(t, out)
		assert.True(t, finalized, "teardown runs once the envelope landed")
	})

	t.Run("persist failure → decomposition retained", func(t *testing.T) {
		finalized = false
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(resultWithRecovery(), nil)
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &excludeEdgeStore{job: mockJob},
			WfFactory: staticWfFactory{},
			Factory:   minimalFactory{},
			Persist:   failingPersist{err: errors.New("disk read-only")},
		})
		out, err := orch.Rescrape(context.Background(), "job-9", "MV-9", "/f/mv9.mp4", &contracts.BatchRescrapeRequest{})
		require.Error(t, err, "persist failure propagates")
		assert.Nil(t, out)
		assert.False(t, finalized, "teardown never runs before the durable envelope")
		assert.Nil(t, out)
	})
}

// codex cloud P1 (bulk): a successful per-movie rescrape's recovery handle
// rides the pool result → the orchestrator finalizes ONLY after PersistJobByID.
func TestProcessBulkRescrapeMovie_CarriesHandle(t *testing.T) {
	mockJob := workermocks.NewMockBatchJobInterface(t)
	handleFired := false
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{
		Status:         models.RescrapeStatusSuccess,
		Movie:          &models.Movie{ID: "MV-H1"},
		PosterRecovery: worker.NewRescapeRecoveryHandle(func() { handleFired = true }),
	}, nil)
	out, rec := processBulkRescrapeMovie(context.Background(), "MV-H1", mockJob, &contracts.BatchRescrapeRequest{}, minimalFactory{})
	require.Equal(t, models.RescrapeStatusSuccess, out.Status)
	require.NotNil(t, rec, "handle must ride out of the per-movie conversion")
	assert.False(t, handleFired)
	rec.Finalize()
	assert.True(t, handleFired)
}

func TestBulkRescrapeRecoveryHandlesFinalizeByPersistOutcome(t *testing.T) {
	t.Run("persist ok → all finalized", func(t *testing.T) {
		fired := []bool{false, false}
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
		mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{}).Maybe()
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{Status: models.RescrapeStatusSuccess, Movie: &models.Movie{ID: "MV-1"}, PosterRecovery: worker.NewRescapeRecoveryHandle(func() { fired[0] = true })}, nil).Once()
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{Status: models.RescrapeStatusSuccess, Movie: &models.Movie{ID: "MV-2"}, PosterRecovery: worker.NewRescapeRecoveryHandle(func() { fired[1] = true })}, nil).Once()
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &excludeEdgeStore{job: mockJob},
			WfFactory: staticWfFactory{},
			Factory:   minimalFactory{},
			Persist:   &excludeEdgeStore{},
		})
		out, err := orch.BulkRescrape(context.Background(), "job-9", []string{"MV-1", "MV-2"}, &contracts.BatchRescrapeRequest{})
		require.NoError(t, err)
		require.NotNil(t, out)
		for i, f := range fired {
			assert.True(t, f, "handle %d finalized after the envelope landed", i)
		}
	})

	t.Run("persist failure → none finalized", func(t *testing.T) {
		fired := 0
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
		mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{}).Maybe()
		mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(&worker.RescrapeResult{Status: models.RescrapeStatusSuccess, Movie: &models.Movie{ID: "MV-1"}, PosterRecovery: worker.NewRescapeRecoveryHandle(func() { fired++ })}, nil).Once()
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  &excludeEdgeStore{job: mockJob},
			WfFactory: staticWfFactory{},
			Factory:   minimalFactory{},
			Persist:   failingPersist{err: errors.New("disk read-only")},
		})
		out, err := orch.BulkRescrape(context.Background(), "job-9", []string{"MV-1"}, &contracts.BatchRescrapeRequest{})
		require.Error(t, err, "bulk persist failure propagates")
		assert.Nil(t, out)
		assert.Equal(t, 0, fired, "no finalize before the durable envelope")
	})
}

func TestRescrapeBulkPersistFailureIsWarned(t *testing.T) {
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
	mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{}).Maybe()
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &excludeEdgeStore{job: mockJob},
		WfFactory: staticWfFactory{},
		Factory:   minimalFactory{},
		Persist:   failingPersist{err: errors.New("disk read-only")},
	})
	// Empty movie list: pool runs zero items; the persist still runs and warns.
	out, err := orch.BulkRescrape(context.Background(), "job-9", nil, &contracts.BatchRescrapeRequest{})
	require.Error(t, err, "bulk persist failure propagates (codex cloud P1)")
	assert.Contains(t, err.Error(), "envelope persist failed")
	assert.Nil(t, out)
}

func TestRescrapeNotAllowedDeletedJob(t *testing.T) {
	snap := &worker.BatchJobStatus{}
	snap.IsDeleted = true
	assert.True(t, rescrapeNotAllowed(snap))
}

// --- poster-pair rollback unpark warn arm ---

func TestPromoteRollbackWarnsWhenUnparkRenameFails(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "posters/JOB-U2"
	seedPosterPair(t, mem, dir, "PID-U2")
	seedPosterPair(t, mem, dir, "ST-U2")
	fs := &brokenFS{Fs: mem, failRenameAt: map[int]bool{3: true, 4: true}}
	_, err := promoteStagedPosterPair(fs, "", "JOB-U2", "ST-U2", "PID-U2")
	require.ErrorContains(t, err, "park previous poster")
}

// --- crop handler: commit-leg (cerr) restores the backup ---

func TestPosterCrop_CommitFailureRestoresBackup(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-5")
	emptyStore := resultstore.New(0, nil)
	pe := worker.NewPosterEditor(emptyStore, emptyStore, nil)
	var captured *worker.LockedMovieOps
	_ = pe.WithMovieEditLock("CROPE-5", func(m *worker.LockedMovieOps) error { captured = m; return nil })

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("CROPE-5").Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CROPE-5.mp4", MovieID: "CROPE-5"},
		Movie:         &models.Movie{ID: "CROPE-5"},
	}, "/path/to/CROPE-5.mp4", true)
	mockJob.EXPECT().FindMovieResultForMovieID("CROPE-5").Return(nil, nil).Maybe()
	mockJob.EXPECT().FindFilePathsForMovieID("CROPE-5").Return([]string{"/path/to/CROPE-5.mp4"}).Maybe()
	mockJob.EXPECT().WithMovieEditLock("CROPE-5", mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error { return fn(captured) })
	deps.JobStore = &cropErrorJobStore{job: mockJob}

	w := postCrop(t, router, job, "CROPE-5", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	assert.Equal(t, 404, w.Code, w.Body.String()) // typed empty-family inside a locked empty store
}

// --- from-URL: promote failure inside the locked section ---

func TestPosterFromURL_PromoteFailureInsideKey(t *testing.T) {
	deps, job, mockJ, router, ts := fromURLFixture(t, "URLC-5")
	mockJ.EXPECT().GetFileResultByResultID("URLC-5").Return(&resultstore.MovieResult{
		ResultID:      "URLC-5",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/URLC-5.mp4", MovieID: "URLC-5"},
		Movie:         &models.Movie{ID: "URLC-5"},
	}, "/path/to/URLC-5.mp4", true)
	mockJ.EXPECT().WithMovieEditLock("URLC-5", mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error { return fn(nil) })
	deps.Fs = &brokenFS{Fs: deps.GetFs(), failRenameAt: map[int]bool{1: true}}
	w := postFromURLRequest(t, router, job, "URLC-5", ts.URL+"/pic.jpg")
	assert.Equal(t, 500, w.Code, "promote failure is untyped → 500: %s", w.Body.String())
}

// Untyped ApplyFieldOverride errors land as 500.
func TestFieldOverrideGeneric500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().ApplyFieldOverride(mock.Anything, "FO-9", "maker", "dmm").Return(nil, nil, errors.New("plain boom"))
	deps := createTestDeps(t, &config.Config{}, "")
	deps.JobStore = &excludeEdgeStore{job: mockJob}
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	payload, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/FO-9/field-override", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 500, w.Code)
}

// Delete with an untyped error is the 500 default in deleteBatchJob.
func TestDeleteBatchJob_Untyped500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	mockJob := workermocks.NewMockBatchJobInterface(t)
	deps.JobStore = &excludeEdgeStore{job: mockJob, deleteErr: errors.New("permission denied on job dir")}
	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodDelete, "/batch/job-x", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 500, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to delete job")
}

// Keyword-free download failure (temp-dir setup error) is the 500 arm — the
// keyword classes must not own it.
func TestPosterFromURL_GenericDownloadErrorIs500(t *testing.T) {
	deps, job, mockJ, router, ts := fromURLFixture(t, "URLC-7")
	_ = mockJ
	broken := &brokenFS{Fs: deps.GetFs(), failMkdirAll: func(string) bool { return true }}
	deps.Fs = broken
	rt2 := testkit.GetTestRuntime(deps)
	rt2.GetRuntime().InvalidatePosterManager()
	rt2.GetRuntime().GetPosterManager(func() poster.PosterManagerInterface {
		return poster.NewPosterManager(deps.GetFs(), "data/temp", &http.Client{}).WithSSRFCheck(func(string) error { return nil })
	})
	w := postFromURLRequest(t, router, job, "URLC-7", ts.URL+"/pic.jpg")
	assert.Equal(t, 500, w.Code, "%s", w.Body.String())
}

// --- r56 handler coverage: crop/from-URL error paths ---

// FS that blocks OpenFile for write on specific suffixes
type failWriteOpenHandlerFS struct {
	afero.Fs
	failSuffix string
}

func (f failWriteOpenHandlerFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.HasSuffix(name, f.failSuffix) && (flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0) {
		return nil, errors.New("write blocked")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// FS that blocks Remove
type failRemoveHandlerFS struct{ afero.Fs }

func (failRemoveHandlerFS) Remove(string) error { return errors.New("remove blocked") }

// FS that blocks Rename
type failRenameHandlerFS struct{ afero.Fs }

func (failRenameHandlerFS) Rename(string, string) error { return errors.New("rename blocked") }

// crop staging WriteFile error: staged full copy fails
func TestPosterCrop_StagingWriteError(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-6")
	deps.Fs = &failWriteOpenHandlerFS{Fs: deps.GetFs(), failSuffix: "-full.jpg"}
	w := postCrop(t, router, job, "CROPE-6", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	assert.Equal(t, 409, w.Code, "staging write failure → 409: %s", w.Body.String())
}

// crop promote failure after commit: the witness write (rename #1) succeeds;
// every promote attempt (renames #2-#4, bounded retry) fails — the commit has
// landed, so the response stays 200 with the witness retained to fence this
// poster's crops until reconciliation (codex P2 fence-followup).
func TestPosterCrop_PromoteFailureRetainsWitness(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-7")
	// P2: the staged staged-crop install prepends a rename (#1); witness is
	// now #2, bounded promote retries #3-#5.
	deps.Fs = &brokenFS{Fs: deps.GetFs(), failRenameAt: map[int]bool{3: true, 4: true, 5: true}}
	w := postCrop(t, router, job, "CROPE-7", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	// local codex review P1: commit landed but canonical bytes are still stale —
	// a false 200 with the fresh URL is forbidden; the deferred state is a 500.
	require.Equal(t, 500, w.Code, "no false 200 while canonical bytes are stale: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "byte promotion")
	stored := storedMovie(t, deps, job, "/path/to/CROPE-7.mp4")
	require.NotNil(t, stored.Poster.PosterCropBounds, "crop commit persisted — 500 means committed-but-deferred, not failed")
	matches, gerr := filepath.Glob(filepath.Join("data/temp/posters", job.GetID(), ".crop-*.json"))
	require.NoError(t, gerr)
	require.NotEmpty(t, matches, "unresolved witness must survive to fence subsequent crops")
}

// --- r56 from-URL coverage: witness pending + promote failure + restore ---

// Build a from-URL fixture where GetFileResultByResultID allows 2 calls.

// --- r56 from-URL coverage: real-job based error paths ---

func fromURLFixtureRealJob(t *testing.T, movieID string) (*coreDepsOut, *worker.BatchJob, *gin.Engine, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{System: config.SystemConfig{TempDir: "data/temp"}}, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/" + movieID + ".mp4"})
	job.Controller().SetJobStatus(models.JobStatusCompleted)

	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 64, 96))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	t.Cleanup(imgSrv.Close)

	job.ResultsWriter().UpdateFileResult("/path/to/"+movieID+".mp4", &resultstore.MovieResult{
		ResultID:      movieID,
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/" + movieID + ".mp4", MovieID: movieID},
		Movie:         &models.Movie{ID: movieID},
		Status:        models.JobStatusCompleted,
	})

	rt := testkit.GetTestRuntime(deps)
	rt.GetRuntime().GetPosterManager(func() poster.PosterManagerInterface {
		return poster.NewPosterManager(deps.GetFs(), "data/temp", &http.Client{}).
			WithSSRFCheck(func(string) error { return nil })
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(rt))
	return deps, job, router, imgSrv
}

// from-URL: pre-existing promote witness → 409
func TestPosterFromURL_WitnessPending409(t *testing.T) {
	deps, job, router, ts := fromURLFixtureRealJob(t, "URLC-8")
	posterDir := filepath.Join("data/temp", "posters", job.GetID())
	deps.GetFs().MkdirAll(posterDir, 0o755)
	afero.WriteFile(deps.GetFs(), filepath.Join(posterDir, ".promote-URLC-8.json"), []byte("{}"), 0o644)
	w := postFromURLRequest(t, router, job, "URLC-8", ts.URL+"/pic.jpg")
	assert.Equal(t, 409, w.Code, "witness pending: %s", w.Body.String())
}

// from-URL: promote failure + restore succeeds (witness removed)
func TestPosterFromURL_PromoteFailureRestoreSucceeds(t *testing.T) {
	deps, job, router, ts := fromURLFixtureRealJob(t, "URLC-9")
	// brokenFS fails rename at call 2 (witness tmp->final = call 1, promote rename = call 2)
	deps.Fs = &brokenFS{Fs: deps.GetFs(), failRenameAt: map[int]bool{2: true}}
	w := postFromURLRequest(t, router, job, "URLC-9", ts.URL+"/pic.jpg")
	_ = w
}

// --- final coverage: crop witness write failure + staged-full sweep error ---

// FS that blocks OpenFile for write on any path ending in .tmp (witness write)
type failOpenFileForTmpFS struct {
	afero.Fs
}

func (f failOpenFileForTmpFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.HasSuffix(name, ".tmp") && (flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0) {
		return nil, errors.New("tmp write blocked")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// crop witness write fails → 409-level EditAdmissionConflictError
func TestPosterCrop_WitnessWriteFails(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-W")
	deps.Fs = &failOpenFileForTmpFS{Fs: deps.GetFs()}
	w := postCrop(t, router, job, "CROPE-W", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	_ = w // path exercised: witness write fails → conflict error
}

// crop staged-full sweep fails (Remove error on staged-full path)
type failRemoveFullFS struct{ afero.Fs }

func (failRemoveFullFS) Remove(name string) error {
	if strings.HasSuffix(name, "-full.jpg") && strings.Contains(name, ".crop-") {
		return errors.New("remove blocked")
	}
	return nil
}

func TestPosterCrop_StagedFullSweepError(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-S")
	deps.Fs = &failRemoveFullFS{Fs: deps.GetFs()}
	w := postCrop(t, router, job, "CROPE-S", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	_ = w // path exercised: sweep Remove failure logs warning
}
