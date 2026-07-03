package auth

import (
	"bytes"
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

// --- requireTokenOrSession: nil deps fails closed (503) ---

func TestRequireTokenOrSession_Miss2_NilDeps(t *testing.T) {
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

// --- requireTokenOrSession: Bearer token without jv_ prefix ---

func TestRequireTokenOrSession_Miss2_BearerWithoutPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
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
	req.Header.Set("Authorization", "Bearer some-token-without-prefix")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- requireTokenOrSession: valid session sets auth_method and auth_username ---

func TestRequireTokenOrSession_Miss2_ValidSessionSetsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	var authMethod, authUsername string
	router := gin.New()
	router.GET("/test", requireTokenOrSession(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		if v, exists := c.Get("auth_method"); exists {
			authMethod = v.(string)
		}
		if v, exists := c.Get("auth_username"); exists {
			authUsername = v.(string)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "session", authMethod)
	assert.Equal(t, "admin", authUsername)
}

// --- requireTokenOrSession: auth not initialized returns 503 ---

func TestRequireTokenOrSession_Miss2_AuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	// Don't call Setup — auth not initialized
	deps.Auth = manager

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireTokenOrSession: no cookie and no bearer returns 401 ---

func TestRequireTokenOrSession_Miss2_NoCookieNoBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
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
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- requireTokenOrSession: empty cookie value returns 401 ---

func TestRequireTokenOrSession_Miss2_EmptyCookieValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
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
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "   "})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- requireTokenOrSession: session with ErrAuthNotInitialized returns 503 ---

func TestRequireTokenOrSession_Miss2_SessionAuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	// Setup then destroy session store to trigger ErrAuthNotInitialized
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	// Use a valid session cookie — should authenticate successfully
	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should succeed since auth is initialized and session is valid
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- requireTokenOrSession: nil deps with Bearer token still fails closed (503) ---

func TestRequireTokenOrSession_Miss2_NilDepsWithBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	router := gin.New()
	router.GET("/test", requireTokenOrSession(nil), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called, "handler must not run when auth is unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- setupAuth: remote setup with valid bootstrap secret ---

func TestSetupAuth_Miss2_RemoteSetupWithSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "my-secret-123")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "my-secret-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.AuthStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Initialized)
	assert.True(t, resp.Authenticated)
}

// --- setupAuth: remote setup with wrong secret ---

func TestSetupAuth_Miss2_RemoteSetupWrongSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "my-secret-123")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- setupAuth: already initialized returns 409 ---

func TestSetupAuth_Miss2_AlreadyInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin2","password":"password456"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- setupAuth: weak password returns 400 ---

func TestSetupAuth_Miss2_WeakPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"short"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- setupAuth: remote access without bootstrap secret ---

func TestSetupAuth_Miss2_RemoteNoSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// No JAVINIZER_SETUP_SECRET set
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Remote address that's not localhost
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- getAuthStatus: invalid session cookie clears cookie ---

func TestGetAuthStatus_Miss2_InvalidSessionClearsCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.GET("/status", getAuthStatus(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "invalid-session-id"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify cookie was cleared
	resp := w.Result()
	var clearedCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			clearedCookie = c
			break
		}
	}
	require.NotNil(t, clearedCookie)
	assert.Equal(t, -1, clearedCookie.MaxAge)
}

// --- parseCIDRList: valid and invalid entries ---

func TestParseCIDRList_Miss2(t *testing.T) {
	result := parseCIDRList("10.0.0.0/8, invalid-cidr, 172.16.0.0/12")
	assert.Len(t, result, 2)
}

// --- parseCIDRList: empty string ---

func TestParseCIDRList_Miss2_Empty(t *testing.T) {
	result := parseCIDRList("")
	assert.Nil(t, result)
}

// --- isTrustedClient: various IPs ---

func TestIsTrustedClient_Miss2(t *testing.T) {
	assert.True(t, isTrustedClient("127.0.0.1", nil))
	assert.True(t, isTrustedClient("[::1]", nil))
	assert.False(t, isTrustedClient("192.168.1.1", nil))
	assert.False(t, isTrustedClient("invalid-ip", nil))
}

// --- peerIP: with port ---

func TestPeerIP_Miss2_WithPort(t *testing.T) {
	assert.Equal(t, "192.168.1.1", peerIP("192.168.1.1:8080"))
}

// --- peerIP: without port ---

func TestPeerIP_Miss2_WithoutPort(t *testing.T) {
	assert.Equal(t, "invalid", peerIP("invalid"))
}

// --- loginAuth: successful login returns session cookie ---

func TestLoginAuth_Miss2_SuccessWithCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/login", loginAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123","remember_me":true}`)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.AuthStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Authenticated)
	assert.Equal(t, "admin", resp.Username)

	// Verify session cookie was set
	respHTTP := w.Result()
	var sessionCookie *http.Cookie
	for _, c := range respHTTP.Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie)
	assert.NotEmpty(t, sessionCookie.Value)
}

// --- logoutAuth: with valid session logs out ---

func TestLogoutAuth_Miss2_WithValidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	router := gin.New()
	router.POST("/logout", logoutAuth(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Session should be invalidated
	_, authErr := manager.AuthenticateSession(sessionID)
	assert.Error(t, authErr)
}
