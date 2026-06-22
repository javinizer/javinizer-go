package auth

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/events"
	"github.com/javinizer/javinizer-go/internal/api/file"
	"github.com/javinizer/javinizer-go/internal/api/genre"
	"github.com/javinizer/javinizer-go/internal/api/history"
	"github.com/javinizer/javinizer-go/internal/api/jobs"
	"github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/api/system"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/api/token"
	apiversion "github.com/javinizer/javinizer-go/internal/api/version"
	"github.com/javinizer/javinizer-go/internal/config"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.APIDeps {
	deps := testkit.CreateTestDeps(t, cfg, configFile)
	return deps
}

func cleanupServerHub(t *testing.T, deps *core.APIDeps) {
	testkit.CleanupServerHub(t, testkit.GetTestRuntime(deps))
}

func NewServer(deps *core.APIDeps) *gin.Engine {
	runtime := core.NewAPIRuntime(deps).EnsureRuntime()
	runtime.ResetWebSocketHub()
	runtime.SetWebSocketUpgrader(websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	})

	router := gin.Default()
	system.RegisterCoreRoutes(router, testkit.GetTestRuntime(deps))
	realtime.RegisterRoutes(router, testkit.GetTestRuntime(deps), RequireTokenOrSession(deps))

	v1 := router.Group("/api/v1")
	RegisterPublicRoutes(v1, testkit.GetTestRuntime(deps))

	protected := v1.Group("")
	protected.Use(RequireTokenOrSession(deps))

	actressDeps := actress.NewActressDeps(deps.Repos.ContentRepos, deps.Repos.TranslationRepos)
	genreDeps := genre.NewGenreDeps(deps.Repos.ReplacementRepos, deps.Repos.TranslationRepos)
	// History handlers call the repository directly — no intermediate service needed.
	jobsDeps := jobs.NewJobDeps(deps.Repos.JobRepo, deps.Repos.BatchFileOpRepo, deps.JobStore, deps.Reverter, deps.EventEmitter, testkit.GetTestRuntime(deps).GetAPIConfig().AllowRevert)
	movieDeps := movie.NewMovieDeps(deps.Repos.MovieRepo,
		movie.WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow),
		movie.WithAllowedDirs(testkit.GetTestRuntime(deps).GetAPIConfig().AllowedDirectories),
	)
	tokenSvc := token.NewTokenService(deps.Repos.ApiTokenRepo)

	movie.RegisterRoutes(protected, movieDeps)
	actress.RegisterRoutes(protected, actressDeps)
	genre.RegisterRoutes(protected, genreDeps, func() {})
	system.RegisterRoutes(protected, testkit.GetTestRuntime(deps))
	apiversion.RegisterRoutes(protected, deps)
	file.RegisterRoutes(protected, testkit.GetTestRuntime(deps))
	batch.RegisterRoutes(protected, testkit.GetTestRuntime(deps))
	history.RegisterRoutes(protected, deps.Repos.HistoryRepo, historypkg.NewLogger(deps.Repos.HistoryRepo))
	jobs.RegisterRoutes(protected, jobsDeps)
	events.RegisterRoutes(protected, deps.Repos.EventRepo)
	token.RegisterRoutes(protected, protected, tokenSvc)

	return router
}
