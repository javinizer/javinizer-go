package scraperutil

import (
	"sync"
)

type ScraperModule interface {
	Name() string
	Description() string
	Constructor() any
	Validator() any
	ConfigFactory() any
	Options() any
	Defaults() any
	Priority() int
	FlattenFunc() any
}

var (
	constructorRegistry = make(map[string]any)
	constructorMu       sync.RWMutex
	defaultsRegistry    = make(map[string]DefaultSettings)
	defaultsMu          sync.RWMutex
)

type DefaultSettings struct {
	Settings any
	Priority int
}

func RegisterModule(module ScraperModule) {
	if module == nil {
		panic("RegisterModule: cannot register nil module")
	}

	name := module.Name()

	if constructor := module.Constructor(); constructor != nil {
		constructorMu.Lock()
		constructorRegistry[name] = constructor
		constructorMu.Unlock()
	}

	if defaults := module.Defaults(); defaults != nil {
		priority := module.Priority()
		defaultsMu.Lock()
		defaultsRegistry[name] = DefaultSettings{
			Settings: defaults,
			Priority: priority,
		}
		defaultsMu.Unlock()

		defaultScraperSettingsRegistry[name] = struct {
			settings any
			priority int
		}{settings: defaults, priority: priority}
	}

	if validator := module.Validator(); validator != nil {
		if v, ok := validator.(ValidatorFunc); ok {
			validatorRegistry[name] = v
		}
	}

	scraperConfigRegistry[name] = func(any) any { return nil }

	if factory := module.ConfigFactory(); factory != nil {
		if f, ok := factory.(ConfigFactory); ok {
			configFactoryRegistry[name] = f
		}
	}

	if options := module.Options(); options != nil {
		if o, ok := options.([]any); ok {
			scraperOptionsRegistry[name] = ScraperOptionsProvider{
				DisplayName: module.Description(),
				Options:     o,
			}
		}
	}

	if flatten := module.FlattenFunc(); flatten != nil {
		if f, ok := flatten.(FlattenFunc); ok {
			flattenRegistry[name] = f
		}
	}
}

func GetScraperConstructor(name string) (any, bool) {
	constructorMu.RLock()
	defer constructorMu.RUnlock()
	c, ok := constructorRegistry[name]
	return c, ok
}

func GetScraperConstructors() map[string]any {
	constructorMu.RLock()
	defer constructorMu.RUnlock()
	result := make(map[string]any, len(constructorRegistry))
	for k, v := range constructorRegistry {
		result[k] = v
	}
	return result
}

func GetDefaults() map[string]DefaultSettings {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	result := make(map[string]DefaultSettings, len(defaultsRegistry))
	for k, v := range defaultsRegistry {
		result[k] = v
	}
	return result
}

func ResetConstructors() {
	constructorMu.Lock()
	defer constructorMu.Unlock()
	constructorRegistry = make(map[string]any)
}

func ResetDefaultsRegistries() {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	defaultsRegistry = make(map[string]DefaultSettings)
}
