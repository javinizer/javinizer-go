package batch

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

type forcedIdentitySource struct {
	stubScraper
	contentID string
}

func (s *forcedIdentitySource) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	return &models.ScraperResult{Source: s.Name(), ID: id, ContentID: s.contentID, Title: "Fresh candidate"}, nil
}

// Exercise the real engine, deferred worker, durable job codec, and recovered
// GET/PATCH boundary. A client may retain the candidate before cancellation,
// but cannot convert it to an ordinary identity-changing save after restart.
func TestForcedRefreshCancelledCandidateCannotBecomeRecoveredSave(t *testing.T) {
	for _, mode := range []string{"missing_target", "existing_target", "identity_changes_after_scrape", "identity_changes_before_persist"} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Priority = []string{"r18dev"}
			cfg.System.TempDir = t.TempDir()
			deps := createTestDeps(t, cfg, "")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			old, err := deps.Repos.MovieRepo.Upsert(ctx, &models.Movie{ID: "REFRESH-101", ContentID: "refresh00101", Title: "Saved original"})
			require.NoError(t, err)
			otherContent := "other00202"
			if mode != "missing_target" {
				_, err = deps.Repos.MovieRepo.Upsert(ctx, &models.Movie{ID: "OTHER-202", ContentID: otherContent, Title: "Saved other"})
				require.NoError(t, err)
			}
			incomingContent := otherContent
			if mode == "identity_changes_after_scrape" || mode == "identity_changes_before_persist" {
				incomingContent = old.ContentID // Initially valid; authority changes before persistence.
			}
			deps.CoreDeps.GetRegistry().RegisterInstance(&forcedIdentitySource{stubScraper: *newStubScraper("r18dev"), contentID: incomingContent})
			fc, err := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.GetRegistry(), deps.Repos.DB.Repositories())
			require.NoError(t, err)
			factory, err := workflow.NewWorkflowFactory(fc)
			require.NoError(t, err)
			wf, err := factory.NewWorkflow("")
			require.NoError(t, err)
			file := "/synthetic/REFRESH-101.mp4"
			job := deps.JobStore.CreateJobBatch([]string{file})
			job.Controller().SetWorkflow(wf)
			var before []*models.Movie
			require.NoError(t, job.Controller().StartScrape(ctx, []string{file}, worker.ScrapePhaseConfig{
				Force: true, SelectedScrapers: []string{"r18dev"}, MovieIDOverride: map[string]string{file: old.ID},
				OnFileScraped: func(string, string, string) {
					if mode == "identity_changes_after_scrape" || mode == "identity_changes_before_persist" {
						require.NoError(t, deps.Repos.DB.Model(&models.Movie{}).Where("content_id = ?", old.ContentID).Update("id", "FORMER-101").Error)
						require.NoError(t, deps.Repos.DB.Model(&models.Movie{}).Where("content_id = ?", otherContent).Update("id", old.ID).Error)
					}
					require.NoError(t, deps.Repos.DB.Order("content_id").Find(&before).Error)
					if mode != "identity_changes_before_persist" {
						cancel()
					}
				},
			}))
			waitErr := job.Controller().Wait()
			if mode == "identity_changes_before_persist" {
				require.NoError(t, waitErr)
				require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
			} else {
				require.ErrorContains(t, waitErr, "cancelled")
				require.Equal(t, models.JobStatusCancelled, job.GetStatus().Status)
			}
			row, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
			require.NoError(t, err)
			recovered, decodeErrors := jobpersist.Decode(row)
			require.Empty(t, decodeErrors)
			require.Contains(t, recovered.Results, file)
			require.Nil(t, recovered.Results[file].Movie, "durable envelope must contain no unconstrained candidate")
			// Reconstruct from the durable row with a fresh store, as startup does.
			deps.JobStore = worker.NewJobStore(deps.Repos.JobRepo, deps.Repos.BatchFileOpRepo, deps.Repos.MovieRepo, cfg.System.TempDir, nil, nil, worker.WithActressRepo(deps.Repos.ActressRepo), worker.WithHistoryRepo(deps.Repos.HistoryRepo), worker.WithEditTransactor(deps.Repos.DB))
			restored, ok := deps.JobStore.GetBatchJob(job.GetID())
			require.True(t, ok)
			require.Nil(t, restored.GetStatus().Results[file].Movie)
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.GET("/api/v1/batch/:id", getBatchJob(rt))
			router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(rt))
			response := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var body contracts.BatchJobResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Nil(t, body.Results[file].Movie)
			candidate := &contracts.MovieView{ID: old.ID, Code: incomingContent, Title: "Stale candidate"}
			response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+recovered.Results[file].ResultID, contracts.UpdateMovieRequest{Movie: candidate})
			require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
			var after []*models.Movie
			require.NoError(t, deps.Repos.DB.Order("content_id").Find(&after).Error)
			require.Equal(t, before, after)
			var count int64
			require.NoError(t, deps.Repos.DB.Model(&models.Movie{}).Where("id = ?", old.ID).Count(&count).Error)
			require.EqualValues(t, 1, count, "display-ID cache/read must remain unambiguous")
		})
	}
}
