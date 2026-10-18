package batch

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestCreditPreviewCandidateRenameMatchesSavedQuarantine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
	a := models.Actress{FirstName: "Original", Origin: "scraper"}
	b := models.Actress{FirstName: "Edited", Origin: "scraper"}
	require.NoError(t, deps.Repos.DB.Create(&a).Error)
	require.NoError(t, deps.Repos.DB.Create(&b).Error)
	m := models.Movie{ContentID: "REVIEW-CANDIDATE", ID: "REVIEW-CANDIDATE", Title: "Title", Actresses: []models.Actress{a}}
	_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &m)
	require.NoError(t, err)
	authority, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
	require.NoError(t, err)
	require.Len(t, authority.Actresses, 1)
	require.False(t, authority.Actresses[0].AmbiguityQuarantined)
	job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "candidate", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: m.ID}, Movie: authority, PersistedMovie: true})
	router := gin.New()
	rt := testkit.GetTestRuntime(deps)
	router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
	view := contracts.MovieViewFromModel(authority)
	view.Actresses[0].FirstName = "Edited"
	view.Actresses[0].Verified, view.Actresses[0].Origin, view.Actresses[0].NameKey = true, "user", "forged"
	beforeTables := previewTableSnapshot(t, deps.Repos.DB)
	path := "/batch/" + job.GetID() + "/results/candidate"
	preview := doJSON(t, router, http.MethodPost, path+"/display-title-preview", contracts.DisplayTitlePreviewRequest{Movie: view})
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var p contracts.DisplayTitlePreviewResponse
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &p))
	require.Equal(t, beforeTables, previewTableSnapshot(t, deps.Repos.DB))
	revision := uint64(1)
	patch := doJSON(t, router, http.MethodPatch, path, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusOK, patch.Code, patch.Body.String())
	stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
	require.NoError(t, err)
	require.Empty(t, stored.DisplayTitle)
	require.True(t, stored.Credits[0].Actress.AmbiguityQuarantined)
	require.Equal(t, p.DisplayTitle, stored.DisplayTitle)
}
