package scraper

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testScraper struct {
	name    string
	enabled bool
}

func (s *testScraper) Name() string { return s.name }
func (s *testScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *testScraper) GetURL(_ context.Context, id string) (string, error) { return "", nil }
func (s *testScraper) IsEnabled() bool                                     { return s.enabled }
func (s *testScraper) Config() *models.ScraperSettings                     { return nil }
func (s *testScraper) Close() error                                        { return nil }

func TestRegisterModule_DefaultSettingsRegistration(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "settings-test",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "settings-test", enabled: deps.Settings.Enabled}, nil
		}),
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
		Priority: 75,
	})

	regEntry, ok := reg.Get("settings-test")
	require.True(t, ok)
	assert.NotNil(t, regEntry.Constructor)

	result, err := regEntry.Constructor(scraperutil.ScraperDeps{Settings: models.ScraperSettings{Enabled: true}})
	require.NoError(t, err)
	scraperInstance := result.(*testScraper)
	assert.Equal(t, "settings-test", scraperInstance.Name())

	defaults := reg.GetAllDefaults()
	assert.Contains(t, defaults, "settings-test")

	regDefaults := reg.GetDefaults()
	assert.Contains(t, regDefaults, "settings-test")
	assert.Equal(t, 75, regDefaults["settings-test"].Priority)
}

func TestRegisterModule_DuplicateNameOverwrites(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	reg.Register(scraperutil.ScraperRegistration{
		Name: "dup-test",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "dup-test-constructor-A"}, nil
		}),
	})

	reg.Register(scraperutil.ScraperRegistration{
		Name: "dup-test",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "dup-test-constructor-B"}, nil
		}),
	})

	regEntry, ok := reg.Get("dup-test")
	require.True(t, ok)

	result, err := regEntry.Constructor(scraperutil.ScraperDeps{Settings: models.ScraperSettings{Enabled: true}})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "dup-test-constructor-B", result.(*testScraper).Name(), "Latest constructor should win for duplicate name")
}

func TestCreate_KnownScraper(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "test-scraper",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "test-scraper", enabled: deps.Settings.Enabled}, nil
		}),
	})

	regEntry, ok := reg.Get("test-scraper")
	require.True(t, ok)

	result, err := regEntry.Constructor(scraperutil.ScraperDeps{Settings: models.ScraperSettings{Enabled: true}})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	scraper := result.(*testScraper)
	assert.Equal(t, "test-scraper", scraper.Name())
	assert.True(t, scraper.IsEnabled())
}

func TestCreate_UnknownScraper(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	_, ok := reg.Get("unknown-scraper")
	assert.False(t, ok, "unknown scraper should not be found")
}

func TestResetAllRegistries_ClearsAll(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "reset-test",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return &testScraper{name: "reset-test", enabled: deps.Settings.Enabled}, nil
		}),
		Defaults: models.ScraperSettings{Enabled: true},
		Priority: 100,
	})

	reg = scraperutil.NewScraperRegistry()

	assert.Empty(t, reg.GetAll())
	assert.Empty(t, reg.GetDefaults())
}

func TestGetScraperConstructors_ReturnsAll(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	for i := 1; i <= 3; i++ {
		name := string(rune('A' + i - 1))
		n := name
		reg.Register(scraperutil.ScraperRegistration{
			Name: name,
			Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
				return &testScraper{name: n, enabled: deps.Settings.Enabled}, nil
			}),
		})
	}

	all := reg.GetAll()

	assert.Len(t, all, 3, "Should return all registered constructors")
	assert.Contains(t, all, "A")
	assert.Contains(t, all, "B")
	assert.Contains(t, all, "C")
}

func TestGetScraperConstructors_EmptyRegistry(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	all := reg.GetAll()
	assert.Empty(t, all, "Empty registry should return empty map")
}

func TestGetRegisteredDefaults_ReturnsAll(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "scraper1",
		Defaults: models.ScraperSettings{
			Enabled:  true,
			Language: "ja",
		},
		Priority: 50,
	})
	reg.Register(scraperutil.ScraperRegistration{
		Name: "scraper2",
		Defaults: models.ScraperSettings{
			Enabled:  false,
			Language: "en",
		},
		Priority: 100,
	})

	defaults := reg.GetDefaults()

	assert.Len(t, defaults, 2)
	assert.Equal(t, 50, defaults["scraper1"].Priority)
	assert.Equal(t, "ja", defaults["scraper1"].Settings.Language)
	assert.Equal(t, true, defaults["scraper1"].Settings.Enabled)
	assert.Equal(t, 100, defaults["scraper2"].Priority)
	assert.Equal(t, "en", defaults["scraper2"].Settings.Language)
	assert.Equal(t, false, defaults["scraper2"].Settings.Enabled)
}

func TestGetRegisteredDefaults_EmptyRegistry(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()

	defaults := reg.GetDefaults()
	assert.Empty(t, defaults, "Empty registry should return empty map")
}

func TestCreate_InvalidConstructor(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name: "error-scraper",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return nil, errors.New("constructor error")
		}),
	})

	regEntry, ok := reg.Get("error-scraper")
	require.True(t, ok)

	result, err := regEntry.Constructor(scraperutil.ScraperDeps{Settings: models.ScraperSettings{Enabled: true}})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "constructor error")
}
