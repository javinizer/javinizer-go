package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// resolveSwaggerPath returns the path to swagger.json, checking multiple locations.
func resolveSwaggerPath() string {
	dockerPath := "/app/docs/swagger/swagger.json"
	if _, err := os.Stat(dockerPath); err == nil {
		return dockerPath
	}
	return "./docs/swagger/swagger.json"
}

// isSameOrigin checks if the origin matches the request host (same-origin).
func isSameOrigin(origin string, r *http.Request) bool {
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	reqScheme := "http"
	if r.TLS != nil {
		reqScheme = "https"
	}

	originPort := u.Port()
	if originPort == "" {
		switch u.Scheme {
		case "http":
			originPort = "80"
		case "https":
			originPort = "443"
		}
	}

	reqHost := r.Host
	reqPort := ""
	if host, port, err := net.SplitHostPort(r.Host); err == nil {
		reqHost = host
		reqPort = port
	}
	if reqPort == "" {
		if reqScheme == "http" {
			reqPort = "80"
		} else {
			reqPort = "443"
		}
	}

	return strings.EqualFold(u.Scheme, reqScheme) &&
		strings.EqualFold(u.Hostname(), reqHost) &&
		originPort == reqPort
}

// isOriginAllowed checks if an origin is allowed based on configuration.
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			logging.Warn("Ignoring wildcard '*' in AllowedOrigins - only exact origin matches are supported for security")
			continue
		}
		if allowed == origin {
			return true
		}
	}
	return false
}

// acceptsHTML checks if the request Accept header includes text/html with q>0.
func acceptsHTML(c *gin.Context) bool {
	accept := c.GetHeader("Accept")
	if accept == "" {
		return false
	}

	parts := strings.Split(accept, ",")
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		segments := strings.Split(part, ";")
		if len(segments) == 0 {
			continue
		}

		mediaType := strings.TrimSpace(segments[0])
		if mediaType != "text/html" {
			continue
		}

		qValue := 1.0
		for i := 1; i < len(segments); i++ {
			param := strings.TrimSpace(segments[i])
			if strings.HasPrefix(param, "q=") {
				qStr := strings.TrimSpace(strings.TrimPrefix(param, "q="))
				if qStr == "0" || qStr == "0.0" || qStr == "0.00" || qStr == "0.000" {
					qValue = 0.0
				}
				break
			}
		}

		if qValue > 0 {
			return true
		}
	}

	return false
}

// NewServer creates and configures the Gin router with all API endpoints.
func NewServer(deps *core.ServerDependencies) *gin.Engine {
	runtime := deps.EnsureRuntime()
	core.SetDefaultRuntimeState(runtime)
	runtime.ResetWebSocketHub()

	runtime.SetWebSocketUpgrader(websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			allowedOrigins := deps.GetConfig().API.Security.AllowedOrigins
			if origin != "" && isSameOrigin(origin, r) {
				return true
			}
			if len(allowedOrigins) == 0 && origin == "" {
				return true
			}
			if len(allowedOrigins) > 0 && isOriginAllowed(origin, allowedOrigins) {
				return true
			}
			logging.Debugf("WebSocket: Rejected origin %s (not same-origin and not in allowed list)", origin)
			return false
		},
	})

	if deps.GetConfig().Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	webAssets := loadWebUIAssets()

	registerCORSMiddleware(router, deps)
	registerDocumentationRoutes(router)
	registerCoreRoutes(router, deps)
	registerAPIV1Routes(router, deps)
	registerStaticWebRoutes(router, webAssets)
	registerNoRouteHandler(router, webAssets)
	logRegisteredRoutes(router)

	return router
}

// serveScalarDocs serves the Scalar API documentation UI.
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

// LogServerInfo logs information about the running server.
func LogServerInfo(cfg *config.Config) {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logging.Infof("Starting API server on %s", addr)
	logging.Infof("📚 API Documentation (Scalar): http://%s/docs", addr)
	logging.Infof("📖 Swagger UI: http://%s/swagger/index.html", addr)
	logging.Infof("🏥 Health check: http://%s/health", addr)
}
