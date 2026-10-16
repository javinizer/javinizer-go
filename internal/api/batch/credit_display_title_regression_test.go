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

func TestCreditDisplayTitleSaveMatchesAuthoritativePreview(t *testing.T) {
	for _, tc := range []struct {
		name                                                                         string
		useCredited, override, forceCanonical, rename, emptyCast, snapshot, japanese bool
		creditSource                                                                 string
		want                                                                         string
	}{
		{name: "per-movie override", useCredited: true, override: true, want: "Per Movie Override - After"},
		{name: "credited name", useCredited: true, want: "Reported Alias - After"},
		{name: "canonical control", want: "Name Canonical - After"},
		{name: "force canonical control", useCredited: true, forceCanonical: true, want: "Name Canonical - After"},
		{name: "accepted canonical rename", rename: true, want: "Name Edited - After"},
		{name: "rename retains per-movie override", rename: true, useCredited: true, override: true, want: "Per Movie Override - After"},
		{name: "rename retains credited alias", rename: true, useCredited: true, want: "Reported Alias - After"},
		{name: "rename force canonical", rename: true, useCredited: true, forceCanonical: true, want: "Name Edited - After"},
		{name: "rename user identity snapshot", rename: true, useCredited: true, snapshot: true, want: "Name Edited - After"},
		{name: "rename legacy identity snapshot", rename: true, useCredited: true, snapshot: true, creditSource: "legacy", want: "Name Edited - After"},
		{name: "rename reported canonical name", rename: true, useCredited: true, snapshot: true, creditSource: "r18dev", want: "Name Canonical - After"},
		{name: "rename Japanese fallback", rename: true, japanese: true, want: "新名 - After"},
		{name: "rename Japanese identity snapshot", rename: true, japanese: true, useCredited: true, snapshot: true, want: "新名 - After"},
		{name: "empty cast", emptyCast: true, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			titleTemplate := "<ACTRESSES> - <TITLE>"
			if tc.emptyCast {
				titleTemplate = "<ACTORS>"
			}
			cfg := newDisplayTitlePreviewConfig(titleTemplate)
			cfg.Metadata.NFO.Feature.UseCreditedName = tc.useCredited
			deps := createTestDeps(t, cfg, "")
			a := models.Actress{FirstName: "Canonical", LastName: "Name", Verified: true, Origin: "user"}
			if tc.japanese {
				a.FirstName, a.LastName, a.JapaneseName = "", "", "旧名"
			}
			require.NoError(t, deps.Repos.DB.Create(&a).Error)
			m := models.Movie{ContentID: "TITLE-401", ID: "TITLE-401", Title: "Before", DisplayTitle: "Old derived title"}
			require.NoError(t, deps.Repos.DB.Create(&m).Error)
			if !tc.emptyCast {
				require.NoError(t, deps.Repos.DB.Exec("INSERT INTO movie_actresses (movie_content_id,actress_id) VALUES (?,?)", m.ContentID, a.ID).Error)
			}
			credit := models.MovieCredit{MovieContentID: m.ContentID, ActressID: a.ID, CreditedName: "Reported Alias", UserOverride: tc.override, OverrideName: "Per Movie Override", DisplayForceCanonical: tc.forceCanonical, Origin: "user"}
			credit.Source = tc.creditSource
			if tc.snapshot {
				credit.CreditedName = a.FullName()
			}
			if !tc.emptyCast {
				require.NoError(t, deps.Repos.DB.Create(&credit).Error)
			}
			authority, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "title-result", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: m.ID}, Movie: authority, PersistedMovie: true})
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
			view := contracts.MovieViewFromModel(authority)
			view.Title = "After"
			if !tc.emptyCast {
				view.Credits[0].OverrideName = "Untrusted client name"
				view.Credits[0].UserOverride = true
				view.Credits[0].ActressID = 999999
			}
			request := contracts.UpdateMovieRequest{Movie: view}
			if tc.rename {
				view.Actresses[0].FirstName = "Edited"
				if tc.japanese {
					view.Actresses[0].FirstName, view.Actresses[0].JapaneseName = "", "新名"
				}
				revision := uint64(1)
				request.ExpectedResultRevision = &revision
			}
			path := "/batch/" + job.GetID() + "/results/title-result"
			beforePreview := authority.Clone()
			beforeTables := previewTableSnapshot(t, deps.Repos.DB)
			preview := doJSON(t, router, http.MethodPost, path+"/display-title-preview", contracts.DisplayTitlePreviewRequest{Movie: view})
			require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
			var p contracts.DisplayTitlePreviewResponse
			require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &p))
			// Preview and organize preview share the read-only projection. Neither
			// may alter the persisted identity, cached result, or caller DTO.
			organize, _, resolveErr := ResolvePreviewData(t.Context(), deps, job.GetID(), "title-result", contracts.OrganizePreviewRequest{Movie: view})
			require.Nil(t, resolveErr)
			factory, err := rt.Snapshot().WorkflowFactory()
			require.NoError(t, err)
			require.Equal(t, p.DisplayTitle, factory.RenderDisplayTitle(t.Context(), organize))
			afterPreview, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			require.Equal(t, beforeTables, previewTableSnapshot(t, deps.Repos.DB))
			require.Equal(t, beforePreview, afterPreview)
			require.Equal(t, beforePreview, authority)
			if !tc.emptyCast {
				require.Equal(t, uint(999999), view.Credits[0].ActressID)
			}
			patch := doJSON(t, router, http.MethodPatch, path, request)
			require.Equal(t, http.StatusOK, patch.Code, patch.Body.String())
			var response contracts.MovieResponse
			require.NoError(t, json.Unmarshal(patch.Body.Bytes(), &response))
			stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), m.ContentID)
			require.NoError(t, err)
			require.Equal(t, tc.want, response.Movie.DisplayTitle)
			require.Equal(t, tc.want, stored.DisplayTitle)
			require.Equal(t, p.DisplayTitle, stored.DisplayTitle)
			if tc.emptyCast {
				require.Empty(t, stored.Credits)
			} else {
				require.Len(t, stored.Credits, 1)
				require.Equal(t, a.ID, stored.Credits[0].ActressID)
				require.Equal(t, tc.override, stored.Credits[0].UserOverride)
				require.NotEqual(t, "Untrusted client name", stored.Credits[0].OverrideName)
			}
			inMemory, err := job.Results().GetMovieResult("synthetic.mp4")
			require.NoError(t, err)
			require.Equal(t, tc.want, inMemory.Movie.DisplayTitle)
			require.Equal(t, stored.RenderGeneration, inMemory.Movie.RenderGeneration)
			require.Equal(t, stored.RenderDirty, inMemory.Movie.RenderDirty)
			envelope, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
			require.NoError(t, err)
			if tc.want == "" {
				require.Contains(t, envelope.Results, `"display_title":""`)
			} else {
				require.Contains(t, envelope.Results, tc.want)
			}
			require.Equal(t, titleTemplate, cfg.Metadata.NFO.Format.DisplayTitle)
		})
	}
}
