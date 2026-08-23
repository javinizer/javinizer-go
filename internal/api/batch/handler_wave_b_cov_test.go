package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// exclusionStore honours the exclusion-acquisition entry point used by the
// exclude handlers (cropErrorJobStore does not).
// acquires always admit, persisting is the default no-op.
type excludeEdgeStore struct {
	worker.JobStoreInterface
	job        worker.BatchJobInterface
	deleteErr  error
	persistErr error
}

func (s *excludeEdgeStore) GetBatchJob(string) (worker.BatchJobInterface, bool) { return s.job, true }
func (s *excludeEdgeStore) AcquireExclusionAccess(string) (worker.BatchJobInterface, func(), error) {
	return s.job, func() {}, nil
}
func (s *excludeEdgeStore) AcquireEditAccess(string) (worker.BatchJobInterface, func(), error) {
	return s.job, func() {}, nil
}
func (s *excludeEdgeStore) DeleteJob(string) error      { return s.deleteErr }
func (s *excludeEdgeStore) PersistJobByID(string) error { return s.persistErr }

func TestExcludeBatchMovie_GoneTypedErrorIs410(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("EX-1").Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ex.mp4", MovieID: "EX-1"},
		Movie:         &models.Movie{ID: "EX-1"},
	}, "/f/ex.mp4", true)
	mockJob.EXPECT().FindFilePathsForMovieID("EX-1").Return([]string{"/f/ex.mp4"}).Maybe()
	mockJob.EXPECT().ExcludeMovieFamily(mock.Anything, "EX-1").Return(worker.ErrJobGone)

	deps := createTestDeps(t, &config.Config{}, "")
	deps.JobStore = &excludeEdgeStore{job: mockJob}
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/exclude", excludeBatchMovie(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+uuidless(t)+"/results/EX-1/exclude", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 410, w.Code)
}

func uuidless(t *testing.T) string { t.Helper(); return "job-any" }

func TestBatchExcludeMovies_PartialFailureListsFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("A-1").Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "A-1"},
		Movie:         &models.Movie{ID: "A-1"},
	}, "/f/a.mp4", true)
	mockJob.EXPECT().GetFileResultByResultID("B-1").Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "B-1"},
		Movie:         &models.Movie{ID: "B-1"},
	}, "/f/b.mp4", true)
	mockJob.EXPECT().FindFilePathsForMovieID("A-1").Return([]string{"/f/a.mp4"}).Maybe()
	mockJob.EXPECT().FindFilePathsForMovieID("B-1").Return([]string{"/f/b.mp4"}).Maybe()
	mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{}).Maybe()
	mockJob.EXPECT().ExcludeMovieFamily(mock.Anything, "A-1").Return(nil)
	mockJob.EXPECT().ExcludeMovieFamily(mock.Anything, "B-1").Return(worker.ErrEditNotAdmitted)

	deps := createTestDeps(t, &config.Config{}, "")
	deps.JobStore = &excludeEdgeStore{job: mockJob}
	router := gin.New()
	router.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(testkit.GetTestRuntime(deps)))
	payload, _ := json.Marshal(contracts.BatchExcludeRequest{ResultIDs: []string{"A-1", "B-1"}})
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/movies/batch-exclude", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	var resp contracts.BatchExcludeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Failed, 1)
	assert.Equal(t, "B-1", resp.Failed[0].ResultID)
	require.Len(t, resp.Excluded, 1)
	assert.Equal(t, "A-1", resp.Excluded[0])
}

func TestDeleteBatchJob_GoneTypedIs410(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	mockJob := workermocks.NewMockBatchJobInterface(t)
	deps.JobStore = &excludeEdgeStore{job: mockJob, deleteErr: worker.ErrJobGone}
	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodDelete, "/batch/job-gone", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 410, w.Code, w.Body.String())
}

func TestDeleteBatchJob_NotFoundTypedIs404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	mockJob := workermocks.NewMockBatchJobInterface(t)
	deps.JobStore = &excludeEdgeStore{job: mockJob, deleteErr: worker.ErrJobNotFound}
	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodDelete, "/batch/job-nope", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code, w.Body.String())
}

// --- writeEditOpError classification coverage ---

func TestWriteEditOpErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"ssrf strings stay 400", errors.New("SSRF blocked: private"), 400},
		{"download strings stay 502", errors.New("download failed: timeout"), 502},
		{"status strings stay 502", errors.New("unexpected status 503"), 502},
		{"unknown error is 500", errors.New("plain boom"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newGinCtx(t)
			writeEditOpError(c, tc.err)
			assert.Equal(t, tc.code, w.Code)
		})
	}
}

// --- field override classification arm ---

func TestApplyBatchFieldOverrideValidationErrIs400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().ApplyFieldOverrideWithRevisions(mock.Anything, "FO-1", "maker", "dmm").
		Return(nil, nil, nil, errors.New("no provenance available for maker"))

	deps := createTestDeps(t, &config.Config{}, "")
	deps.JobStore = &excludeEdgeStore{job: mockJob}
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	payload, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/FO-1/field-override", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code, w.Body.String())
}

// --- apply-launch failure arm (StartApply error → envelope persist warn) ---

func TestPrepareAndLaunchApplyStartError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	rt := testkit.GetTestRuntime(deps)
	oneShot := make(chan struct{})
	var fired atomic.Bool

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetID().Return("job-apply-edge").Maybe()
	mockJob.EXPECT().SetWorkflow(mock.Anything).Return()
	mockJob.EXPECT().StartApply(mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, workerApplyPhaseConfigAlias) error {
			fired.Store(true)
			close(oneShot)
			return errors.New("apply wedged")
		})
	mockJob.EXPECT().Wait().Return(nil).Maybe()

	store := &excludeEdgeStore{job: mockJob, persistErr: errors.New("disk read-only")}
	deps.JobStore = store

	c, _ := newGinCtx(t)
	prepareAndLaunchApply(c, rt, rt.Snapshot(), mockJob, worker.ApplyPhaseConfig{}, "done")
	select {
	case <-oneShot:
	case <-time.After(3 * time.Second):
		t.Fatal("apply goroutine never ran")
	}
	assert.True(t, fired.Load())
}

type workerApplyPhaseConfigAlias = worker.ApplyPhaseConfig
