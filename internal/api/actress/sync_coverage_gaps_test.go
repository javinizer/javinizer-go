package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncGaps_CandidatesOffsetValidation(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates?offset=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates?offset=-1", nil))
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestSyncGaps_CancelJobRepoError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	require.NoError(t, db.Close())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/missing/cancel", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "database is closed")
}

func TestSyncGaps_ListTasksRepoError(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	require.NoError(t, db.Close())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/missing/tasks", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "database is closed")
}

func TestSyncGaps_WriteActressSyncErrorSentinelPaths(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	writeActressSyncError(c, worker.ErrActressSyncManagerUnavailable)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	writeActressSyncError(c2, database.ErrAlreadyTerminal)
	assert.Equal(t, http.StatusConflict, w2.Code)

	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	writeActressSyncError(c3, database.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, w3.Code)
}

func TestSyncGaps_CancelTerminalConflict(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := &models.ActressSyncJob{ID: "terminal-gap", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/"+job.ID+"/cancel", nil))
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSyncGaps_NoCandidatesBodyShape(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "missing"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	require.Equal(t, http.StatusConflict, w.Code)
	var resp actressSyncNoCandidatesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "no_candidates", resp.Error)
	assert.NotNil(t, resp.SkippedIDs)
}
