package batch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Save and both preview paths must make the same intent decision even when an
// explicit restore equals the old cached cast. Preview must leave no writes.
func TestCreditEchoPreviewMatchesSave(t *testing.T) {
	for _, mode := range []string{"old echo", "legacy echo", "explicit restore"} {
		for _, omitCode := range []bool{false, true} {
			for _, parts := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/omitCode=%v/parts=%d", mode, omitCode, parts), func(t *testing.T) {
					f := newSuppressionFixtureWithConfig(t, parts, newDisplayTitlePreviewConfig("<ACTORS>"))
					require.NoError(t, f.deps.Repos.DB.Model(&models.Actress{}).Where("id = ?", f.actress.ID).Updates(map[string]any{"name_key": "name canonical", "verified": false, "origin": "scrape"}).Error)
					baseline := f.stored(t)
					for i, path := range f.paths {
						setJobResult(f.job, path, &resultstore.MovieResult{ResultID: fmt.Sprintf("suppression-result-%d", i), Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: f.movie.ID}, Movie: baseline.Clone(), PersistedMovie: true})
					}
					rt := testkit.GetTestRuntime(f.deps)
					f.router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
					old := f.read(t)
					view := old.Results[f.paths[0]].Movie
					require.Equal(t, "name canonical", view.Actresses[0].NameKey)
					f.suppress(t)
					fresh := f.read(t)
					require.Empty(t, fresh.Results[f.paths[0]].Movie.Actresses)
					request := contracts.UpdateMovieRequest{Movie: view}
					switch mode {
					case "legacy echo":
						view.CastVersion = ""
					case "explicit restore":
						view.CastVersion = fresh.Results[f.paths[0]].Movie.CastVersion
						request.ExpectedResultRevisions = map[string]uint64{}
						for _, result := range fresh.Results {
							request.ExpectedResultRevisions[result.ResultID] = result.Revision
						}
					}
					if omitCode {
						view.Code = ""
					}
					view.Title = "Edited title"
					// Read-only fields cannot opt out of suppression or replace the
					// canonical candidate, including in rollback reconciliation.
					view.Actresses[0].NameKey = "forged"
					view.Actresses[0].Verified = true
					view.Actresses[0].Origin = "forged"
					view.Credits[0].Suppressed = false
					view.Credits[0].OverrideName = "forged override"
					beforeTables := previewTableSnapshot(t, f.deps.Repos.DB)
					beforeInput, err := json.Marshal(view)
					require.NoError(t, err)
					beforeJob, err := f.job.Results().GetMovieResult(f.paths[0])
					require.NoError(t, err)
					preview := doJSON(t, f.router, http.MethodPost, "/batch/"+f.job.GetID()+"/results/suppression-result-0/display-title-preview", contracts.DisplayTitlePreviewRequest{Movie: view})
					require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
					var projected contracts.DisplayTitlePreviewResponse
					require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &projected))
					organize, _, resolveErr := ResolvePreviewData(t.Context(), f.deps, f.job.GetID(), "suppression-result-0", contracts.OrganizePreviewRequest{Movie: view})
					require.Nil(t, resolveErr)
					factory, err := rt.Snapshot().WorkflowFactory()
					require.NoError(t, err)
					require.Equal(t, projected.DisplayTitle, factory.RenderDisplayTitle(t.Context(), organize))
					require.Equal(t, beforeTables, previewTableSnapshot(t, f.deps.Repos.DB))
					afterInput, err := json.Marshal(view)
					require.NoError(t, err)
					require.Equal(t, beforeInput, afterInput)
					afterJob, err := f.job.Results().GetMovieResult(f.paths[0])
					require.NoError(t, err)
					require.Equal(t, beforeJob, afterJob)
					response := f.patch(t, request, http.StatusOK)
					saved := f.stored(t)
					require.Equal(t, projected.DisplayTitle, saved.DisplayTitle)
					require.Equal(t, projected.DisplayTitle, response.Movie.DisplayTitle)
					require.Equal(t, mode != "explicit restore", saved.Credits[0].Suppressed)
					require.Equal(t, "name canonical", saved.Credits[0].Actress.NameKey)
					require.False(t, saved.Credits[0].Actress.Verified)
					require.Equal(t, "scrape", saved.Credits[0].Actress.Origin)
					require.Equal(t, "Saved override", saved.Credits[0].OverrideName)
					if mode == "explicit restore" {
						require.NotEmpty(t, projected.DisplayTitle)
					} else {
						require.Empty(t, projected.DisplayTitle)
					}
					for _, path := range f.paths {
						live, err := f.job.Results().GetMovieResult(path)
						require.NoError(t, err)
						require.Equal(t, saved.DisplayTitle, live.Movie.DisplayTitle)
						require.Equal(t, fresh.Results[path].Revision+1, live.Revision)
					}
				})
			}
		}
	}
}
