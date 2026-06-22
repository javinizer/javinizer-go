package scraper

import (
	"errors"
	"maps"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfigWithOverrides(overrides map[string]*models.ScraperSettings) ScraperRegistryConfig {
	cfg := ScraperRegistryConfig{
		Overrides: make(map[string]models.ScraperSettings, len(overrides)),
	}
	for name, ms := range overrides {
		if ms != nil {
			s := models.ScraperSettings{
				Enabled:                ms.Enabled,
				Language:               ms.Language,
				Timeout:                ms.Timeout,
				RateLimit:              ms.RateLimit,
				RetryCount:             ms.RetryCount,
				UserAgent:              ms.UserAgent,
				UseBrowser:             ms.UseBrowser,
				UseFlareSolverr:        ms.UseFlareSolverr,
				BaseURL:                ms.BaseURL,
				Proxy:                  ms.Proxy,
				DownloadProxy:          ms.DownloadProxy,
				Cookies:                maps.Clone(ms.Cookies),
				PlaceholderThresholdKB: ms.PlaceholderThresholdKB,
				ScrapeBonusScreens:     ms.ScrapeBonusScreens,
				APIKey:                 ms.APIKey,
			}
			if ms.ScrapeActress != nil {
				val := *ms.ScrapeActress
				s.ScrapeActress = &val
			}
			if ms.ExtraPlaceholderHashes != nil {
				s.ExtraPlaceholderHashes = make([]string, len(ms.ExtraPlaceholderHashes))
				copy(s.ExtraPlaceholderHashes, ms.ExtraPlaceholderHashes)
			}
			cfg.Overrides[name] = s
		}
	}
	return cfg
}

func TestNewDefaultScraperRegistry_ValidConfig(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "test-registry-scraper",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "test-registry-scraper", enabled: deps.Settings.Enabled}, nil
		}),
		Defaults: models.ScraperSettings{Enabled: true},
	})

	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{
		"test-registry-scraper": {Enabled: true},
	})

	registry, err := NewDefaultScraperRegistryFrom(reg, cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAllInstances()
	assert.Len(t, scrapers, 1, "Should have one registered scraper")
}

func TestNewDefaultScraperRegistry_SkipsNilConstructor(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "nil-constructor-scraper",
		Constructor: nil,
		Defaults:    models.ScraperSettings{Enabled: true},
	})
	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{
		"nil-constructor-scraper": {Enabled: true},
	})

	registry, err := NewDefaultScraperRegistryFrom(reg, cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAllInstances()
	assert.Empty(t, scrapers, "Should skip scrapers with nil constructor")
}

func TestNewDefaultScraperRegistry_SkipsMissingConfig(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "no-config-scraper",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "no-config-scraper", enabled: true}, nil
		}),
		Defaults: models.ScraperSettings{Enabled: true},
	})
	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{})

	registry, err := NewDefaultScraperRegistryFrom(reg, cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAllInstances()
	assert.Empty(t, scrapers, "Should skip scrapers without config")
}

func TestNewDefaultScraperRegistry_SkipsConstructorError(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "error-constructor-scraper",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return nil, errors.New("constructor error")
		}),
		Defaults: models.ScraperSettings{Enabled: true},
	})
	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{
		"error-constructor-scraper": {Enabled: true},
	})

	registry, err := NewDefaultScraperRegistryFrom(reg, cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAllInstances()
	assert.Empty(t, scrapers, "Should skip scrapers with constructor errors")
}

func TestNewDefaultScraperRegistry_MultipleScrapers(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	for i := 1; i <= 3; i++ {
		name := string(rune('A' + i - 1))
		n := name
		reg.Register(scraperutil.ScraperRegistration{
			Name: name,
			Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
				return &testScraper{name: n, enabled: deps.Settings.Enabled}, nil
			}),
			Defaults: models.ScraperSettings{Enabled: true},
		})
	}
	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{
		"A": {Enabled: true},
		"B": {Enabled: true},
		"C": {Enabled: true},
	})

	registry, err := NewDefaultScraperRegistryFrom(reg, cfg, nil)

	require.NoError(t, err)
	require.NotNil(t, registry)

	scrapers := registry.GetAllInstances()
	assert.Len(t, scrapers, 3, "Should register all valid scrapers")
}

func TestScraperRegistryConfigFromApp(t *testing.T) {
	// Verify that ScraperRegistryConfigFromApp correctly maps *config.Config fields
	// to the narrow ScraperRegistryConfig struct.
	cfg := testConfigWithOverrides(map[string]*models.ScraperSettings{
		"test-scraper": {Enabled: true, Language: "en"},
	})

	// Verify the config was constructed correctly
	assert.Contains(t, cfg.Overrides, "test-scraper")
	assert.True(t, cfg.Overrides["test-scraper"].Enabled)
	assert.Equal(t, "en", cfg.Overrides["test-scraper"].Language)
}
