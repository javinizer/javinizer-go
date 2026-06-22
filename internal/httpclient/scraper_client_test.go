package httpclient

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNewScraperHTTPClient_BasicConfig(t *testing.T) {
	cfg := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	client, err := newScraperHTTPClient(cfg, nil, models.FlareSolverrConfig{})
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewScraperHTTPClient_WithHeaders(t *testing.T) {
	cfg := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	client, err := newScraperHTTPClient(cfg, nil, models.FlareSolverrConfig{},
		WithScraperHeaders(map[string]string{"X-Custom": "test"}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewScraperHTTPClient_WithCookies(t *testing.T) {
	cfg := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	client, err := newScraperHTTPClient(cfg, nil, models.FlareSolverrConfig{},
		WithScraperCookies(map[string]string{"session": "abc123"}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewScraperHTTPClient_WithProxyProfile(t *testing.T) {
	cfg := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	client, err := newScraperHTTPClient(cfg, nil, models.FlareSolverrConfig{},
		WithProxyProfile(),
	)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestInitScraperClient_BasicConfig(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	result := InitScraperClient(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, result)
	assert.NotNil(t, result.Client)
	assert.False(t, result.ProxyEnabled)
}

func TestInitScraperClient_WithProxy(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "test",
			Profiles: map[string]models.ProxyProfile{
				"test": {URL: "http://proxy:8080"},
			},
		},
	}

	result := InitScraperClient(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, result)
	assert.NotNil(t, result.Client)
	assert.True(t, result.ProxyEnabled)
}

func TestInitScraperClient_WithGlobalProxy(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:    true,
		Timeout:    30,
		RetryCount: 3,
	}

	globalProxy := &models.ProxyConfig{
		Enabled: true,
		Profile: "test",
		Profiles: map[string]models.ProxyProfile{
			"test": {URL: "http://global-proxy:8080"},
		},
	}

	result := InitScraperClient(settings, globalProxy, models.FlareSolverrConfig{})
	assert.NotNil(t, result)
	assert.NotNil(t, result.Client)
	assert.True(t, result.ProxyEnabled)
}
