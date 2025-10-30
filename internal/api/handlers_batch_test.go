package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchScrape(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BatchScrapeResponse)
	}{
		{
			name: "valid batch scrape request",
			requestBody: BatchScrapeRequest{
				Files:       []string{"/path/to/IPX-535.mp4", "/path/to/ABC-123.mkv"},
				Strict:      false,
				Force:       false,
				Destination: "/output",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchScrapeResponse) {
				assert.NotEmpty(t, resp.JobID)
			},
		},
		{
			name: "single file",
			requestBody: BatchScrapeRequest{
				Files: []string{"/path/to/IPX-535.mp4"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchScrapeResponse) {
				assert.NotEmpty(t, resp.JobID)
			},
		},
		{
			name: "invalid request - missing files",
			requestBody: map[string]interface{}{
				"strict": false,
			},
			expectedStatus: 400,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			registry := models.NewScraperRegistry()
			mockRepo := newMockMovieRepo()
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
			}
			agg := aggregator.New(cfg)
			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)
			jobQueue := worker.NewJobQueue()

			router := gin.New()
			router.POST("/batch/scrape", batchScrape(registry, agg, mockRepo, mat, jobQueue, cfg))

			var body []byte
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response BatchScrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestGetBatchJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string // Returns job ID
		jobID          string
		expectedStatus int
		validateFn     func(*testing.T, *BatchJobResponse)
	}{
		{
			name: "get existing job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchJobResponse) {
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, 1, resp.TotalFiles)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job-id"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobQueue := worker.NewJobQueue()
			jobID := tt.setupJob(jobQueue)

			router := gin.New()
			router.GET("/batch/:id", getBatchJob(jobQueue))

			req := httptest.NewRequest("GET", "/batch/"+jobID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response BatchJobResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestCancelBatchJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		expectedStatus int
	}{
		{
			name: "cancel existing job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID
			},
			expectedStatus: 200,
		},
		{
			name: "cancel nonexistent job",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job-id"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobQueue := worker.NewJobQueue()
			jobID := tt.setupJob(jobQueue)

			router := gin.New()
			router.POST("/batch/:id/cancel", cancelBatchJob(jobQueue))

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/cancel", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestUpdateBatchMovie(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (string, string) // Returns jobID, movieID
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *MovieResponse)
	}{
		{
			name: "update movie successfully",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})

				// Simulate a completed scrape with movie data
				result := &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Original Title"},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/IPX-535.mp4", result)

				return job.ID, "IPX-535"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{
					ID:    "IPX-535",
					Title: "Updated Title",
				},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *MovieResponse) {
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "Updated Title", resp.Movie.Title)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "IPX-535"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{ID: "IPX-535"},
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/ABC-123.mp4"})
				result := &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123"},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/ABC-123.mp4", result)
				return job.ID, "NONEXISTENT-999"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{ID: "NONEXISTENT-999"},
			},
			expectedStatus: 404,
		},
		{
			name: "invalid request body",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID, "IPX-535"
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobQueue := worker.NewJobQueue()
			mockRepo := newMockMovieRepo()

			jobID, movieID := tt.setupJob(jobQueue)

			router := gin.New()
			router.PATCH("/batch/:id/movies/:movieId", updateBatchMovie(mockRepo, jobQueue))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("PATCH", "/batch/"+jobID+"/movies/"+movieID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestOrganizeJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		requestBody    interface{}
		expectedStatus int
	}{
		{
			name: "organize completed job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
				CopyOnly:    false,
			},
			expectedStatus: 200,
		},
		{
			name: "organize job not completed",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				// Job still running
				return job.ID
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
			},
			expectedStatus: 400,
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job"
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
		{
			name: "invalid request body",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			jobQueue := worker.NewJobQueue()
			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
			}
			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			// We need a mock DB, but since we're not actually processing,
			// we can pass nil for this test (the handler will launch in background)

			jobID := tt.setupJob(jobQueue)

			router := gin.New()
			router.POST("/batch/:id/organize", organizeJob(mat, jobQueue, nil, cfg))

			var body []byte
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/organize", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestPreviewOrganize(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (string, string) // Returns jobID, movieID
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *OrganizePreviewResponse)
	}{
		{
			name: "preview successfully",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				result := &worker.FileResult{
					FilePath: "/path/to/IPX-535.mp4",
					MovieID:  "IPX-535",
					Status:   worker.JobStatusCompleted,
					Data: &models.Movie{
						ID:    "IPX-535",
						Title: "Test Movie",
					},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/IPX-535.mp4", result)
				return job.ID, "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				CopyOnly:    false,
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *OrganizePreviewResponse) {
				assert.NotEmpty(t, resp.FolderName)
				assert.NotEmpty(t, resp.FileName)
				assert.NotEmpty(t, resp.FullPath)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID, "NONEXISTENT-999"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			jobQueue := worker.NewJobQueue()
			cfg := &config.Config{
				Output: config.OutputConfig{
					FolderFormat: "<ID>",
					FileFormat:   "<ID>",
				},
			}

			jobID, movieID := tt.setupJob(jobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/:movieId/preview", previewOrganize(jobQueue, cfg))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/"+movieID+"/preview", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response OrganizePreviewResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}
