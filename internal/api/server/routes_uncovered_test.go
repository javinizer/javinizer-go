package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestServeScalarDocs_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/docs", serveScalarDocs)

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "scalar")
}

func TestRegisterDocumentationRoutes_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerDocumentationRoutes(router)

	// Test OpenAPI JSON endpoint
	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Test HEAD OpenAPI JSON
	reqHead := httptest.NewRequest(http.MethodHead, "/docs/openapi.json", nil)
	wHead := httptest.NewRecorder()
	router.ServeHTTP(wHead, reqHead)
	assert.Equal(t, http.StatusOK, wHead.Code)
}

func TestLogRegisteredRoutes_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {})

	// Should not panic
	assert.NotPanics(t, func() {
		logRegisteredRoutes(router)
	})
}

func TestLoadWebUIAssets_Uncovered(t *testing.T) {
	// Should not panic even if web UI is unavailable
	assets := loadWebUIAssets()
	// Assets may or may not be available depending on build, but should not panic
	_ = assets
}

func TestRegisterNoRouteHandler_NoUI_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	// Empty assets (no UI available)
	assets := webUIAssets{}
	registerNoRouteHandler(router, assets)

	// Should return JSON 404 for API routes
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
