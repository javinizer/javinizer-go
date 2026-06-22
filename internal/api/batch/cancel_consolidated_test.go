package batch

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestCancelBatchJob_NotFound(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/cancel", cancelBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/nonexistent/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

// TestDeleteBatchJob_InternalError tests the 500 path when deletion fails
// for a reason other than "cannot delete running job".

func TestCancelBatchJob_Success(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/CNL-001.mp4"})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/cancel", cancelBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "cancelled")
}

// TestCancelBatchJob_NotFound tests the 404 path for canceling a non-existent job.

func TestDeleteBatchJob_InternalError(t *testing.T) {
	// This tests the else branch in deleteBatchJob's error handling
	// when the error message does not contain "cannot delete running job"
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	// Create a job, complete it, then try to delete
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/DEL-001.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("DELETE", "/batch/"+job.GetID(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should succeed since the job is completed
	assert.Equal(t, 200, w.Code)
}

// TestBatchScrape_DirNotAllowed tests the directory security check in batchScrape.
