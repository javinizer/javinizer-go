package batch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Use actual GET JSON so omitted nested names really are omitted on the wire.
// Reject the second rename after the first has written inside the transaction,
// then verify every table and both in-memory family results are unchanged.
func TestNestedActressRenameRejectsEmptyNamesAtomically(t *testing.T) {
	for _, mode := range []string{"id-only", "empty", "whitespace", "thumbnail"} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
			actors := []models.Actress{{FirstName: "First", LastName: "Actor", Origin: "scraper"}, {JapaneseName: "名前", Verified: true, Origin: "user"}}
			require.NoError(t, deps.Repos.DB.Create(&actors).Error)
			movie, err := deps.Repos.MovieRepo.Upsert(t.Context(), &models.Movie{ContentID: "EMPTY-NAME-1", ID: "EMPTY-NAME-1", Title: "Stored title", Actresses: actors})
			require.NoError(t, err)
			// The valid first rename deletes translations, retargets aliases,
			// reconciles a collision, quarantines candidates and dirties rendering.
			contender := models.Actress{FirstName: "Accepted rename", LastName: "Actor", Origin: "scraper"}
			require.NoError(t, deps.Repos.DB.Create(&contender).Error)
			require.NoError(t, deps.Repos.DB.Create(&models.ActressTranslation{ActressID: actors[0].ID, Language: "en", FirstName: "Translated"}).Error)
			alias := models.ActressAlias{AliasName: "Historical Actor", CanonicalName: actors[0].FullName()}
			require.NoError(t, deps.Repos.DB.Create(&alias).Error)
			require.Len(t, movie.Credits, 2)
			collision := models.CreditCollision{CreditID: movie.Credits[0].ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: contender.FullName(), CanonicalValue: actors[0].FullName(), Status: models.CollisionStatusOpen}
			require.Equal(t, actors[0].ID, movie.Credits[0].ActressID)
			require.NoError(t, deps.Repos.DB.Create(&collision).Error)
			job := deps.JobStore.CreateJobBatch([]string{"synthetic-part1.mp4", "synthetic-part2.mp4"})
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			for i, path := range []string{"synthetic-part1.mp4", "synthetic-part2.mp4"} {
				setJobResult(job, path, &resultstore.MovieResult{ResultID: fmt.Sprintf("part%d", i+1), Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID, PartNumber: i + 1}, Movie: movie.Clone(), PersistedMovie: true})
			}
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.GET("/batch/:id", getBatchJob(rt))
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
			router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			router.POST("/batch/:id/results/:resultId/preview", previewOrganize(rt))
			wire := doJSON(t, router, http.MethodGet, "/batch/"+job.GetID()+"?include_data=true", nil)
			require.Equal(t, http.StatusOK, wire.Code, wire.Body.String())
			var response struct {
				Results map[string]struct {
					Movie map[string]any `json:"movie"`
				} `json:"results"`
			}
			require.NoError(t, json.Unmarshal(wire.Body.Bytes(), &response))
			body := response.Results["synthetic-part1.mp4"].Movie
			require.Equal(t, movie.ContentID, body["code"])
			require.NotEmpty(t, body["cast_version"])
			invalid := map[string]any{"id": actors[1].ID}
			if mode != "id-only" {
				blank := ""
				if mode == "whitespace" {
					blank = " \t\n\u3000"
				}
				invalid["first_name"], invalid["last_name"], invalid["japanese_name"] = blank, blank, blank
			}
			if mode == "thumbnail" {
				invalid["thumb_edited"], invalid["thumb_url"] = true, "new-thumb"
			}
			body["actresses"] = []any{map[string]any{"id": actors[0].ID, "first_name": "Accepted rename", "last_name": "Actor"}, invalid}
			body["title"] = "Must roll back"
			beforeDB := previewTableSnapshot(t, deps.Repos.DB)
			// Prove the first rename has real side effects to roll back, using
			// the same transaction-scoped helper without the invalid second row.
			require.NoError(t, deps.Repos.DB.WithPreviewTx(t.Context(), func(u database.EditUnit) error {
				require.NoError(t, worker.ApplyActressRenames(t.Context(), u.Actresses, []worker.ActressRenamePlan{{ID: actors[0].ID, FirstName: contender.FirstName, LastName: contender.LastName}}))
				txDB := u.Actresses.(*database.ActressRepository).GetDB()
				changed, err := u.Actresses.FindByID(t.Context(), actors[0].ID)
				require.NoError(t, err)
				require.True(t, changed.AmbiguityQuarantined)
				var translations int64
				require.NoError(t, txDB.Model(&models.ActressTranslation{}).Where("actress_id = ?", actors[0].ID).Count(&translations).Error)
				require.Zero(t, translations)
				var changedAlias models.ActressAlias
				require.NoError(t, txDB.First(&changedAlias, alias.ID).Error)
				require.Equal(t, contender.FullName(), changedAlias.CanonicalName)
				var changedCollision models.CreditCollision
				require.NoError(t, txDB.First(&changedCollision, collision.ID).Error)
				require.Equal(t, contender.FullName(), changedCollision.CanonicalValue)
				var changedMovie models.Movie
				require.NoError(t, txDB.First(&changedMovie, "content_id = ?", movie.ContentID).Error)
				require.Greater(t, changedMovie.RenderGeneration, movie.RenderGeneration)
				return nil
			}))
			require.Equal(t, beforeDB, previewTableSnapshot(t, deps.Repos.DB))
			beforeResults := make([]*resultstore.MovieResult, 0, 2)
			for _, path := range []string{"synthetic-part1.mp4", "synthetic-part2.mp4"} {
				result, err := job.Results().GetMovieResult(path)
				require.NoError(t, err)
				beforeResults = append(beforeResults, result)
			}
			path := "/batch/" + job.GetID() + "/results/part1"
			for _, endpoint := range []struct{ method, suffix string }{{http.MethodPost, "/display-title-preview"}, {http.MethodPost, "/preview"}, {http.MethodPatch, ""}} {
				t.Run(endpoint.method+endpoint.suffix, func(t *testing.T) {
					result := doJSON(t, router, endpoint.method, path+endpoint.suffix, map[string]any{"movie": body, "operation_mode": "in-place", "expected_result_revisions": map[string]uint64{"part1": 1, "part2": 1}})
					require.Equal(t, http.StatusBadRequest, result.Code, "%s: %s", endpoint.suffix, result.Body.String())
					require.Contains(t, result.Body.String(), fmt.Sprintf("actress %d must retain at least one non-empty name", actors[1].ID))
					require.Equal(t, beforeDB, previewTableSnapshot(t, deps.Repos.DB), endpoint.suffix)
					for i, file := range []string{"synthetic-part1.mp4", "synthetic-part2.mp4"} {
						after, err := job.Results().GetMovieResult(file)
						require.NoError(t, err)
						require.Equal(t, beforeResults[i], after, endpoint.suffix)
					}
				})
			}
		})
	}
}

func TestNestedActressRenameCompatibleCastRequests(t *testing.T) {
	for _, mode := range []string{"omitted", "null", "unchanged", "empty cast", "last-only", "Japanese-only", "thumbnail", "thumbnail clear", "unknown named ID"} {
		t.Run(mode, func(t *testing.T) {
			deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
			actor := models.Actress{FirstName: "First", LastName: "Actor", ThumbURL: "old-thumb", Verified: true, Origin: "user"}
			require.NoError(t, deps.Repos.DB.Create(&actor).Error)
			movie, err := deps.Repos.MovieRepo.Upsert(t.Context(), &models.Movie{ContentID: "COMPAT-NAME-1", ID: "COMPAT-NAME-1", Title: "Stored", Actresses: []models.Actress{actor}})
			require.NoError(t, err)
			job := deps.JobStore.CreateJobBatch([]string{"synthetic.mp4"})
			job.Controller().SetJobStatus(models.JobStatusCompleted)
			setJobResult(job, "synthetic.mp4", &resultstore.MovieResult{ResultID: "result", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "synthetic.mp4", MovieID: movie.ID}, Movie: movie.Clone(), PersistedMovie: true})
			router := gin.New()
			rt := testkit.GetTestRuntime(deps)
			router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
			router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			data, err := json.Marshal(contracts.MovieViewFromModel(movie))
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(data, &body))
			body["title"] = "Edited"
			cast := body["actresses"].([]any)
			writable := cast[0].(map[string]any)
			switch mode {
			case "omitted":
				delete(body, "actresses")
			case "null":
				body["actresses"] = nil
			case "empty cast":
				body["actresses"] = []any{}
			case "unknown named ID":
				writable["id"], writable["first_name"], writable["last_name"] = 987654, "New identity", ""
			case "last-only":
				writable["first_name"] = ""
				writable["last_name"] = "Surname"
			case "Japanese-only":
				writable["first_name"], writable["last_name"], writable["japanese_name"] = "", "", "新名"
			case "thumbnail":
				writable["thumb_edited"], writable["thumb_url"] = true, "new-thumb"
			case "thumbnail clear":
				writable["thumb_edited"], writable["thumb_url"] = true, ""
			}
			path := "/batch/" + job.GetID() + "/results/result"
			before := previewTableSnapshot(t, deps.Repos.DB)
			preview := doJSON(t, router, http.MethodPost, path+"/display-title-preview", map[string]any{"movie": body})
			require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
			var view contracts.MovieView
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(encoded, &view))
			_, _, resolveErr := ResolvePreviewData(t.Context(), deps, job.GetID(), "result", contracts.OrganizePreviewRequest{Movie: &view})
			require.Nil(t, resolveErr)
			require.Equal(t, before, previewTableSnapshot(t, deps.Repos.DB))
			saved := doJSON(t, router, http.MethodPatch, path, map[string]any{"movie": body, "expected_result_revision": 1})
			require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
			stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
			require.NoError(t, err)
			require.Equal(t, "Edited", stored.Title)
			actual, err := deps.Repos.ActressRepo.FindByID(t.Context(), actor.ID)
			require.NoError(t, err)
			switch mode {
			case "empty cast":
				require.Empty(t, stored.Actresses)
				require.Equal(t, actor.FirstName, actual.FirstName)
			case "unknown named ID":
				require.Len(t, stored.Actresses, 1)
				require.NotZero(t, stored.Actresses[0].ID)
				require.NotEqual(t, uint(987654), stored.Actresses[0].ID)
				require.NotEqual(t, actor.ID, stored.Actresses[0].ID)
				require.Equal(t, "New identity", stored.Actresses[0].FirstName)
				require.Equal(t, actor.FirstName, actual.FirstName)
			case "last-only":
				require.Empty(t, actual.FirstName)
				require.Equal(t, "Surname", actual.LastName)
			case "Japanese-only":
				require.Empty(t, actual.FirstName)
				require.Empty(t, actual.LastName)
				require.Equal(t, "新名", actual.JapaneseName)
			case "thumbnail":
				require.Equal(t, "new-thumb", actual.ThumbURL)
			case "thumbnail clear":
				require.Empty(t, actual.ThumbURL)
			default:
				require.Len(t, stored.Actresses, 1)
				require.Equal(t, actor.FirstName, actual.FirstName)
				require.Equal(t, actor.LastName, actual.LastName)
			}
		})
	}
}
