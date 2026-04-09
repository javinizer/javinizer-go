package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testModule struct {
	name        string
	constructor ScraperConstructor
	defaults    any
	priority    int
}

func (m *testModule) Name() string        { return m.name }
func (m *testModule) Description() string { return "Test " + m.name }
func (m *testModule) Constructor() any    { return m.constructor }
func (m *testModule) Validator() any      { return nil }
func (m *testModule) ConfigFactory() any  { return nil }
func (m *testModule) Options() any        { return nil }
func (m *testModule) Defaults() any       { return m.defaults }
func (m *testModule) Priority() int       { return m.priority }
func (m *testModule) FlattenFunc() any    { return nil }

func TestRegisterModule_DefaultSettingsRegistration(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	constructor := ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "settings-test", enabled: settings.Enabled}, nil
	})

	module := &testModule{
		name:        "settings-test",
		constructor: constructor,
		defaults:    config.ScraperSettings{Enabled: true, Language: "en"},
		priority:    75,
	}
	scraperutil.RegisterModule(module)

	constructors := GetScraperConstructors()
	assert.Contains(t, constructors, "settings-test")
	scraperInstance, err := constructors["settings-test"](config.ScraperSettings{Enabled: true}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "settings-test", scraperInstance.Name())

	defaults := GetRegisteredDefaults()
	assert.Contains(t, defaults, "settings-test")
	assert.Equal(t, 75, defaults["settings-test"].Priority)
	assert.Equal(t, "en", defaults["settings-test"].Settings.Language)
}

func TestRegisterModule_DuplicateNameOverwrites(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	module1 := &testModule{
		name: "dup-test",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "dup-test-constructor-A", enabled: settings.Enabled}, nil
		}),
	}
	scraperutil.RegisterModule(module1)

	module2 := &testModule{
		name: "dup-test",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "dup-test-constructor-B", enabled: settings.Enabled}, nil
		}),
	}
	scraperutil.RegisterModule(module2)

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("dup-test", settings, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
	assert.Equal(t, "dup-test-constructor-B", scraper.Name(), "Latest constructor should win for duplicate name")
}

func TestCreate_KnownScraper(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	module := &testModule{
		name: "test-scraper",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "test-scraper", enabled: settings.Enabled}, nil
		}),
	}
	scraperutil.RegisterModule(module)

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("test-scraper", settings, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
	assert.Equal(t, "test-scraper", scraper.Name())
	assert.True(t, scraper.IsEnabled())
}

func TestCreate_UnknownScraper(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("unknown-scraper", settings, nil, nil)

	assert.Error(t, err)
	assert.Nil(t, scraper)
	assert.Contains(t, err.Error(), "scraper not found:")
}

func TestResetAllRegistries_ClearsAll(t *testing.T) {
	module := &testModule{
		name: "reset-test",
		constructor: ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			return &testScraper{name: "reset-test", enabled: settings.Enabled}, nil
		}),
		defaults: config.ScraperSettings{Enabled: true},
		priority: 100,
	}
	scraperutil.RegisterModule(module)

	ResetAllRegistries()

	assert.Empty(t, GetScraperConstructors())
	assert.Empty(t, GetRegisteredDefaults())
	assert.Empty(t, scraperutil.GetDefaultScraperSettings())
}

type testScraper struct {
	name    string
	enabled bool
}

func (s *testScraper) Name() string { return s.name }
func (s *testScraper) Search(id string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *testScraper) GetURL(id string) (string, error) { return "", nil }
func (s *testScraper) IsEnabled() bool                  { return s.enabled }
func (s *testScraper) Config() *config.ScraperSettings  { return nil }
func (s *testScraper) Close() error                     { return nil }
