package batch

import (
	"encoding/json"
	"fmt"
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
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestIssue277HTTPTwoToOneRequiresExplicitSkip(t *testing.T) {
	for _, skipFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(skipFirst), func(t *testing.T) {
			root := t.TempDir()
			output := filepath.Join(root, "output")
			require.NoError(t, os.MkdirAll(output, 0755))
			cfg := config.DefaultConfig(nil, nil)
			cfg.API.Security.AllowedDirectories = []string{root}
			cfg.Output.Template.FolderFormat = "<ID>"
			cfg.Output.Template.FileFormat = "<ID>"
			cfg.Output.Template.SubfolderFormat = nil
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)
			movie := models.Movie{ID: "IPX-277", ContentID: "two-to-one-277", Title: "Synthetic"}
			_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &movie)
			require.NoError(t, err)
			stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
			require.NoError(t, err)
			sources := []string{filepath.Join(root, "a", "IPX-277.mp4"), filepath.Join(root, "b", "IPX-277.mp4")}
			for i, source := range sources {
				require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
				require.NoError(t, os.WriteFile(source, []byte(fmt.Sprintf("video-%d", i)), 0644))
			}
			job := deps.JobStore.CreateJobBatch(sources)
			for i, source := range sources {
				setJobResult(job, source, &resultstore.MovieResult{ResultID: fmt.Sprintf("result%d", i), Status: models.JobStatusCompleted, Movie: stored.Clone(), PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mp4"}})
			}
			setJobStatus(job, models.JobStatusCompleted)
			router := gin.New()
			router.POST("/batch/:id/video-collisions", videoCollisionReview(rt))
			router.POST("/batch/:id/organize", organizeJob(rt))
			base := "/batch/" + job.GetID()
			req := contracts.OrganizeRequest{Destination: output, SkipNFO: false, SkipDownload: true}
			response := doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			var report videoCollisionReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Equal(t, 1, report.Unresolved)
			require.Equal(t, "duplicate", report.Items[1].Kind)
			response = doJSON(t, router, http.MethodPost, base+"/organize", req)
			require.Equal(t, 409, response.Code, response.Body.String())
			skipIndex := 1
			if skipFirst {
				skipIndex = 0
			}
			selected := report.Items[skipIndex]
			req.VideoCollisionDecisions = map[string]videocollision.Decision{selected.Source: {Source: selected.Source, Destination: selected.Destination, Policy: videocollision.Skip}}
			response = doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Zero(t, report.Unresolved)
			require.Equal(t, 1, report.New)
			require.Equal(t, 1, report.Skipped)
			response = doJSON(t, router, http.MethodPost, base+"/organize", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NoError(t, job.Controller().Wait())
			status := job.GetStatus()
			require.Zero(t, status.Failed, "%+v", status.Results)
			require.Equal(t, "skipped", status.Results[selected.Source].VideoOutcome)
			require.FileExists(t, selected.Source)
			body, err := os.ReadFile(selected.Destination)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("video-%d", 1-skipIndex), string(body))
			entries, err := os.ReadDir(filepath.Dir(selected.Destination))
			require.NoError(t, err)
			hasNFO := false
			for _, entry := range entries {
				if filepath.Ext(entry.Name()) == ".nfo" {
					hasNFO = true
				}
			}
			require.True(t, hasNFO)
			entries, err = os.ReadDir(filepath.Dir(selected.Source))
			require.NoError(t, err)
			for _, entry := range entries {
				require.NotEqual(t, ".nfo", filepath.Ext(entry.Name()))
			}
		})
	}
}
