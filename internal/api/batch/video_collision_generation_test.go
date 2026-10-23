package batch

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func issue277GenerationFixture(t *testing.T) (*core.APIDeps, *worker.BatchJob, *gin.Engine, contracts.OrganizeRequest, videocollision.Collision) {
	t.Helper()
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
	movie := models.Movie{ID: "IPX-277", ContentID: "generation277", Title: "Synthetic"}
	_, err := deps.Repos.MovieRepo.Upsert(t.Context(), &movie)
	require.NoError(t, err)
	stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	source := filepath.Join(root, "IPX-277-original.mp4")
	require.NoError(t, os.WriteFile(source, []byte("incoming"), 0644))
	job := deps.JobStore.CreateJobBatch([]string{source})
	setJobResult(job, source, &resultstore.MovieResult{ResultID: "generation277", Status: models.JobStatusCompleted, Movie: stored, PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}})
	setJobStatus(job, models.JobStatusCompleted)
	router := gin.New()
	router.POST("/batch/:id/video-collisions", videoCollisionReview(rt))
	router.POST("/batch/:id/organize", organizeJob(rt))
	req := contracts.OrganizeRequest{Destination: output, CopyOnly: true, SkipNFO: true, SkipDownload: true}
	response := doJSON(t, router, http.MethodPost, "/batch/"+job.GetID()+"/video-collisions", req)
	require.Equal(t, 200, response.Code, response.Body.String())
	var report videoCollisionReport
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.Len(t, report.Items, 1)
	target := report.Items[0].Destination
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("foreign"), 0644))
	response = doJSON(t, router, http.MethodPost, "/batch/"+job.GetID()+"/video-collisions", req)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.NotEmpty(t, report.Items[0].ReviewToken)
	return deps, job, router, req, report.Items[0]
}

func TestIssue277HTTPApplyGenerationInvalidatesReview(t *testing.T) {
	for _, expected := range []bool{false, true} {
		t.Run(map[bool]string{false: "omitted expected generation", true: "current expected generation"}[expected], func(t *testing.T) {
			_, job, router, req, item := issue277GenerationFixture(t)
			base := "/batch/" + job.GetID()
			before, err := os.Stat(item.Destination)
			require.NoError(t, err)
			generation := job.GetStatus().ApplyGeneration
			skipped := req
			skipped.VideoCollisionPolicy = videocollision.Skip
			response := doJSON(t, router, http.MethodPost, base+"/organize", skipped)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NoError(t, job.Controller().Wait())
			require.Equal(t, "skipped", job.GetStatus().Results[item.Source].VideoOutcome)
			require.Equal(t, generation+1, job.GetStatus().ApplyGeneration)
			setJobStatus(job, models.JobStatusCompleted)
			if expected {
				current := job.GetStatus().ApplyGeneration
				req.ExpectedApplyGeneration = &current
			}
			req.VideoCollisionDecisions = map[string]videocollision.Decision{item.Source: {Source: item.Source, Destination: item.Destination, Policy: videocollision.Replace, ReviewToken: item.ReviewToken}}
			response = doJSON(t, router, http.MethodPost, base+"/organize", req)
			require.Equal(t, 409, response.Code, response.Body.String())
			var stale contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stale))
			require.Equal(t, "VIDEO_COLLISION_STALE", stale.Code)
			after, err := os.Stat(item.Destination)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			body, err := os.ReadFile(item.Destination)
			require.NoError(t, err)
			require.Equal(t, "foreign", string(body))
			response = doJSON(t, router, http.MethodPost, base+"/video-collisions", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			var refreshed videoCollisionReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refreshed))
			require.NotEqual(t, item.ReviewToken, refreshed.Items[0].ReviewToken)
			decision := req.VideoCollisionDecisions[item.Source]
			decision.ReviewToken = refreshed.Items[0].ReviewToken
			req.VideoCollisionDecisions[item.Source] = decision
			response = doJSON(t, router, http.MethodPost, base+"/organize", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NoError(t, job.Controller().Wait())
			require.Zero(t, job.GetStatus().Failed)
			require.Equal(t, "replaced", job.GetStatus().Results[item.Source].VideoOutcome)
			require.Equal(t, generation+2, job.GetStatus().ApplyGeneration)
		})
	}
}

type issue277InterveningApplyJob struct {
	worker.BatchJobInterface
	before func(context.Context, worker.ApplyPhaseConfig)
}

func (j issue277InterveningApplyJob) StartApply(ctx context.Context, cfg worker.ApplyPhaseConfig) error {
	j.before(ctx, cfg)
	return j.BatchJobInterface.StartApply(ctx, cfg)
}

func TestIssue277HTTPReviewAdmissionRaceWithoutExpectedGeneration(t *testing.T) {
	deps, job, router, req, item := issue277GenerationFixture(t)
	original, ok := deps.JobStore.GetBatchJob(job.GetID())
	require.True(t, ok)
	deps.JobStore = &excludeEdgeStore{JobStoreInterface: deps.JobStore, job: issue277InterveningApplyJob{BatchJobInterface: original, before: func(ctx context.Context, cfg worker.ApplyPhaseConfig) {
		require.NotNil(t, cfg.ExpectedApplyGeneration)
		require.Equal(t, uint64(0), *cfg.ExpectedApplyGeneration)
		skipped := cfg
		skipped.OrganizeOptions.VideoCollisionPolicy = videocollision.Skip
		skipped.ExpectedApplyGeneration = nil
		skipped.ApplyGenerationRef = new(uint64)
		require.NoError(t, original.StartApply(ctx, skipped))
		require.NoError(t, original.Wait())
		require.Equal(t, uint64(1), job.GetStatus().ApplyGeneration)
	}}}
	req.VideoCollisionPolicy = videocollision.Skip
	response := doJSON(t, router, http.MethodPost, "/batch/"+job.GetID()+"/organize", req)
	require.Equal(t, 409, response.Code, response.Body.String())
	var conflict contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &conflict))
	require.Equal(t, "APPLY_GENERATION_MISMATCH", conflict.Code)
	require.Equal(t, uint64(1), job.GetStatus().ApplyGeneration)
	body, err := os.ReadFile(item.Destination)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(body))
	body, err = os.ReadFile(item.Source)
	require.NoError(t, err)
	require.Equal(t, "incoming", string(body))
}
