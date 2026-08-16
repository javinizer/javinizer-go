package actress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemainingGaps_CandidatesOffsetNonNumeric(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: db.Repositories()})
	t.Cleanup(rt.Shutdown)
	router := gin.New()
	router.GET("/candidates", listActressSyncCandidates(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/candidates?offset=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/candidates?offset=-1", nil))
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestRemainingGaps_ListTasksCountError(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: db.Repositories()})
	t.Cleanup(rt.Shutdown)
	require.NoError(t, db.Close())
	router := gin.New()
	router.GET("/jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/jobs/missing/tasks", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "database is closed")
}

func TestRemainingGaps_CancelJobGetError(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	coreDeps, err := commandutil.NewDependenciesWithOptions(config.DefaultConfig(nil, nil), &commandutil.DependenciesOptions{DB: db, ScraperRegistry: scraperutil.NewScraperRegistry()})
	require.NoError(t, err)
	rt := core.NewAPIRuntime(&core.APIDeps{CoreDeps: coreDeps, Repos: db.Repositories()})
	t.Cleanup(rt.Shutdown)
	require.NoError(t, db.Close())
	router := gin.New()
	router.POST("/jobs/:jobID/cancel", cancelActressSyncJob(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/jobs/missing/cancel", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "database is closed")
}
