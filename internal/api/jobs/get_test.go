package jobs

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

func TestGetJob(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		setupFn        func(*testing.T, *core.APIDeps) string // returns job ID
		expectedStatus int
		validateFn     func(*testing.T, []byte)
	}{
		{
			name: "valid job returns item",
			setupFn: func(t *testing.T, deps *core.APIDeps) string {
				return seedJobsData(t, deps) // returns organized job ID
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var item contracts.JobListItem
				require.NoError(t, json.Unmarshal(body, &item))
				assert.NotEmpty(t, item.ID, "id should be populated")
				assert.Equal(t, models.JobStatusOrganized, item.Status)
				assert.Equal(t, int64(3), item.OperationCount, "organized job should have 3 operations")
				assert.Equal(t, int64(1), item.RevertedCount, "organized job should have 1 reverted operation")
				assert.NotEmpty(t, item.StartedAt, "started_at should be populated")
			},
		},
		{
			name: "non-existent job returns 404",
			setupFn: func(t *testing.T, _ *core.APIDeps) string {
				return "nonexistent-id"
			},
			expectedStatus: http.StatusNotFound,
			validateFn: func(t *testing.T, body []byte) {
				var errResp contracts.ErrorResponse
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Equal(t, "Job not found", errResp.Error)
			},
		},
		{
			name: "job with no operations",
			setupFn: func(t *testing.T, deps *core.APIDeps) string {
				job := createTestJob(t, deps, models.JobStatusCompleted)
				return job.ID
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var item contracts.JobListItem
				require.NoError(t, json.Unmarshal(body, &item))
				assert.Equal(t, int64(0), item.OperationCount)
				assert.Equal(t, int64(0), item.RevertedCount)
				assert.Equal(t, models.JobStatusCompleted, item.Status)
			},
		},
		{
			name: "reverted job includes reverted_at",
			setupFn: func(t *testing.T, deps *core.APIDeps) string {
				now := time.Now()
				job := &models.Job{
					ID:          uuid.New().String(),
					Status:      models.JobStatusReverted,
					TotalFiles:  2,
					Completed:   2,
					Failed:      0,
					Progress:    1.0,
					Destination: "/dest/reverted",
					StartedAt:   now.Add(-2 * time.Hour),
					RevertedAt:  &now,
				}
				require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))
				return job.ID
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var item contracts.JobListItem
				require.NoError(t, json.Unmarshal(body, &item))
				assert.Equal(t, models.JobStatusReverted, item.Status)
				assert.NotNil(t, item.RevertedAt)
			},
		},
		{
			name: "organized job includes organized_at",
			setupFn: func(t *testing.T, deps *core.APIDeps) string {
				return seedJobsData(t, deps)
			},
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, body []byte) {
				var item contracts.JobListItem
				require.NoError(t, json.Unmarshal(body, &item))
				assert.Equal(t, models.JobStatusOrganized, item.Status)
				assert.NotNil(t, item.OrganizedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupJobsTestDeps(t)
			defer func() { _ = db.Close() }()

			jobID := tt.setupFn(t, deps)

			router := gin.New()
			router.GET("/api/v1/jobs/:id", getJob(newTestJobDeps(deps)))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, w.Body.Bytes())
			}
		})
	}
}
