package server

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"golang.org/x/time/rate"

	swaggerPkg "github.com/javinizer/javinizer-go/docs/swagger"
	"github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/desktop"
	"github.com/javinizer/javinizer-go/internal/api/events"
	"github.com/javinizer/javinizer-go/internal/api/file"
	"github.com/javinizer/javinizer-go/internal/api/genre"
	"github.com/javinizer/javinizer-go/internal/api/history"
	"github.com/javinizer/javinizer-go/internal/api/jobs"
	"github.com/javinizer/javinizer-go/internal/api/middleware"
	"github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/api/system"
	"github.com/javinizer/javinizer-go/internal/api/temp"
	"github.com/javinizer/javinizer-go/internal/api/token"
	apiversion "github.com/javinizer/javinizer-go/internal/api/version"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"

	webui "github.com/javinizer/javinizer-go/web"
)

type webUIAssets struct {
	distFS      fs.FS
	staticFS    http.FileSystem
	indexHTML   []byte
	uiAvailable bool
}

func loadWebUIAssets() webUIAssets {
	assets := webUIAssets{}

	distFS, distErr := webui.DistFS()
	if distErr != nil {
		logging.Warnf("Web UI assets unavailable: %v", distErr)
		return assets
	}

	assets.distFS = distFS
	assets.staticFS = http.FS(distFS)

	if _, err := fs.Stat(distFS, "index.html"); err != nil {
		logging.Warnf("Web UI index.html not found in embedded assets: %v", err)
		return assets
	}

	indexBytes, readErr := fs.ReadFile(distFS, "index.html")
	if readErr != nil {
		logging.Warnf("Failed to read embedded Web UI index.html: %v", readErr)
		return assets
	}

	assets.indexHTML = indexBytes
	assets.uiAvailable = true
	return assets
}

func registerCORSMiddleware(router *gin.Engine, rt *core.APIRuntime) {
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		allowedOrigins := rt.GetAPIConfig().AllowedOrigins

		if origin != "" && isSameOrigin(origin, c.Request) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Add("Vary", "Origin")
		} else if len(allowedOrigins) > 0 && isOriginAllowed(origin, allowedOrigins) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Add("Vary", "Origin")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		allowedHeaders := []string{"Content-Type", "Authorization", "Accept", "Origin", "X-Requested-With", "X-Session-ID"}
		c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
}

func registerDocumentationRoutes(router *gin.Engine) {
	openAPIHandler := func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Data(http.StatusOK, "application/json", swaggerPkg.SwaggerJSON())
	}
	router.GET("/docs/openapi.json", openAPIHandler)
	router.HEAD("/docs/openapi.json", openAPIHandler)
	router.GET("/docs", serveScalarDocs)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

func registerCoreRoutes(router *gin.Engine, rt *core.APIRuntime) {
	system.RegisterCoreRoutes(router, rt)
	rt.EnsureRuntime()
	realtime.RegisterRoutes(router, rt, auth.RequireTokenOrSession(rt.Deps()))
}

func registerAPIV1Routes(router *gin.Engine, rt *core.APIRuntime) {
	deps := rt.Deps()

	v1 := router.Group("/api/v1")
	auth.RegisterPublicRoutes(v1, rt)

	protected := v1.Group("")
	protected.Use(auth.RequireTokenOrSession(deps))
	protected.Use(middleware.InstallEnvironmentInjector(deps.CoreDeps))

	var rateLimiter *middleware.IPRateLimiter
	apiCfg := rt.GetAPIConfig()
	secCfg := apiCfg.SecurityConfig()
	if rpm := apiCfg.RateLimitRPM; rpm > 0 {
		rateLimiter = middleware.NewIPRateLimiter(rate.Every(time.Minute/time.Duration(rpm)), rpm)
	}

	writeProtected := protected.Group("")
	writeProtected.Use(middleware.RateLimitMiddleware(rateLimiter))

	actressDeps := actress.NewActressDeps(deps.Repos.ContentRepos, deps.Repos.TranslationRepos)
	genreDeps := genre.NewGenreDeps(deps.Repos.ReplacementRepos, deps.Repos.TranslationRepos)
	genreInvalidateCaches := core.InvalidateWorkflowCachesOnRuntime(rt)
	// History handlers call the repository directly — no intermediate service needed.
	jobsDeps := jobs.NewJobDeps(deps.Repos.JobRepo, deps.Repos.BatchFileOpRepo, deps.JobStore, deps.Reverter, deps.EventEmitter, apiCfg.AllowRevert)
	// Per W-3: the factory must always be available when the API layer runs
	// (API server starts after bootstrapping). If PosterGen is nil, pass nil
	// rather than constructing a new one outside the factory.
	posterGenForMovie := rt.GetPosterGen()
	if posterGenForMovie == nil {
		logging.Error("registerAPIV1Routes: workflow factory PosterGen is nil — poster generation will be unavailable")
	}
	movieDeps := movie.NewMovieDeps(deps.Repos.MovieRepo,
		movie.WithWorkflow(rt.GetWorkflow),
		movie.WithAllowedDirs(secCfg.AllowedDirectories),
		movie.WithPosterGen(posterGenForMovie),
	)
	tokenSvc := token.NewTokenService(deps.Repos.ApiTokenRepo)

	movie.RegisterRoutes(writeProtected, movieDeps)
	actress.RegisterRoutes(protected, actressDeps)
	genre.RegisterRoutes(writeProtected, genreDeps, genreInvalidateCaches)
	system.RegisterRoutes(protected, rt)
	apiversion.RegisterRoutes(protected, deps)
	desktop.RegisterRoutes(protected, deps)
	file.RegisterRoutes(writeProtected, rt)
	batch.RegisterRoutes(writeProtected, rt)
	historyLogger := historypkg.NewLogger(deps.Repos.HistoryRepo)
	history.RegisterRoutes(protected, deps.Repos.HistoryRepo, historyLogger)
	jobs.RegisterRoutes(protected, jobsDeps)
	events.RegisterRoutes(protected, deps.Repos.EventRepo)
	temp.RegisterRoutes(protected, rt)
	token.RegisterRoutes(protected, writeProtected, tokenSvc)
}

func registerStaticWebRoutes(router *gin.Engine, assets webUIAssets) {
	if !assets.uiAvailable {
		return
	}

	if appFS, err := fs.Sub(assets.distFS, "_app"); err == nil {
		router.StaticFS("/_app", http.FS(appFS))
	} else {
		logging.Warnf("Web UI _app assets unavailable: %v", err)
	}

	if _, err := fs.Stat(assets.distFS, "favicon.ico"); err == nil {
		router.GET("/favicon.ico", func(c *gin.Context) { c.FileFromFS("favicon.ico", assets.staticFS) })
	}

	if _, err := fs.Stat(assets.distFS, "robots.txt"); err == nil {
		router.GET("/robots.txt", func(c *gin.Context) { c.FileFromFS("robots.txt", assets.staticFS) })
	}
}

func registerNoRouteHandler(router *gin.Engine, assets webUIAssets) {
	router.NoRoute(func(c *gin.Context) {
		logging.Debugf("NoRoute hit: %s %s (Accept: %s)", c.Request.Method, c.Request.URL.Path, c.Request.Header.Get("Accept"))

		method := c.Request.Method
		if assets.uiAvailable && acceptsHTML(c) {
			if method == http.MethodHead {
				c.Status(http.StatusNoContent)
				return
			}
			if method == http.MethodGet {
				c.Data(http.StatusOK, "text/html; charset=utf-8", assets.indexHTML)
				return
			}
		}

		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Not Found",
			"message": "The requested resource does not exist",
			"path":    c.Request.URL.Path,
			"method":  c.Request.Method,
		})
	})
}

func logRegisteredRoutes(router *gin.Engine) {
	logging.Debugf("Registered routes:")
	for _, route := range router.Routes() {
		logging.Debugf("  %s %s", route.Method, route.Path)
	}
}
