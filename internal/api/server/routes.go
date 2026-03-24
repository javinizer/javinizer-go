package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/file"
	"github.com/javinizer/javinizer-go/internal/api/history"
	"github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/api/system"
	"github.com/javinizer/javinizer-go/internal/api/temp"
	apiversion "github.com/javinizer/javinizer-go/internal/api/version"
	"github.com/javinizer/javinizer-go/internal/logging"
	webui "github.com/javinizer/javinizer-go/web"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
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

func registerCORSMiddleware(router *gin.Engine, deps *core.ServerDependencies) {
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		allowedOrigins := deps.GetConfig().API.Security.AllowedOrigins

		if len(allowedOrigins) == 0 {
			if isSameOrigin(origin, c.Request) {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Add("Vary", "Origin")
			}
		} else if isOriginAllowed(origin, allowedOrigins) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Add("Vary", "Origin")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		allowedHeaders := []string{"Content-Type", "Authorization", "Accept", "Origin", "X-Requested-With"}
		c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
}

func registerDocumentationRoutes(router *gin.Engine) {
	router.StaticFile("/docs/openapi.json", resolveSwaggerPath())
	router.GET("/docs", serveScalarDocs)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

func registerCoreRoutes(router *gin.Engine, deps *core.ServerDependencies) {
	system.RegisterCoreRoutes(router, deps)
	realtime.RegisterRoutes(router, deps, auth.RequireAuthenticated(deps))
}

func registerAPIV1Routes(router *gin.Engine, deps *core.ServerDependencies) {
	v1 := router.Group("/api/v1")
	auth.RegisterPublicRoutes(v1, deps)

	protected := v1.Group("")
	protected.Use(auth.RequireAuthenticated(deps))

	movie.RegisterRoutes(protected, deps)
	actress.RegisterRoutes(protected, deps)
	system.RegisterRoutes(protected, deps)
	apiversion.RegisterRoutes(protected, deps)
	file.RegisterRoutes(protected, deps)
	batch.RegisterRoutes(protected, deps)
	history.RegisterRoutes(protected, deps)
	temp.RegisterRoutes(protected, deps)
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
