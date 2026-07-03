package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- requireTokenOrSession: nil deps fails closed (503) ---

func TestMiss4_RequireTokenOrSession_NilDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(nil), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called, "handler must not run when auth is unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireTokenOrSession: nil auth fails closed (503) ---

func TestMiss4_RequireTokenOrSession_NilAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	// GetTestRuntime auto-wires NoOpAuth for test convenience; nil it out
	// afterward to simulate a wiring bug and assert the fail-closed path.
	rt := testkit.GetTestRuntime(deps)
	deps.Auth = nil

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(rt), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called, "handler must not run when auth is unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireAuthenticated: nil auth fails closed (503) ---

func TestMiss4_RequireAuthenticated_NilAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	rt := testkit.GetTestRuntime(deps)
	deps.Auth = nil

	called := false
	router := gin.New()
	router.GET("/test", requireAuthenticated(rt), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called, "handler must not run when auth is unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireAuthenticated: NoOpAuth satisfies authDisabler, so the middleware short-circuits to c.Next() before the credentials check ---

func TestMiss4_RequireAuthenticated_NoOpAuthPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	deps.Auth = testkit.NoOpAuth{}
	rt := testkit.GetTestRuntime(deps)

	called := false
	router := gin.New()
	router.GET("/test", requireAuthenticated(rt), func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, called, "handler should be called when NoOpAuth bypasses auth")
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- requireTokenOrSession: session with ErrAuthNotInitialized returns 503 ---

func TestMiss4_RequireTokenOrSession_SessionAuthNotInit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	// Don't setup — not initialized
	deps.Auth = manager

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "some-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireTokenOrSession: Bearer token with invalid prefix ---

func TestMiss4_RequireTokenOrSession_BearerInvalidPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer raw-token-without-prefix")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- setupAuth: already initialized returns 409 ---

func TestMiss4_SetupAuth_AlreadyInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/api/v1/auth/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := strings.NewReader(`{"username":"newuser","password":"newpass123"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/setup", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- setupAuth: with bootstrap secret, wrong secret ---

func TestMiss4_SetupAuth_WrongBootstrapSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	os.Setenv("JAVINIZER_SETUP_SECRET", "test-secret-456")
	defer os.Unsetenv("JAVINIZER_SETUP_SECRET")

	router := gin.New()
	router.POST("/api/v1/auth/setup", setupAuth(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "wrong-secret")
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- loginAuth: auth not initialized returns 503 ---

func TestMiss4_LoginAuth_AuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	// Don't call Setup — auth not initialized
	deps.Auth = manager

	router := gin.New()
	router.POST("/api/v1/auth/login", loginAuth(testkit.GetTestRuntime(deps)))

	body := strings.NewReader(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- loginAuth: invalid credentials returns 401 ---

func TestMiss4_LoginAuth_InvalidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/api/v1/auth/login", loginAuth(testkit.GetTestRuntime(deps)))

	body := strings.NewReader(`{"username":"admin","password":"wrongpassword"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- parseCIDRList tests ---

func TestMiss4_ParseCIDRList(t *testing.T) {
	assert.Nil(t, parseCIDRList(""))
	assert.Nil(t, parseCIDRList("   "))

	cidrs := parseCIDRList("10.0.0.0/8, 192.168.0.0/16")
	assert.Len(t, cidrs, 2)

	cidrs = parseCIDRList("not-a-cidr, 10.0.0.0/8")
	assert.Len(t, cidrs, 1)
}

// --- isTrustedClient ---

func TestMiss4_IsTrustedClient(t *testing.T) {
	assert.True(t, isTrustedClient("127.0.0.1", nil))
	assert.True(t, isTrustedClient("[::1]", nil))
	assert.False(t, isTrustedClient("8.8.8.8", nil))
	assert.False(t, isTrustedClient("not-an-ip", nil))
}

// --- peerIP ---

func TestMiss4_PeerIP(t *testing.T) {
	assert.Equal(t, "127.0.0.1", peerIP("127.0.0.1:12345"))
	assert.Equal(t, "invalid", peerIP("invalid"))
}
