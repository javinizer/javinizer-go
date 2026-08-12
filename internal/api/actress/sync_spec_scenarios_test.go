package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateActressSyncJob_OversizedSelectedScope(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	ids := make([]uint, 10001)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	body, _ := json.Marshal(map[string]any{"scope": "selected", "actress_ids": ids})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "10000")
}

func TestListActressSyncJobTasks_UnknownView(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := models.ActressSyncJob{ID: "view-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?view=garbage", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActressSyncJobTasks_NonNumericLimit(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := models.ActressSyncJob{ID: "limit-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?limit=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActressSyncJobTasks_ZeroLimit(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := models.ActressSyncJob{ID: "zero-limit-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?limit=0", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActressSyncJobTasks_OversizedLimitClamps(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := models.ActressSyncJob{ID: "clamp-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?limit=2000", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCreateActressSyncJob_NoCandidatesHasSkippedIDs(t *testing.T) {
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

func TestListActressSyncCandidates_UnknownFilter(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates?filter=nonexistent", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActressSyncCandidates_SlimShape(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "items")
	assert.Contains(t, body, "total")
	assert.Contains(t, body, "limit")
	assert.Contains(t, body, "offset")
	assert.NotContains(t, body, "ids")
}
