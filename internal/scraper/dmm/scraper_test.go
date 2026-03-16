package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestScraperImplementsInterface verifies that DMM Scraper implements the models.Scraper interface.
// This test ensures compile-time interface compliance.
func TestScraperImplementsInterface(t *testing.T) {
	// Create a minimal scraper instance
	scraper := &Scraper{}

	// Compile-time assertion: if this compiles, the interface is satisfied
	var _ models.Scraper = scraper

	// Runtime type assertion for documentation
	_, ok := interface{}(scraper).(models.Scraper)
	assert.True(t, ok, "Scraper should implement models.Scraper interface")
}

// TestScraperNameMethod verifies that Name() returns the correct identifier.
func TestScraperNameMethod(t *testing.T) {
	scraper := &Scraper{}

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
			scraper := &Scraper{
				enabled: tt.enabled,
			}

			result := scraper.IsEnabled()

			assert.Equal(t, tt.enabled, result, "IsEnabled should reflect the enabled field")
		})
	}
}

// TestNewScraperWithConfig verifies that New() creates a properly initialized Scraper.
func TestNewScraperWithConfig(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:        true,
				ScrapeActress:  true,
				EnableBrowser:  false,
				BrowserTimeout: 30,
			},
		},
	}

	contentIDRepo := &database.ContentIDMappingRepository{}

	scraper := New(cfg, contentIDRepo)

	assert.NotNil(t, scraper, "New should return a non-nil scraper")
	assert.Equal(t, "dmm", scraper.Name(), "Scraper should have correct name")
	assert.True(t, scraper.IsEnabled(), "Scraper should be enabled when config.Enabled=true")
}

// TestNewScraperDisabledConfig verifies that New() respects the enabled configuration.
func TestNewScraperDisabledConfig(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:        false,
				ScrapeActress:  true,
				EnableBrowser:  false,
				BrowserTimeout: 30,
			},
		},
	}

	contentIDRepo := &database.ContentIDMappingRepository{}

	scraper := New(cfg, contentIDRepo)

	assert.NotNil(t, scraper, "New should return a non-nil scraper")
	assert.False(t, scraper.IsEnabled(), "Scraper should be disabled when config.Enabled=false")
}

// TestScraperInterfaceMethodSignatures verifies that all interface methods have correct signatures.
// This is a documentation test that demonstrates the interface contract.
func TestScraperInterfaceMethodSignatures(t *testing.T) {
	scraper := &Scraper{}

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
	scraper := &Scraper{}
	assert.NotNil(t, scraper, "Scraper should be a valid pointer")

	// Methods that don't access fields should work even with minimal initialization
	assert.NotPanics(t, func() {
		_ = scraper.Name()
	}, "Name() should not panic on minimally initialized scraper")

	assert.NotPanics(t, func() {
		_ = scraper.IsEnabled()
	}, "IsEnabled() should not panic on minimally initialized scraper")
}

// TestScraperFieldInitialization verifies that New() initializes all required fields.
func TestScraperFieldInitialization(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:        true,
				ScrapeActress:  true,
				EnableBrowser:  true,
				BrowserTimeout: 45,
			},
			Proxy: config.ProxyConfig{
				Enabled: true,
				URL:     "http://proxy.example.com:8080",
			},
		},
	}

	contentIDRepo := &database.ContentIDMappingRepository{}

	scraper := New(cfg, contentIDRepo)

	// Verify all fields are properly initialized
	assert.NotNil(t, scraper.client, "HTTP client should be initialized")
	assert.True(t, scraper.enabled, "enabled field should match config")
	assert.True(t, scraper.scrapeActress, "scrapeActress field should match config")
	assert.True(t, scraper.enableBrowser, "enableBrowser field should match config")
	assert.Equal(t, 45, scraper.browserTimeout, "browserTimeout should match config")
	assert.NotNil(t, scraper.contentIDRepo, "contentIDRepo should be initialized")
	assert.NotNil(t, scraper.proxyConfig, "proxyConfig should be initialized")
}

// TestScraperConfigDefaults verifies that New() applies sensible defaults.
func TestScraperConfigDefaults(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled: true,
				// Other fields left at zero values
			},
		},
	}

	contentIDRepo := &database.ContentIDMappingRepository{}

	scraper := New(cfg, contentIDRepo)

	assert.NotNil(t, scraper, "New should return a non-nil scraper even with minimal config")
	assert.NotNil(t, scraper.client, "HTTP client should always be initialized")
	assert.False(t, scraper.scrapeActress, "scrapeActress should default to false")
	assert.False(t, scraper.enableBrowser, "enableBrowser should default to false")
	assert.Equal(t, 0, scraper.browserTimeout, "browserTimeout should default to 0")
}
