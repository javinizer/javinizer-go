package jobs

import (
	"context"
	"encoding/json"
	"net/http"
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

func TestGetJob_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)

	// Create a job with various timestamps
	now := time.Now()
	jobID := uuid.New().String()
	job := &models.Job{
		ID:          jobID,
		Status:      models.JobStatusCompleted,
		TotalFiles:  5,
		Completed:   4,
		Failed:      1,
		Progress:    0.8,
		Destination: "/dest/test",
		StartedAt:   now,
		CompletedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	router := gin.New()
	router.GET("/api/v1/jobs/:id", getJob(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.ID)
	assert.Equal(t, models.JobStatusCompleted, resp.Status)
	assert.Equal(t, 5, resp.TotalFiles)
}

func TestGetJob_NotFound_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)

	router := gin.New()
	router.GET("/api/v1/jobs/:id", getJob(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListJobs_StatusFilter_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)

	// Create jobs with different statuses
	job1 := &models.Job{
		ID:        uuid.New().String(),
		Status:    models.JobStatusCompleted,
		StartedAt: time.Now(),
	}
	job2 := &models.Job{
		ID:        uuid.New().String(),
		Status:    models.JobStatusOrganized,
		StartedAt: time.Now(),
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job1))
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job2))

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	// Filter by completed
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?status=completed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Jobs, 1)
	assert.Equal(t, models.JobStatusCompleted, resp.Jobs[0].Status)
}
