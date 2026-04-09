package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

type mockModule struct {
	name          string
	description   string
	constructor   ScraperConstructor
	validator     scraperutil.ValidatorFunc
	configFactory scraperutil.ConfigFactory
	options       []any
	defaults      config.ScraperSettings
	priority      int
	flattenFunc   scraperutil.FlattenFunc
}

func (m *mockModule) Name() string        { return m.name }
func (m *mockModule) Description() string { return m.description }
func (m *mockModule) Constructor() any    { return m.constructor }
func (m *mockModule) Validator() any      { return m.validator }
func (m *mockModule) ConfigFactory() any  { return m.configFactory }
func (m *mockModule) Options() any        { return m.options }
func (m *mockModule) Defaults() any       { return m.defaults }
func (m *mockModule) Priority() int       { return m.priority }
func (m *mockModule) FlattenFunc() any    { return m.flattenFunc }

func TestRegisterModule_ValidModule(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	constructor := ScraperConstructor(func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return nil, nil
	})
	validator := scraperutil.ValidatorFunc(func(a any) error { return nil })
	configFactory := scraperutil.ConfigFactory(func() any { return &config.ScraperSettings{} })
	options := []any{}
	defaults := config.ScraperSettings{Enabled: true}

	module := &mockModule{
		name:          "test-scraper",
		description:   "Test Scraper",
		constructor:   constructor,
		validator:     validator,
		configFactory: configFactory,
		options:       options,
		defaults:      defaults,
		priority:      50,
	}

	scraperutil.RegisterModule(module)

	assert.NotNil(t, GetScraperConstructors()["test-scraper"], "scraper constructor should be registered")
	assert.NotNil(t, scraperutil.GetValidator("test-scraper"), "validator should be registered")
	assert.NotNil(t, scraperutil.GetConfigFactory("test-scraper"), "config factory should be registered")
	_, exists := scraperutil.GetScraperOptions("test-scraper")
	assert.True(t, exists, "scraper options should be registered")
}

func TestRegisterModule_NilModule(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	defer func() {
		r := recover()
		assert.NotNil(t, r, "RegisterModule should panic on nil module")
	}()

	scraperutil.RegisterModule(nil)
}

func TestModule_InterfaceSatisfaction(t *testing.T) {
	var _ scraperutil.ScraperModule = (*mockModule)(nil)
}

func TestRegisterModule_TypeAssertions(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	validator := func(a any) error { return nil }
	configFactory := func() any { return &config.ScraperSettings{} }

	module := &mockModule{
		name:          "type-test",
		description:   "Type Test",
		constructor:   nil,
		validator:     validator,
		configFactory: configFactory,
		options:       []any{},
		defaults:      config.ScraperSettings{},
		priority:      10,
	}

	scraperutil.RegisterModule(module)

	validatorFunc := scraperutil.GetValidator("type-test")
	assert.NotNil(t, validatorFunc, "validator should be retrievable")
	assert.Nil(t, validatorFunc(nil), "validator should work")

	factory := scraperutil.GetConfigFactory("type-test")
	assert.NotNil(t, factory, "config factory should be retrievable")
	result := factory()
	assert.NotNil(t, result, "factory should produce non-nil result")
}

func TestRegisterModule_AllRegistriesCalled(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	constructor := func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return nil, nil
	}

	module := &mockModule{
		name:          "all-registries",
		description:   "All Registries Test",
		constructor:   constructor,
		validator:     func(a any) error { return nil },
		configFactory: func() any { return &config.ScraperSettings{} },
		options:       []any{},
		defaults:      config.ScraperSettings{Enabled: true},
		priority:      75,
		flattenFunc:   func(a any) any { return &config.ScraperSettings{} },
	}

	scraperutil.RegisterModule(module)

	assert.NotNil(t, GetScraperConstructors()["all-registries"], "constructor registry should have entry")
	assert.NotNil(t, scraperutil.GetValidator("all-registries"), "validator registry should have entry")
	assert.NotNil(t, scraperutil.GetConfigFactory("all-registries"), "config factory registry should have entry")
	_, hasOptions := scraperutil.GetScraperOptions("all-registries")
	assert.True(t, hasOptions, "options registry should have entry")

	defaults := GetRegisteredDefaults()
	assert.NotNil(t, defaults["all-registries"], "defaults registry should have entry")
	assert.Equal(t, 75, defaults["all-registries"].Priority, "priority should be registered correctly")

	flatten := scraperutil.GetFlattenFunc("all-registries")
	assert.NotNil(t, flatten, "flatten func should be registered")
}

func TestResetAllRegistries_ClearsAllRegistries(t *testing.T) {
	constructor := func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return nil, nil
	}

	module := &mockModule{
		name:          "reset-test",
		description:   "Reset Test",
		constructor:   constructor,
		validator:     func(a any) error { return nil },
		configFactory: func() any { return &config.ScraperSettings{} },
		options:       []any{},
		defaults:      config.ScraperSettings{},
		priority:      10,
	}

	scraperutil.RegisterModule(module)
	ResetAllRegistries()

	assert.Nil(t, GetScraperConstructors()["reset-test"], "constructors should be cleared")
	assert.Nil(t, scraperutil.GetValidator("reset-test"), "validators should be cleared")
	assert.Nil(t, scraperutil.GetConfigFactory("reset-test"), "config factories should be cleared")
	_, hasOptions := scraperutil.GetScraperOptions("reset-test")
	assert.False(t, hasOptions, "options should be cleared")
	defaults := GetRegisteredDefaults()
	_, hasDefault := defaults["reset-test"]
	assert.False(t, hasDefault, "defaults should be cleared")
}

func TestResetAllRegistries_NoLeaksBetweenTests(t *testing.T) {
	t.Cleanup(func() { ResetAllRegistries() })

	module1 := &mockModule{
		name:          "test-one",
		description:   "Test One",
		constructor:   nil,
		validator:     func(a any) error { return nil },
		configFactory: func() any { return &config.ScraperSettings{} },
		options:       []any{},
		defaults:      config.ScraperSettings{},
		priority:      10,
	}

	scraperutil.RegisterModule(module1)

	assert.NotNil(t, scraperutil.GetValidator("test-one"), "test-one should be registered")
	assert.Nil(t, scraperutil.GetValidator("test-two"), "test-two should not exist yet")
}
