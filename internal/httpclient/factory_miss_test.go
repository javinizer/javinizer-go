package httpclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewTransport: SOCKS5 with auth (lines 80-91) ---
// The fallback DialContext path (lines 80-82) is unreachable in practice
// because proxy.SOCKS5 always returns a ContextDialer. We test the main path.

func TestMiss_NewTransport_SOCKS5WithAuthFull(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL:      "socks5://localhost:1080",
		Username: "socksuser",
		Password: "sockspass",
	}
	transport, err := NewTransport(proxyProfile)
	require.NoError(t, err)
	require.NotNil(t, transport)
	// SOCKS5 should set DialContext and clear Proxy
	assert.NotNil(t, transport.DialContext, "SOCKS5 should set DialContext")
	assert.Nil(t, transport.Proxy, "SOCKS5 should clear HTTP_PROXY")
}

// --- NewRestyClientNoProxy: transport creation succeeds (lines 146-149) ---
// This tests that the error branch in NewRestyClientNoProxy is not taken
// (since NewTransport(nil) should never fail).

func TestMiss_NewRestyClientNoProxy_TransportSet(t *testing.T) {
	client := NewRestyClientNoProxy(30*time.Second, 3)
	require.NotNil(t, client)
	// Verify transport was set (not the Resty default)
	transport, ok := client.GetClient().Transport.(*http.Transport)
	require.True(t, ok, "Expected *http.Transport")
	assert.Nil(t, transport.Proxy, "No-proxy transport should have nil Proxy")
}

// --- FlareSolverr: ResolveURL with session failure then one-off fallback (lines 256-259) ---

func TestMiss_FlareSolverr_ResolveURL_SessionCreateFailFallback(t *testing.T) {
	createCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			w.WriteHeader(500)
			return
		}
		cmd, _ := reqBody["cmd"].(string)
		switch cmd {
		case "sessions.create":
			createCount++
			if createCount <= 1 {
				// First session creation fails
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"error","message":"create failed"}`))
			} else {
				// Retry also fails
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"error","message":"create failed again"}`))
			}
		case "request.get":
			// One-off request (no session) succeeds
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"<html>one-off</html>","cookies":[{"name":"cf","value":"123"}]}}`))
		default:
			w.WriteHeader(400)
		}
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	html, cookies, err := fs.ResolveURL("https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "<html>one-off</html>", html)
	assert.Len(t, cookies, 1)
	assert.Equal(t, "cf", cookies[0].Name)
}

// --- FlareSolverr: resolveURLRequest non-200 status (lines 329-331) ---

func TestMiss_FlareSolverr_ResolveURLRequest_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	_, _, err = fs.resolveURLRequest("https://example.com", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

// --- FlareSolverr: resolveURLRequest status != "ok" (lines 333-335) ---

func TestMiss_FlareSolverr_ResolveURLRequest_StatusNotOk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","message":"something went wrong"}`))
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	_, _, err = fs.resolveURLRequest("https://example.com", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

// --- FlareSolverr: CreateSession non-ok response (lines 370-372) ---

func TestMiss_FlareSolverr_CreateSession_StatusNotOk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","message":"session creation failed"}`))
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	_, err = fs.CreateSession()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session creation failed")
}

// --- NewRestyClientWithFlareSolverr: enabled with proxy (line 450-452) ---

func TestMiss_NewRestyClientWithFlareSolverr_EnabledWithProxy(t *testing.T) {
	fsCfg := models.FlareSolverrConfig{
		Enabled:    true,
		URL:        "http://localhost:8191/v1",
		Timeout:    30,
		MaxRetries: 3,
	}
	proxyProfile := &models.ProxyProfile{
		URL:      "http://proxy:8080",
		Username: "user",
		Password: "pass",
	}
	result, err := NewRestyClientWithFlareSolverr(proxyProfile, fsCfg, 30*time.Second, 3)
	require.NoError(t, err)
	require.NotNil(t, result.Client)
	require.NotNil(t, result.FlareSolverr)
	// Verify the request proxy was set on the FlareSolverr instance
	require.NotNil(t, result.FlareSolverr.requestProxy)
	assert.Equal(t, "http://proxy:8080", result.FlareSolverr.requestProxy.URL)
	assert.Equal(t, "user", result.FlareSolverr.requestProxy.Username)
	assert.Equal(t, "pass", result.FlareSolverr.requestProxy.Password)
}

// --- FlareSolverr: resetPersistentSessionLocked with empty sessionID (lines 289-291) ---

func TestMiss_FlareSolverr_ResetPersistentSession_EmptySessionID(t *testing.T) {
	fs := &FlareSolverr{}
	// Should be a no-op with empty sessionID
	fs.resetPersistentSessionLocked("")
	assert.Equal(t, "", fs.persistentSessionID)
}

// --- FlareSolverr: ResolveURLWithSession caches cookies (lines 307-310) ---

func TestMiss_FlareSolverr_ResolveURLWithSession_CachedCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			w.WriteHeader(500)
			return
		}
		cmd, _ := reqBody["cmd"].(string)
		switch cmd {
		case "sessions.create":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","session":"test-session"}`))
		case "request.get":
			// Return response WITHOUT cookies on first call
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"<html>content</html>","cookies":[]},"session":"test-session"}`))
		case "sessions.destroy":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(400)
		}
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	// Create a session and store cookies in it
	fs.persistentSessionID = "test-session"
	fs.sessions.Store("test-session", &flareSolverrSession{
		Token:   "test-session",
		Cookies: []http.Cookie{{Name: "cached", Value: "cookie"}},
	})

	html, cookies, err := fs.ResolveURLWithSession("https://example.com", "test-session")
	require.NoError(t, err)
	assert.Equal(t, "<html>content</html>", html)
	// Should return cached cookies since response had none
	assert.Len(t, cookies, 1)
	assert.Equal(t, "cached", cookies[0].Name)
}

// --- FlareSolverr: ResolveURL persistent session retry then one-off success ---

func TestMiss_FlareSolverr_ResolveURL_PersistentSessionRetryOneOff(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			w.WriteHeader(500)
			return
		}
		cmd, _ := reqBody["cmd"].(string)
		switch cmd {
		case "sessions.create":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","session":"retry-session"}`))
		case "request.get":
			// All session requests fail; one-off succeeds
			sid, _ := reqBody["session"].(string)
			if sid != "" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"error","message":"session expired"}`))
			} else {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"<html>one-off-fallback</html>","cookies":[]}}`))
			}
		case "sessions.destroy":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(400)
		}
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	html, _, err := fs.ResolveURL("https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "<html>one-off-fallback</html>", html)
}
