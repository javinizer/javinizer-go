package httpclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMediaReferer_JavDB(t *testing.T) {
	assert.Equal(t, "https://javdb.com/", ResolveMediaReferer("https://jdbstatic.com/image.jpg", ""))
}

func TestResolveMediaReferer_JavBus(t *testing.T) {
	assert.Equal(t, "https://www.javbus.com/", ResolveMediaReferer("https://pics.javbus.com/img.jpg", ""))
}

func TestResolveMediaReferer_DMM(t *testing.T) {
	assert.Equal(t, "https://www.dmm.co.jp/", ResolveMediaReferer("https://pics.dmm.co.jp/img.jpg", ""))
}

func TestResolveMediaReferer_Caribbeancom(t *testing.T) {
	assert.Equal(t, "https://www.caribbeancom.com/", ResolveMediaReferer("https://www.caribbeancom.com/img.jpg", ""))
}

func TestResolveMediaReferer_AVEntertainments(t *testing.T) {
	assert.Equal(t, "https://www.aventertainments.com/", ResolveMediaReferer("https://www.aventertainments.com/img.jpg", ""))
}

func TestResolveMediaReferer_LibreDMM(t *testing.T) {
	assert.Equal(t, "https://www.libredmm.com/", ResolveMediaReferer("https://www.libredmm.com/img.jpg", ""))
}

func TestResolveMediaReferer_ConfiguredFallback(t *testing.T) {
	assert.Equal(t, "https://custom.referer/", ResolveMediaReferer("https://unknown.host/img.jpg", "https://custom.referer/"))
}

func TestResolveMediaReferer_OriginFallback(t *testing.T) {
	assert.Equal(t, "https://example.com/", ResolveMediaReferer("https://example.com/path/img.jpg", ""))
}

func TestResolveMediaReferer_InvalidURL(t *testing.T) {
	assert.Equal(t, "https://fallback/", ResolveMediaReferer("://invalid", "https://fallback/"))
}

func TestResolveMediaReferer_NoSchemeNoReferer(t *testing.T) {
	assert.Equal(t, "", ResolveMediaReferer("notaurl", ""))
}

func TestBuildClient_Error(t *testing.T) {
	b := newScraperClientBuilder()
	b.config.retryCount = 0
	client, err := b.BuildClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestBuildWithFlareSolverr_Error(t *testing.T) {
	b := newScraperClientBuilder()
	client, fs, err := b.BuildWithFlareSolverr()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Nil(t, fs)
}

func TestBuildWithProxy_ReturnsProfile(t *testing.T) {
	proxy := models.ProxyConfig{
		Enabled:        true,
		Profiles:       map[string]models.ProxyProfile{"default": {URL: "http://proxy:8080"}},
		DefaultProfile: "default",
	}
	b := newScraperClientBuilder().Apply(withGlobalProxy(proxy))
	client, profile, err := b.BuildWithProxy()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, profile)
	assert.Equal(t, "http://proxy:8080", profile.URL)
}

func TestBuildWithProxy_NoProxy(t *testing.T) {
	b := newScraperClientBuilder()
	client, profile, err := b.BuildWithProxy()
	require.NoError(t, err)
	require.NotNil(t, client)
	// No proxy configured: profile is non-nil but has empty URL
	assert.NotNil(t, profile)
}

func TestInitScraperClient_WithProxyEnabled(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
		Proxy: &models.ProxyConfig{
			Enabled:        true,
			Profiles:       map[string]models.ProxyProfile{"default": {URL: "http://proxy:8080"}},
			DefaultProfile: "default",
		},
	}
	globalProxy := &models.ProxyConfig{Enabled: false}
	result := InitScraperClient(settings, globalProxy, models.FlareSolverrConfig{})
	require.NotNil(t, result)
	assert.True(t, result.ProxyEnabled)
}

func TestInitScraperClient_GlobalProxyEnabled(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30, RetryCount: 3}
	globalProxy := &models.ProxyConfig{
		Enabled:        true,
		Profiles:       map[string]models.ProxyProfile{"default": {URL: "http://proxy:8080"}},
		DefaultProfile: "default",
	}
	result := InitScraperClient(settings, globalProxy, models.FlareSolverrConfig{})
	require.NotNil(t, result)
	assert.True(t, result.ProxyEnabled)
}

func TestInitScraperClient_InvalidProxyFallback(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
		Proxy: &models.ProxyConfig{
			Enabled:        true,
			Profiles:       map[string]models.ProxyProfile{"default": {URL: "://bad-url"}},
			DefaultProfile: "default",
		},
	}
	result := InitScraperClient(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, result)
	require.NotNil(t, result.Client)
}

func TestFlareSolverr_ResolveURL_PersistentSessionRetry(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"status":"ok","session":"test-session"}`))
		case "request.get":
			if callCount <= 2 {
				// First request with session fails
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"error","message":"session expired"}`))
			} else {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"<html>content</html>","cookies":[]}}`))
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

	html, cookies, err := fs.ResolveURL("https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "<html>content</html>", html)
	assert.Empty(t, cookies)
}

func TestFlareSolverr_DestroySession_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection to cause transport error
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	defer server.Close()

	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        server.URL,
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)

	err = fs.DestroySession("test-session")
	// Transport error from hijacked connection
	assert.Error(t, err)
}

func TestFlareSolverr_CreateSession_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
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
}
