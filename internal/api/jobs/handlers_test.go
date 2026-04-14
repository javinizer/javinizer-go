package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *JobListResponse)
	}{
		{
			name:           "empty jobs list",
			queryParams:    "",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *JobListResponse) {
				assert.Empty(t, resp.Jobs)
			},
		},
		{
			name:           "list all jobs",
			queryParams:    "",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *JobListResponse) {
				assert.Len(t, resp.Jobs, 3)

				// Find the organized job and verify operation_count
				var organizedJob *JobListItem
				for i := range resp.Jobs {
					if resp.Jobs[i].Status == "organized" {
						organizedJob = &resp.Jobs[i]
						break
					}
				}
				require.NotNil(t, organizedJob, "should find an organized job")
				assert.Equal(t, int64(3), organizedJob.OperationCount, "organized job should have 3 operations")
				assert.Equal(t, int64(1), organizedJob.RevertedCount, "organized job should have 1 reverted operation")
			},
		},
		{
			name:           "filter by status organized",
			queryParams:    "?status=organized",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *JobListResponse) {
				assert.Len(t, resp.Jobs, 1)
				assert.Equal(t, "organized", resp.Jobs[0].Status)
			},
		},
		{
			name:           "filter by status with no matches",
			queryParams:    "?status=running",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *JobListResponse) {
				assert.Empty(t, resp.Jobs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupJobsTestDeps(t)
			defer func() { _ = db.Close() }()

			if tt.seedData {
				seedJobsData(t, deps)
			}

			router := gin.New()
			router.GET("/api/v1/jobs", listJobs(deps))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				var resp JobListResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

func TestListOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		setupFn        func(*testing.T, *ServerDependencies) string // returns the job ID to use
		expectedStatus int
		validateFn     func(*testing.T, []byte)
	}{
		{
			name: "valid job with operations",
			setupFn: func(t *testing.T, deps *ServerDependencies) string {
				return seedJobsData(t, deps)
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var resp OperationListResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "organized", resp.JobStatus)
				assert.Equal(t, int64(3), resp.Total)
				assert.Len(t, resp.Operations, 3)

				// Verify each OperationItem has required fields populated
				for _, op := range resp.Operations {
					assert.NotZero(t, op.ID, "operation ID should be non-zero")
					assert.NotEmpty(t, op.MovieID, "movie_id should be populated")
					assert.NotEmpty(t, op.OriginalPath, "original_path should be populated")
					assert.NotEmpty(t, op.NewPath, "new_path should be populated")
					assert.NotEmpty(t, op.OperationType, "operation_type should be populated")
					assert.NotEmpty(t, op.RevertStatus, "revert_status should be populated")
					assert.NotEmpty(t, op.CreatedAt, "created_at should be populated")
				}
			},
		},
		{
			name: "non-existent job",
			setupFn: func(t *testing.T, deps *ServerDependencies) string {
				return "nonexistent-id"
			},
			expectedStatus: http.StatusNotFound,
			validateFn: func(t *testing.T, body []byte) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job not found", errResp.Error)
			},
		},
		{
			name: "job with no operations",
			setupFn: func(t *testing.T, deps *ServerDependencies) string {
				job := createTestJob(t, deps, "organized")
				return job.ID
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var resp OperationListResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Empty(t, resp.Operations)
				assert.Equal(t, int64(0), resp.Total)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupJobsTestDeps(t)
			defer func() { _ = db.Close() }()

			jobID := tt.setupFn(t, deps)

			router := gin.New()
			router.GET("/api/v1/jobs/:id/operations", listOperations(deps))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID+"/operations", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, w.Body.Bytes())
			}
		})
	}
}

func TestRevertDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Verify that revert endpoints return 403 when AllowRevert is false (default)
	t.Run("revert_batch_returns_403_when_disabled", func(t *testing.T) {
		deps, db := setupJobsTestDeps(t)
		// AllowRevert defaults to false in setupJobsTestDeps — but we explicitly
		// set up with it disabled to be clear about intent
		cfg := deps.GetConfig()
		cfg.Output.AllowRevert = false
		deps.SetConfig(cfg)
		defer func() { _ = db.Close() }()

		// Create an organized job (would normally be revertible)
		_ = createTestJob(t, deps, "organized")

		router := gin.New()
		router.POST("/api/v1/jobs/:id/revert", revertBatch(deps))
		router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(deps))
		router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

		// Test revertBatch
		req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/some-id/revert", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
		var errResp ErrorResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
		assert.Equal(t, "Revert is disabled. Enable it in Settings > File Operations.", errResp.Error)

		// Test revertOperation
		req = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/some-id/operations/ABC-001/revert", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)

		// Test revertCheck
		req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/some-id/revert-check", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestRevertBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		setupFn        func(*testing.T, *ServerDependencies, afero.Fs) string // returns jobID
		expectedStatus int
		validateFn     func(*testing.T, []byte, *ServerDependencies)
	}{
		{
			name: "non-existent job",
			setupFn: func(t *testing.T, deps *ServerDependencies, _ afero.Fs) string {
				return "nonexistent-id"
			},
			expectedStatus: http.StatusNotFound,
			validateFn: func(t *testing.T, body []byte, _ *ServerDependencies) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job not found", errResp.Error)
			},
		},
		{
			name: "non-organized status",
			setupFn: func(t *testing.T, deps *ServerDependencies, _ afero.Fs) string {
				job := createTestJob(t, deps, "completed")
				return job.ID
			},
			expectedStatus: http.StatusBadRequest,
			validateFn: func(t *testing.T, body []byte, _ *ServerDependencies) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job is not in organized status", errResp.Error)
			},
		},
		{
			name: "successful batch revert",
			setupFn: func(t *testing.T, deps *ServerDependencies, fs afero.Fs) string {
				return seedRevertableJob(t, deps, fs, []string{"ABC-001", "ABC-002"})
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte, deps *ServerDependencies) {
				var resp RevertResultResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "reverted", resp.Status)
				assert.Equal(t, 2, resp.Total)
				assert.Equal(t, 2, resp.Succeeded)
				assert.Equal(t, 0, resp.Failed)
			},
		},
		{
			name: "already reverted returns 400",
			setupFn: func(t *testing.T, deps *ServerDependencies, fs afero.Fs) string {
				jobID := seedRevertableJob(t, deps, fs, []string{"DEF-001", "DEF-002"})

				// Perform the revert first
				router := gin.New()
				router.POST("/api/v1/jobs/:id/revert", revertBatch(deps))
				req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code)

				return jobID
			},
			expectedStatus: http.StatusBadRequest,
			validateFn: func(t *testing.T, body []byte, _ *ServerDependencies) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job is not in organized status", errResp.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db, fs := setupJobsTestDepsWithReverter(t)
			defer func() { _ = db.Close() }()

			jobID := tt.setupFn(t, deps, fs)

			router := gin.New()
			router.POST("/api/v1/jobs/:id/revert", revertBatch(deps))

			req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, w.Body.Bytes(), deps)
			}
		})
	}
}

func TestRevertOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		setupFn        func(*testing.T, *ServerDependencies, afero.Fs) (jobID string, movieID string)
		expectedStatus int
		validateFn     func(*testing.T, []byte)
	}{
		{
			name: "non-existent job",
			setupFn: func(t *testing.T, _ *ServerDependencies, _ afero.Fs) (string, string) {
				return "nonexistent-id", "ABC-001"
			},
			expectedStatus: http.StatusNotFound,
			validateFn: func(t *testing.T, body []byte) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job not found", errResp.Error)
			},
		},
		{
			name: "non-revertible status",
			setupFn: func(t *testing.T, deps *ServerDependencies, _ afero.Fs) (string, string) {
				job := createTestJob(t, deps, "pending")
				return job.ID, "ABC-001"
			},
			expectedStatus: http.StatusBadRequest,
			validateFn: func(t *testing.T, body []byte) {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job is not in a revertible status", errResp.Error)
			},
		},
		{
			name: "reverted status allows individual retry",
			setupFn: func(t *testing.T, deps *ServerDependencies, fs afero.Fs) (string, string) {
				// Create a reverted job with one failed operation (simulating partially-reverted batch)
				jobID := uuid.New().String()
				now := time.Now()
				job := &models.Job{
					ID:          jobID,
					Status:      string(models.JobStatusReverted),
					TotalFiles:  2,
					Completed:   2,
					Failed:      0,
					Progress:    1.0,
					Destination: "/dest",
					StartedAt:   now.Add(-2 * time.Hour),
					RevertedAt:  &now,
				}
				require.NoError(t, deps.JobRepo.Create(job))

				// Create a failed operation that can be retried
				dstPath := "/dest/RETRY-001/RETRY-001.mp4"
				require.NoError(t, fs.MkdirAll("/dest/RETRY-001", 0777))
				require.NoError(t, afero.WriteFile(fs, dstPath, []byte("content"), 0666))

				op := &models.BatchFileOperation{
					BatchJobID:    jobID,
					MovieID:       "RETRY-001",
					OriginalPath:  "/src/RETRY-001.mp4",
					NewPath:       dstPath,
					OperationType: models.OperationTypeMove,
					RevertStatus:  models.RevertStatusFailed,
				}
				require.NoError(t, deps.BatchFileOpRepo.Create(op))

				return jobID, "RETRY-001"
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var resp RevertResultResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, 1, resp.Total)
				assert.Equal(t, 1, resp.Succeeded)
			},
		},
		{
			name: "successful individual revert",
			setupFn: func(t *testing.T, deps *ServerDependencies, fs afero.Fs) (string, string) {
				jobID := seedRevertableJob(t, deps, fs, []string{"IND-001", "IND-002"})
				return jobID, "IND-001"
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var resp RevertResultResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, 1, resp.Total)
				assert.Equal(t, 1, resp.Succeeded)
				assert.Equal(t, 0, resp.Failed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db, fs := setupJobsTestDepsWithReverter(t)
			defer func() { _ = db.Close() }()

			jobID, movieID := tt.setupFn(t, deps, fs)

			router := gin.New()
			router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(deps))

			req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/"+movieID+"/revert", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, w.Body.Bytes())
			}
		})
	}
}
