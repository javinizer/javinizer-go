package batch

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrganizeJob_OperationMode(t *testing.T) {
	tests := []struct {
		name           string
		operationMode  string
		configMode     string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "organize mode accepted",
			operationMode:  "organize",
			configMode:     "in-place",
			expectedStatus: 200,
		},
		{
			name:           "in-place mode accepted",
			operationMode:  "in-place",
			configMode:     "organize",
			expectedStatus: 200,
		},
		{
			name:           "preview mode rejected",
			operationMode:  "preview",
			configMode:     "organize",
			expectedStatus: 400,
			expectedError:  "Preview mode should use the preview endpoint",
		},
		{
			name:           "invalid operation mode rejected",
			operationMode:  "invalid-mode",
			configMode:     "organize",
			expectedStatus: 400,
			expectedError:  "Invalid operation_mode",
		},
		{
			name:           "no operation mode uses config default",
			operationMode:  "",
			configMode:     "organize",
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
				Output: config.OutputConfig{
					OperationMode: config.GetOperationMode(tt.configMode),
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{"/path", "/output"},
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			job := deps.JobQueue.CreateJob([]string{"/path/to/file.mp4"})
			result := &worker.FileResult{
				FilePath:  "/path/to/file.mp4",
				MovieID:   "TEST-001",
				Status:    worker.JobStatusCompleted,
				Data:      &models.Movie{ID: "TEST-001", Title: "Test"},
				StartedAt: time.Now(),
			}
			job.UpdateFileResult("/path/to/file.mp4", result)
			job.MarkCompleted()

			router := gin.New()
			router.POST("/batch/:id/organize", organizeJob(deps))

			reqBody := OrganizeRequest{
				Destination:   "/output",
				OperationMode: tt.operationMode,
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+job.ID+"/organize", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
			if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestPreviewOrganize_OperationMode(t *testing.T) {
	tests := []struct {
		name           string
		operationMode  string
		expectedInResp string
	}{
		{
			name:           "organize mode",
			operationMode:  "organize",
			expectedInResp: "organize",
		},
		{
			name:           "in-place mode",
			operationMode:  "in-place",
			expectedInResp: "in-place",
		},
		{
			name:           "in-place-norenamefolder mode",
			operationMode:  "in-place-norenamefolder",
			expectedInResp: "in-place-norenamefolder",
		},
		{
			name:           "metadata-artwork mode",
			operationMode:  "metadata-artwork",
			expectedInResp: "metadata-artwork",
		},
		{
			name:           "preview mode",
			operationMode:  "preview",
			expectedInResp: "preview",
		},
		{
			name:           "empty mode defaults to config",
			operationMode:  "",
			expectedInResp: "organize",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{
				Output: config.OutputConfig{
					FolderFormat:        "<ID>",
					FileFormat:          "<ID>",
					OperationMode:       config.GetOperationMode("organize"),
					DownloadPoster:      true,
					DownloadExtrafanart: true,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{"/path", "/output"},
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			job := deps.JobQueue.CreateJob([]string{"/path/to/TEST-001.mp4"})
			result := &worker.FileResult{
				FilePath:  "/path/to/TEST-001.mp4",
				MovieID:   "TEST-001",
				Status:    worker.JobStatusCompleted,
				Data:      &models.Movie{ID: "TEST-001", Title: "Test Movie"},
				StartedAt: time.Now(),
			}
			job.UpdateFileResult("/path/to/TEST-001.mp4", result)

			router := gin.New()
			router.POST("/batch/:id/movies/:movieId/preview", previewOrganize(deps))

			reqBody := OrganizePreviewRequest{
				Destination:   "/output",
				OperationMode: tt.operationMode,
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+job.ID+"/movies/TEST-001/preview", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, 200, w.Code, "Response body: %s", w.Body.String())

			var resp OrganizePreviewResponse
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedInResp, resp.OperationMode, "operation_mode in response should match")
		})
	}
}

func TestPreviewOrganize_InvalidOperationMode(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/TEST-001.mp4"})
	result := &worker.FileResult{
		FilePath:  "/path/to/TEST-001.mp4",
		MovieID:   "TEST-001",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		StartedAt: time.Now(),
	}
	job.UpdateFileResult("/path/to/TEST-001.mp4", result)

	router := gin.New()
	router.POST("/batch/:id/movies/:movieId/preview", previewOrganize(deps))

	reqBody := OrganizePreviewRequest{
		Destination:   "/output",
		OperationMode: "invalid-mode",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/batch/"+job.ID+"/movies/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code, "Response body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "Invalid operation_mode")
}
