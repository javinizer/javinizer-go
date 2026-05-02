package auth

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthenticatedTestServer(t *testing.T) (*gin.Engine, *ServerDependencies) {
	t.Helper()

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	deps.ApiTokenRepo = createTestApiTokenRepo(t, deps)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := NewServer(deps)
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})

	return router, deps
}

func newJSONRequest(t *testing.T, method, path string, payload any, cookie *http.Cookie) *http.Request {
	t.Helper()

	var body []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		body = encoded
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if path == "/api/v1/auth/setup" {
		if secret := os.Getenv("JAVINIZER_SETUP_SECRET"); secret != "" {
			req.Header.Set("X-Setup-Secret", secret)
		}
	}
	return req
}

func parseAuthStatus(t *testing.T, recorder *httptest.ResponseRecorder) AuthStatusResponse {
	t.Helper()
	var status AuthStatusResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
	return status
}

func extractSessionCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	resp := recorder.Result()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatalf("session cookie %q not set", sessionCookieName)
	return nil
}

func TestAuth_FirstRunBlocksProtectedRoutes(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	// Protected API route should require initialization first.
	req := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// Status route remains public and should report uninitialized state.
	statusReq := newJSONRequest(t, http.MethodGet, "/api/v1/auth/status", nil, nil)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)
	assert.Equal(t, http.StatusOK, statusW.Code)

	status := parseAuthStatus(t, statusW)
	assert.False(t, status.Initialized)
	assert.False(t, status.Authenticated)
	assert.Empty(t, status.Username)

	// WebSocket endpoint is also protected.
	wsReq := newJSONRequest(t, http.MethodGet, "/ws/progress", nil, nil)
	wsW := httptest.NewRecorder()
	router.ServeHTTP(wsW, wsReq)
	assert.Equal(t, http.StatusServiceUnavailable, wsW.Code)
}

func TestAuth_SetupAndProtectedAccess(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusOK, setupW.Code)

	setupStatus := parseAuthStatus(t, setupW)
	assert.True(t, setupStatus.Initialized)
	assert.True(t, setupStatus.Authenticated)
	assert.Equal(t, "admin", setupStatus.Username)

	sessionCookie := extractSessionCookie(t, setupW)

	protectedReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, sessionCookie)
	protectedW := httptest.NewRecorder()
	router.ServeHTTP(protectedW, protectedReq)
	assert.Equal(t, http.StatusOK, protectedW.Code)

	missingSessionReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, nil)
	missingSessionW := httptest.NewRecorder()
	router.ServeHTTP(missingSessionW, missingSessionReq)
	assert.Equal(t, http.StatusUnauthorized, missingSessionW.Code)

	statusReq := newJSONRequest(t, http.MethodGet, "/api/v1/auth/status", nil, sessionCookie)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)
	assert.Equal(t, http.StatusOK, statusW.Code)

	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "admin", status.Username)

	// Setup can only be called once.
	setupAgainReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupAgainW := httptest.NewRecorder()
	router.ServeHTTP(setupAgainW, setupAgainReq)
	assert.Equal(t, http.StatusConflict, setupAgainW.Code)
}

func TestAuth_SetupAfterInitializationReturnsConflictBeforePayloadValidation(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	invalidPayloadReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]any{}, nil)
	invalidPayloadW := httptest.NewRecorder()
	router.ServeHTTP(invalidPayloadW, invalidPayloadReq)
	assert.Equal(t, http.StatusConflict, invalidPayloadW.Code)
}

func TestAuth_LoginLogoutFlow(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	// Initialize credentials.
	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	initialSession := extractSessionCookie(t, setupW)

	// Logout invalidates the existing session.
	logoutReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/logout", nil, initialSession)
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	assert.Equal(t, http.StatusOK, logoutW.Code)

	protectedReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, initialSession)
	protectedW := httptest.NewRecorder()
	router.ServeHTTP(protectedW, protectedReq)
	assert.Equal(t, http.StatusUnauthorized, protectedW.Code)

	invalidLoginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "wrong",
	}, nil)
	invalidLoginW := httptest.NewRecorder()
	router.ServeHTTP(invalidLoginW, invalidLoginReq)
	assert.Equal(t, http.StatusUnauthorized, invalidLoginW.Code)

	validLoginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	validLoginW := httptest.NewRecorder()
	router.ServeHTTP(validLoginW, validLoginReq)
	assert.Equal(t, http.StatusOK, validLoginW.Code)

	newSession := extractSessionCookie(t, validLoginW)
	protectedWithNewSessionReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, newSession)
	protectedWithNewSessionW := httptest.NewRecorder()
	router.ServeHTTP(protectedWithNewSessionW, protectedWithNewSessionReq)
	assert.Equal(t, http.StatusOK, protectedWithNewSessionW.Code)
}

func TestAuth_LoginRememberMePersistsAcrossRestart(t *testing.T) {
	router, deps := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	logoutReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/logout", nil, extractSessionCookie(t, setupW))
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	require.Equal(t, http.StatusOK, logoutW.Code)

	loginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username":    "admin",
		"password":    "password123",
		"remember_me": true,
	}, nil)
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	require.Equal(t, http.StatusOK, loginW.Code)

	sessionCookie := extractSessionCookie(t, loginW)
	assert.Greater(t, sessionCookie.MaxAge, 0)

	reloaded, err := NewAuthManager(deps.ConfigFile, time.Hour)
	require.NoError(t, err)

	username, err := reloaded.AuthenticateSession(sessionCookie.Value)
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
}

func TestAuth_LoginWithoutRememberMeUsesEphemeralSession(t *testing.T) {
	router, deps := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	logoutReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/logout", nil, extractSessionCookie(t, setupW))
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	require.Equal(t, http.StatusOK, logoutW.Code)

	loginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username":    "admin",
		"password":    "password123",
		"remember_me": false,
	}, nil)
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	require.Equal(t, http.StatusOK, loginW.Code)

	sessionCookie := extractSessionCookie(t, loginW)
	assert.Equal(t, 0, sessionCookie.MaxAge)

	reloaded, err := NewAuthManager(deps.ConfigFile, time.Hour)
	require.NoError(t, err)

	_, err = reloaded.AuthenticateSession(sessionCookie.Value)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuth_WebSocketRequiresAuthentication(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	// Initialize authentication.
	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)

	// Missing session should fail before WebSocket upgrade.
	wsUnauthorizedReq := newJSONRequest(t, http.MethodGet, "/ws/progress", nil, nil)
	wsUnauthorizedW := httptest.NewRecorder()
	router.ServeHTTP(wsUnauthorizedW, wsUnauthorizedReq)
	assert.Equal(t, http.StatusUnauthorized, wsUnauthorizedW.Code)

	// Authenticated request reaches the upgrader and fails with 400 because this isn't
	// a proper WebSocket handshake in httptest.
	wsAuthorizedReq := newJSONRequest(t, http.MethodGet, "/ws/progress", nil, sessionCookie)
	wsAuthorizedW := httptest.NewRecorder()
	router.ServeHTTP(wsAuthorizedW, wsAuthorizedReq)
	assert.Equal(t, http.StatusBadRequest, wsAuthorizedW.Code)
}

func TestAuth_LoginBeforeSetupReturnsServiceUnavailable(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	loginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	assert.Equal(t, http.StatusServiceUnavailable, loginW.Code)
}

func TestAuth_SetupValidationErrors(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	shortPasswordReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "short",
	}, nil)
	shortPasswordW := httptest.NewRecorder()
	router.ServeHTTP(shortPasswordW, shortPasswordReq)
	assert.Equal(t, http.StatusBadRequest, shortPasswordW.Code)

	missingUsernameReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "",
		"password": "password123",
	}, nil)
	missingUsernameW := httptest.NewRecorder()
	router.ServeHTTP(missingUsernameW, missingUsernameReq)
	assert.Equal(t, http.StatusBadRequest, missingUsernameW.Code)
}

func TestAuth_StatusWithInvalidCookieClearsSession(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	invalidCookie := &http.Cookie{
		Name:  sessionCookieName,
		Value: "definitely-invalid-session",
	}

	statusReq := newJSONRequest(t, http.MethodGet, "/api/v1/auth/status", nil, invalidCookie)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)
	assert.Equal(t, http.StatusOK, statusW.Code)

	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.False(t, status.Authenticated)
	assert.Empty(t, status.Username)

	resp := statusW.Result()
	var clearCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName {
			clearCookie = cookie
			break
		}
	}
	require.NotNil(t, clearCookie)
	assert.Equal(t, -1, clearCookie.MaxAge)
}

func TestAuth_SessionCookieIgnoresForwardedHTTPSWithoutTrustedProxies(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupReq.Header.Set("X-Forwarded-Proto", "https")
	setupReq.Header.Set("Forwarded", `for=1.2.3.4;proto=https`)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.False(t, sessionCookie.Secure)
}

func TestAuth_SessionCookieSecureWithTrustedProxyAndForwardedProto(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	cfg.API.Security.TrustedProxies = []string{"127.0.0.1"}
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := NewServer(deps)
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupReq.Header.Set("X-Forwarded-Proto", "https")
	setupReq.RemoteAddr = "127.0.0.1:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.True(t, sessionCookie.Secure)
}

func TestAuth_SessionCookieSecureWithForceSecureCookies(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	cfg.API.Security.ForceSecureCookies = true
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := NewServer(deps)
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.True(t, sessionCookie.Secure)
}

func TestAuth_SessionCookieNotSecureFromUntrustedProxy(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	cfg.API.Security.TrustedProxies = []string{"10.0.0.1"}
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := NewServer(deps)
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupReq.Header.Set("X-Forwarded-Proto", "https")
	setupReq.RemoteAddr = "192.168.1.1:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.False(t, sessionCookie.Secure)
}

func TestAuth_SessionCookieSecureOnTLS(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupReq.TLS = &tls.ConnectionState{}
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.True(t, sessionCookie.Secure)
}

func TestAuth_SessionCookieNotSecureOnPlainHTTP(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	sessionCookie := extractSessionCookie(t, setupW)
	assert.False(t, sessionCookie.Secure)
}

func TestAuth_LoginRateLimited(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	for i := 0; i < maxFailedLoginAttempts; i++ {
		loginReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "admin",
			"password": "wrong-password",
		}, nil)
		loginW := httptest.NewRecorder()
		router.ServeHTTP(loginW, loginReq)
		assert.Equal(t, http.StatusUnauthorized, loginW.Code)
	}

	blockedReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	blockedW := httptest.NewRecorder()
	router.ServeHTTP(blockedW, blockedReq)
	assert.Equal(t, http.StatusTooManyRequests, blockedW.Code)
}

func TestAuth_SetupRejectedFromRemoteWithoutSecret(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", nil)
	setupReq.Header.Set("Content-Type", "application/json")
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusForbidden, setupW.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(setupW.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "bootstrap secret")
}

func createSetupServerWithoutSecret(t *testing.T) (*gin.Engine, *ServerDependencies) {
	t.Helper()

	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	router := NewServer(deps)
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})

	return router, deps
}

func TestAuth_SetupAllowedFromLocalhostWithoutSecret(t *testing.T) {
	router, _ := createSetupServerWithoutSecret(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.RemoteAddr = "127.0.0.1:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusOK, setupW.Code)
}

func TestAuth_SetupAllowedFromDockerBridgeWithoutSecret(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_TRUSTED_CIDRS", "172.16.0.0/12")
	resetTrustedCIDRsCache()
	t.Cleanup(resetTrustedCIDRsCache)
	router, _ := createSetupServerWithoutSecret(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.RemoteAddr = "172.30.0.1:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusOK, setupW.Code)
}

func TestAuth_SetupRejectedFromDockerBridgeWithoutEnvVar(t *testing.T) {
	router, _ := createSetupServerWithoutSecret(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.RemoteAddr = "172.30.0.1:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusForbidden, setupW.Code)
}

func TestAuth_SetupAllowedFromCustomTrustedCIDR(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_TRUSTED_CIDRS", "10.0.0.0/8,192.168.0.0/16")
	resetTrustedCIDRsCache()
	t.Cleanup(resetTrustedCIDRsCache)

	for _, addr := range []string{"10.0.0.5:12345", "192.168.1.100:12345"} {
		t.Run(addr, func(t *testing.T) {
			router, _ := createSetupServerWithoutSecret(t)

			body := map[string]string{"username": "admin", "password": "password123"}
			bodyBytes, err := json.Marshal(body)
			require.NoError(t, err)

			setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
			setupReq.Header.Set("Content-Type", "application/json")
			setupReq.RemoteAddr = addr
			setupW := httptest.NewRecorder()
			router.ServeHTTP(setupW, setupReq)
			assert.Equal(t, http.StatusOK, setupW.Code)
		})
	}
}

func TestAuth_SetupRejectedFromPrivateLanWithoutSecret(t *testing.T) {
	router, _ := createSetupServerWithoutSecret(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	for _, addr := range []string{"192.168.1.100:12345", "10.0.0.5:12345"} {
		t.Run(addr, func(t *testing.T) {
			setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
			setupReq.Header.Set("Content-Type", "application/json")
			setupReq.RemoteAddr = addr
			setupW := httptest.NewRecorder()
			router.ServeHTTP(setupW, setupReq)
			assert.Equal(t, http.StatusForbidden, setupW.Code)

			var errResp ErrorResponse
			require.NoError(t, json.Unmarshal(setupW.Body.Bytes(), &errResp))
			assert.Contains(t, errResp.Error, "trusted network")
		})
	}
}

func TestAuth_SetupRejectsSpoofedForwardedHeader(t *testing.T) {
	router, _ := createSetupServerWithoutSecret(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.Header.Set("X-Forwarded-For", "127.0.0.1")
	setupReq.RemoteAddr = "203.0.113.5:12345"
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusForbidden, setupW.Code)
}

func TestAuth_SetupAllowedWithCorrectBootstrapSecret(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.Header.Set("X-Setup-Secret", "test-bootstrap-secret")
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusOK, setupW.Code)
}

func TestAuth_SetupRejectedWithWrongBootstrapSecret(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	body := map[string]string{"username": "admin", "password": "password123"}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewReader(bodyBytes))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.Header.Set("X-Setup-Secret", "wrong-secret")
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	assert.Equal(t, http.StatusForbidden, setupW.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(setupW.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "bootstrap secret")
}

func TestAuth_SetupAlreadyInitializedReturns409(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	setupAgainReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupAgainW := httptest.NewRecorder()
	router.ServeHTTP(setupAgainW, setupAgainReq)
	assert.Equal(t, http.StatusConflict, setupAgainW.Code)
}

func createTestApiTokenRepo(t *testing.T, deps *ServerDependencies) *database.ApiTokenRepository {
	t.Helper()
	return database.NewApiTokenRepository(deps.DB)
}

func bearerRequest(t *testing.T, method, path, token string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func setupTokenAuthTestServer(t *testing.T) (*gin.Engine, *ServerDependencies) {
	t.Helper()

	router, deps := setupAuthenticatedTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	return router, deps
}

func createTestToken(t *testing.T, deps *ServerDependencies) string {
	t.Helper()
	svc := token.NewTokenService(deps.ApiTokenRepo)
	_, fullToken, err := svc.Create("test-token")
	require.NoError(t, err)
	return fullToken
}

func TestRequireTokenOrSession_BearerTokenValid(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireTokenOrSession_BearerTokenInvalid(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", "jv_invalidtoken1234567890abcdef")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "invalid or revoked token")
}

func TestRequireTokenOrSession_NoBearerValidSession(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	sessionCookie := extractSessionCookie(t, setupW)

	protectedReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, sessionCookie)
	protectedW := httptest.NewRecorder()
	router.ServeHTTP(protectedW, protectedReq)
	assert.Equal(t, http.StatusOK, protectedW.Code)
}

func TestRequireTokenOrSession_NoBearerNoSession(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "authentication required")
}

func TestRequireTokenOrSession_AuthNotInitializedWithBearer(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	deps.ApiTokenRepo = createTestApiTokenRepo(t, deps)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	testRouter := gin.Default()
	testRouter.GET("/api/v1/test", RequireTokenOrSession(deps), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := bearerRequest(t, http.MethodGet, "/api/v1/test", "jv_sometoken1234567890abcdef")
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "authentication is not initialized")
}

func TestRequireTokenOrSession_AuthNotInitializedNoBearer(t *testing.T) {
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")

	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	deps.ApiTokenRepo = createTestApiTokenRepo(t, deps)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	testRouter := gin.Default()
	testRouter.GET("/api/v1/test", RequireTokenOrSession(deps), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRequireTokenOrSession_BearerTakesPrecedenceOverSession(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	sessionCookie := extractSessionCookie(t, setupW)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireTokenOrSession_MalformedAuthHeaderFallsBackToSession(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	sessionCookie := extractSessionCookie(t, setupW)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireTokenOrSession_RevokedToken(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	err = svc.Revoke(tokens[0].ID)
	require.NoError(t, err)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "invalid or revoked token")
}

func TestRequireTokenOrSession_TokenWithoutJvPrefix(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", "not_a_jv_token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "invalid or revoked token")
}

func TestTokenAuth_Revocation(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	err = svc.Revoke(tokens[0].ID)
	require.NoError(t, err)

	req2 := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestTokenAuth_LastUsedAt(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	assert.Nil(t, tokens[0].LastUsedAt)

	req := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	updated, err := deps.ApiTokenRepo.FindByID(tokens[0].ID)
	require.NoError(t, err)
	assert.NotNil(t, updated.LastUsedAt)
	assert.WithinDuration(t, time.Now(), *updated.LastUsedAt, 5*time.Second)
}

func TestTokenAuth_LastUsedAtNotUpdatedForSession(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	createTestToken(t, deps)

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	sessionCookie := extractSessionCookie(t, setupW)

	protectedReq := newJSONRequest(t, http.MethodGet, "/api/v1/config", nil, sessionCookie)
	protectedW := httptest.NewRecorder()
	router.ServeHTTP(protectedW, protectedReq)
	assert.Equal(t, http.StatusOK, protectedW.Code)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	assert.Nil(t, tokens[0].LastUsedAt)
}

func TestRequireAuthenticated_NilDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRouter := gin.New()
	called := false
	testRouter.GET("/test", requireAuthenticated(nil), func(c *gin.Context) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestRequireAuthenticated_ExportedWrapper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRouter := gin.New()
	called := false
	testRouter.GET("/test", RequireAuthenticated(nil), func(c *gin.Context) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestRequireTokenOrSession_ExportedWrapper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRouter := gin.New()
	called := false
	testRouter.GET("/test", RequireTokenOrSession(nil), func(c *gin.Context) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestRequireTokenOrSession_EmptyBearerValue(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "invalid or revoked token")
}

func TestRequireTokenOrSession_EmptyBearerValueNoSession(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireTokenOrSession_WhitespaceOnlyToken(t *testing.T) {
	router, _ := setupTokenAuthTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer    ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "invalid or revoked token")
}

func TestRequireTokenOrSession_ContextValuesSetOnTokenAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	expectedTokenID := tokens[0].ID

	var capturedAuthMethod, capturedTokenID, capturedUsername string
	testRouter := gin.New()
	testRouter.GET("/api/v1/test", RequireTokenOrSession(deps), func(c *gin.Context) {
		if v, exists := c.Get("auth_method"); exists {
			capturedAuthMethod = v.(string)
		}
		if v, exists := c.Get("token_id"); exists {
			capturedTokenID = v.(string)
		}
		if v, exists := c.Get("auth_username"); exists {
			capturedUsername = v.(string)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := bearerRequest(t, http.MethodGet, "/api/v1/test", fullToken)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "token", capturedAuthMethod)
	assert.Equal(t, expectedTokenID, capturedTokenID)
	assert.Equal(t, "api_token", capturedUsername)
}

func TestRequireTokenOrSession_ContextValuesSetOnSessionAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	deps.ApiTokenRepo = createTestApiTokenRepo(t, deps)
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	var capturedAuthMethod, capturedUsername string
	testRouter := gin.New()
	testRouter.GET("/api/v1/test", RequireTokenOrSession(deps), func(c *gin.Context) {
		if v, exists := c.Get("auth_method"); exists {
			capturedAuthMethod = v.(string)
		}
		if v, exists := c.Get("auth_username"); exists {
			capturedUsername = v.(string)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()

	setupRouter := NewServer(deps)
	setupRouter.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)
	sessionCookie := extractSessionCookie(t, setupW)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "session", capturedAuthMethod)
	assert.Equal(t, "admin", capturedUsername)

	t.Cleanup(func() {
		cleanupServerHub(t, deps)
		_ = deps.DB.Close()
	})
}

func TestRequireTokenOrSession_MultipleTokensSameName(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	_, token1, err := svc.Create("same-name")
	require.NoError(t, err)
	_, token2, err := svc.Create("same-name")
	require.NoError(t, err)

	req1 := bearerRequest(t, http.MethodGet, "/api/v1/config", token1)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	req2 := bearerRequest(t, http.MethodGet, "/api/v1/config", token2)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestRequireTokenOrSession_RegeneratedTokenOldFails(t *testing.T) {
	router, deps := setupTokenAuthTestServer(t)
	fullToken := createTestToken(t, deps)

	svc := token.NewTokenService(deps.ApiTokenRepo)
	tokens, err := svc.List()
	require.NoError(t, err)
	require.NotEmpty(t, tokens)

	req1 := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	_, newFullToken, err := svc.Regenerate(tokens[0].ID)
	require.NoError(t, err)

	req2 := bearerRequest(t, http.MethodGet, "/api/v1/config", fullToken)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)

	req3 := bearerRequest(t, http.MethodGet, "/api/v1/config", newFullToken)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestParseCIDRList(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		assert.Nil(t, parseCIDRList(""))
	})

	t.Run("single valid CIDR", func(t *testing.T) {
		result := parseCIDRList("10.0.0.0/8")
		require.Len(t, result, 1)
		assert.True(t, result[0].Contains(net.ParseIP("10.1.2.3")))
	})

	t.Run("multiple valid CIDRs", func(t *testing.T) {
		result := parseCIDRList("10.0.0.0/8,192.168.0.0/16")
		require.Len(t, result, 2)
	})

	t.Run("invalid CIDR skipped", func(t *testing.T) {
		result := parseCIDRList("10.0.0.0/8,not-a-cidr,192.168.0.0/16")
		require.Len(t, result, 2)
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		result := parseCIDRList(" 10.0.0.0/8 , 192.168.0.0/16 ")
		require.Len(t, result, 2)
	})

	t.Run("empty segments skipped", func(t *testing.T) {
		result := parseCIDRList("10.0.0.0/8,,192.168.0.0/16")
		require.Len(t, result, 2)
	})
}

func TestPeerIP(t *testing.T) {
	t.Run("extracts IP from host:port", func(t *testing.T) {
		assert.Equal(t, "192.168.1.1", peerIP("192.168.1.1:12345"))
	})

	t.Run("returns raw address when no port", func(t *testing.T) {
		assert.Equal(t, "192.168.1.1", peerIP("192.168.1.1"))
	})

	t.Run("handles IPv6 with brackets", func(t *testing.T) {
		assert.Equal(t, "::1", peerIP("[::1]:12345"))
	})
}

func TestIsTrustedClient(t *testing.T) {
	t.Run("loopback IPv4 is trusted", func(t *testing.T) {
		assert.True(t, isTrustedClient("127.0.0.1"))
	})

	t.Run("loopback IPv6 is trusted", func(t *testing.T) {
		assert.True(t, isTrustedClient("::1"))
	})

	t.Run("private IP is not trusted by default", func(t *testing.T) {
		assert.False(t, isTrustedClient("192.168.1.1"))
	})

	t.Run("invalid IP is not trusted", func(t *testing.T) {
		assert.False(t, isTrustedClient("not-an-ip"))
	})

	t.Run("IPv6 with brackets is trusted for loopback", func(t *testing.T) {
		assert.True(t, isTrustedClient("[::1]"))
	})
}

func TestRequireAuthenticated_InitializedWithValidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	require.NoError(t, manager.Setup("admin", "password123"))
	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	called := false
	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(deps), func(c *gin.Context) {
		called = true
		username, _ := c.Get("auth_username")
		assert.Equal(t, "admin", username)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAuthenticated_InitializedNoCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	require.NoError(t, manager.Setup("admin", "password123"))

	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(deps), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAuthenticated_NotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(deps), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRequireAuthenticated_InvalidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("JAVINIZER_SETUP_SECRET", "test-bootstrap-secret")
	cfg := config.DefaultConfig()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager

	require.NoError(t, manager.Setup("admin", "password123"))

	testRouter := gin.New()
	testRouter.GET("/test", requireAuthenticated(deps), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "invalid-session-id"})
	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
