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
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestIssue277HTTPImplicitMultipartDuplicatePreflight(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "output")
	require.NoError(t, os.MkdirAll(output, 0755))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.Output.Template.FolderFormat = "shared"
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = nil
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	source := filepath.Join(root, "a", "IPX-277.mp4")
	part := filepath.Join(root, "a", "IPX-277-cd2.mp4")
	competitor := filepath.Join(root, "b", "other.mp4")
	for path, body := range map[string]string{source: "primary", part: "part two", competitor: "competitor"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	}
	job := deps.JobStore.CreateJobBatch([]string{source, competitor})
	for path, id := range map[string]string{source: "IPX-277", competitor: "IPX-277-cd2"} {
		movie := models.Movie{ID: id, ContentID: id, Title: "Synthetic"}
		_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &movie)
		require.NoError(t, err)
		stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
		require.NoError(t, err)
		setJobResult(job, path, &resultstore.MovieResult{ResultID: id, Status: models.JobStatusCompleted, Movie: stored, PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: path, Name: filepath.Base(path), MovieID: id, Extension: ".mp4"}})
	}
	setJobStatus(job, models.JobStatusCompleted)
	router := gin.New()
	router.POST("/batch/:id/video-collisions", videoCollisionReview(rt))
	router.POST("/batch/:id/organize", organizeJob(rt))
	base := "/batch/" + job.GetID()
	req := contracts.OrganizeRequest{Destination: output, SkipNFO: true, SkipDownload: true}
	response := doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
	require.Equal(t, 200, response.Code, response.Body.String())
	var report videoCollisionReport
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.Equal(t, 1, report.Unresolved)
	var collision videocollision.Collision
	for _, item := range report.Items {
		if item.Source == competitor {
			collision = item
		}
	}
	require.Equal(t, "duplicate", collision.Kind)
	require.Equal(t, filepath.Join(output, "shared", "IPX-277-cd2.mp4"), collision.Destination)
	response = doJSON(t, router, http.MethodPost, base+"/organize", req)
	require.Equal(t, 409, response.Code, response.Body.String())
	req.VideoCollisionDecisions = map[string]videocollision.Decision{competitor: {Source: competitor, Destination: collision.Destination, Policy: videocollision.Skip}}
	response = doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.Zero(t, report.Unresolved)
	require.Equal(t, 2, report.New)
	response = doJSON(t, router, http.MethodPost, base+"/organize", req)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, job.Controller().Wait())
	require.Zero(t, job.GetStatus().Failed)
	body, err := os.ReadFile(collision.Destination)
	require.NoError(t, err)
	require.Equal(t, "part two", string(body))
	require.FileExists(t, competitor)
	require.NoFileExists(t, part)
}
