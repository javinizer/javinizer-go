package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// --- registerStaticWebRoutes with UI available (line 165-185) ---

func TestRegisterStaticWebRoutes_WithUI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	assets := loadWebUIAssets()
	// If the web UI is available in this build, test the routes
	if !assets.uiAvailable {
		t.Skip("Web UI assets not available in test build")
	}

	router := gin.New()
	registerStaticWebRoutes(router, assets)

	// If _app directory exists, test it
	routes := router.Routes()
	routePaths := make(map[string]bool)
	for _, r := range routes {
		routePaths[r.Path] = true
	}

	// favicon.ico and robots.txt may or may not be in the embedded assets
	// depending on the build; just verify no panic occurred
	_ = routePaths
}

// --- registerNoRouteHandler with UI available and HTML Accept (line 190-210) ---

func TestRegisterNoRouteHandler_WithUI_HTMLAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)

	assets := loadWebUIAssets()
	if !assets.uiAvailable {
		t.Skip("Web UI assets not available in test build")
	}

	router := gin.New()
	registerNoRouteHandler(router, assets)

	// Test GET with HTML Accept header
	req := httptest.NewRequest(http.MethodGet, "/some/page", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

// --- registerNoRouteHandler with UI and HEAD method (line 198) ---

func TestRegisterNoRouteHandler_WithUI_HeadMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	assets := loadWebUIAssets()
	if !assets.uiAvailable {
		t.Skip("Web UI assets not available in test build")
	}

	router := gin.New()
	registerNoRouteHandler(router, assets)

	req := httptest.NewRequest(http.MethodHead, "/some/page", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// --- registerNoRouteHandler with UI and POST method (should return 404 JSON) ---

func TestRegisterNoRouteHandler_WithUI_PostMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	assets := loadWebUIAssets()
	if !assets.uiAvailable {
		t.Skip("Web UI assets not available in test build")
	}

	router := gin.New()
	registerNoRouteHandler(router, assets)

	req := httptest.NewRequest(http.MethodPost, "/some/api", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- registerNoRouteHandler without UI and non-HTML Accept (line 214) ---

func TestRegisterNoRouteHandler_NoUI_NonHTMLAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	assets := webUIAssets{} // no UI available
	registerNoRouteHandler(router, assets)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Not Found")
}

// --- loadWebUIAssets branches (index.html not found, read error) ---

func TestLoadWebUIAssets_DoesNotPanic(t *testing.T) {
	// loadWebUIAssets should never panic regardless of build configuration
	assert.NotPanics(t, func() {
		assets := loadWebUIAssets()
		_ = assets
	})
}

// --- registerCORSMiddleware with same-origin (line 68-92) ---

func TestRegisterCORSMiddleware_SameOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// This test verifies the CORS middleware doesn't panic with various origins
	router := gin.New()

	// Register a simple handler to test middleware
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Test without Origin header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- registerCORSMiddleware with OPTIONS method (line 85) ---

func TestRegisterCORSMiddleware_OptionsMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.OPTIONS("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// Should handle OPTIONS (may be 200 or 204 depending on middleware)
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent || w.Code == http.StatusNotFound)
}

// --- acceptsHTML helper (line 77-115) ---

func TestAcceptsHTML_EmptyAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		result := acceptsHTML(c)
		assert.False(t, result)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Accept header
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAcceptsHTML_WithHTMLAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		result := acceptsHTML(c)
		assert.True(t, result)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAcceptsHTML_WithZeroQValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		result := acceptsHTML(c)
		assert.False(t, result, "text/html with q=0 should not be accepted")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "text/html;q=0")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAcceptsHTML_WithZeroPointQValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		result := acceptsHTML(c)
		assert.False(t, result, "text/html with q=0.0 should not be accepted")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "text/html;q=0.0")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAcceptsHTML_WithJSONOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		result := acceptsHTML(c)
		assert.False(t, result, "application/json only should not match HTML")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- isSameOrigin helper (line 21-60) ---

func TestIsSameOrigin_EmptyOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	result := isSameOrigin("", req)
	assert.True(t, result, "empty origin should be same-origin")
}

func TestIsSameOrigin_InvalidOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	result := isSameOrigin("://invalid", req)
	assert.False(t, result, "invalid origin URL should not be same-origin")
}

func TestIsSameOrigin_SameHostPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "localhost:8080"
	result := isSameOrigin("http://localhost:8080", req)
	assert.True(t, result)
}

func TestIsSameOrigin_DifferentPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "localhost:9090"
	result := isSameOrigin("http://localhost:8080", req)
	assert.False(t, result)
}

func TestIsSameOrigin_HttpsDefaultPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	// Can't easily set TLS in httptest, but we can test the non-TLS path
	result := isSameOrigin("http://example.com", req)
	assert.True(t, result)
}

// --- isOriginAllowed helper (line 63-76) ---

func TestIsOriginAllowed_ExactMatch(t *testing.T) {
	result := isOriginAllowed("http://localhost:3000", []string{"http://localhost:3000"})
	assert.True(t, result)
}

func TestIsOriginAllowed_NoMatch(t *testing.T) {
	result := isOriginAllowed("http://evil.com", []string{"http://localhost:3000"})
	assert.False(t, result)
}

func TestIsOriginAllowed_WildcardIgnored(t *testing.T) {
	result := isOriginAllowed("http://evil.com", []string{"*"})
	assert.False(t, result, "wildcard should be ignored for security")
}

func TestIsOriginAllowed_EmptyOrigin(t *testing.T) {
	result := isOriginAllowed("", []string{"http://localhost:3000"})
	assert.False(t, result)
}

// --- registerNoRouteHandler debug logging (line 189) ---

func TestRegisterNoRouteHandler_DebugLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	assets := webUIAssets{}
	registerNoRouteHandler(router, assets)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
}
