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

func TestBatchRescrapeMovies(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (jobID string, movieIDs []string)
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BulkRescrapeResponse)
	}{
		{
			name: "empty movie_ids returns 400",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{}
			},
			requestBody: BulkRescrapeRequest{
				MovieIDs:         []string{},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 400,
		},
		{
			name: "no scrapers returns 400",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{"IPX-535"}
			},
			requestBody: BulkRescrapeRequest{
				MovieIDs: []string{"IPX-535"},
			},
			expectedStatus: 400,
		},
		{
			name: "job not found returns 404",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				return "nonexistent-job", []string{"IPX-535"}
			},
			requestBody: BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 404,
		},
		{
			name: "invalid JSON returns 400",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				return job.ID, []string{"IPX-535"}
			},
			requestBody:    "{invalid-json",
			expectedStatus: 400,
		},
		{
			name: "best-effort: mixed found and not-found movie IDs",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{"IPX-535", "NONEXISTENT-999"}
			},
			requestBody: BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535", "NONEXISTENT-999"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 2)
				assert.NotNil(t, resp.Job)
				var foundNotFound bool
				for _, r := range resp.Results {
					if r.MovieID == "NONEXISTENT-999" && r.Status == "failed" {
						foundNotFound = true
					}
				}
				assert.True(t, foundNotFound, "should have a failed result for NONEXISTENT-999")
			},
		},
		{
			name: "all movies not found still returns 200 with failed results",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/ABC-123.mp4"})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{"MISSING-001", "MISSING-002"}
			},
			requestBody: BulkRescrapeRequest{
				MovieIDs:         []string{"MISSING-001", "MISSING-002"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 2)
				assert.Equal(t, 0, resp.Succeeded)
				assert.Equal(t, 2, resp.Failed)
				for _, r := range resp.Results {
					assert.Equal(t, "failed", r.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := config.DefaultConfig()
			cfg.Scrapers.UserAgent = "Test Agent"
			cfg.Scrapers.Referer = "https://test.com"
			cfg.Scrapers.RequestTimeoutSeconds = 30
			cfg.Scrapers.Priority = []string{"r18dev"}
			cfg.Scrapers.Proxy = config.ProxyConfig{Enabled: false}
			cfg.API.Security.AllowedDirectories = []string{"/path", "/test"}

			deps := createTestDeps(t, cfg, "")

			jobID, _ := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/batch-rescrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())

			if tt.validateFn != nil && w.Code == 200 {
				var resp BulkRescrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}
