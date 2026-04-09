package scraperutil

type ScraperOptionsProvider struct {
	DisplayName string
	Options     []any
}

var scraperOptionsRegistry = make(map[string]ScraperOptionsProvider)

type ValidatorFunc func(any) error

var validatorRegistry = make(map[string]ValidatorFunc)

func GetValidator(name string) ValidatorFunc {
	return validatorRegistry[name]
}

func ResetValidators() {
	validatorRegistry = make(map[string]ValidatorFunc)
}

type ScraperConfigAccessor func(any) any

var scraperConfigRegistry = make(map[string]ScraperConfigAccessor)

func GetScraperConfigs() map[string]ScraperConfigAccessor {
	result := make(map[string]ScraperConfigAccessor, len(scraperConfigRegistry))
	for k, v := range scraperConfigRegistry {
		result[k] = v
	}
	return result
}

func ResetScraperConfigs() {
	scraperConfigRegistry = make(map[string]ScraperConfigAccessor)
}

type ConfigFactory func() any

var configFactoryRegistry = make(map[string]ConfigFactory)

func GetConfigFactory(name string) ConfigFactory {
	return configFactoryRegistry[name]
}

func ResetConfigFactories() {
	configFactoryRegistry = make(map[string]ConfigFactory)
}

func GetScraperOptions(name string) (ScraperOptionsProvider, bool) {
	provider, exists := scraperOptionsRegistry[name]
	return provider, exists
}

func ResetScraperOptions() {
	scraperOptionsRegistry = make(map[string]ScraperOptionsProvider)
}

type FlattenFunc func(any) any

var flattenRegistry = map[string]FlattenFunc{}

func GetFlattenFunc(name string) FlattenFunc {
	return flattenRegistry[name]
}

func ResetFlattenFuncs() {
	flattenRegistry = make(map[string]FlattenFunc)
}

type ScraperConfigInterface interface {
	IsEnabled() bool
	GetUserAgent() string
	GetRequestDelay() int
	GetMaxRetries() int
	GetProxy() any
	GetDownloadProxy() any
}

func GetPriorities() []string {
	if len(defaultScraperSettingsRegistry) == 0 {
		return nil
	}

	type pair struct {
		name     string
		priority int
	}
	pairs := make([]pair, 0, len(defaultScraperSettingsRegistry))
	for name, def := range defaultScraperSettingsRegistry {
		pairs = append(pairs, pair{name: name, priority: def.priority})
	}

	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].priority > pairs[i].priority ||
				(pairs[j].priority == pairs[i].priority && pairs[j].name < pairs[i].name) {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	result := make([]string, len(pairs))
	for i, p := range pairs {
		result[i] = p.name
	}
	return result
}

var defaultScraperSettingsRegistry = map[string]struct {
	settings any
	priority int
}{}

func GetDefaultScraperSettings() map[string]any {
	result := make(map[string]any, len(defaultScraperSettingsRegistry))
	for k, v := range defaultScraperSettingsRegistry {
		result[k] = v.settings
	}
	return result
}

func ResetDefaults() {
	defaultScraperSettingsRegistry = map[string]struct {
		settings any
		priority int
	}{}
}

func ResetAllRegistries() {
	ResetValidators()
	ResetScraperConfigs()
	ResetConfigFactories()
	ResetFlattenFuncs()
	ResetDefaults()
	ResetScraperOptions()
	ResetConstructors()
	ResetDefaultsRegistries()
}
