package httpclient

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient_WithTimeoutUncovered(t *testing.T) {
	client, err := NewHTTPClient(nil, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, client.Timeout)
}

func TestNewHTTPClient_SOCKS5ProxyUncovered(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL:      "socks5://localhost:1080",
		Username: "user",
		Password: "pass",
	}
	client, err := NewHTTPClient(proxyProfile, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.Timeout)
}

func TestNewRestyClientNoProxy_DefaultsUncovered(t *testing.T) {
	client := NewRestyClientNoProxy(45*time.Second, 7)
	require.NotNil(t, client)
	assert.Equal(t, 7, client.RetryCount)
}

func TestSanitizeProxyURL_NoCredentialsUncovered(t *testing.T) {
	result := SanitizeProxyURL("http://proxy.example.com:8080")
	assert.Equal(t, "http://proxy.example.com:8080", result)
}

func TestSanitizeProxyURL_EmptyStringUncovered(t *testing.T) {
	result := SanitizeProxyURL("")
	assert.Equal(t, "", result)
}

func TestSanitizeProxyURL_WhitespaceOnlyUncovered(t *testing.T) {
	result := SanitizeProxyURL("  ")
	assert.Equal(t, "", result)
}

func TestNormalizeProxyURL_HostPortUncovered(t *testing.T) {
	assert.Equal(t, "http://proxy:8080", normalizeProxyURL("proxy:8080"))
}

func TestNormalizeProxyURL_EmptyUncovered(t *testing.T) {
	assert.Equal(t, "", normalizeProxyURL(""))
}

func TestNormalizeProxyURL_WithSchemeUncovered(t *testing.T) {
	assert.Equal(t, "https://proxy:8080", normalizeProxyURL("https://proxy:8080"))
}

func TestNewTransport_SOCKS5WithAuthUncovered(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL:      "socks5://localhost:1080",
		Username: "socksuser",
		Password: "sockspass",
	}
	transport, err := NewTransport(proxyProfile)
	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.DialContext, "SOCKS5 should set DialContext")
	assert.Nil(t, transport.Proxy, "SOCKS5 should clear HTTP_PROXY")
}

func TestNewRestyClientWithFlareSolverr_DisabledUncovered(t *testing.T) {
	result, err := NewRestyClientWithFlareSolverr(nil, models.FlareSolverrConfig{Enabled: false}, 30*time.Second, 3)
	require.NoError(t, err)
	require.NotNil(t, result.Client)
	assert.Nil(t, result.FlareSolverr)
}

func TestNewRestyClientWithFlareSolverr_EnabledInvalidURLUncovered(t *testing.T) {
	fsCfg := models.FlareSolverrConfig{
		Enabled:    true,
		URL:        "",
		Timeout:    30,
		MaxRetries: 3,
	}
	result, err := NewRestyClientWithFlareSolverr(nil, fsCfg, 30*time.Second, 3)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestNewRestyClientWithFlareSolverr_EnabledValidURLUncovered(t *testing.T) {
	fsCfg := models.FlareSolverrConfig{
		Enabled:    true,
		URL:        "http://localhost:8191/v1",
		Timeout:    30,
		MaxRetries: 3,
	}
	result, err := NewRestyClientWithFlareSolverr(nil, fsCfg, 30*time.Second, 3)
	require.NoError(t, err)
	require.NotNil(t, result.Client)
	require.NotNil(t, result.FlareSolverr)
}

func TestBuildFlareSolverrRequestProxy_NilProfileUncovered(t *testing.T) {
	result := buildFlareSolverrRequestProxy(nil)
	assert.Nil(t, result)
}

func TestBuildFlareSolverrRequestProxy_EmptyURLUncovered(t *testing.T) {
	result := buildFlareSolverrRequestProxy(&models.ProxyProfile{URL: ""})
	assert.Nil(t, result)
}

func TestBuildFlareSolverrRequestProxy_CredentialsInURLUncovered(t *testing.T) {
	result := buildFlareSolverrRequestProxy(&models.ProxyProfile{
		URL: "http://user:pass@proxy.example.com:8080",
	})
	require.NotNil(t, result)
	assert.Equal(t, "http://proxy.example.com:8080", result.URL)
	assert.Equal(t, "user", result.Username)
	assert.Equal(t, "pass", result.Password)
}

func TestSecondsToMinutesUncovered(t *testing.T) {
	assert.Equal(t, 0, secondsToMinutes(0))
	assert.Equal(t, 0, secondsToMinutes(-1))
	assert.Equal(t, 1, secondsToMinutes(1))
	assert.Equal(t, 1, secondsToMinutes(60))
	assert.Equal(t, 2, secondsToMinutes(61))
}

func TestConvertFlareSolverrCookiesUncovered(t *testing.T) {
	resp := flareSolverrResponse{}
	resp.Solution.Cookies = []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}{
		{Name: "cf_clearance", Value: "abc123"},
	}
	cookies := convertFlareSolverrCookies(resp)
	assert.Len(t, cookies, 1)
	assert.Equal(t, "cf_clearance", cookies[0].Name)
	assert.Equal(t, "abc123", cookies[0].Value)
}

func TestNewFlareSolverr_EmptyURLUncovered(t *testing.T) {
	_, err := newFlareSolverr(&models.FlareSolverrConfig{URL: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL is required")
}

func TestNewFlareSolverr_ValidConfigUncovered(t *testing.T) {
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL:        "http://localhost:8191/v1",
		Timeout:    30,
		MaxRetries: 3,
	})
	require.NoError(t, err)
	require.NotNil(t, fs)
	assert.NoError(t, fs.Close())
}

func TestFlareSolverr_CloseUncovered(t *testing.T) {
	fs := &FlareSolverr{}
	assert.NoError(t, fs.Close())
}

func TestScraperClientBuilder_TimeoutOptionUncovered(t *testing.T) {
	client, err := newScraperClientBuilder().
		Apply(withTimeout(45 * time.Second)).
		build(false)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestScraperClientBuilder_GlobalProxyOptionUncovered(t *testing.T) {
	globalProxy := models.ProxyConfig{
		Enabled: true,
		Profiles: map[string]models.ProxyProfile{
			"default": {URL: "http://proxy.example.com:8080"},
		},
		DefaultProfile: "default",
	}
	client, err := newScraperClientBuilder().
		Apply(withGlobalProxy(globalProxy)).
		build(false)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestScraperClientBuilder_FlareSolverrFallbackUncovered(t *testing.T) {
	// FlareSolverr enabled but with invalid config — should fall back to plain client
	fsCfg := models.FlareSolverrConfig{
		Enabled:    true,
		URL:        "", // invalid — will cause fallback
		Timeout:    30,
		MaxRetries: 3,
	}
	client, err := newScraperClientBuilder().
		Apply(withFlareSolverr(true), withGlobalFlareSolverr(fsCfg)).
		build(false)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Nil(t, client.FlareSolverr, "should fall back to plain client when FlareSolverr creation fails")
}

func TestScraperClientBuilder_NilSettingsUncovered(t *testing.T) {
	builder := FromScraperSettings(nil, nil, models.FlareSolverrConfig{})
	require.NotNil(t, builder)
	client, err := builder.build(false)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestScraperClientBuilder_SettingsWithProxyUncovered(t *testing.T) {
	settings := &models.ScraperSettings{
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profiles: map[string]models.ProxyProfile{
				"scraper-proxy": {URL: "http://scraper-proxy:8080"},
			},
			DefaultProfile: "scraper-proxy",
		},
	}
	builder := FromScraperSettings(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, builder)
}

func TestIsTokenCharUncovered(t *testing.T) {
	assert.True(t, isTokenChar('A'))
	assert.True(t, isTokenChar('z'))
	assert.True(t, isTokenChar('0'))
	assert.True(t, isTokenChar('-'))
	assert.True(t, isTokenChar('_'))
	assert.True(t, isTokenChar('!'))
	assert.False(t, isTokenChar(' '))
	assert.False(t, isTokenChar('('))
}
