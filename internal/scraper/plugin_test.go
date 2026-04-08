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

func TestRegisterScraper_DefaultSettingsRegistration(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()
	scraperutil.ResetDefaults()
	scraperutil.ResetValidators()
	scraperutil.ResetScraperConfigs()
	scraperutil.ResetConfigFactories()
	scraperutil.ResetFlattenFuncs()

	// Register a scraper constructor
	RegisterScraper("settings-test", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "settings-test", enabled: settings.Enabled}, nil
	})

	// Register default settings
	RegisterScraperDefaults("settings-test", DefaultSettings{
		Settings: config.ScraperSettings{Enabled: true, Language: "en"},
		Priority: 75,
	})

	// Verify constructor is in the map and works correctly
	constructors := GetScraperConstructors()
	assert.Contains(t, constructors, "settings-test")
	scraperInstance, err := constructors["settings-test"](config.ScraperSettings{Enabled: true}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "settings-test", scraperInstance.Name())

	// Verify defaults are in the map
	defaults := GetRegisteredDefaults()
	assert.Contains(t, defaults, "settings-test")
	assert.Equal(t, 75, defaults["settings-test"].Priority)
	assert.Equal(t, "en", defaults["settings-test"].Settings.Language)

	// Verify GetRegisteredDefaults returns a copy (mutation doesn't affect registry)
	defaultsCopy := GetRegisteredDefaults()
	// Create a new map entry to verify isolation
	defaultsCopy["new-entry"] = DefaultSettings{Priority: 999}
	originalDefaults := GetRegisteredDefaults()
	assert.Equal(t, 75, originalDefaults["settings-test"].Priority, "Registry should not be affected by mutations to returned copy")
	assert.NotContains(t, originalDefaults, "new-entry", "Registry should not be affected by additions to returned copy")
}

func TestRegisterScraper_DuplicateNameOverwrites(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()

	// Register scraper "dup-test" with constructor A
	RegisterScraper("dup-test", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "dup-test-constructor-A", enabled: settings.Enabled}, nil
	})

	// Register scraper "dup-test" again with constructor B (different function)
	RegisterScraper("dup-test", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "dup-test-constructor-B", enabled: settings.Enabled}, nil
	})

	// Verify Create calls constructor B (latest registration wins)
	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("dup-test", settings, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
	assert.Equal(t, "dup-test-constructor-B", scraper.Name(), "Latest constructor should win for duplicate name")
}

func TestRegisterScraperDefaults_DuplicateNameOverwrites(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()
	scraperutil.ResetDefaults()

	// Register defaults for "dup-test" with priority 50
	RegisterScraperDefaults("dup-test", DefaultSettings{
		Settings: config.ScraperSettings{Enabled: true},
		Priority: 50,
	})

	// Register defaults for "dup-test" again with priority 100
	RegisterScraperDefaults("dup-test", DefaultSettings{
		Settings: config.ScraperSettings{Enabled: true, Language: "jp"},
		Priority: 100,
	})

	// Verify latest wins
	defaults := GetRegisteredDefaults()
	assert.Equal(t, 100, defaults["dup-test"].Priority, "Latest defaults registration should win")
	assert.Equal(t, "jp", defaults["dup-test"].Settings.Language, "Latest defaults should overwrite all fields")
}

func TestRegisterScraper_NilHandlerRejected(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()

	// Register a nil constructor - should panic at init-time (preferable to runtime crashes)
	require.Panics(t, func() {
		RegisterScraper("nil-handler", nil)
	}, "RegisterScraper should panic for nil constructor")
}

func TestRegisterScraperDefaults_NilSettingsRejected(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()
	scraperutil.ResetDefaults()

	// Empty struct is valid settings (zero value), so RegisterScraperDefaults with empty DefaultSettings should succeed
	RegisterScraperDefaults("empty-settings", DefaultSettings{})

	// Verify it was registered
	defaults := GetRegisteredDefaults()
	assert.Contains(t, defaults, "empty-settings")

	// Create with valid constructor but empty settings
	RegisterScraper("empty-settings", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "empty-settings", enabled: settings.Enabled}, nil
	})

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("empty-settings", settings, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
}

func TestCreate_DBBackedScraper(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()
	scraperutil.ResetDefaults()

	// Register a scraper that expects database access
	RegisterScraper("db-scraper", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		// Verify db is passed correctly
		if db == nil {
			return nil, assert.AnError
		}
		return &testDBScraper{name: "db-scraper", db: db, enabled: settings.Enabled}, nil
	})

	// Create a test database
	tmpDir := t.TempDir()
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = tmpDir + "/test.db"
	db, err := database.New(testCfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)
	defer db.Close()

	// Create scraper with database
	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("db-scraper", settings, db, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
	assert.Equal(t, "db-scraper", scraper.Name())

	// Verify db was actually set on the scraper
	dbScraper, ok := scraper.(*testDBScraper)
	assert.True(t, ok, "Scraper should be *testDBScraper")
	assert.NotNil(t, dbScraper.db, "Database should be set on scraper")
	assert.Equal(t, db, dbScraper.db, "Database should match what was passed to Create")
}

func TestCreate_KnownScraper(t *testing.T) {
	// Reset registries before test
	ResetConstructors()
	ResetDefaults()

	// Register a test scraper
	RegisterScraper("test-scraper", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return &testScraper{name: "test-scraper", enabled: settings.Enabled}, nil
	})

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("test-scraper", settings, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, scraper)
	assert.Equal(t, "test-scraper", scraper.Name())
	assert.True(t, scraper.IsEnabled())
}

func TestCreate_UnknownScraper(t *testing.T) {
	// Reset registries before test
	ResetConstructors()
	ResetDefaults()

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("unknown-scraper", settings, nil, nil)

	assert.Error(t, err)
	assert.Nil(t, scraper)
	assert.Contains(t, err.Error(), "scraper not found:")
	assert.Contains(t, err.Error(), "unknown-scraper")
}

func TestCreate_ConstructorError(t *testing.T) {
	// Reset registries before test
	ResetConstructors()
	ResetDefaults()

	// Register a scraper that returns an error
	RegisterScraper("error-scraper", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return nil, assert.AnError
	})

	settings := config.ScraperSettings{Enabled: true}
	scraper, err := Create("error-scraper", settings, nil, nil)

	assert.Error(t, err)
	assert.Nil(t, scraper)
	assert.Contains(t, err.Error(), "failed to create error-scraper scraper:")
}

func TestResetDefaults_ClearsBothRegistries(t *testing.T) {
	// Reset all registries before test
	ResetConstructors()
	ResetDefaults()
	scraperutil.ResetDefaults()
	scraperutil.ResetValidators()
	scraperutil.ResetScraperConfigs()
	scraperutil.ResetConfigFactories()
	scraperutil.ResetFlattenFuncs()

	// Setup: register something in both registries
	RegisterScraperDefaults("test-scraper", DefaultSettings{
		Settings: config.ScraperSettings{Enabled: true},
		Priority: 100,
	})

	// Verify both have entries
	assert.NotEmpty(t, GetRegisteredDefaults())
	assert.NotEmpty(t, scraperutil.GetDefaultScraperSettings())

	// Reset via scraper.ResetDefaults()
	ResetDefaults()

	// Verify both are empty
	assert.Empty(t, GetRegisteredDefaults())
	assert.Empty(t, scraperutil.GetDefaultScraperSettings())
}

// testScraper is a minimal implementation for testing
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

// testDBScraper is a minimal implementation for testing DB-backed scrapers
type testDBScraper struct {
	name    string
	enabled bool
	db      *database.DB
}

func (s *testDBScraper) Name() string                    { return s.name }
func (s *testDBScraper) IsEnabled() bool                 { return s.enabled }
func (s *testDBScraper) Close() error                    { return nil }
func (s *testDBScraper) Config() *config.ScraperSettings { return nil }
func (s *testDBScraper) Search(id string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *testDBScraper) GetURL(id string) (string, error) { return "", nil }
