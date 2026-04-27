package javstash

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenFunc_WithValidConfig(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javstash")
	require.NotNil(t, fn)

	cfg := &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: true, RequestDelay: 1000}, Language: "en", BaseURL: "https://javstash.org/graphql", APIKey: "test-key"}
	result := fn(cfg)
	require.NotNil(t, result)

	settings, ok := result.(*config.ScraperSettings)
	require.True(t, ok)
	assert.True(t, settings.Enabled)
	assert.Equal(t, 1000, settings.RateLimit)
	assert.Equal(t, "en", settings.Language)
	assert.Equal(t, "https://javstash.org/graphql", settings.BaseURL)
	require.NotNil(t, settings.Extra)
	assert.Equal(t, "test-key", settings.Extra["api_key"])
}

func TestFlattenFunc_WithProxy(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javstash")
	require.NotNil(t, fn)

	proxyCfg := &config.ProxyConfig{Enabled: true, Profile: "test"}
	cfg := &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: true, RequestDelay: 500, Proxy: proxyCfg, DownloadProxy: proxyCfg}, APIKey: "test-key"}
	result := fn(cfg)
	require.NotNil(t, result)

	settings, ok := result.(*config.ScraperSettings)
	require.True(t, ok)
	assert.NotNil(t, settings.Proxy)
	assert.NotNil(t, settings.DownloadProxy)
}

func TestFlattenFunc_WithNonScraperConfig(t *testing.T) {
	fn := scraperutil.GetFlattenFunc("javstash")
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
			errMsg:  "javstash: config is nil",
		},
		{
			name: "disabled scraper is valid",
			cfg: &config.ScraperSettings{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled with api_key in config is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Extra: map[string]any{
					"api_key": "test-api-key",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled with api_key from env is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "enabled without api_key returns error",
			cfg: &config.ScraperSettings{
				Enabled: true,
			},
			wantErr: true,
			errMsg:  "javstash: api_key is required (set in config or JAVSTASH_API_KEY env var)",
		},
		{
			name: "RateLimit -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: -1,
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: true,
			errMsg:  "javstash: rate_limit must be non-negative, got -1",
		},
		{
			name: "RateLimit 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 0,
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "RetryCount -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: -1,
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: true,
			errMsg:  "javstash: retry_count must be non-negative, got -1",
		},
		{
			name: "Timeout -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: -1,
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: true,
			errMsg:  "javstash: timeout must be non-negative, got -1",
		},
		{
			name: "invalid base URL (no protocol) is invalid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				BaseURL: "javstash.com",
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: true,
			errMsg:  "javstash.base_url must be a valid HTTP or HTTPS URL",
		},
		{
			name: "valid http base URL is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				BaseURL: "http://javstash.com",
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "valid https base URL is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				BaseURL: "https://javstash.com",
				Extra: map[string]any{
					"api_key": "test-key",
				},
			},
			wantErr: false,
		},
	}

	c := &JavstashConfig{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "enabled with api_key from env is valid" {
				t.Setenv("JAVSTASH_API_KEY", "env-api-key")
			}
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
		config         *JavstashConfig
		getter         string
		expectedResult interface{}
	}{
		{
			name:           "IsEnabled returns true when enabled",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: true}},
			getter:         "IsEnabled",
			expectedResult: true,
		},
		{
			name:           "IsEnabled returns false when disabled",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Enabled: false}},
			getter:         "IsEnabled",
			expectedResult: false,
		},
		{
			name:           "GetUserAgent returns custom user agent",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{UserAgent: "custom-agent/1.0"}},
			getter:         "GetUserAgent",
			expectedResult: "custom-agent/1.0",
		},
		{
			name:           "GetUserAgent returns empty string when not set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{UserAgent: ""}},
			getter:         "GetUserAgent",
			expectedResult: "",
		},
		{
			name:           "GetRequestDelay returns configured delay",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{RequestDelay: 1000}},
			getter:         "GetRequestDelay",
			expectedResult: 1000,
		},
		{
			name:           "GetRequestDelay returns 0 when not set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{RequestDelay: 0}},
			getter:         "GetRequestDelay",
			expectedResult: 0,
		},
		{
			name:           "GetMaxRetries returns 0 from BaseScraperConfig default",
			config:         &JavstashConfig{},
			getter:         "GetMaxRetries",
			expectedResult: 0,
		},
		{
			name:           "GetProxy returns proxy config when set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Proxy: proxyConfig}},
			getter:         "GetProxy",
			expectedResult: proxyConfig,
		},
		{
			name:           "GetProxy returns nil when not set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{Proxy: nil}},
			getter:         "GetProxy",
			expectedResult: nil,
		},
		{
			name:           "GetDownloadProxy returns download proxy config when set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{DownloadProxy: downloadProxyConfig}},
			getter:         "GetDownloadProxy",
			expectedResult: downloadProxyConfig,
		},
		{
			name:           "GetDownloadProxy returns nil when not set",
			config:         &JavstashConfig{BaseScraperConfig: config.BaseScraperConfig{DownloadProxy: nil}},
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
