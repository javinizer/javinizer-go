package batch

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// A review-page thumbnail edit must reach the shared identity: the movie upsert
// merge is deliberately fill-only for scraper data, so an explicit user edit has
// to travel through the identity-edit plan instead of being discarded.
func TestUpdateBatchMoviePersistsActressThumbnailEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	ctx := context.Background()

	identity := models.Actress{
		FirstName: "Thumb", LastName: "Owner", JapaneseName: "親指",
		ThumbURL: "https://old.test/thumb.jpg", Verified: true, Origin: "user",
	}
	require.NoError(t, deps.Repos.DB.Create(&identity).Error)

	movie := models.Movie{ContentID: "thumb-edit-movie", ID: "THUMB-001", Title: "Thumb Edit"}
	require.NoError(t, deps.Repos.DB.Create(&movie).Error)
	credit := models.MovieCredit{
		MovieContentID: movie.ContentID, ActressID: identity.ID,
		CreditedName: "Owner Thumb", CreditedJapaneseName: "親指",
		Origin: string(models.CreditOriginUser), OrderPinned: true,
	}
	require.NoError(t, deps.Repos.DB.Create(&credit).Error)
	require.NoError(t, deps.Repos.DB.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))

	filePath := "/library/thumb-edit.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "result-thumb-edit",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))

	before := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	var batch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(before.Body.Bytes(), &batch))
	view := batch.Results[filePath].Movie
	require.NotNil(t, view)
	require.Len(t, view.Actresses, 1, "projection must expose the credited actress")
	revision := batch.Results[filePath].Revision

	view.Actresses[0].ThumbURL = "https://new.test/thumb.jpg"
	view.UpdatedAt = time.Time{}
	view.Actresses[0].UpdatedAt = time.Time{}

	patched := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID,
		contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())

	var response contracts.MovieResponse
	require.NoError(t, json.Unmarshal(patched.Body.Bytes(), &response))
	require.Len(t, response.Movie.Actresses, 1)
	require.Equal(t, "https://new.test/thumb.jpg", response.Movie.Actresses[0].ThumbURL, "PATCH echo must carry the edited thumbnail")

	var stored models.Actress
	require.NoError(t, deps.Repos.DB.First(&stored, identity.ID).Error)
	require.Equal(t, "https://new.test/thumb.jpg", stored.ThumbURL, "identity row must persist the thumbnail")

	after := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, after.Code, after.Body.String())
	var refreshed contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(after.Body.Bytes(), &refreshed))
	require.Equal(t, "https://new.test/thumb.jpg", refreshed.Results[filePath].Movie.Actresses[0].ThumbURL, "refetch must not revert the edit")

	_ = ctx
}
