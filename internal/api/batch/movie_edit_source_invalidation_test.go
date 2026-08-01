package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateBatchMovie_PosterSourceChangeInvalidatesCropBounds(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	setup := func(t *testing.T) (*core.APIDeps, *gin.Engine, string) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/path/to/SRC-001.mp4"})
		setJobResult(job, "/path/to/SRC-001.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/SRC-001.mp4", MovieID: "SRC-001"},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: "SRC-001", Title: "Src", Poster: models.PosterState{
				PosterURL:        "https://example.com/a.jpg",
				CoverURL:         "https://example.com/a-cover.jpg",
				ShouldCropPoster: false,
				CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600},
			}},
		})
		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		return deps, router, job.GetID()
	}

	patch := func(t *testing.T, router *gin.Engine, jobID, posterURL, coverURL string, shouldCrop bool, bounds *contracts.CropBounds) {
		view := contracts.MovieViewFromModel(&models.Movie{ID: "SRC-001", Title: "Src", Poster: models.PosterState{
			PosterURL:        posterURL,
			CoverURL:         coverURL,
			ShouldCropPoster: shouldCrop,
			CropBounds:       bounds.ToModel(),
		}})
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/SRC-001", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	storedBounds := func(t *testing.T, deps *core.APIDeps, jobID string) *models.CropBounds {
		job, ok := deps.JobStore.GetBatchJob(jobID)
		require.True(t, ok)
		for _, r := range job.GetStatus().Results {
			if r.Movie != nil {
				return r.Movie.Poster.CropBounds
			}
		}
		t.Fatal("no movie result found")
		return nil
	}

	valid := &contracts.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	t.Run("poster_url change clears bounds", func(t *testing.T) {
		deps, router, jobID := setup(t)
		patch(t, router, jobID, "https://example.com/b.jpg", "https://example.com/a-cover.jpg", false, valid)
		assert.Nil(t, storedBounds(t, deps, jobID),
			"a whole-movie PATCH that swaps the poster source must invalidate crop bounds measured against the old image")
	})

	t.Run("cover_url change with poster set preserves bounds", func(t *testing.T) {
		deps, router, jobID := setup(t)
		patch(t, router, jobID, "https://example.com/a.jpg", "https://example.com/b-cover.jpg", false, valid)
		assert.NotNil(t, storedBounds(t, deps, jobID),
			"downloadPoster never reads CoverURL while PosterURL is set — a cover-only edit must not drop the crop")
	})

	t.Run("cover_url change clears bounds when cover is the poster source", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/path/to/SRC-002.mp4"})
		setJobResult(job, "/path/to/SRC-002.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/SRC-002.mp4", MovieID: "SRC-002"},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: "SRC-002", Title: "Src", Poster: models.PosterState{
				CoverURL:         "https://example.com/a-cover.jpg",
				ShouldCropPoster: false,
				CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600},
			}},
		})
		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

		view := contracts.MovieViewFromModel(&models.Movie{ID: "SRC-002", Title: "Src", Poster: models.PosterState{
			CoverURL:         "https://example.com/b-cover.jpg",
			ShouldCropPoster: false,
			CropBounds:       valid.ToModel(),
		}})
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/SRC-002", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		job2, ok := deps.JobStore.GetBatchJob(job.GetID())
		require.True(t, ok)
		for _, r := range job2.GetStatus().Results {
			assert.Nil(t, r.Movie.Poster.CropBounds, "cover is the effective poster source here — changing it must clear bounds")
		}
	})

	t.Run("should_crop_poster change clears bounds", func(t *testing.T) {
		deps, router, jobID := setup(t)
		patch(t, router, jobID, "https://example.com/a.jpg", "https://example.com/a-cover.jpg", true, valid)
		assert.Nil(t, storedBounds(t, deps, jobID))
	})

	t.Run("unchanged poster source preserves bounds", func(t *testing.T) {
		deps, router, jobID := setup(t)
		patch(t, router, jobID, "https://example.com/a.jpg", "https://example.com/a-cover.jpg", false, valid)
		require.NotNil(t, storedBounds(t, deps, jobID),
			"the review-page save-after-crop flow must keep the user's crop")
		assert.Equal(t, contracts.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}, *valid)
	})
}
