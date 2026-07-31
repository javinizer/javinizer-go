package actress

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActressSyncCandidates(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(nil))
	t.Cleanup(func() { _ = db.Close() })
	repos := db.Repositories()
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{DMMID: 42, FirstName: "Needs", LastName: "Metadata"}))
	registry := scraperutil.NewScraperRegistry()
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry, DB: db}, Repos: repos}
	rt := core.NewAPIRuntime(deps)
	router := gin.New()
	router.GET("/actresses/sync-candidates", listActressSyncCandidates(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "42")
}

func TestRegisterRoutes(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(nil))
	t.Cleanup(func() { _ = db.Close() })
	repos := db.Repositories()
	registry := scraperutil.NewScraperRegistry()
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry, DB: db}, Repos: repos}
	rt := core.NewAPIRuntime(deps)
	router := gin.New()
	group := router.Group("/api/v1")
	RegisterRoutes(group, ActressDeps{ContentRepos: repos.ContentRepos}, rt)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/actresses/sync-candidates", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
