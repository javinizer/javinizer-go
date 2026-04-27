package javdb

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScraperConfig struct {
	enabled       bool
	requestDelay  int
	proxy         any
	downloadProxy any
}

func (m *mockScraperConfig) IsEnabled() bool       { return m.enabled }
func (m *mockScraperConfig) GetUserAgent() string  { return "" }
func (m *mockScraperConfig) GetRequestDelay() int  { return m.requestDelay }
func (m *mockScraperConfig) GetMaxRetries() int    { return 0 }
func (m *mockScraperConfig) GetProxy() any         { return m.proxy }
func (m *mockScraperConfig) GetDownloadProxy() any { return m.downloadProxy }

func TestFlattenFunc_WithValidConfig(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javdb")
	require.NotNil(t, fn)

	result := fn(&mockScraperConfig{enabled: true, requestDelay: 2000})
	require.NotNil(t, result)

	settings, ok := result.(*config.ScraperSettings)
	require.True(t, ok)
	assert.True(t, settings.Enabled)
	assert.Equal(t, 2000, settings.RateLimit)
	assert.Equal(t, "https://javdb.com", settings.BaseURL)
}

func TestFlattenFunc_WithProxy(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javdb")
	require.NotNil(t, fn)

	proxyCfg := &config.ProxyConfig{Enabled: true, Profile: "test"}
	result := fn(&mockScraperConfig{enabled: true, proxy: proxyCfg, downloadProxy: proxyCfg})
	require.NotNil(t, result)

	settings, ok := result.(*config.ScraperSettings)
	require.True(t, ok)
	assert.NotNil(t, settings.Proxy)
	assert.NotNil(t, settings.DownloadProxy)
}

func TestFlattenFunc_WithNonScraperConfig(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javdb")
	require.NotNil(t, fn)

	result := fn("not a config")
	assert.Nil(t, result)
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.ScraperSettings
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "javdb: config is nil",
		},
		{
			name: "disabled scraper is valid",
			cfg: &config.ScraperSettings{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "RateLimit -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: -1,
			},
			wantErr: true,
			errMsg:  "javdb: rate_limit must be non-negative, got -1",
		},
		{
			name: "RateLimit 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 0,
			},
			wantErr: false,
		},
		{
			name: "RateLimit 1000 is valid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
			},
			wantErr: false,
		},
		{
			name: "RetryCount -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: -1,
			},
			wantErr: true,
			errMsg:  "javdb: retry_count must be non-negative, got -1",
		},
		{
			name: "RetryCount 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: 0,
			},
			wantErr: false,
		},
		{
			name: "RetryCount 3 is valid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: 3,
			},
			wantErr: false,
		},
		{
			name: "Timeout -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: -1,
			},
			wantErr: true,
			errMsg:  "javdb: timeout must be non-negative, got -1",
		},
		{
			name: "Timeout 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: 0,
			},
			wantErr: false,
		},
		{
			name: "Timeout 30 is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: 30,
			},
			wantErr: false,
		},
		{
			name: "valid http base URL is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				BaseURL: "https://javdb.com",
			},
			wantErr: false,
		},
		{
			name: "invalid base URL (no protocol) is invalid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				BaseURL: "javdb.com",
			},
			wantErr: true,
			errMsg:  "javdb.base_url must be a valid HTTP or HTTPS URL",
		},
		{
			name: "FlareSolverr enabled is valid",
			cfg: &config.ScraperSettings{
				Enabled:         true,
				UseFlareSolverr: true,
			},
			wantErr: false,
		},
		{
			name: "FlareSolverr enabled with config is valid",
			cfg: &config.ScraperSettings{
				Enabled:         true,
				UseFlareSolverr: true,
			},
			wantErr: false,
		},
		{
			name: "all valid fields",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RateLimit:  500,
				RetryCount: 3,
				Timeout:    60,
			},
			wantErr: false,
		},
	}

	c := &JavDBConfig{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.ValidateConfig(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetterMethods(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		Profile: "default",
	}
	downloadProxyConfig := &config.ProxyConfig{
		Enabled: true,
		Profile: "download",
	}

	tests := []struct {
		name           string
		config         *JavDBConfig
		getter         string
		expectedResult interface{}
	}{
		{
			name:           "IsEnabled returns true when enabled",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: true}},
			getter:         "IsEnabled",
			expectedResult: true,
		},
		{
			name:           "IsEnabled returns false when disabled",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: false}},
			getter:         "IsEnabled",
			expectedResult: false,
		},
		{
			name:           "GetUserAgent returns custom user agent",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{UserAgent: "custom-agent/1.0"}},
			getter:         "GetUserAgent",
			expectedResult: "custom-agent/1.0",
		},
		{
			name:           "GetUserAgent returns empty string when not set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{UserAgent: ""}},
			getter:         "GetUserAgent",
			expectedResult: "",
		},
		{
			name:           "GetRequestDelay returns configured delay",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{RequestDelay: 1000}},
			getter:         "GetRequestDelay",
			expectedResult: 1000,
		},
		{
			name:           "GetRequestDelay returns 0 when not set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{RequestDelay: 0}},
			getter:         "GetRequestDelay",
			expectedResult: 0,
		},
		{
			name:           "GetMaxRetries returns configured retry count",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{MaxRetries: 3}},
			getter:         "GetMaxRetries",
			expectedResult: 3,
		},
		{
			name:           "GetMaxRetries returns 0 when not set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{MaxRetries: 0}},
			getter:         "GetMaxRetries",
			expectedResult: 0,
		},
		{
			name:           "GetProxy returns proxy config when set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{Proxy: proxyConfig}},
			getter:         "GetProxy",
			expectedResult: proxyConfig,
		},
		{
			name:           "GetProxy returns nil when not set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{Proxy: nil}},
			getter:         "GetProxy",
			expectedResult: nil,
		},
		{
			name:           "GetDownloadProxy returns download proxy config when set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{DownloadProxy: downloadProxyConfig}},
			getter:         "GetDownloadProxy",
			expectedResult: downloadProxyConfig,
		},
		{
			name:           "GetDownloadProxy returns nil when not set",
			config:         &JavDBConfig{BaseScraperConfig: config.BaseScraperConfig{DownloadProxy: nil}},
			getter:         "GetDownloadProxy",
			expectedResult: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.getter {
			case "IsEnabled":
				assert.Equal(t, tt.expectedResult, tt.config.IsEnabled())
			case "GetUserAgent":
				assert.Equal(t, tt.expectedResult, tt.config.GetUserAgent())
			case "GetRequestDelay":
				assert.Equal(t, tt.expectedResult, tt.config.GetRequestDelay())
			case "GetMaxRetries":
				assert.Equal(t, tt.expectedResult, tt.config.GetMaxRetries())
			case "GetProxy":
				if tt.expectedResult == nil {
					assert.Nil(t, tt.config.GetProxy())
				} else {
					assert.Equal(t, tt.expectedResult, tt.config.GetProxy())
				}
			case "GetDownloadProxy":
				if tt.expectedResult == nil {
					assert.Nil(t, tt.config.GetDownloadProxy())
				} else {
					assert.Equal(t, tt.expectedResult, tt.config.GetDownloadProxy())
				}
			}
		})
	}
}
