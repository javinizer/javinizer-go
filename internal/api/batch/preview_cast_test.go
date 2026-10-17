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

// Both membership proposals must run the same reconciliation as Save, without
// granting durable identity to the preview's provisional cast.
func TestCreditPreviewMembershipMatchesSave(t *testing.T) {
	for _, add := range []bool{false, true} {
		t.Run(map[bool]string{false: "remove", true: "add"}[add], func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
			a := models.Actress{FirstName: "Original", Verified: true, Origin: "user"}
			b := models.Actress{FirstName: "Added", Verified: true, Origin: "user"}
			require.NoError(t, deps.Repos.DB.Create(&a).Error)
			require.NoError(t, deps.Repos.DB.Create(&b).Error)
			m := models.Movie{ContentID: "MEMBERS-401", ID: "MEMBERS-401", Title: "Title", Actresses: []models.Actress{a}}
			_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &m)
			require.NoError(t, err)
			authority, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "members", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: m.ID}, Movie: authority, PersistedMovie: true})
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
			view := contracts.MovieViewFromModel(authority)
			view.Actresses = []contracts.ActressView{}
			if add {
				view.Actresses = contracts.MovieViewFromModel(&models.Movie{Actresses: []models.Actress{a, b}}).Actresses
			}
			path := "/batch/" + job.GetID() + "/results/members"
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
			if add {
				require.Contains(t, stored.DisplayTitle, "Original")
				require.Contains(t, stored.DisplayTitle, "Added")
			} else {
				require.Empty(t, stored.DisplayTitle)
			}
		})
	}
}

func TestCreditPreviewRenameRespectsVisibilityAndSaveGuards(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		staleRevision bool
	}{
		{name: "current version"},
		{name: "missing version", version: "missing"},
		{name: "stale version", version: "stale"},
		{name: "stale result revision", staleRevision: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
			a := models.Actress{FirstName: "Original", Verified: true, Origin: "user"}
			hidden := models.Actress{FirstName: "Hidden", Verified: true, Origin: "user"}
			require.NoError(t, deps.Repos.DB.Create(&a).Error)
			require.NoError(t, deps.Repos.DB.Create(&hidden).Error)
			m := models.Movie{ContentID: "GUARD-401", ID: "GUARD-401", Title: "Before", Actresses: []models.Actress{a}}
			_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &m)
			require.NoError(t, err)
			suppressed := models.MovieCredit{MovieContentID: m.ContentID, ActressID: hidden.ID, Suppressed: true, CreditedName: "Hidden", Origin: "user"}
			require.NoError(t, deps.Repos.DB.Create(&suppressed).Error)
			authority, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "guard", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: m.ID}, Movie: authority, PersistedMovie: true})
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
			view := contracts.MovieViewFromModel(authority)
			view.Actresses[0].FirstName = "Edited"
			for i := range view.Credits {
				view.Credits[i].Suppressed = false
				view.Credits[i].UserOverride = true
				view.Credits[i].OverrideName = "Forged"
			}
			if tc.version == "missing" {
				view.CastVersion = ""
			} else if tc.version != "" {
				view.CastVersion = tc.version
			}
			path := "/batch/" + job.GetID() + "/results/guard"
			preview := doJSON(t, router, http.MethodPost, path+"/display-title-preview", contracts.DisplayTitlePreviewRequest{Movie: view})
			var p contracts.DisplayTitlePreviewResponse
			if tc.version != "" {
				require.Equal(t, http.StatusConflict, preview.Code, preview.Body.String())
			} else {
				require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
				require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &p))
				require.Equal(t, "Edited", p.DisplayTitle)
			}
			beforeSave, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			require.Equal(t, authority, beforeSave)
			revision := uint64(1)
			if tc.staleRevision {
				revision = 0
			}
			patch := doJSON(t, router, http.MethodPatch, path, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
			stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			if tc.version != "" || tc.staleRevision {
				require.Equal(t, http.StatusConflict, patch.Code, patch.Body.String())
				require.Equal(t, authority, stored)
			} else {
				require.Equal(t, http.StatusOK, patch.Code, patch.Body.String())
				require.Equal(t, p.DisplayTitle, stored.DisplayTitle)
			}
			for _, credit := range stored.Credits {
				if credit.ID == suppressed.ID {
					require.True(t, credit.Suppressed)
				}
			}
		})
	}
}
