package scraperutil_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

type mockScraperConfig struct {
	enabled       bool
	userAgent     string
	requestDelay  int
	maxRetries    int
	proxy         any
	downloadProxy any
}

func (m *mockScraperConfig) IsEnabled() bool       { return m.enabled }
func (m *mockScraperConfig) GetUserAgent() string  { return m.userAgent }
func (m *mockScraperConfig) GetRequestDelay() int  { return m.requestDelay }
func (m *mockScraperConfig) GetMaxRetries() int    { return m.maxRetries }
func (m *mockScraperConfig) GetProxy() any         { return m.proxy }
func (m *mockScraperConfig) GetDownloadProxy() any { return m.downloadProxy }

func boolPtr(b bool) *bool { return &b }

func proxyAsConfig(p any) *config.ProxyConfig {
	if p == nil {
		return nil
	}
	return p.(*config.ProxyConfig)
}

func TestExtractFlattenedConfig(t *testing.T) {
	t.Run("extracts from ScraperConfigInterface", func(t *testing.T) {
		cfg := &mockScraperConfig{enabled: true, requestDelay: 500}
		fc, ok := scraperutil.ExtractFlattenedConfig(cfg)
		assert.True(t, ok)
		assert.Equal(t, true, fc.Enabled)
		assert.Equal(t, 500, fc.RateLimit)
	})

	t.Run("returns false for non-interface", func(t *testing.T) {
		fc, ok := scraperutil.ExtractFlattenedConfig("not a config")
		assert.False(t, ok)
		assert.Nil(t, fc)
	})

	t.Run("extracts proxy values", func(t *testing.T) {
		cfg := &mockScraperConfig{
			enabled:       true,
			proxy:         &config.ProxyConfig{Enabled: true},
			downloadProxy: &config.ProxyConfig{Enabled: false},
		}
		fc, ok := scraperutil.ExtractFlattenedConfig(cfg)
		assert.True(t, ok)
		assert.NotNil(t, fc.Proxy)
		assert.NotNil(t, fc.DownloadProxy)
	})
}

func TestDefaultFlattenConfig(t *testing.T) {
	builder := func(fc *scraperutil.FlattenedConfig, overrides scraperutil.FlattenOverrides) any {
		return &config.ScraperSettings{
			Enabled:       fc.Enabled,
			RateLimit:     fc.RateLimit,
			BaseURL:       overrides.BaseURL,
			Language:      overrides.Language,
			UseBrowser:    overrides.UseBrowser,
			ScrapeActress: overrides.ScrapeActress,
			Cookies:       overrides.Cookies,
			Extra:         overrides.Extra,
			Proxy:         proxyAsConfig(fc.Proxy),
			DownloadProxy: proxyAsConfig(fc.DownloadProxy),
		}
	}

	testCases := []struct {
		name      string
		cfg       any
		overrides scraperutil.FlattenOverrides
		want      *config.ScraperSettings
	}{
		{
			name:      "no overrides returns base settings with proxies nil",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
			},
		},
		{
			name:      "BaseURL override included",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 500},
			overrides: scraperutil.FlattenOverrides{BaseURL: "https://example.com"},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 500,
				BaseURL:   "https://example.com",
			},
		},
		{
			name:      "Language override included",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{Language: "ja"},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
				Language:  "ja",
			},
		},
		{
			name:      "Extra map merges extra fields",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{Extra: map[string]any{"api_key": "secret"}},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
				Extra:     map[string]any{"api_key": "secret"},
			},
		},
		{
			name:      "UseBrowser sets UseBrowser",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{UseBrowser: true},
			want: &config.ScraperSettings{
				Enabled:    true,
				RateLimit:  1000,
				UseBrowser: true,
			},
		},
		{
			name:      "ScrapeActress sets ScrapeActress pointer",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{ScrapeActress: boolPtr(true)},
			want: &config.ScraperSettings{
				Enabled:       true,
				RateLimit:     1000,
				ScrapeActress: boolPtr(true),
			},
		},
		{
			name:      "Cookies override sets Cookies",
			cfg:       &mockScraperConfig{enabled: true, requestDelay: 1000},
			overrides: scraperutil.FlattenOverrides{Cookies: map[string]string{"cf_clearance": "abc123"}},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
				Cookies:   map[string]string{"cf_clearance": "abc123"},
			},
		},
		{
			name:      "returns nil when cfg is not ScraperConfigInterface",
			cfg:       "not a config",
			overrides: scraperutil.FlattenOverrides{},
			want:      nil,
		},
		{
			name: "extracts proxy/downloadProxy from ScraperConfigInterface",
			cfg: &mockScraperConfig{
				enabled:       true,
				requestDelay:  1000,
				proxy:         &config.ProxyConfig{Enabled: true},
				downloadProxy: &config.ProxyConfig{Enabled: true},
			},
			overrides: scraperutil.FlattenOverrides{},
			want: &config.ScraperSettings{
				Enabled:       true,
				RateLimit:     1000,
				Proxy:         &config.ProxyConfig{Enabled: true},
				DownloadProxy: &config.ProxyConfig{Enabled: true},
			},
		},
		{
			name: "proxy=nil yields nil Proxy in result",
			cfg: &mockScraperConfig{
				enabled:       true,
				requestDelay:  1000,
				proxy:         nil,
				downloadProxy: nil,
			},
			overrides: scraperutil.FlattenOverrides{},
			want: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fn := scraperutil.DefaultFlattenConfig(tc.overrides, builder)
			got := fn(tc.cfg)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			gotSettings, ok := got.(*config.ScraperSettings)
			assert.True(t, ok, "expected *config.ScraperSettings, got %T", got)
			assert.Equal(t, tc.want.Enabled, gotSettings.Enabled)
			assert.Equal(t, tc.want.RateLimit, gotSettings.RateLimit)
			assert.Equal(t, tc.want.BaseURL, gotSettings.BaseURL)
			assert.Equal(t, tc.want.Language, gotSettings.Language)
			assert.Equal(t, tc.want.UseBrowser, gotSettings.UseBrowser)
			assert.Equal(t, tc.want.Extra, gotSettings.Extra)
			assert.Equal(t, tc.want.Cookies, gotSettings.Cookies)
			if tc.want.ScrapeActress != nil {
				assert.NotNil(t, gotSettings.ScrapeActress)
				assert.Equal(t, *tc.want.ScrapeActress, *gotSettings.ScrapeActress)
			} else {
				assert.Nil(t, gotSettings.ScrapeActress)
			}
			if tc.want.Proxy != nil {
				assert.NotNil(t, gotSettings.Proxy)
			} else {
				assert.Nil(t, gotSettings.Proxy)
			}
			if tc.want.DownloadProxy != nil {
				assert.NotNil(t, gotSettings.DownloadProxy)
			} else {
				assert.Nil(t, gotSettings.DownloadProxy)
			}
		})
	}
}

func TestDefaultFlattenConfigFromConfig(t *testing.T) {
	builder := func(fc *scraperutil.FlattenedConfig, overrides scraperutil.FlattenOverrides) any {
		return &config.ScraperSettings{
			Enabled:       fc.Enabled,
			RateLimit:     fc.RateLimit,
			BaseURL:       overrides.BaseURL,
			Language:      overrides.Language,
			Proxy:         proxyAsConfig(fc.Proxy),
			DownloadProxy: proxyAsConfig(fc.DownloadProxy),
		}
	}

	t.Run("builds settings from already-asserted interface", func(t *testing.T) {
		c := &mockScraperConfig{
			enabled:       true,
			requestDelay:  500,
			proxy:         &config.ProxyConfig{Enabled: true},
			downloadProxy: nil,
		}
		got := scraperutil.DefaultFlattenConfigFromConfig(c, scraperutil.FlattenOverrides{BaseURL: "https://example.com", Language: "en"}, builder)
		s := got.(*config.ScraperSettings)
		assert.Equal(t, true, s.Enabled)
		assert.Equal(t, 500, s.RateLimit)
		assert.Equal(t, "https://example.com", s.BaseURL)
		assert.Equal(t, "en", s.Language)
		assert.NotNil(t, s.Proxy)
		assert.Nil(t, s.DownloadProxy)
	})
}
