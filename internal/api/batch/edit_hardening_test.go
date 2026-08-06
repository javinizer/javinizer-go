package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEditHardeningRouter mounts the four edit endpoints + exclusion routes
// against testkit deps.
func newEditHardeningRouter(t *testing.T, cfg *config.Config) (*core.APIDeps, *worker.BatchJob, *gin.Engine) {
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/AAA-100.mp4"})
	setJobResult(job, "/path/to/AAA-100.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/AAA-100.mp4", MovieID: "AAA-100"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "AAA-100", Title: "orig"},
	})
	rt := testkit.GetTestRuntime(deps)
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(rt))
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(rt))
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(rt))
	router.POST("/batch/:id/results/:resultId/exclude", excludeBatchMovie(rt))
	router.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(rt))
	return deps, job, router
}

// jobpersistMarshalSwapForTest swaps the envelope codec's marshal fn to force
// an encode failure INSIDE the tx; restoreMarshalForTest restores it.
func jobpersistMarshalSwapForTest(msg string) func(v any) ([]byte, error) {
	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, fmtErr(msg) }
	return orig
}

type fmtErrString string

func (e fmtErrString) Error() string { return string(e) }
func fmtErr(s string) error          { return fmtErrString(s) }

func restoreMarshalForTest(orig func(v any) ([]byte, error)) { jobpersist.MarshalFn = orig }
func doJSON(t *testing.T, router *gin.Engine, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if payload != nil {
		b, err := json.Marshal(payload)
		require.NoError(t, err)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestEditEndpoints_ScrapingJob_Returns409 — the D16 admission gate: all
// four edit endpoints reject review edits while the job is Pending or
// Running with the scrape phase marker.
func TestEditEndpoints_ScrapingJob_Returns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"PATCH", http.MethodPatch, "/batch/%s/results/AAA-100", contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-100"}}},
		{"poster-crop", http.MethodPost, "/batch/%s/results/AAA-100/poster-crop", contracts.PosterCropRequest{X: 0, Y: 0, Width: 10, Height: 10}},
		{"poster-from-url", http.MethodPost, "/batch/%s/results/AAA-100/poster-from-url", contracts.PosterFromURLRequest{URL: "https://x/p.jpg"}},
		{"field-override", http.MethodPost, "/batch/%s/results/AAA-100/field-override", contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"}},
	}

	for _, state := range []string{"pending", "running-scrape", "running-unknown"} {
		for _, tc := range cases {
			t.Run(state+"/"+tc.name, func(t *testing.T) {
				_, job, router := newEditHardeningRouter(t, cfg)
				switch state {
				case "pending":
					// default
				case "running-scrape":
					job.Controller().SetJobStatus(models.JobStatusRunning)
					job.Lifecycle().SetCurrentPhase(string(worker.JobPhaseScrape))
				case "running-unknown":
					// Legacy: no phase marker persisted — conservatively reject.
					job.Controller().SetJobStatus(models.JobStatusRunning)
				}
				w := doJSON(t, router, tc.method, fmt.Sprintf(tc.path, job.GetID()), tc.body)
				assert.Equal(t, http.StatusConflict, w.Code, "state=%s %s body=%s", state, tc.name, w.Body.String())
			})
		}
	}

	// Positive: Completed and apply-Running ARE admitted.
	for _, state := range []string{"completed", "running-apply"} {
		t.Run("admitted/"+state, func(t *testing.T) {
			_, job, router := newEditHardeningRouter(t, cfg)
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			if state == "running-apply" {
				job.Controller().SetJobStatus(models.JobStatusRunning)
				job.Lifecycle().SetCurrentPhase(string(worker.JobPhaseApply))
			}
			w := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/batch/%s/results/AAA-100", job.GetID()), contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-100", Title: "x"}})
			assert.Equal(t, http.StatusOK, w.Code, "state=%s body=%s", state, w.Body.String())
		})
	}
}

// TestExcludeDuringRunningPhase_Returns409 — exclusions are admitted editor
// operations: 409 while a phase is Running (BOTH currentPhase values).
func TestExcludeDuringRunningPhase_Returns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}

	for _, phase := range []string{string(worker.JobPhaseScrape), string(worker.JobPhaseApply)} {
		t.Run("single/"+phase, func(t *testing.T) {
			_, job, router := newEditHardeningRouter(t, cfg)
			job.Controller().SetJobStatus(models.JobStatusRunning)
			job.Lifecycle().SetCurrentPhase(phase)
			w := doJSON(t, router, http.MethodPost, fmt.Sprintf("/batch/%s/results/AAA-100/exclude", job.GetID()), nil)
			assert.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())
		})
		t.Run("bulk/"+phase, func(t *testing.T) {
			_, job, router := newEditHardeningRouter(t, cfg)
			job.Controller().SetJobStatus(models.JobStatusRunning)
			job.Lifecycle().SetCurrentPhase(phase)
			w := doJSON(t, router, http.MethodPost, fmt.Sprintf("/batch/%s/movies/batch-exclude", job.GetID()), contracts.BatchExcludeRequest{ResultIDs: []string{"AAA-100"}})
			assert.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())
		})
	}

	// Pending exclusions stay admitted (legacy all-excluded auto-cancel flow).
	t.Run("pending-admitted", func(t *testing.T) {
		_, job, router := newEditHardeningRouter(t, cfg)
		w := doJSON(t, router, http.MethodPost, fmt.Sprintf("/batch/%s/results/AAA-100/exclude", job.GetID()), nil)
		assert.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})
}

// TestStore_EditAfterDelete410 — the store-level tombstone surfaces as HTTP
// 410 (not 404) on edit endpoints.
func TestStore_EditAfterDelete410(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/AAA-100.mp4"})
	setJobResult(job, "/path/to/AAA-100.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/AAA-100.mp4", MovieID: "AAA-100"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "AAA-100"},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	jobID := job.GetID()
	require.NoError(t, deps.JobStore.DeleteJob(jobID))

	rt := testkit.GetTestRuntime(deps)
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))

	w := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/batch/%s/results/AAA-100", jobID), contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-100", Title: "x"}})
	assert.Equal(t, http.StatusGone, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "deleted")
}

// TestUpdateBatchMovie_TransactionCommitFailure_Returns5xxAndRollsEverythingBack
// — the composite tx promise (D4): forcing the envelope-encode leg to fail
// inside the tx yields 5xx, no movie-row change, no envelope change, no
// in-memory publication. A subsequent clean save commits everything.
func TestUpdateBatchMovie_TransactionCommitFailure_Returns5xxAndRollsEverythingBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/AAA-200.mp4"})
	setJobResult(job, "/path/to/AAA-200.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/AAA-200.mp4", MovieID: "AAA-200"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "AAA-200", Title: "before"},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	require.NoError(t, deps.JobStore.PersistJobByID(job.GetID()))

	rt := testkit.GetTestRuntime(deps)
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))

	// Envelope before.
	before, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	require.NotNil(t, before)

	orig := jobpersistMarshalSwapForTest("forced encode failure")
	w := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/batch/%s/results/AAA-200", job.GetID()), contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-200", Title: "after"}})
	restoreMarshalForTest(orig)

	require.Equal(t, http.StatusInternalServerError, w.Code, "tx failure must surface 5xx, body=%s", w.Body.String())

	// Movie row: never written.
	mv, mErr := deps.Repos.MovieRepo.FindByID(t.Context(), "AAA-200")
	if mErr == nil && mv != nil {
		assert.Equal(t, "before", mv.Title, "movie row must be unchanged by rolled-back tx")
	}
	// Envelope: unchanged.
	after, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	assert.Equal(t, before.Results, after.Results, "envelope must be unchanged by rolled-back tx")
	// In-memory: unchanged.
	res, rErr := job.Results().GetMovieResult("/path/to/AAA-200.mp4")
	require.NoError(t, rErr)
	assert.Equal(t, "before", res.Movie.Title, "in-memory state must not publish a failed commit")

	// Success side (atomically commits row + envelope, publishes last).
	w2 := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/batch/%s/results/AAA-200", job.GetID()), contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "AAA-200", Title: "after"}})
	require.Equal(t, http.StatusOK, w2.Code, "body=%s", w2.Body.String())
	finalRow, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)
	assert.Contains(t, finalRow.Results, "after", "committed envelope carries the save")
	mv, mErr = deps.Repos.MovieRepo.FindByID(t.Context(), "AAA-200")
	require.NoError(t, mErr)
	require.NotNil(t, mv)
	assert.Equal(t, "after", mv.Title, "committed movie row carries the save")
}
