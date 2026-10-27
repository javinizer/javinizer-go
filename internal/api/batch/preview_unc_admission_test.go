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
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type uncAdmissionFS struct {
	afero.Fs
	statPaths []string
}

func (fs *uncAdmissionFS) Stat(name string) (os.FileInfo, error) {
	fs.statPaths = append(fs.statPaths, name)
	return nil, os.ErrPermission
}

func TestPreviewOrganizeRejectsUNCBeforePathOrWorkflowAccess(t *testing.T) {
	// UNC detection is cross-platform. This exercises the real registered
	// endpoint without simulating Windows or accessing any remote share.
	tests := []struct {
		name        string
		destination string
	}{
		{"standard", `\\unc-policy.invalid\media`},
		{"Win32 namespace", `\\?\uNc\unc-policy.invalid\media`},
		{"NT namespace", `\??\UnC\unc-policy.invalid\media`},
		{"device namespace", `\\.\unC\unc-policy.invalid\media`},
		{"server only", `\\unc-policy.invalid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "IPX-535.mp4")
			video := []byte("unchanged source video")
			require.NoError(t, os.WriteFile(source, video, 0o600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.System.TempDir = filepath.Join(root, "temp")
			cfg.Output.Operation.OperationMode = "organize"
			// Even an explicitly listed destination/server cannot override
			// AllowUNC=false. Rejection must precede filesystem validation.
			cfg.API.Security.AllowedDirectories = []string{root, tt.destination}
			cfg.API.Security.AllowedUNCServers = []string{"unc-policy.invalid"}
			cfg.API.Security.AllowUNC = false
			deps := createTestDeps(t, cfg, "")
			fs := &uncAdmissionFS{Fs: afero.NewMemMapFs()}
			deps.Fs = fs

			// A legacy job has no ApplyPlan to replace the request's explicit
			// organize mode or destination, and no running workflow.
			job := deps.JobStore.CreateJobBatch([]string{source})
			setJobResult(job, source, &resultstore.MovieResult{
				ResultID: "preview-result", Revision: 7,
				FileMatchInfo: models.FileMatchInfo{Path: source, MovieID: "IPX-535"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "IPX-535", ContentID: "ipx00535", Title: "Unchanged title"},
			})
			setJobStatus(job, models.JobStatusCompleted)
			before := job.GetStatus()
			require.Nil(t, before.ApplyPlan)

			// Authority resolution precedes workflow construction. Observe
			// both lookup methods so a forbidden preview cannot reach either.
			reads := 0
			repo := deps.Repos.MovieRepo
			deps.Repos.MovieRepo = authorityLookupRepo{
				MovieRepositoryInterface: repo,
				findByContentID: func(ctx context.Context, id string) (*models.Movie, error) {
					reads++
					return repo.FindByContentID(ctx, id)
				},
				findByID: func(ctx context.Context, id string) (*models.Movie, error) {
					reads++
					return repo.FindByID(ctx, id)
				},
			}

			router := gin.New()
			RegisterRoutes(router.Group(""), testkit.GetTestRuntime(deps))
			body, err := json.Marshal(contracts.OrganizePreviewRequest{
				Destination: tt.destination, OperationMode: "organize",
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/preview-result/preview", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)

			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			var denied contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &denied))
			require.Equal(t, "Access denied to requested directory", denied.Error)
			require.Empty(t, fs.statPaths, "UNC denial must precede path validation")
			require.Zero(t, reads, "UNC denial must precede movie resolution and workflow execution")
			require.Equal(t, before, job.GetStatus(), "denial must not mutate job, result, or revision")
			operations, err := deps.Repos.BatchFileOpRepo.FindByBatchJobID(context.Background(), job.GetID())
			require.NoError(t, err)
			require.Empty(t, operations)
			after, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, video, after)
		})
	}
}
