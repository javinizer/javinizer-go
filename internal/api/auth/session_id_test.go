package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionIDFromRequest_Sources exercises every branch of
// sessionIDFromRequest: cookie wins, header fallback, query fallback, the
// empty case, and whitespace-only values that fall through to the next source.
// macOS WKWebView does not reliably persist cookies, so the desktop client
// sends the session ID via the X-Session-ID header (and <img> tags append it
// as ?session=) — these fallbacks are the desktop auth path under test.
func TestSessionIDFromRequest_Sources(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		cookie string
		header string
		query  string
		want   string
	}{
		{"cookie takes precedence", "cookie-sid", "header-sid", "query-sid", "cookie-sid"},
		{"header fallback when no cookie", "", "header-sid", "query-sid", "header-sid"},
		{"query fallback when no cookie or header", "", "", "query-sid", "query-sid"},
		{"empty when no source", "", "", "", ""},
		{"whitespace-only cookie falls through to header", "   ", "header-sid", "", "header-sid"},
		{"whitespace-only header falls through to query", "", "  ", "query-sid", "query-sid"},
		{"whitespace-only query returns empty", "", "", "  ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			target := "/api/v1/auth/status"
			if tc.query != "" {
				target += "?" + url.Values{"session": {tc.query}}.Encode()
			}
			c.Request = httptest.NewRequest(http.MethodGet, target, nil)
			if tc.cookie != "" {
				c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.cookie})
			}
			if tc.header != "" {
				c.Request.Header.Set("X-Session-ID", tc.header)
			}

			got := sessionIDFromRequest(c)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGetAuthStatus_AuthenticatesViaSessionIDHeader drives getAuthStatus
// through the public /api/v1/auth/status route authenticating with the
// X-Session-ID header alone (no cookie), mirroring the desktop webview path.
func TestGetAuthStatus_AuthenticatesViaSessionIDHeader(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	sessionID := setupSession(t, router)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	statusReq.Header.Set("X-Session-ID", sessionID)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)

	require.Equal(t, http.StatusOK, statusW.Code)
	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "admin", status.Username)
}

// TestGetAuthStatus_AuthenticatesViaSessionQuery drives getAuthStatus
// authenticating with the ?session= query parameter, the path used by <img>
// tags in the desktop SPA that cannot set headers.
func TestGetAuthStatus_AuthenticatesViaSessionQuery(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	sessionID := setupSession(t, router)

	target := "/api/v1/auth/status?" + url.Values{"session": {sessionID}}.Encode()
	statusReq := httptest.NewRequest(http.MethodGet, target, nil)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)

	require.Equal(t, http.StatusOK, statusW.Code)
	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "admin", status.Username)
}

// TestGetAuthStatus_NoSessionUnauthenticated confirms the empty-source branch
// reports an initialized-but-unauthenticated status without error.
func TestGetAuthStatus_NoSessionUnauthenticated(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	setupSession(t, router)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)

	require.Equal(t, http.StatusOK, statusW.Code)
	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.False(t, status.Authenticated)
	assert.Empty(t, status.Username)
}

// TestLogout_ViaSessionIDHeader ends a session referenced only by the
// X-Session-ID header, then proves the session is no longer authenticatable.
func TestLogout_ViaSessionIDHeader(t *testing.T) {
	router, _ := setupAuthenticatedTestServer(t)

	sessionID := setupSession(t, router)

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.Header.Set("X-Session-ID", sessionID)
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	assert.Equal(t, http.StatusOK, logoutW.Code)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	statusReq.Header.Set("X-Session-ID", sessionID)
	statusW := httptest.NewRecorder()
	router.ServeHTTP(statusW, statusReq)

	require.Equal(t, http.StatusOK, statusW.Code)
	status := parseAuthStatus(t, statusW)
	assert.True(t, status.Initialized)
	assert.False(t, status.Authenticated)
	assert.Empty(t, status.Username)
}

// setupSession initializes admin credentials via /api/v1/auth/setup and returns
// the resulting session ID, exercising the same flow the desktop app performs
// on first run.
func setupSession(t *testing.T, router *gin.Engine) string {
	t.Helper()

	setupReq := newJSONRequest(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, nil)
	setupW := httptest.NewRecorder()
	router.ServeHTTP(setupW, setupReq)
	require.Equal(t, http.StatusOK, setupW.Code)

	return extractSessionCookie(t, setupW).Value
}
