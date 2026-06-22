package scraperutil

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestRegisterModule_ValidModule(t *testing.T) {
	reg := NewScraperRegistry()
	reg.Register(ScraperRegistration{
		Name:        "test-scraper",
		Description: "Test test-scraper",
		Constructor: func(deps ScraperDeps) (models.Scraper, error) {
			return nil, nil
		},
		ValidateFn: func(ss *models.ScraperSettings) error { return nil },
		Options:    []models.ScraperOption{},
		Defaults:   models.ScraperSettings{},
		Priority:   50,
	})

	assert.NotNil(t, reg.GetValidateFn("test-scraper"))
	_, exists := reg.GetOptions("test-scraper")
	assert.True(t, exists)
}

func TestGetScraperConstructor(t *testing.T) {
	reg := NewScraperRegistry()

	_, exists := reg.GetScraperConstructor("nonexistent")
	assert.False(t, exists)

	constructor := ScraperConstructor(func(deps ScraperDeps) (models.Scraper, error) {
		return nil, nil
	})
	reg.Register(ScraperRegistration{
		Name:        "with-constructor",
		Constructor: constructor,
	})

	c, exists := reg.GetScraperConstructor("with-constructor")
	assert.True(t, exists)
	assert.NotNil(t, c)
}

func TestGetScraperConstructors(t *testing.T) {
	reg := NewScraperRegistry()

	constructors := reg.GetScraperConstructors()
	assert.Empty(t, constructors)

	reg.Register(ScraperRegistration{
		Name:        "scraper1",
		Constructor: ScraperConstructor(func(deps ScraperDeps) (models.Scraper, error) { return nil, nil }),
	})
	reg.Register(ScraperRegistration{
		Name:        "scraper2",
		Constructor: ScraperConstructor(func(deps ScraperDeps) (models.Scraper, error) { return nil, nil }),
	})

	constructors = reg.GetScraperConstructors()
	assert.Len(t, constructors, 2)
}

func TestGetDefaults(t *testing.T) {
	reg := NewScraperRegistry()

	defaults := reg.GetDefaults()
	assert.Empty(t, defaults)

	reg.Register(ScraperRegistration{
		Name:     "with-defaults",
		Defaults: models.ScraperSettings{Enabled: true},
		Priority: 100,
	})

	defaults = reg.GetDefaults()
	assert.Len(t, defaults, 1)
	assert.Equal(t, 100, defaults["with-defaults"].Priority)
}

func TestResetConstructors(t *testing.T) {
	reg := NewScraperRegistry()
	reg.Register(ScraperRegistration{
		Name:        "to-reset",
		Constructor: ScraperConstructor(func(deps ScraperDeps) (models.Scraper, error) { return nil, nil }),
	})

	reg = NewScraperRegistry()

	_, exists := reg.GetScraperConstructor("to-reset")
	assert.False(t, exists)
}

func TestResetDefaultsRegistries(t *testing.T) {
	reg := NewScraperRegistry()
	reg.Register(ScraperRegistration{
		Name:     "to-reset-defaults",
		Defaults: models.ScraperSettings{},
		Priority: 50,
	})

	reg = NewScraperRegistry()

	defaults := reg.GetDefaults()
	_, exists := defaults["to-reset-defaults"]
	assert.False(t, exists)
}
