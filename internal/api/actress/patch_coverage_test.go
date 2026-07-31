package actress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestListActressesFilteredPaths(t *testing.T) {
	_, repo, _ := setupActressTestDB(t)
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 1, FirstName: "Complete", JapaneseName: "完全", ThumbURL: "https://example.com/1.jpg"}))
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 2, FirstName: "Missing"}))
	router := gin.New()
	router.GET("/actresses", listActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))

	for _, url := range []string{
		"/actresses?filter=missing_metadata",
		"/actresses?filter=missing_metadata&q=Missing",
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var response actressesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, int64(1), response.Total)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses?filter=bogus", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSyncJobHandlerLifecycle(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	repos := db.Repositories()
	actress := &models.Actress{DMMID: 77, FirstName: "Lifecycle"}
	require.NoError(t, repos.ActressRepo.Create(t.Context(), actress))
	registry := scraperutil.NewScraperRegistry()
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: registry})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: repos})
	t.Cleanup(rt.Shutdown)
	router := gin.New()
	router.POST("/jobs", createActressSyncJob(rt))
	router.GET("/jobs/active", listActiveActressSyncJobs(rt))
	router.GET("/jobs/:jobID", getActressSyncJob(rt))
	router.GET("/jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	router.POST("/jobs/:jobID/cancel", cancelActressSyncJob(rt))

	create := httptest.NewRecorder()
	body := strings.NewReader(fmt.Sprintf(`{"scope":"selected","actress_ids":[%d]}`, actress.ID))
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/jobs", body))
	require.Equal(t, http.StatusAccepted, create.Code, create.Body.String())
	var created actressSyncJobResponse
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &created))
	require.NotEmpty(t, created.Job.ID)

	for _, path := range []string{"/jobs/active", "/jobs/" + created.Job.ID, "/jobs/" + created.Job.ID + "/tasks"} {
		assert.Eventually(t, func() bool {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			return w.Code == http.StatusOK
		}, time.Second, 10*time.Millisecond, path)
	}
	assert.Eventually(t, func() bool {
		cancel := httptest.NewRecorder()
		router.ServeHTTP(cancel, httptest.NewRequest(http.MethodPost, "/jobs/"+created.Job.ID+"/cancel", nil))
		return cancel.Code == http.StatusOK
	}, time.Second, 10*time.Millisecond)
}

func TestSyncHandlersReportRepositoryErrors(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	registry := scraperutil.NewScraperRegistry()
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: registry})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: repos})
	rt.EnsureActressSyncManager()
	t.Cleanup(rt.Shutdown)
	require.NoError(t, db.Close())

	router := gin.New()
	router.GET("/candidates", listActressSyncCandidates(rt))
	router.POST("/jobs", createActressSyncJob(rt))
	router.GET("/jobs/active", listActiveActressSyncJobs(rt))
	router.GET("/jobs/:jobID", getActressSyncJob(rt))
	router.GET("/jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	router.POST("/jobs/:jobID/cancel", cancelActressSyncJob(rt))
	requests := []struct{ method, path, body string }{
		{http.MethodGet, "/candidates", ""},
		{http.MethodPost, "/jobs", `{"scope":"missing"}`},
		{http.MethodGet, "/jobs/active", ""},
		{http.MethodGet, "/jobs/id", ""},
		{http.MethodGet, "/jobs/id/tasks", ""},
		{http.MethodPost, "/jobs/id/cancel", ""},
	}
	for _, request := range requests {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(request.method, request.path, strings.NewReader(request.body)))
		assert.Equal(t, http.StatusInternalServerError, w.Code, "%s %s: %s", request.method, request.path, w.Body.String())
	}
}

func TestSyncHandlersReportUnavailableManager(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	router := gin.New()
	router.GET("/candidates", listActressSyncCandidates(rt))
	router.POST("/jobs", createActressSyncJob(rt))
	router.GET("/jobs/active", listActiveActressSyncJobs(rt))
	router.GET("/jobs/:jobID", getActressSyncJob(rt))
	router.GET("/jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	router.POST("/jobs/:jobID/cancel", cancelActressSyncJob(rt))

	requests := []struct{ method, path, body string }{
		{http.MethodGet, "/candidates", ""},
		{http.MethodPost, "/jobs", `{"scope":"missing"}`},
		{http.MethodGet, "/jobs/active", ""},
		{http.MethodGet, "/jobs/id", ""},
		{http.MethodGet, "/jobs/id/tasks", ""},
		{http.MethodPost, "/jobs/id/cancel", ""},
	}
	for _, request := range requests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code, "%s %s: %s", request.method, request.path, w.Body.String())
	}
}
