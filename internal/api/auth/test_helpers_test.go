package auth

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/file"
	"github.com/javinizer/javinizer-go/internal/api/history"
	"github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/api/system"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	apiversion "github.com/javinizer/javinizer-go/internal/api/version"
	"github.com/javinizer/javinizer-go/internal/config"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	return testkit.CreateTestDeps(t, cfg, configFile)
}

func cleanupServerHub(t *testing.T, deps *core.ServerDependencies) {
	testkit.CleanupServerHub(t, deps)
}

func NewServer(deps *core.ServerDependencies) *gin.Engine {
	runtime := deps.EnsureRuntime()
	core.SetDefaultRuntimeState(runtime)
	runtime.ResetWebSocketHub()
	runtime.SetWebSocketUpgrader(websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	})

	router := gin.Default()
	system.RegisterCoreRoutes(router, deps)
	realtime.RegisterRoutes(router, deps, RequireAuthenticated(deps))

	v1 := router.Group("/api/v1")
	RegisterPublicRoutes(v1, deps)

	protected := v1.Group("")
	protected.Use(RequireAuthenticated(deps))
	movie.RegisterRoutes(protected, deps)
	actress.RegisterRoutes(protected, deps)
	system.RegisterRoutes(protected, deps)
	apiversion.RegisterRoutes(protected, deps)
	file.RegisterRoutes(protected, deps)
	batch.RegisterRoutes(protected, deps)
	history.RegisterRoutes(protected, deps)

	return router
}
