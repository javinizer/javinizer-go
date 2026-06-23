package system

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- testProxy: direct mode success with verification token ---

func TestProxyCov_DirectSuccessWithToken(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	proxy := startTestForwardProxy(t)
	defer proxy.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: proxy.URL},
	}

	deps := newTestDeps(cfg, func(d *core.APIDeps) {
		d.TokenStore = core.NewTokenStore()
	})

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.ProxyTestRequest{
		Mode:      "direct",
		TargetURL: target.URL,
		Proxy: models.ProxyConfig{
			Enabled: true,
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.NotEmpty(t, response.VerificationToken, "should have a verification token when TokenStore is set")
	assert.Greater(t, response.TokenExpiresAt, int64(0))
}

// --- testProxy: direct mode success without token store ---

func TestProxyCov_DirectSuccessNoTokenStore(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	proxy := startTestForwardProxy(t)
	defer proxy.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: proxy.URL},
	}

	deps := newTestDeps(cfg)
	// TokenStore is nil — should not panic, just skip token generation

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.ProxyTestRequest{
		Mode:      "direct",
		TargetURL: target.URL,
		Proxy: models.ProxyConfig{
			Enabled: true,
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Empty(t, response.VerificationToken, "no token when TokenStore is nil")
}

// --- testProxy: default target URL when empty ---

func TestProxyCov_DefaultTargetURL(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	// Empty target_url should default to httpbin.org/ip
	reqBody := `{"mode":"direct","target_url":"","proxy":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Will fail because proxy isn't configured, but target URL should be set
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// --- testProxy: SSRF check blocks internal URL ---

func TestProxyCov_SSRFBlock(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	reqBody := `{"mode":"direct","target_url":"http://127.0.0.1:8080","proxy":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}

// --- testProxy: invalid mode (binding validation catches it) ---

func TestProxyCov_InvalidMode(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	reqBody := `{"mode":"unknown","target_url":"https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid proxy test request")
}

// --- testProxy: flaresolverr mode with proxy resolution ---

func TestProxyCov_FlareSolverrWithProxy(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","message":"Challenge solved!","solution":{"url":"https://example.com","status":200,"cookies":[],"response":"<html>test</html>"},"startTimestamp":0,"endTimestamp":0}`))
	}))
	defer fs.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://proxy:8080"},
	}

	deps := newTestDeps(cfg, func(d *core.APIDeps) {
		d.TokenStore = core.NewTokenStore()
	})

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","proxy":{"enabled":true},"flaresolverr":{"enabled":true,"url":"` + fs.URL + `","timeout":5}}`
	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.NotEmpty(t, response.FlareSolverrURL)
}

// --- testProxy: flaresolverr success with token ---

func TestProxyCov_FlareSolverrSuccessWithToken(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","message":"Challenge solved!","solution":{"url":"https://example.com","status":200,"cookies":[{"name":"cf","value":"token"}],"response":"<html>test</html>"},"startTimestamp":0,"endTimestamp":0}`))
	}))
	defer fs.Close()

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg, func(d *core.APIDeps) {
		d.TokenStore = core.NewTokenStore()
	})

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":true,"url":"` + fs.URL + `","timeout":5}}`
	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.NotEmpty(t, response.VerificationToken, "should have a verification token")
}

// --- testProxy: direct proxy with custom user agent ---

func TestProxyCov_DirectWithCustomUserAgent(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	var receivedUA string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	proxy := startTestForwardProxy(t)
	defer proxy.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: proxy.URL},
	}
	cfg.Scrapers.UserAgent = "TestAgent/1.0"

	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.ProxyTestRequest{
		Mode:      "direct",
		TargetURL: target.URL,
		Proxy: models.ProxyConfig{
			Enabled: true,
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "TestAgent/1.0", receivedUA)
}

// --- formatDirectProxyError: proxyconnect keyword ---

func TestProxyCov_FormatDirectProxyError_ProxyConnect(t *testing.T) {
	err := errors.New("proxyconnect tcp: method not allowed")
	result := formatDirectProxyError(err)
	assert.Contains(t, result, "not a forward proxy")
}

// --- formatDirectProxyError: regular error ---

func TestProxyCov_FormatDirectProxyError_RegularError(t *testing.T) {
	err := errors.New("connection refused")
	result := formatDirectProxyError(err)
	assert.NotContains(t, result, "not a forward proxy")
	assert.Contains(t, result, "direct proxy request failed")
}

// --- isValidHTTPURL: edge cases ---

func TestProxyCov_IsValidHTTPURL_EdgeCases(t *testing.T) {
	assert.True(t, isValidHTTPURL("http://localhost"))
	assert.True(t, isValidHTTPURL("https://example.com:8443/path?q=1#frag"))
	assert.False(t, isValidHTTPURL(""))
	assert.False(t, isValidHTTPURL("ftp://files.example.com"))
	assert.False(t, isValidHTTPURL("not-a-url"))
	assert.False(t, isValidHTTPURL("http://"))
}

// TestProxyCov_DirectTokenScopeIsGlobal verifies that a successful direct proxy
// test issues a verification token with scope "global" (not "direct"), so that
// it can be validated by config-save validation which only accepts "global" and
// "flaresolverr" scopes.
func TestProxyCov_DirectTokenScopeIsGlobal(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	proxy := startTestForwardProxy(t)
	defer proxy.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: proxy.URL},
	}

	tokenStore := core.NewTokenStore()
	deps := newTestDeps(cfg, func(d *core.APIDeps) {
		d.TokenStore = tokenStore
	})

	router := gin.New()
	router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(contracts.ProxyTestRequest{
		Mode:      "direct",
		TargetURL: target.URL,
		Proxy: models.ProxyConfig{
			Enabled: true,
			Profile: "main", // Frontend sends the default profile name here, not DefaultProfile
			Profiles: map[string]models.ProxyProfile{
				"main": {URL: proxy.URL},
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.VerificationToken)

	// The token must validate with scope "global", not "direct" — config-save
	// validation (config.go) only checks "global" and "flaresolverr" scopes.
	// The hash must match what the save endpoint would compute: the frontend
	// sends Profile="main" in the test request, but the saved config stores
	// it as DefaultProfile. The test endpoint normalizes Profile→DefaultProfile
	// before hashing, so the hash matches newCfg.Scrapers.Proxy.
	saveProxyCfg := models.ProxyConfig{
		Enabled:        true,
		DefaultProfile: "main",
		Profiles: map[string]models.ProxyProfile{
			"main": {URL: proxy.URL},
		},
	}
	hash, _ := core.HashProxyConfig(saveProxyCfg)
	assert.True(t, tokenStore.Validate(response.VerificationToken, "global", hash),
		"direct proxy test token should validate with scope 'global' and match the save-endpoint hash")
	assert.False(t, tokenStore.Validate(response.VerificationToken, "direct", hash),
		"direct proxy test token should NOT have scope 'direct'")
}
