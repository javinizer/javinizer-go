package batch

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestBatchRescrapeIntegration(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(worker.JobStoreInterface) (jobID string, movieIDs []string)
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *contracts.BulkRescrapeResponse)
	}{
		{
			name: "bulk rescrape 3 movies with scrapers returns results for each",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{
					"/path/to/IPX-535.mp4",
					"/path/to/ABC-123.mp4",
					"/path/to/SSIS-001.mp4",
				})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Movie 1"},
					StartedAt:     time.Now(),
				})
				setJobResult(job, "/path/to/ABC-123.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/ABC-123.mp4", MovieID: "ABC-123"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "ABC-123", Title: "Movie 2"},
					StartedAt:     time.Now(),
				})
				setJobResult(job, "/path/to/SSIS-001.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/SSIS-001.mp4", MovieID: "SSIS-001"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "SSIS-001", Title: "Movie 3"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535", "ABC-123", "SSIS-001"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535", "ABC-123", "SSIS-001"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 3, "should have result for each movie ID")
				assert.NotNil(t, resp.Job)
				assert.Equal(t, resp.Succeeded+resp.Failed, 3)
				resultIDs := make([]string, len(resp.Results))
				for i, r := range resp.Results {
					resultIDs[i] = r.MovieID
				}
				assert.Contains(t, resultIDs, "IPX-535")
				assert.Contains(t, resultIDs, "ABC-123")
				assert.Contains(t, resultIDs, "SSIS-001")
			},
		},
		{
			name: "bulk rescrape with conservative preset",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				Preset:           "conservative",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 1)
				assert.NotNil(t, resp.Job)
			},
		},
		{
			name: "bulk rescrape with gap-fill preset",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				Preset:           "gap-fill",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 1)
			},
		},
		{
			name: "bulk rescrape with aggressive preset",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				Preset:           "aggressive",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 1)
			},
		},
		{
			name: "bulk rescrape with invalid preset passed to seam",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				Preset:           "invalid_preset_name",
			},
			expectedStatus: 400, // API layer now validates presets at the boundary
		},
		{
			name: "bulk rescrape with scalar and array strategies",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				ScalarStrategy:   "prefer-nfo",
				ArrayStrategy:    "merge",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 1)
			},
		},
		{
			name: "bulk rescrape with force flag",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
				Force:            true,
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 1)
			},
		},
		{
			name: "bulk rescrape succeeded plus failed equals total",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID(), []string{"IPX-535", "NONEXISTENT-999"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535", "NONEXISTENT-999"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BulkRescrapeResponse) {
				assert.Len(t, resp.Results, 2)
				assert.Equal(t, resp.Succeeded+resp.Failed, 2)
				var foundFailed bool
				for _, r := range resp.Results {
					if r.MovieID == "NONEXISTENT-999" && r.Status == models.RescrapeStatusFailed {
						foundFailed = true
					}
				}
				assert.True(t, foundFailed, "NONEXISTENT-999 should have failed status")
			},
		},
		{
			name: "bulk rescrape invalid JSON returns 400",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				return job.GetID(), []string{"IPX-535"}
			},
			requestBody:    "{invalid-json",
			expectedStatus: 400,
		},
		{
			name: "bulk rescrape job not found returns 404",
			setupJob: func(jq worker.JobStoreInterface) (string, []string) {
				return "nonexistent-job", []string{"IPX-535"}
			},
			requestBody: contracts.BulkRescrapeRequest{
				MovieIDs:         []string{"IPX-535"},
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.UserAgent = "Test Agent"
			cfg.Scrapers.Referer = "https://test.com"
			cfg.Scrapers.RequestTimeoutSeconds = 30
			cfg.Scrapers.Priority = []string{"r18dev"}
			cfg.Scrapers.Proxy = models.ProxyConfig{Enabled: false}
			cfg.API.Security.AllowedDirectories = []string{"/path", "/test"}

			deps := createTestDeps(t, cfg, "")

			jobID, _ := tt.setupJob(deps.JobStore)

			router := gin.New()
			router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

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
				var resp contracts.BulkRescrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}
