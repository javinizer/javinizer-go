package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var (
	wsHub         *ws.Hub
	wsHubCancel   context.CancelFunc // Track cancel function to clean up old hubs
	wsUpgrader    websocket.Upgrader
	wsHubShutdown chan struct{} // Signal when hub goroutine exits
)

// ServerDependencies holds all dependencies needed to create the API server
// Access to Config, Registry, Aggregator, and Matcher must be synchronized
// to prevent data races during config reload.
type ServerDependencies struct {
	mu          sync.RWMutex                  // Protects Registry, Aggregator, Matcher during reload
	config      atomic.Pointer[config.Config] // Thread-safe config access
	ConfigFile  string
	Registry    *models.ScraperRegistry
	DB          *database.DB
	Aggregator  *aggregator.Aggregator
	MovieRepo   *database.MovieRepository
	ActressRepo *database.ActressRepository
	HistoryRepo *database.HistoryRepository
	Matcher     *matcher.Matcher
	JobQueue    *worker.JobQueue
	wsCancel    context.CancelFunc // Cancel function for WebSocket hub context
}

// GetConfig returns the current configuration (thread-safe)
func (d *ServerDependencies) GetConfig() *config.Config {
	cfg := d.config.Load()
	if cfg == nil {
		logging.Errorf("CRITICAL: GetConfig() called before SetConfig() - this is a programming error")
		panic("GetConfig() called with nil config - ensure SetConfig() is called during ServerDependencies initialization")
	}
	return cfg
}

// SetConfig atomically sets the configuration (thread-safe)
func (d *ServerDependencies) SetConfig(cfg *config.Config) {
	if cfg == nil {
		logging.Errorf("CRITICAL: SetConfig() called with nil config - this is a programming error")
		panic("SetConfig() called with nil config - config must not be nil")
	}
	d.config.Store(cfg)
}

// Shutdown gracefully shuts down server resources, including the WebSocket hub
func (d *ServerDependencies) Shutdown() {
	if d.wsCancel != nil {
		d.wsCancel()
	}
}

// GetRegistry returns the current scraper registry (thread-safe)
func (d *ServerDependencies) GetRegistry() *models.ScraperRegistry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Registry
}

// GetAggregator returns the current aggregator (thread-safe)
func (d *ServerDependencies) GetAggregator() *aggregator.Aggregator {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Aggregator
}

// GetMatcher returns the current matcher (thread-safe)
func (d *ServerDependencies) GetMatcher() *matcher.Matcher {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Matcher
}

// resolveSwaggerPath returns the path to swagger.json, checking multiple locations
// Returns Docker path first (/app/docs/swagger/swagger.json), then falls back to local dev path
func resolveSwaggerPath() string {
	// Try Docker path first (production deployment)
	dockerPath := "/app/docs/swagger/swagger.json"
	if _, err := os.Stat(dockerPath); err == nil {
		return dockerPath
	}

	// Fall back to local development path
	return "./docs/swagger/swagger.json"
}

// isSameOrigin checks if the origin matches the request host (same-origin)
func isSameOrigin(origin string, r *http.Request) bool {
	if origin == "" {
		// No Origin header (e.g., some non-browser clients) - treat as same-origin
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return u.Host == r.Host
}

// isOriginAllowed checks if an origin is allowed based on configuration
// Note: This does NOT handle same-origin checking - use isSameOrigin for that
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	// Check each allowed origin
	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

// acceptsHTML checks if the request Accept header includes text/html with q>0
// Used to distinguish browser requests from API clients
// Properly parses Accept header to respect quality values (q-values) per RFC 9110
func acceptsHTML(c *gin.Context) bool {
	accept := c.GetHeader("Accept")
	if accept == "" {
		return false
	}

	// Parse Accept header: split by comma and check each media type
	parts := strings.Split(accept, ",")
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))

		// Extract media type and parameters
		segments := strings.Split(part, ";")
		if len(segments) == 0 {
			continue
		}

		mediaType := strings.TrimSpace(segments[0])

		// Check if this is text/html
		if mediaType == "text/html" {
			// Parse parameters to find q-value
			qValue := 1.0 // Default quality is 1.0 if not specified

			for i := 1; i < len(segments); i++ {
				param := strings.TrimSpace(segments[i])
				if strings.HasPrefix(param, "q=") {
					// Extract q-value
					qStr := strings.TrimPrefix(param, "q=")
					qStr = strings.TrimSpace(qStr)
					// Simple parsing: check if it starts with "0" or "0."
					if qStr == "0" || qStr == "0.0" || qStr == "0.00" || qStr == "0.000" {
						qValue = 0.0
						break
					}
					// Any other value means q > 0
					break
				}
			}

			// Only accept if q > 0
			if qValue > 0 {
				return true
			}
			// If q=0, continue checking other media types
		}
	}

	return false
}

// NewServer creates and configures the Gin router with all API endpoints
func NewServer(deps *ServerDependencies) *gin.Engine {
	// Clean up existing WebSocket hub if it exists (important for tests that call NewServer multiple times)
	if wsHubCancel != nil {
		wsHubCancel() // Cancel the old hub's context
		if wsHubShutdown != nil {
			// Wait for the old hub goroutine to fully exit (max 500ms)
			select {
			case <-wsHubShutdown:
				// Old hub shut down successfully
			case <-time.After(500 * time.Millisecond):
				// Timeout - old hub didn't shut down gracefully, but proceed anyway
				logging.Warnf("Old WebSocket hub did not shut down within timeout")
			}
		}
	}

	// Initialize job queue and WebSocket hub
	wsHub = ws.NewHub()
	wsHubShutdown = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	wsHubCancel = cancel
	deps.wsCancel = cancel

	go func() {
		wsHub.Run(ctx)
		close(wsHubShutdown) // Signal that hub goroutine has exited
	}()

	// Configure WebSocket upgrader with dynamic origin checking from config
	// Read allowedOrigins from deps.GetConfig() each time to respect config reloads
	wsUpgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			// Read current allowed origins from config (respects config reloads)
			allowedOrigins := deps.GetConfig().API.Security.AllowedOrigins

			// Empty config → allow same-origin only (secure default)
			if len(allowedOrigins) == 0 {
				return isSameOrigin(origin, r)
			}

			// Check for wildcard
			for _, allowed := range allowedOrigins {
				if allowed == "*" {
					logging.Debugf("WebSocket: Allowing connection from any origin (wildcard enabled)")
					return true
				}
			}

			// Check for exact origin match
			if isOriginAllowed(origin, allowedOrigins) {
				return true
			}

			// Reject
			logging.Debugf("WebSocket: Rejected origin %s (not in allowed list)", origin)
			return false
		},
	}

	// Setup Gin router
	if deps.GetConfig().Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Enable CORS for web UI with dynamic origin validation
	// Read allowedOrigins from deps.GetConfig() each time to respect config reloads
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Read current allowed origins from config (respects config reloads)
		allowedOrigins := deps.GetConfig().API.Security.AllowedOrigins

		// Handle CORS based on configuration
		if len(allowedOrigins) == 0 {
			// Empty config → allow same-origin only
			if isSameOrigin(origin, c.Request) {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		} else {
			// Check for wildcard in allowed origins
			hasWildcard := false
			for _, allowed := range allowedOrigins {
				if allowed == "*" {
					hasWildcard = true
					break
				}
			}

			if hasWildcard {
				// Wildcard: allow all origins (no credentials for wildcard)
				c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			} else if isOriginAllowed(origin, allowedOrigins) {
				// Specific origin allowed
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		// Allow requested headers dynamically (echo back Access-Control-Request-Headers)
		// This allows SPAs and API clients to use custom headers without CORS preflight failures
		requestedHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
		if requestedHeaders != "" {
			// Use requested headers from preflight
			c.Writer.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
		} else {
			// Default to common headers
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Serve OpenAPI spec directly for Scalar
	router.StaticFile("/docs/openapi.json", resolveSwaggerPath())

	// Scalar API documentation (modern, beautiful UI)
	router.GET("/docs", serveScalarDocs)

	// Also provide traditional Swagger UI as fallback
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Health check endpoint
	router.GET("/health", healthCheck(deps))

	// WebSocket endpoint for progress updates
	router.GET("/ws/progress", handleWebSocket(wsHub))

	// API v1 routes (define BEFORE static files to ensure API takes precedence)
	v1 := router.Group("/api/v1")
	{
		// Movie endpoints
		v1.POST("/scrape", scrapeMovie(deps))
		v1.GET("/movies/:id", getMovie(deps))
		v1.GET("/movies", listMovies(deps))
		v1.POST("/movies/:id/rescrape", rescrapeMovie(deps))
		v1.POST("/movies/:id/compare-nfo", compareNFO(deps))

		// Actress endpoints
		v1.GET("/actresses", listActresses(deps.ActressRepo))
		v1.GET("/actresses/:id", getActress(deps.ActressRepo))
		v1.POST("/actresses", createActress(deps.ActressRepo))
		v1.PUT("/actresses/:id", updateActress(deps.ActressRepo))
		v1.DELETE("/actresses/:id", deleteActress(deps.ActressRepo))
		v1.GET("/actresses/search", searchActresses(deps.ActressRepo))

		// System endpoints
		v1.GET("/config", getConfig(deps))
		v1.PUT("/config", updateConfig(deps))
		v1.GET("/scrapers", getAvailableScrapers(deps))
		v1.POST("/proxy/test", testProxy(deps))
		v1.POST("/translation/models", getTranslationModels(deps))

		// File endpoints
		v1.GET("/cwd", getCurrentWorkingDirectory(deps))
		v1.POST("/scan", scanDirectory(deps))
		v1.POST("/browse", browseDirectory(deps))

		// Batch endpoints
		v1.POST("/batch/scrape", batchScrape(deps))
		v1.GET("/batch/:id", getBatchJob(deps))
		v1.POST("/batch/:id/cancel", cancelBatchJob(deps))
		v1.PATCH("/batch/:id/movies/:movieId", updateBatchMovie(deps))
		v1.POST("/batch/:id/movies/:movieId/poster-crop", updateBatchMoviePosterCrop(deps))
		v1.POST("/batch/:id/movies/:movieId/exclude", excludeBatchMovie(deps))
		v1.POST("/batch/:id/movies/:movieId/preview", previewOrganize(deps))
		v1.POST("/batch/:id/movies/:movieId/rescrape", rescrapeBatchMovie(deps))
		v1.POST("/batch/:id/organize", organizeJob(deps))
		v1.POST("/batch/:id/update", updateBatchJob(deps))
		// Temp resource endpoints (for review page preview)
		v1.GET("/temp/posters/:jobId/:filename", serveTempPoster())
		v1.GET("/temp/image", serveTempImage(deps))
		// Persistent resource endpoints (for cropped posters stored in database)
		v1.GET("/posters/:filename", serveCroppedPoster())

		// History endpoints
		v1.GET("/history", getHistory(deps.HistoryRepo))
		v1.GET("/history/stats", getHistoryStats(deps.HistoryRepo))
		v1.DELETE("/history/:id", deleteHistory(deps.HistoryRepo))
		v1.DELETE("/history", deleteHistoryBulk(deps.HistoryRepo))
	}

	// Serve frontend static files (for Docker deployment)
	// Frontend should be built and placed in web/dist by the Dockerfile
	// Define AFTER API routes so API takes precedence
	router.Static("/_app", "/app/web/dist/_app") // SvelteKit assets
	router.StaticFile("/favicon.ico", "/app/web/dist/favicon.ico")
	router.StaticFile("/robots.txt", "/app/web/dist/robots.txt")

	// Fallback: serve index.html for browser SPA routing only
	// API requests to non-existent endpoints should return proper 404 JSON
	router.NoRoute(func(c *gin.Context) {
		// Log unmatched routes for debugging
		logging.Debugf("NoRoute hit: %s %s (Accept: %s)",
			c.Request.Method,
			c.Request.URL.Path,
			c.Request.Header.Get("Accept"))

		// Handle requests that accept HTML (browser traffic)
		method := c.Request.Method
		if acceptsHTML(c) {
			// HEAD requests should not return a body per HTTP semantics
			if method == http.MethodHead {
				c.Status(http.StatusNoContent)
				return
			}
			// Serve SPA for GET requests
			if method == http.MethodGet {
				c.File("/app/web/dist/index.html")
				return
			}
		}

		// Return proper 404 JSON for API requests
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Not Found",
			"message": "The requested resource does not exist",
			"path":    c.Request.URL.Path,
			"method":  c.Request.Method,
		})
	})

	// Print all registered routes for debugging
	logging.Debugf("Registered routes:")
	for _, route := range router.Routes() {
		logging.Debugf("  %s %s", route.Method, route.Path)
	}

	return router
}

// serveScalarDocs serves the Scalar API documentation UI
func serveScalarDocs(c *gin.Context) {
	html := `<!doctype html>
<html>
  <head>
    <title>Javinizer API Documentation</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/docs/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// LogServerInfo logs information about the running server
func LogServerInfo(cfg *config.Config) {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logging.Infof("Starting API server on %s", addr)
	logging.Infof("📚 API Documentation (Scalar): http://%s/docs", addr)
	logging.Infof("📖 Swagger UI: http://%s/swagger/index.html", addr)
	logging.Infof("🏥 Health check: http://%s/health", addr)
}
