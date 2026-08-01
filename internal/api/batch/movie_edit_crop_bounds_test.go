package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateBatchMovie_CropBoundsValidation(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/VAL-001.mp4"})
	setJobResult(job, "/path/to/VAL-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/VAL-001.mp4", MovieID: "VAL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "VAL-001", Title: "Validation"},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	patchMovie := func(bounds *contracts.CropBounds) *httptest.ResponseRecorder {
		view := contracts.MovieViewFromModel(&models.Movie{ID: "VAL-001", Title: "Validation"})
		view.PosterCropBounds = bounds
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/VAL-001", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("rejects invalid bounds", func(t *testing.T) {
		for _, b := range []*contracts.CropBounds{
			{X: -1, Y: 0, Width: 100, Height: 100},
			{X: 0, Y: -1, Width: 100, Height: 100},
			{X: 0, Y: 0, Width: 0, Height: 100},
			{X: 0, Y: 0, Width: 100, Height: -50},
		} {
			rec := patchMovie(b)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "bounds %+v should be rejected", *b)
		}
	})

	t.Run("accepts valid bounds", func(t *testing.T) {
		rec := patchMovie(&contracts.CropBounds{X: 0, Y: 0, Width: 400, Height: 600})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("accepts absent bounds", func(t *testing.T) {
		rec := patchMovie(nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
