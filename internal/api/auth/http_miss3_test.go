package auth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- requireAuthenticated: not initialized returns 503 ---

func TestRequireAuthenticated_Miss3_AuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	called := false
	router := gin.New()
	router.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireAuthenticated: valid session passes through ---

func TestRequireAuthenticated_Miss3_ValidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	var username string
	called := false
	router := gin.New()
	router.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) {
		called = true
		if v, exists := c.Get("auth_username"); exists {
			username = v.(string)
		}
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, "admin", username)
}

// --- requireAuthenticated: invalid session clears cookie and returns 401 ---

func TestRequireAuthenticated_Miss3_InvalidSession(t *testing.T) {
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
	router.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bad-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- requireAuthenticated: ErrAuthNotInitialized from AuthenticateSession returns 503 ---

func TestRequireAuthenticated_Miss3_SessionAuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	// Remove credentials to make IsInitialized() false
	credPath := credentialPathForConfig(configFile)
	os.Remove(credPath)
	manager2, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager2

	called := false
	router := gin.New()
	router.GET("/test", requireAuthenticated(testkit.GetTestRuntime(deps)), func(c *gin.Context) { called = true })

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "some-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- requireTokenOrSession: invalid session clears cookie ---

func TestRequireTokenOrSession_Miss3_InvalidSessionClearsCookie(t *testing.T) {
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
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bad-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

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

// --- requireTokenOrSession: session ErrAuthNotInitialized returns 503 ---

func TestRequireTokenOrSession_Miss3_SessionErrAuthNotInit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	credPath := credentialPathForConfig(configFile)
	os.Remove(credPath)
	manager2, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager2

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

// --- computeTrustedCIDRs: extra env CIDRs ---

func TestComputeTrustedCIDRs_Miss3_WithExtraEnvCIDRs(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_TRUSTED_CIDRS", "10.0.0.0/8, bad-cidr")

	cidrs := computeTrustedCIDRs(nil)
	require.Len(t, cidrs, 3) // 2 default + 1 valid extra
}

// --- setupAuth: nil deps returns 503 ---

func TestSetupAuth_Miss3_NilDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/setup", setupAuth(nil))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- setupAuth: invalid JSON body returns 400 ---

func TestSetupAuth_Miss3_InvalidJSON(t *testing.T) {
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

	req := httptest.NewRequest("POST", "/setup", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- setupAuth: ErrInvalidUsername returns 400 ---

func TestSetupAuth_Miss3_InvalidUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	body := []byte(`{"username":"","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "test-secret")
	w := httptest.NewRecorder()

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- loginAuth: not initialized returns 503 ---

func TestLoginAuth_Miss3_NotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := gin.New()
	router.POST("/login", loginAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- loginAuth: invalid credentials returns 401 ---

func TestLoginAuth_Miss3_InvalidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/login", loginAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"wrongpassword"}`)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- loginAuth: rate limited returns 429 ---

func TestLoginAuth_Miss3_RateLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/login", loginAuth(testkit.GetTestRuntime(deps)))

	for i := 0; i < 6; i++ {
		body := []byte(`{"username":"admin","password":"wrongpassword"}`)
		req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	body := []byte(`{"username":"admin","password":"wrongpassword"}`)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// --- loginAuth: invalid JSON returns 400 ---

func TestLoginAuth_Miss3_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager

	router := gin.New()
	router.POST("/login", loginAuth(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/login", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- loginAuth: nil deps returns 503 ---

func TestLoginAuth_Miss3_NilDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/login", loginAuth(nil))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- isSecureRequest: nil request returns false ---

func TestIsSecureRequest_Miss3_NilRequest(t *testing.T) {
	assert.False(t, isSecureRequest(nil, nil))
}

// --- isSecureRequest: ForceSecureCookies ---

func TestIsSecureRequest_Miss3_ForceSecureCookies(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	cfg := &core.SecurityNarrowConfig{ForceSecureCookies: true}
	assert.True(t, isSecureRequest(r, cfg))
}

// --- isSecureRequest: trusted proxy with X-Forwarded-Proto: https ---

func TestIsSecureRequest_Miss3_TrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "10.0.0.1:12345"
	cfg := &core.SecurityNarrowConfig{TrustedProxies: []string{"10.0.0.1"}}
	assert.True(t, isSecureRequest(r, cfg))
}
