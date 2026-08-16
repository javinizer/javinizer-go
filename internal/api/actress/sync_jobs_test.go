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
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSyncTestRouter(t *testing.T) (*gin.Engine, *database.DB) {
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
	rt.SetConfig(config.DefaultConfig(nil, nil))
	rt.Runtime = core.NewRuntimeState()
	router := gin.New()
	router.GET("/actresses/sync-candidates", listActressSyncCandidates(rt))
	router.POST("/actresses/sync-jobs", createActressSyncJob(rt))
	router.GET("/actresses/sync-jobs/active", listActiveActressSyncJobs(rt))
	router.GET("/actresses/sync-jobs/:jobID", getActressSyncJob(rt))
	router.GET("/actresses/sync-jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	router.POST("/actresses/sync-jobs/:jobID/cancel", cancelActressSyncJob(rt))
	return router, db
}

func TestCreateActressSyncJob_InvalidScope(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "invalid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_SelectedWithoutIDs(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "selected"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActressSyncJob_MissingNoCandidates(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "missing"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusConflict, w.Code)
	var resp actressSyncNoCandidatesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "no_candidates", resp.Error)
}

func TestCreateActressSyncJob_SelectedNotFound(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	body, _ := json.Marshal(map[string]any{"scope": "selected", "actress_ids": []uint{99999}})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateActressSyncJob_InvalidJSON(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader([]byte("not json"))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActiveActressSyncJobs_Empty(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/active", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetActressSyncJob_NotFound(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/nonexistent", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListActressSyncJobTasks_LimitParam(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	actress := &models.Actress{JapaneseName: "花子"}
	require.NoError(t, db.Repositories().ActressRepo.Create(context.Background(), actress))
	job := models.ActressSyncJob{ID: "job-limit", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	for _, id := range []string{"t-1", "t-2", "t-3"} {
		require.NoError(t, db.Create(&models.ActressSyncTask{
			ID: id, JobID: job.ID, Label: id, DedupeKey: id,
			Status: models.ActressSyncTaskPending, Stage: "queued",
			Messages: []string{}, UpdatedFields: []string{}, ActressID: &actress.ID,
		}).Error)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?limit=2", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Tasks []json.RawMessage `json:"tasks"`
		Total int               `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Tasks, 2, "limit narrows the page")
	require.Equal(t, 3, resp.Total, "total must be the real job task count, not the page size")
}

func TestListActressSyncJobTasks_NotFound(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/nonexistent/tasks", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancelActressSyncJob_NotFound(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/nonexistent/cancel", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
