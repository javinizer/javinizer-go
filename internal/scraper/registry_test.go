package scraper

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultScraperRegistry_NilConfig(t *testing.T) {
	registry, err := NewDefaultScraperRegistry(nil, nil)

	assert.Error(t, err)
	assert.Nil(t, registry)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestNewDefaultScraperRegistry_ValidConfig(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	// Register a test scraper
	module := &testModule{
		name: "test-registry-scraper",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "test-registry-scraper", enabled: settings.Enabled}, nil
		}),
		defaults: config.ScraperSettings{Enabled: true},
	}
	scraperutil.RegisterModule(module)

	// Create config with scraper settings
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Overrides: map[string]*config.ScraperSettings{
				"test-registry-scraper": {Enabled: true},
			},
		},
	}

	registry, err := NewDefaultScraperRegistry(cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify scraper was registered
	scrapers := registry.GetAll()
	assert.Len(t, scrapers, 1, "Should have one registered scraper")
}

func TestNewDefaultScraperRegistry_SkipsNilConstructor(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	// Register a scraper with nil constructor
	module := &testModule{
		name:        "nil-constructor-scraper",
		constructor: nil,
		defaults:    config.ScraperSettings{Enabled: true},
	}
	scraperutil.RegisterModule(module)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Overrides: map[string]*config.ScraperSettings{
				"nil-constructor-scraper": {Enabled: true},
			},
		},
	}

	registry, err := NewDefaultScraperRegistry(cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify no scrapers were registered (nil constructor skipped)
	scrapers := registry.GetAll()
	assert.Empty(t, scrapers, "Should skip scrapers with nil constructor")
}

func TestNewDefaultScraperRegistry_SkipsMissingConfig(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	// Register a scraper
	module := &testModule{
		name: "no-config-scraper",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "no-config-scraper", enabled: true}, nil
		}),
		defaults: config.ScraperSettings{Enabled: true},
	}
	scraperutil.RegisterModule(module)

	// Create config without settings for this scraper
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Overrides: map[string]*config.ScraperSettings{
				// No entry for "no-config-scraper"
			},
		},
	}

	registry, err := NewDefaultScraperRegistry(cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify no scrapers were registered (missing config skipped)
	scrapers := registry.GetAll()
	assert.Empty(t, scrapers, "Should skip scrapers without config")
}

func TestNewDefaultScraperRegistry_SkipsConstructorError(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	// Register a scraper that returns error
	module := &testModule{
		name: "error-constructor-scraper",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return nil, errors.New("constructor error")
		}),
		defaults: config.ScraperSettings{Enabled: true},
	}
	scraperutil.RegisterModule(module)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Overrides: map[string]*config.ScraperSettings{
				"error-constructor-scraper": {Enabled: true},
			},
		},
	}

	registry, err := NewDefaultScraperRegistry(cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify no scrapers were registered (error constructor skipped)
	scrapers := registry.GetAll()
	assert.Empty(t, scrapers, "Should skip scrapers with constructor errors")
}

func TestNewDefaultScraperRegistry_MultipleScrapers(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	// Register multiple scrapers
	for i := 1; i <= 3; i++ {
		name := string(rune('A' + i - 1))
		module := &testModule{
			name: name,
			constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
				return &testScraper{name: name, enabled: settings.Enabled}, nil
			}),
			defaults: config.ScraperSettings{Enabled: true},
		}
		scraperutil.RegisterModule(module)
	}

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Overrides: map[string]*config.ScraperSettings{
				"A": {Enabled: true},
				"B": {Enabled: true},
				"C": {Enabled: true},
			},
		},
	}

	registry, err := NewDefaultScraperRegistry(cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAll()
	assert.Len(t, scrapers, 3, "Should register all valid scrapers")
}
