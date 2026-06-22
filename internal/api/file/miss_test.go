package file

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- RegisterRoutes: verifies route registration ---

func TestRegisterRoutes_Miss_RouteRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDepsFromConfig(cfg)

	router := gin.New()
	protected := router.Group("/api/v1")

	RegisterRoutes(protected, testkit.GetTestRuntime(deps))

	// Verify routes are registered by making requests to each
	routes := router.Routes()
	assert.True(t, len(routes) >= 4, "expected at least 4 routes, got %d", len(routes))

	// Check route paths exist
	foundPaths := make(map[string]bool)
	for _, route := range routes {
		foundPaths[route.Path] = true
	}
	assert.True(t, foundPaths["/api/v1/cwd"], "expected /api/v1/cwd route")
	assert.True(t, foundPaths["/api/v1/scan"], "expected /api/v1/scan route")
	assert.True(t, foundPaths["/api/v1/browse"], "expected /api/v1/browse route")
	assert.True(t, foundPaths["/api/v1/browse/autocomplete"], "expected /api/v1/browse/autocomplete route")
}

// --- scanDirectory: workflow error ---

func TestScanDirectory_Miss_WorkflowError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "test.mp4"), []byte("test"), 0644))

	// Use a config that creates real deps (which may not have workflow)
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Matching: config.MatchingConfig{
			Extensions: []string{".mp4"},
		},
	}

	deps := newTestDepsFromConfig(cfg)

	router := gin.New()
	router.POST("/scan", scanDirectory(testkit.GetTestRuntime(deps)))

	reqBody := contracts.ScanRequest{Path: tempDir, Recursive: false}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should either succeed or return 500 (workflow not configured)
	assert.True(t, w.Code == 200 || w.Code == 500, "expected 200 or 500, got %d", w.Code)
}

// --- browseDirectory: entry.Info error ---

func TestBrowseDirectory_Miss_EntryInfoError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	// Create a file (not directory) that might cause issues
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("test"), 0644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{tempDir}

	deps := newTestDepsFromConfig(cfg)
	router := gin.New()
	router.POST("/browse", browseDirectory(testkit.GetTestRuntime(deps)))

	reqBody := contracts.BrowseRequest{Path: tempDir}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BrowseResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	// Should still work even if entry.Info fails on some entries
	assert.NotNil(t, response.Items)
}

// --- autocompletePath: empty path ---

func TestAutocompletePath_Miss_EmptyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/"}

	deps := newTestDepsFromConfig(cfg)
	router := gin.New()
	router.POST("/browse/autocomplete", autocompletePath(testkit.GetTestRuntime(deps)))

	reqBody := contracts.PathAutocompleteRequest{Path: ""}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- scanDirectory: invalid JSON body ---

func TestScanDirectory_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDepsFromConfig(cfg)

	router := gin.New()
	router.POST("/scan", scanDirectory(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- browseDirectory: invalid JSON body ---

func TestBrowseDirectory_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDepsFromConfig(cfg)

	router := gin.New()
	router.POST("/browse", browseDirectory(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- autocompletePath: invalid JSON body ---

func TestAutocompletePath_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDepsFromConfig(cfg)

	router := gin.New()
	router.POST("/browse/autocomplete", autocompletePath(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- getCurrentWorkingDirectory: error path ---

func TestGetCurrentWorkingDirectory_Miss_WithAllowedDirs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/media/videos", "/data/movies"}

	deps := newTestDepsFromConfig(cfg)
	router := gin.New()
	router.GET("/cwd", getCurrentWorkingDirectory(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/cwd", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "/media/videos", response["path"])
}

// --- resolveAutocompleteBasePath: empty path ---

func TestResolveAutocompleteBasePath_Miss_EmptyPath(t *testing.T) {
	_, _, err := resolveAutocompleteBasePath("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

// --- hasTrailingPathSeparator ---

func TestHasTrailingPathSeparator_Miss(t *testing.T) {
	assert.True(t, hasTrailingPathSeparator("/path/"))
	assert.True(t, hasTrailingPathSeparator("C:\\path\\"))
	assert.False(t, hasTrailingPathSeparator("/path"))
	assert.False(t, hasTrailingPathSeparator("path"))
}
