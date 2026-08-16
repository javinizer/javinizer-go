package actress

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncJobsError_CountTasksError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	_ = db.Close()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/nonexistent/tasks", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSyncJobsError_CancelJobGetError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	_ = db.Close()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/nonexistent/cancel", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSyncJobsError_CancelJobAfterCancelGetError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewBufferString(`{"scope":"missing"}`)))
	_ = w
	_ = db.Close()
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/any/cancel", nil))
	assert.True(t, w2.Code >= 400)
}
