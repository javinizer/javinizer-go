package actress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func primeSyncTestManager(t *testing.T, router *gin.Engine) {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/active", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestListActressSyncJobTasks_CountTasksError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	primeSyncTestManager(t, router)

	job := models.ActressSyncJob{ID: "count-error-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.DB.Create(&job).Error)

	callbackName := "test:fail-count-tasks:" + t.Name()
	require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actress_sync_tasks" {
			return
		}
		if _, isCount := tx.Statement.Dest.(*int64); isCount {
			_ = tx.AddError(errors.New("forced CountTasks failure"))
		}
	}))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestCancelActressSyncJob_CancelJobError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	primeSyncTestManager(t, router)

	job := models.ActressSyncJob{ID: "cancel-error-job", Status: models.ActressSyncJobPending, Scope: "missing"}
	require.NoError(t, db.DB.Create(&job).Error)

	callbackName := "test:fail-cancel-job:" + t.Name()
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actress_sync_jobs" {
			_ = tx.AddError(errors.New("forced CancelJob failure"))
		}
	}))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/"+job.ID+"/cancel", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestCancelActressSyncJob_GetJobAfterCancelError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	primeSyncTestManager(t, router)

	job := models.ActressSyncJob{ID: "post-cancel-get-error-job", Status: models.ActressSyncJobPending, Scope: "missing"}
	require.NoError(t, db.DB.Create(&job).Error)

	var jobReads atomic.Int32
	callbackName := "test:fail-second-get-job:" + t.Name()
	require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actress_sync_jobs" {
			return
		}
		if _, isFindJob := tx.Statement.Dest.(*models.ActressSyncJob); !isFindJob {
			return
		}
		if jobReads.Add(1) == 3 {
			_ = tx.AddError(errors.New("forced post-cancel GetJob failure"))
		}
	}))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/"+job.ID+"/cancel", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
