package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"

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

func TestCancelActressSyncJob_TerminalReturns409(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := &models.ActressSyncJob{ID: "terminal-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(job).Error)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs/"+job.ID+"/cancel", nil))
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.NotContains(t, w.Body.String(), "terminal-job")
}

func TestCreateActressSyncJob_StoppedManagerReturns503(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	r := gin.New()
	r.POST("/jobs", createActressSyncJob(rt))
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"scope": "missing"})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCreateActressSyncJob_SuccessHasSkippedIDsKey(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	actress := &models.Actress{DMMID: 12345, JapaneseName: "\xe3\x83\x86\xe3\x82\xb9\xe3\x83\x88"}
	require.NoError(t, db.Repositories().ActressRepo.Create(nil, actress))
	body, _ := json.Marshal(map[string]any{"scope": "selected", "actress_ids": []uint{actress.ID}})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/actresses/sync-jobs", bytes.NewReader(body)))
	require.Equal(t, http.StatusAccepted, w.Code)
	assert.Contains(t, w.Body.String(), "skipped_ids")
}

func TestListActressSyncJobTasks_Sanitized500Body(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	require.NoError(t, db.Close())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/any/tasks", nil))
	assert.NotContains(t, w.Body.String(), "database is closed")
	assert.NotContains(t, w.Body.String(), "sql: database is closed")
}
func TestCreateActressSyncJob_Sentinel503AfterManagerShutdown(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: db.Repositories()})
	t.Cleanup(rt.Shutdown)
	manager := rt.EnsureActressSyncManager()
	require.NotNil(t, manager)
	manager.Shutdown()
	body, _ := json.Marshal(map[string]any{"scope": "missing"})
	router := gin.New()
	router.POST("/jobs", createActressSyncJob(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body)))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
func TestListActressSyncJobTasks_BadLimitOnNamedViews(t *testing.T) {
	router, db := setupSyncTestRouter(t)
	job := &models.ActressSyncJob{ID: "named-view-job", Status: models.ActressSyncJobCompleted, Scope: "missing"}
	require.NoError(t, db.Create(job).Error)
	for _, view := range []string{"active", "diagnostics"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-jobs/"+job.ID+"/tasks?view="+view+"&limit=abc", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, "view=%s with bad limit", view)
	}
}
