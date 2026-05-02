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

func TestBatchExcludeIntegration(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		movieIDs       []string
		expectedStatus int
		validateFn     func(*testing.T, *BatchExcludeResponse)
	}{
		{
			name: "exclude 3 movies at once all succeed",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{
					"/path/to/IPX-535.mp4",
					"/path/to/ABC-123.mp4",
					"/path/to/SSIS-001.mp4",
				})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Movie 1"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123", Title: "Movie 2"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/SSIS-001.mp4", &worker.FileResult{
					FilePath:  "/path/to/SSIS-001.mp4",
					MovieID:   "SSIS-001",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "SSIS-001", Title: "Movie 3"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"IPX-535", "ABC-123", "SSIS-001"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 3)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Contains(t, resp.Excluded, "ABC-123")
				assert.Contains(t, resp.Excluded, "SSIS-001")
				assert.Empty(t, resp.Failed)
				assert.NotNil(t, resp.Job)
			},
		},
		{
			name: "exclude multipart movie excludes all parts",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{
					"/path/to/IPX-535-CD1.mp4",
					"/path/to/IPX-535-CD2.mp4",
					"/path/to/ABC-123.mp4",
				})
				job.UpdateFileResult("/path/to/IPX-535-CD1.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535-CD1.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Multi-Part Movie"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/IPX-535-CD2.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535-CD2.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Multi-Part Movie"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123", Title: "Other Movie"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"IPX-535"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Empty(t, resp.Failed)
				assert.NotNil(t, resp.Job)
				assert.NotNil(t, resp.Job.Excluded)
				excludedPaths := resp.Job.Excluded
				assert.Contains(t, excludedPaths, "/path/to/IPX-535-CD1.mp4")
				assert.Contains(t, excludedPaths, "/path/to/IPX-535-CD2.mp4")
			},
		},
		{
			name: "exclude all movies in job triggers MarkCancelled",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4", "/path/to/ABC-123.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test 1"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123", Title: "Test 2"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"IPX-535", "ABC-123"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 2)
				assert.Empty(t, resp.Failed)
				assert.NotNil(t, resp.Job)
				assert.Equal(t, string(worker.JobStatusCancelled), resp.Job.Status)
			},
		},
		{
			name: "exclude with some not-found and some found returns partial success",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"IPX-535", "MISSING-001", "MISSING-002"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 1)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Len(t, resp.Failed, 2)
				failedIDs := make([]string, len(resp.Failed))
				for i, f := range resp.Failed {
					failedIDs[i] = f.MovieID
				}
				assert.Contains(t, failedIDs, "MISSING-001")
				assert.Contains(t, failedIDs, "MISSING-002")
			},
		},
		{
			name: "exclude by resolved content ID (Movie.ID differs from MovieID)",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/ABP-071.mp4"})
				job.UpdateFileResult("/path/to/ABP-071.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABP-071.mp4",
					MovieID:   "ABP-071",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABP-071DOD", Title: "Resolved Movie"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"ABP-071DOD"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Contains(t, resp.Excluded, "ABP-071DOD")
				assert.Empty(t, resp.Failed)
			},
		},
		{
			name: "exclude all not-found movies returns empty excluded with failed list",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				})
				return job.ID
			},
			movieIDs:       []string{"MISSING-001", "MISSING-002"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Empty(t, resp.Excluded)
				assert.Len(t, resp.Failed, 2)
				assert.NotNil(t, resp.Job)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")

			jobID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(deps))

			body, err := json.Marshal(BatchExcludeRequest{MovieIDs: tt.movieIDs})
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/batch-exclude", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())

			if tt.validateFn != nil && w.Code == 200 {
				var resp BatchExcludeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}
