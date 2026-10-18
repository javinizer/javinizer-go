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

func TestCreditPreviewMixedCachedEchoAndCurrentVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
	a := models.Actress{FirstName: "Original", Verified: true, Origin: "user"}
	require.NoError(t, deps.Repos.DB.Create(&a).Error)
	m := models.Movie{ContentID: "MIXED-ECHO", ID: "MIXED-ECHO", Title: "Title", Actresses: []models.Actress{a}}
	_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &m)
	require.NoError(t, err)
	baseline, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
	require.NoError(t, err)
	job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "mixed", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: m.ID}, Movie: baseline, PersistedMovie: true})
	require.NoError(t, deps.Repos.ActressRepo.RenameNameFields(t.Context(), a.ID, "Current", "", ""))
	authority, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
	require.NoError(t, err)
	view := contracts.MovieViewFromModel(baseline)
	view.CastVersion = models.ActressCastVersion(authority.Actresses)
	view.Title = "Changed title only"
	router := gin.New()
	rt := testkit.GetTestRuntime(deps)
	router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
	path := "/batch/" + job.GetID() + "/results/mixed"
	preview := doJSON(t, router, http.MethodPost, path+"/display-title-preview", contracts.DisplayTitlePreviewRequest{Movie: view})
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var p contracts.DisplayTitlePreviewResponse
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &p))
	revision := uint64(1)
	patch := doJSON(t, router, http.MethodPatch, path, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusOK, patch.Code, patch.Body.String())
	stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
	require.NoError(t, err)
	require.Equal(t, p.DisplayTitle, stored.DisplayTitle)
	require.Equal(t, "Original", stored.DisplayTitle, "current cast version explicitly accepts restoring the old cached name")
}
