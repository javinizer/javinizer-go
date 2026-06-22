package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestRegisterModule_ValidModule(t *testing.T) {
	constructor := scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
		return nil, nil
	})

	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "test-scraper",
		Description: "Test Scraper",
		Constructor: constructor,
		ValidateFn: func(ss *models.ScraperSettings) error {
			return nil
		},
		Options:  []models.ScraperOption{},
		Defaults: models.ScraperSettings{Enabled: true},
		Priority: 50,
	})

	assert.NotNil(t, reg.GetValidateFn("test-scraper"), "validateFn should be registered")
	_, exists := reg.GetOptions("test-scraper")
	assert.True(t, exists, "scraper options should be registered")
}

func TestRegisterModule_TypeAssertions(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "type-test",
		Description: "Type Test",
		ValidateFn: func(ss *models.ScraperSettings) error {
			return nil
		},
		Options:  []models.ScraperOption{},
		Defaults: models.ScraperSettings{},
		Priority: 10,
	})

	validateFn := reg.GetValidateFn("type-test")
	assert.NotNil(t, validateFn, "validateFn should be retrievable")
}

func TestRegisterModule_AllRegistriesCalled(t *testing.T) {
	constructor := scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
		return nil, nil
	})

	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "all-registries",
		Description: "All Registries Test",
		Constructor: constructor,
		ValidateFn: func(ss *models.ScraperSettings) error {
			return nil
		},
		Options:  []models.ScraperOption{},
		Defaults: models.ScraperSettings{Enabled: true},
		Priority: 75,
	})

	regEntry, exists := reg.Get("all-registries")
	assert.True(t, exists, "all-registries should be registered")
	assert.NotNil(t, regEntry.Constructor, "constructor should be non-nil")

	assert.NotNil(t, reg.GetValidateFn("all-registries"), "validateFn registry should have entry")
	_, hasOptions := reg.GetOptions("all-registries")
	assert.True(t, hasOptions, "options registry should have entry")

	defaults := reg.GetDefaults()
	assert.NotNil(t, defaults["all-registries"], "defaults registry should have entry")
	assert.Equal(t, 75, defaults["all-registries"].Priority, "priority should be registered correctly")
}

func TestResetAllRegistries_ClearsAllRegistries(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "reset-test",
		Description: "Reset Test",
		Constructor: scraperutil.ScraperConstructor(func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return nil, nil
		}),
		ValidateFn: func(ss *models.ScraperSettings) error { return nil },
		Options:    []models.ScraperOption{},
		Defaults:   models.ScraperSettings{},
		Priority:   10,
	})

	reg = scraperutil.NewScraperRegistry()

	_, exists := reg.Get("reset-test")
	assert.False(t, exists, "entries should be cleared")
	assert.Nil(t, reg.GetValidateFn("reset-test"), "validateFns should be cleared")
	_, hasOptions := reg.GetOptions("reset-test")
	assert.False(t, hasOptions, "options should be cleared")
	defaults := reg.GetDefaults()
	_, hasDefault := defaults["reset-test"]
	assert.False(t, hasDefault, "defaults should be cleared")
}

func TestResetAllRegistries_NoLeaksBetweenTests(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "test-one",
		Description: "Test One",
		Constructor: nil,
		ValidateFn:  func(ss *models.ScraperSettings) error { return nil },
		Options:     []models.ScraperOption{},
		Defaults:    models.ScraperSettings{},
		Priority:    10,
	})

	assert.NotNil(t, reg.GetValidateFn("test-one"), "test-one should be registered")
	assert.Nil(t, reg.GetValidateFn("test-two"), "test-two should not exist yet")
}
