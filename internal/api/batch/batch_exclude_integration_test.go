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

func TestBatchExcludeIntegration(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(worker.JobStoreInterface) string
		movieIDs       []string
		expectedStatus int
		validateFn     func(*testing.T, *contracts.BatchExcludeResponse)
	}{
		{
			name: "exclude 3 movies at once all succeed",
			setupJob: func(jq worker.JobStoreInterface) string {
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
				return job.GetID()
			},
			movieIDs:       []string{"IPX-535", "ABC-123", "SSIS-001"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
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
			setupJob: func(jq worker.JobStoreInterface) string {
				job := jq.CreateJobBatch([]string{
					"/path/to/IPX-535-CD1.mp4",
					"/path/to/IPX-535-CD2.mp4",
					"/path/to/ABC-123.mp4",
				})
				setJobResult(job, "/path/to/IPX-535-CD1.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535-CD1.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Multi-Part Movie"},
					StartedAt:     time.Now(),
				})
				setJobResult(job, "/path/to/IPX-535-CD2.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535-CD2.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Multi-Part Movie"},
					StartedAt:     time.Now(),
				})
				setJobResult(job, "/path/to/ABC-123.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/ABC-123.mp4", MovieID: "ABC-123"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "ABC-123", Title: "Other Movie"},
					StartedAt:     time.Now(),
				})
				return job.GetID()
			},
			movieIDs:       []string{"IPX-535"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
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
			setupJob: func(jq worker.JobStoreInterface) string {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4", "/path/to/ABC-123.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test 1"},
					StartedAt:     time.Now(),
				})
				setJobResult(job, "/path/to/ABC-123.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/ABC-123.mp4", MovieID: "ABC-123"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "ABC-123", Title: "Test 2"},
					StartedAt:     time.Now(),
				})
				return job.GetID()
			},
			movieIDs:       []string{"IPX-535", "ABC-123"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 2)
				assert.Empty(t, resp.Failed)
				assert.NotNil(t, resp.Job)
				assert.Equal(t, models.JobStatusCancelled, resp.Job.Status)
			},
		},
		{
			name: "exclude with some not-found and some found returns partial success",
			setupJob: func(jq worker.JobStoreInterface) string {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID()
			},
			movieIDs:       []string{"IPX-535", "MISSING-001", "MISSING-002"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 1)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Len(t, resp.Failed, 2)
				failedIDs := make([]string, len(resp.Failed))
				for i, f := range resp.Failed {
					failedIDs[i] = f.ResultID
				}
				assert.Contains(t, failedIDs, "MISSING-001")
				assert.Contains(t, failedIDs, "MISSING-002")
			},
		},
		{
			name: "exclude by resultID when Movie.ID differs from MovieID",
			setupJob: func(jq worker.JobStoreInterface) string {
				job := jq.CreateJobBatch([]string{"/path/to/ABP-071.mp4"})
				setJobResult(job, "/path/to/ABP-071.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/ABP-071.mp4", MovieID: "ABP-071"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "ABP-071DOD", Title: "Resolved Movie"},
					StartedAt:     time.Now(),
				})
				return job.GetID()
			},
			movieIDs:       []string{"ABP-071"}, // resultID derived from FileMatchInfo.MovieID
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
				assert.Contains(t, resp.Excluded, "ABP-071")
				assert.Empty(t, resp.Failed)
			},
		},
		{
			name: "exclude all not-found movies returns empty excluded with failed list",
			setupJob: func(jq worker.JobStoreInterface) string {
				job := jq.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
				setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt:     time.Now(),
				})
				return job.GetID()
			},
			movieIDs:       []string{"MISSING-001", "MISSING-002"},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.BatchExcludeResponse) {
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

			jobID := tt.setupJob(deps.JobStore)

			router := gin.New()
			router.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(testkit.GetTestRuntime(deps)))

			body, err := json.Marshal(contracts.BatchExcludeRequest{ResultIDs: tt.movieIDs})
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/batch-exclude", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())

			if tt.validateFn != nil && w.Code == 200 {
				var resp contracts.BatchExcludeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}
