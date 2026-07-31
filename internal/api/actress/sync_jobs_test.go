package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSyncTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	repos := db.Repositories()
	registry := scraperutil.NewScraperRegistry()
	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry, DB: db},
		Repos:    repos,
	}
	rt := core.NewAPIRuntime(deps)
	router := gin.New()
	router.POST("/actresses/sync-jobs", createActressSyncJob(rt))
	router.GET("/actresses/sync-jobs/active", listActiveActressSyncJobs(rt))
	router.GET("/actresses/sync-jobs/:jobID", getActressSyncJob(rt))
	router.GET("/actresses/sync-jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	router.POST("/actresses/sync-jobs/:jobID/cancel", cancelActressSyncJob(rt))
	return router
}

func TestCreateActressSyncJob_InvalidScope(t *testing.T) {
	router := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "invalid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_SelectedWithoutIDs(t *testing.T) {
	router := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "selected"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_MissingNoCandidates(t *testing.T) {
	router := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "missing"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_SelectedNotFound(t *testing.T) {
	router := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "selected", "actress_ids": []uint{99999}})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_InvalidJSON(t *testing.T) {
	router := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader([]byte("not json"))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActiveActressSyncJobs_Empty(t *testing.T) {
	router := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/active", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetActressSyncJob_NotFound(t *testing.T) {
	router := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/nonexistent", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListActressSyncJobTasks_NotFound(t *testing.T) {
	router := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/nonexistent/tasks", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancelActressSyncJob_NotFound(t *testing.T) {
	router := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/nonexistent/cancel", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestIsActressSyncValidationError(t *testing.T) {
	assert.False(t, isActressSyncValidationError(nil))
	assert.True(t, isActressSyncValidationError(anonErr("scope must be missing or selected")))
	assert.True(t, isActressSyncValidationError(anonErr("actress_ids is required for selected scope")))
	assert.True(t, isActressSyncValidationError(anonErr("no actresses require metadata sync")))
	assert.False(t, isActressSyncValidationError(anonErr("some other error")))
}

type anonErr string

func (e anonErr) Error() string { return string(e) }
