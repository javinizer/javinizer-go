package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPreviewOrganizeCancellationAfterAuthoritativeReadDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "IPX-535.mp4")
	videoBytes := []byte("unchanged video")
	require.NoError(t, os.WriteFile(filePath, videoBytes, 0o644))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.Output.Operation.OperationMode = "in-place"
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	repo := deps.Repos.MovieRepo
	movie := &models.Movie{ID: "IPX-535", ContentID: "ipx00535", Title: "Authoritative title"}
	require.NoError(t, repo.Create(context.Background(), movie))
	beforeMovie, err := repo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID: "preview-result", Revision: 7,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movie.ID, ContentID: movie.ContentID, Title: "Stale job title"},
	})
	setJobStatus(job, models.JobStatusCompleted)
	beforeStatus := job.GetStatus()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	projectionRepo, ok := repo.(database.MovieProjectionRepositoryInterface)
	require.True(t, ok)
	reads := 0
	deps.Repos.MovieRepo = authorityProjectionRepo{
		authorityLookupRepo: authorityLookupRepo{
			MovieRepositoryInterface: repo,
			findByID:                 repo.FindByID,
			findByContentID: func(readCtx context.Context, contentID string) (*models.Movie, error) {
				reads++
				require.NoError(t, readCtx.Err(), "the authority lookup must succeed before cancellation")
				loaded, readErr := repo.FindByContentID(readCtx, contentID)
				require.NoError(t, readErr)
				require.Equal(t, beforeMovie, loaded)
				// Cancel at a synchronous repository boundary, not a timer or
				// context callback. The real Preview must reject this context.
				cancel()
				return loaded, nil
			},
		},
		findProjection: projectionRepo.FindAuthoritativeProjections,
	}
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(rt))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/preview-result/preview", bytes.NewBufferString(`{"operation_mode":"in-place"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 1, reads)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var response contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "Preview failed: context canceled", response.Error, "must reach Preview rather than fail authority resolution")
	require.Equal(t, beforeStatus, job.GetStatus(), "preview failure must not start apply or edit job results")
	afterMovie, err := repo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, beforeMovie, afterMovie)
	operations, err := deps.Repos.BatchFileOpRepo.FindByBatchJobID(context.Background(), job.GetID())
	require.NoError(t, err)
	require.Empty(t, operations)
	got, err := os.ReadFile(filePath)
	require.NoError(t, err)
	require.Equal(t, videoBytes, got)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "preview failure must not generate or organize files")
	require.Equal(t, filepath.Base(filePath), entries[0].Name())
}
