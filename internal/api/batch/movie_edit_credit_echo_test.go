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

func TestBatchMoviePatchIgnoresReadOnlyCreditsEcho(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	ctx := context.Background()

	movie := models.Movie{ContentID: "PR260-CREDIT-ECHO", ID: "PR260-CREDIT-ECHO", Title: "Before"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&movie).Error)
	actress := models.Actress{FirstName: "Shared", LastName: "Identity", Verified: true, Origin: "user"}
	decoy := models.Actress{FirstName: "Wrong", LastName: "Credit", Verified: true, Origin: "user"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&actress).Error)
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&decoy).Error)
	require.NoError(t, deps.CoreDeps.DB.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
	credit := models.MovieCredit{
		MovieContentID:       movie.ContentID,
		ActressID:            actress.ID,
		CreditedName:         "Reported Name",
		CreditedJapaneseName: "報告名",
		ReportedThumbURL:     "https://example.test/reported.jpg",
		Origin:               string(models.CreditOriginScrape),
		OrderIndex:           4,
	}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&credit).Error)

	filePath := "/path/to/PR260-CREDIT-ECHO.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:       "result-credit-echo",
		FileMatchInfo:  models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
		Status:         models.JobStatusCompleted,
		Movie:          &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Actresses: []models.Actress{actress}},
		PersistedMovie: true,
		Revision:       1,
		StartedAt:      time.Now(),
	})

	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))

	response := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	for _, field := range []string{"id", "actress_id", "credited_name", "credited_japanese_name", "reported_thumb_url", "override_name", "user_override", "suppressed", "order_index"} {
		require.Contains(t, response.Body.String(), `"`+field+`"`, "credit field %s must be exposed", field)
	}
	var batch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &batch))
	view := batch.Results[filePath].Movie
	require.NotNil(t, view)
	require.Len(t, view.Credits, 1)
	require.Equal(t, contracts.MovieCreditView{
		ID:                   credit.ID,
		ActressID:            actress.ID,
		CreditedName:         "Reported Name",
		CreditedJapaneseName: "報告名",
		ReportedThumbURL:     "https://example.test/reported.jpg",
		OrderIndex:           4,
	}, view.Credits[0])

	view.Title = "After"
	view.Actresses[0].FirstName = "Renamed"
	// Echoed credits are deliberately hostile: PATCH must continue to save via
	// the actresses contract rather than treating this read-only row as input.
	view.Credits[0].ActressID = decoy.ID
	view.Credits[0].OverrideName = "Injected override"
	view.Credits[0].UserOverride = true
	response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{
		Movie:                  view,
		ExpectedResultRevision: &batch.Results[filePath].Revision,
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	renamed, err := deps.Repos.ActressRepo.FindByID(ctx, actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", renamed.FirstName, "identity edits must still use the actresses payload")
	credits, err := deps.Repos.MovieCreditRepo.ListByMovie(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, actress.ID, credits[0].ActressID)
	require.False(t, credits[0].UserOverride)
	require.Empty(t, credits[0].OverrideName)

	emptyJSON, err := json.Marshal(&contracts.MovieView{})
	require.NoError(t, err)
	require.NotContains(t, string(emptyJSON), `"credits"`)
}
