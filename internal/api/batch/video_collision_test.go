package batch

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestIssue277HTTPReviewApplyAndPersistence(t *testing.T) {
	for _, policy := range []videocollision.Policy{videocollision.Fail, videocollision.Skip, videocollision.Replace} {
		for _, copyOnly := range []bool{true, false} {
			t.Run(string(policy)+map[bool]string{true: "/copy", false: "/move"}[copyOnly], func(t *testing.T) {
				cfg := config.DefaultConfig(nil, nil)
				root := t.TempDir()
				input := filepath.Join(root, "input")
				output := filepath.Join(root, "output")
				require.NoError(t, os.MkdirAll(input, 0755))
				require.NoError(t, os.MkdirAll(output, 0755))
				cfg.API.Security.AllowedDirectories = []string{root}
				cfg.Output.Template.FolderFormat = "<ID>"
				cfg.Output.Template.FileFormat = "<ID>"
				cfg.Output.Template.SubfolderFormat = nil
				cfg.Output.Operation.RenameFile = true
				cfg.Output.Operation.AllowRevert = false
				deps := createTestDeps(t, cfg, "")
				rt := testkit.GetTestRuntime(deps)
				movie := models.Movie{ID: "IPX-277", ContentID: "ipx00277", Title: "Synthetic"}
				_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &movie)
				require.NoError(t, err)
				stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
				require.NoError(t, err)
				source := filepath.Join(input, "IPX-277-original.mp4")
				require.NoError(t, os.WriteFile(source, []byte("incoming video"), 0644))
				job := deps.JobStore.CreateJobBatch([]string{source})
				setJobResult(job, source, &resultstore.MovieResult{ResultID: "review277", Status: models.JobStatusCompleted, Movie: stored, PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}})
				setJobStatus(job, models.JobStatusCompleted)
				router := gin.New()
				router.POST("/batch/:id/video-collisions", videoCollisionReview(rt))
				router.POST("/batch/:id/organize", organizeJob(rt))
				base := "/batch/" + job.GetID()
				req := contracts.OrganizeRequest{Destination: output, CopyOnly: copyOnly, SkipNFO: true, SkipDownload: true}
				response := doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var review videoCollisionReport
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &review))
				require.Len(t, review.Items, 1)
				target := review.Items[0].Destination
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
				require.NoError(t, os.WriteFile(target, []byte("foreign video"), 0644))
				response = doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
				require.Equal(t, 200, response.Code, response.Body.String())
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &review))
				require.Equal(t, 1, review.Unresolved)
				item := review.Items[0]
				require.Equal(t, source, item.Source)
				require.Equal(t, "file", item.Kind)
				require.NotEmpty(t, item.ReviewToken)
				if policy == videocollision.Replace {
					forged := videocollision.Decision{Source: source, Destination: target, Policy: policy, ReviewToken: item.ReviewToken + "x"}
					req.VideoCollisionDecisions = map[string]videocollision.Decision{source: forged}
					response = doJSON(t, router, http.MethodPost, base+"/organize", req)
					require.Equal(t, 409, response.Code, response.Body.String())
					forged.ReviewToken = item.ReviewToken
					req.VideoCollisionDecisions[source] = forged
					require.NoError(t, os.WriteFile(target, []byte("changed reviewed target"), 0644))
					response = doJSON(t, router, http.MethodPost, base+"/organize", req)
					require.Equal(t, 409, response.Code, response.Body.String())
					var stale contracts.ErrorResponse
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stale))
					require.Equal(t, "VIDEO_COLLISION_STALE", stale.Code)
					require.NotNil(t, stale.VideoCollisionReview)
					forged.ReviewToken = stale.VideoCollisionReview.Items[0].ReviewToken
					require.NotEqual(t, item.ReviewToken, forged.ReviewToken)
					req.VideoCollisionDecisions[source] = forged
				} else {
					req.VideoCollisionPolicy = policy
				}
				response = doJSON(t, router, http.MethodPost, base+"/organize", req)
				if policy == videocollision.Fail {
					require.Equal(t, 409, response.Code, response.Body.String())
					return
				}
				require.Equal(t, 200, response.Code, response.Body.String())
				require.NoError(t, job.Controller().Wait())
				status := job.GetStatus()
				require.Equal(t, 0, status.Failed, "%+v", status.Results[source])
				require.Equal(t, models.JobStatusOrganized, status.Status)
				want := "foreign video"
				outcome := "skipped"
				if policy == videocollision.Replace {
					want = "incoming video"
					outcome = "replaced"
				}
				body, err := os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, want, string(body))
				require.Equal(t, outcome, status.Results[source].VideoOutcome)
				if policy == videocollision.Skip || copyOnly {
					require.FileExists(t, source)
				} else {
					require.NoFileExists(t, source)
				}
				require.NoError(t, deps.JobStore.PersistJobByID(job.GetID()))
				row, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
				require.NoError(t, err)
				decoded, errs := jobpersist.Decode(row)
				require.Empty(t, errs)
				require.Equal(t, outcome, decoded.Results[source].VideoOutcome)
			})
		}
	}
}
