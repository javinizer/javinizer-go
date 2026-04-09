package scraperutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testModule struct {
	name        string
	constructor any
	validator   ValidatorFunc
	factory     ConfigFactory
	options     []any
	defaults    any
	priority    int
	flatten     FlattenFunc
}

func (m *testModule) Name() string        { return m.name }
func (m *testModule) Description() string { return "Test " + m.name }
func (m *testModule) Constructor() any    { return m.constructor }
func (m *testModule) Validator() any      { return m.validator }
func (m *testModule) ConfigFactory() any  { return m.factory }
func (m *testModule) Options() any        { return m.options }
func (m *testModule) Defaults() any       { return m.defaults }
func (m *testModule) Priority() int       { return m.priority }
func (m *testModule) FlattenFunc() any    { return m.flatten }

func TestRegisterModule_NilModule(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "RegisterModule should panic on nil module")
	}()
	RegisterModule(nil)
}

func TestRegisterModule_ValidModule(t *testing.T) {
	ResetAllRegistries()

	module := &testModule{
		name:        "test-scraper",
		constructor: func() {},
		validator:   func(a any) error { return nil },
		factory:     func() any { return struct{}{} },
		options:     []any{struct{}{}},
		defaults:    struct{}{},
		priority:    50,
		flatten:     func(a any) any { return nil },
	}
	RegisterModule(module)

	assert.NotNil(t, GetValidator("test-scraper"))
	assert.NotNil(t, GetConfigFactory("test-scraper"))
	assert.NotNil(t, GetFlattenFunc("test-scraper"))
	_, exists := GetScraperOptions("test-scraper")
	assert.True(t, exists)
}

func TestGetScraperConstructor(t *testing.T) {
	ResetAllRegistries()

	_, exists := GetScraperConstructor("nonexistent")
	assert.False(t, exists)

	constructor := func() {}
	RegisterModule(&testModule{
		name:        "with-constructor",
		constructor: constructor,
	})

	c, exists := GetScraperConstructor("with-constructor")
	assert.True(t, exists)
	assert.NotNil(t, c)
}

func TestGetScraperConstructors(t *testing.T) {
	ResetAllRegistries()

	constructors := GetScraperConstructors()
	assert.Empty(t, constructors)

	RegisterModule(&testModule{
		name:        "scraper1",
		constructor: func() {},
	})
	RegisterModule(&testModule{
		name:        "scraper2",
		constructor: func() {},
	})

	constructors = GetScraperConstructors()
	assert.Len(t, constructors, 2)
}

func TestGetDefaults(t *testing.T) {
	ResetAllRegistries()

	defaults := GetDefaults()
	assert.Empty(t, defaults)

	RegisterModule(&testModule{
		name:     "with-defaults",
		defaults: struct{ Enabled bool }{true},
		priority: 100,
	})

	defaults = GetDefaults()
	assert.Len(t, defaults, 1)
	assert.Equal(t, 100, defaults["with-defaults"].Priority)
}

func TestResetConstructors(t *testing.T) {
	RegisterModule(&testModule{
		name:        "to-reset",
		constructor: func() {},
	})

	ResetConstructors()

	_, exists := GetScraperConstructor("to-reset")
	assert.False(t, exists)
}

func TestResetDefaultsRegistries(t *testing.T) {
	RegisterModule(&testModule{
		name:     "to-reset-defaults",
		defaults: struct{}{},
		priority: 50,
	})

	ResetDefaultsRegistries()

	defaults := GetDefaults()
	_, exists := defaults["to-reset-defaults"]
	assert.False(t, exists)
}

var _ ScraperModule = (*testModule)(nil)
