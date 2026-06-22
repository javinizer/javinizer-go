package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestRequireAuthenticated_NotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	// Create manager but don't set up credentials — IsInitialized() == false
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.False(t, called, "next handler should not be called when auth not initialized")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "authentication is not initialized")
}

func TestRequireAuthenticated_NoCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.False(t, called, "next handler should not be called without cookie")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "authentication required")
}

func TestRequireAuthenticated_InvalidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "invalid-session-id",
	})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.False(t, called, "next handler should not be called with invalid session")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAuthenticated_ValidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	// Create a valid session
	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	called := false
	var capturedUsername string
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
		val, _ := c.Get("auth_username")
		capturedUsername, _ = val.(string)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: sessionID,
	})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.True(t, called, "next handler should be called with valid session")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "admin", capturedUsername)
}

func TestRequireAuthenticated_AuthNotInitializedDuringCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
	})

	// Not initialized + has a cookie → should still return 503
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "some-session",
	})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRequireAuthenticated_EmptyCookieValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "   ", // whitespace-only value
	})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
