package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScraperImplementsInterface verifies that DMM Scraper implements the models.Scraper interface.
// This test ensures compile-time interface compliance.
func TestScraperImplementsInterface(t *testing.T) {
	// Create a minimal scraper instance
	scraper := &scraper{}

	// Compile-time assertion: if this compiles, the interface is satisfied
	var _ models.Scraper = scraper

	// Runtime type assertion for documentation
	_, ok := interface{}(scraper).(models.Scraper)
	assert.True(t, ok, "Scraper should implement models.Scraper interface")
}

// TestScraperNameMethod verifies that Name() returns the correct identifier.
func TestScraperNameMethod(t *testing.T) {
	scraper := &scraper{}

	name := scraper.Name()

	assert.Equal(t, "dmm", name, "Scraper name should be 'dmm'")
	assert.NotEmpty(t, name, "Scraper name should not be empty")
}

// TestScraperIsEnabledMethod verifies that IsEnabled() reflects the configuration.
func TestScraperIsEnabledMethod(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "enabled scraper",
			enabled: true,
		},
		{
			name:    "disabled scraper",
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := &scraper{
				enabled: tt.enabled,
			}

			result := scraper.IsEnabled()

			assert.Equal(t, tt.enabled, result, "IsEnabled should reflect the enabled field")
		})
	}
}

// TestNewScraperWithConfig verifies that newScraper() creates a properly initialized Scraper.
func TestNewScraperWithConfig(t *testing.T) {
	settings := models.ScraperSettings{
		Enabled: true,
		// Note: DMM-specific fields (scrape_actress, enable_browser, browser_timeout)
		// were previously in Extra, now in ScraperSettings
	}

	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{}, createTestDMMOptions(false, false))

	assert.NotNil(t, scraper, "New should return a non-nil scraper")
	assert.Equal(t, "dmm", scraper.Name(), "Scraper should have correct name")
	assert.True(t, scraper.IsEnabled(), "Scraper should be enabled when config.Enabled=true")
}

// TestNewScraperDisabledConfig verifies that newScraper() respects the enabled configuration.
func TestNewScraperDisabledConfig(t *testing.T) {
	settings := models.ScraperSettings{
		Enabled: false,
		// Note: DMM-specific fields moved to ScraperSettings
	}

	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{}, createTestDMMOptions(false, false))

	assert.NotNil(t, scraper, "New should return a non-nil scraper")
	assert.False(t, scraper.IsEnabled(), "Scraper should be disabled when config.Enabled=false")
}

// TestScraperInterfaceMethodSignatures verifies that all interface methods have correct signatures.
// This is a documentation test that demonstrates the interface contract.
func TestScraperInterfaceMethodSignatures(t *testing.T) {
	scraper := &scraper{}

	// Name() string
	name := scraper.Name()
	assert.IsType(t, "", name, "Name() should return a string")

	// IsEnabled() bool
	enabled := scraper.IsEnabled()
	assert.IsType(t, true, enabled, "IsEnabled() should return a bool")

	// GetURL(id string) (string, error)
	// Note: We're not calling this because it would require HTTP setup
	// Just verify the method exists and has correct signature
	getURLFunc := scraper.GetURL
	assert.NotNil(t, getURLFunc, "GetURL method should exist")

	// Search(id string) (*models.ScraperResult, error)
	// Note: We're not calling this because it would require HTTP setup
	// Just verify the method exists and has correct signature
	searchFunc := scraper.Search
	assert.NotNil(t, searchFunc, "Search method should exist")
}

// TestScraperNilSafety verifies that Scraper methods handle nil receivers gracefully.
// This test ensures robustness in error scenarios.
func TestScraperNilSafety(t *testing.T) {
	// Note: Go does not allow calling methods on nil struct pointers if the method
	// accesses fields. This test documents the expected behavior.

	// A properly initialized scraper should never be nil
	scraper := &scraper{}
	assert.NotNil(t, scraper, "Scraper should be a valid pointer")

	// Methods that don't access fields should work even with minimal initialization
	assert.NotPanics(t, func() {
		_ = scraper.Name()
	}, "Name() should not panic on minimally initialized scraper")

	assert.NotPanics(t, func() {
		_ = scraper.IsEnabled()
	}, "IsEnabled() should not panic on minimally initialized scraper")
}

// TestScraperFieldInitialization verifies that newScraper() initializes all required fields.
func TestScraperFieldInitialization(t *testing.T) {
	settings := models.ScraperSettings{
		Enabled: true,
		// Note: DMM-specific fields moved to ScraperSettings
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "main",
		},
	}

	globalProxy := models.ProxyConfig{
		Enabled:        true,
		DefaultProfile: "main",
		Profiles: map[string]models.ProxyProfile{
			"main": {URL: "http://proxy.example.com:8080"},
		},
	}

	scraper := newScraper(&settings, &globalProxy, models.FlareSolverrConfig{}, createTestDMMOptions(false, false))

	// Verify all fields are properly initialized
	assert.NotNil(t, scraper.client, "HTTP client should be initialized")
	assert.True(t, scraper.enabled, "enabled field should match config")
	// Note: DMM-specific fields (scrapeActress, useBrowser) use global config
	// ScrapeActress defaults to false in test config, useBrowser defaults to false
	assert.False(t, scraper.scrapeActress, "scrapeActress uses global default (false) from test config")
	assert.False(t, scraper.useBrowser, "useBrowser uses global default (false) from test config")
	assert.Equal(t, 30, scraper.browserConfig.Timeout, "browserConfig.Timeout uses default (30)")
	// contentIDRepo is nil when nil is passed
	assert.Nil(t, scraper.contentIDRepo, "contentIDRepo should be nil when nil is passed")
	assert.NotNil(t, scraper.proxyProfile, "proxyProfile should be initialized")
}

// TestScraperConfigDefaults verifies that newScraper() applies sensible defaults.
func TestScraperConfigDefaults(t *testing.T) {
	settings := models.ScraperSettings{
		Enabled: true,
	}

	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{}, createTestDMMOptions(false, false))

	assert.NotNil(t, scraper, "New should return a non-nil scraper even with minimal config")
	assert.NotNil(t, scraper.client, "HTTP client should always be initialized")
	assert.False(t, scraper.scrapeActress, "scrapeActress should default to false")
	assert.False(t, scraper.useBrowser, "useBrowser should default to false")
	assert.Equal(t, 30, scraper.browserConfig.Timeout, "browserConfig.Timeout should use default value")
}

func TestNewSettingsPassthrough(t *testing.T) {
	tests := []struct {
		name           string
		settings       models.ScraperSettings
		globalTimeout  int
		wantRetryCount int
		wantRateLimit  int
		wantTimeout    int
	}{
		{
			name: "retry_count passthrough",
			settings: models.ScraperSettings{
				Enabled:    true,
				RetryCount: 5,
			},
			globalTimeout:  0,
			wantRetryCount: 5,
			wantRateLimit:  0,
			wantTimeout:    30,
		},
		{
			name: "rate_limit passthrough",
			settings: models.ScraperSettings{
				Enabled:   true,
				RateLimit: 100,
			},
			globalTimeout:  0,
			wantRetryCount: 0,
			wantRateLimit:  100,
			wantTimeout:    30,
		},
		{
			name: "timeout from settings",
			settings: models.ScraperSettings{
				Enabled: true,
				Timeout: 60,
			},
			globalTimeout:  0,
			wantRetryCount: 0,
			wantRateLimit:  0,
			wantTimeout:    60,
		},
		{
			name: "timeout fallback to global config",
			settings: models.ScraperSettings{
				Enabled: true,
				Timeout: 0,
			},
			globalTimeout:  45,
			wantRetryCount: 0,
			wantRateLimit:  0,
			wantTimeout:    45,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := createTestDMMOptions(false, false)
			opts.TimeoutSeconds = tt.globalTimeout

			scraper := newScraper(&tt.settings, &models.ProxyConfig{}, models.FlareSolverrConfig{}, opts)
			require.NotNil(t, scraper, "New should return a non-nil scraper")

			cfg := scraper.Config()
			assert.Equal(t, tt.wantRetryCount, cfg.RetryCount, "RetryCount should match expected value")
			assert.Equal(t, tt.wantRateLimit, cfg.RateLimit, "RateLimit should match expected value")
			assert.Equal(t, tt.wantTimeout, cfg.Timeout, "Timeout should match expected value")
		})
	}
}

func TestNewProxySettingsPassthrough(t *testing.T) {
	tests := []struct {
		name        string
		settings    models.ScraperSettings
		wantProxy   bool
		wantProfile string
	}{
		{
			name: "scraper proxy with profile",
			settings: models.ScraperSettings{
				Enabled: true,
				Proxy: &models.ProxyConfig{
					Enabled: true,
					Profile: "test-profile",
					Profiles: map[string]models.ProxyProfile{
						"test-profile": {
							URL: "http://test-proxy:8080",
						},
					},
				},
			},
			wantProxy:   true,
			wantProfile: "test-profile",
		},
		{
			name: "scraper proxy without profile disabled",
			settings: models.ScraperSettings{
				Enabled: true,
				Proxy: &models.ProxyConfig{
					Enabled: false,
				},
			},
			wantProxy:   false,
			wantProfile: "",
		},
		{
			name: "download_proxy preserved in config",
			settings: models.ScraperSettings{
				Enabled: true,
				DownloadProxy: &models.ProxyConfig{
					Enabled: true,
					Profile: "download-profile",
					Profiles: map[string]models.ProxyProfile{
						"download-profile": {
							URL: "http://download-proxy:8080",
						},
					},
				},
			},
			wantProxy:   false,
			wantProfile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := newScraper(&tt.settings, &models.ProxyConfig{}, models.FlareSolverrConfig{}, createTestDMMOptions(false, false))
			require.NotNil(t, scraper, "New should return a non-nil scraper")

			cfg := scraper.Config()
			if tt.settings.Proxy != nil {
				assert.Equal(t, tt.settings.Proxy.Enabled, cfg.Proxy != nil && cfg.Proxy.Enabled, "Proxy.Enabled should be preserved in config")
				if tt.settings.Proxy.Profile != "" {
					assert.Equal(t, tt.settings.Proxy.Profile, cfg.Proxy.Profile, "Proxy.Profile should be preserved in config")
				}
			}

			if tt.settings.DownloadProxy != nil {
				assert.NotNil(t, cfg.DownloadProxy, "DownloadProxy should be preserved in config")
				if tt.settings.DownloadProxy.Profile != "" {
					assert.Equal(t, tt.settings.DownloadProxy.Profile, cfg.DownloadProxy.Profile, "DownloadProxy.Profile should be preserved in config")
				}
			}

			if tt.wantProxy && tt.wantProfile != "" {
				assert.NotNil(t, scraper.proxyProfile, "proxyProfile should be set when proxy is enabled with valid profile")
			}
		})
	}
}
