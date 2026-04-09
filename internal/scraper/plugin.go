package scraper

import (
	"fmt"
	"sort"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

type ScraperConstructor func(config.ScraperSettings, *database.DB, *config.ScrapersConfig) (models.Scraper, error)

type DefaultSettings struct {
	Settings config.ScraperSettings
	Priority int
}

func GetScraperConstructors() map[string]ScraperConstructor {
	constructors := scraperutil.GetScraperConstructors()
	result := make(map[string]ScraperConstructor, len(constructors))
	for k, v := range constructors {
		if c, ok := v.(ScraperConstructor); ok {
			result[k] = c
		} else if fn, ok := v.(func(config.ScraperSettings, *database.DB, *config.ScrapersConfig) (models.Scraper, error)); ok {
			result[k] = ScraperConstructor(fn)
		}
	}
	return result
}

func GetRegisteredDefaults() map[string]DefaultSettings {
	defaults := scraperutil.GetDefaults()
	result := make(map[string]DefaultSettings, len(defaults))
	for k, v := range defaults {
		if s, ok := v.Settings.(config.ScraperSettings); ok {
			result[k] = DefaultSettings{
				Settings: s,
				Priority: v.Priority,
			}
		}
	}
	return result
}

func Create(
	name string,
	settings config.ScraperSettings,
	db *database.DB,
	globalScrapersConfig *config.ScrapersConfig,
) (models.Scraper, error) {
	constructorAny, exists := scraperutil.GetScraperConstructor(name)
	if !exists {
		return nil, fmt.Errorf("scraper not found: %q (available: %v)", name, getRegisteredScraperNames())
	}

	constructor, ok := constructorAny.(ScraperConstructor)
	if !ok {
		if fn, ok := constructorAny.(func(config.ScraperSettings, *database.DB, *config.ScrapersConfig) (models.Scraper, error)); ok {
			constructor = ScraperConstructor(fn)
		} else {
			return nil, fmt.Errorf("scraper %q has invalid constructor", name)
		}
	}

	scraper, err := constructor(settings, db, globalScrapersConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s scraper: %w", name, err)
	}

	return scraper, nil
}

func getRegisteredScraperNames() []string {
	constructors := scraperutil.GetScraperConstructors()
	names := make([]string, 0, len(constructors))
	for name := range constructors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ResetAllRegistries() {
	scraperutil.ResetConstructors()
	scraperutil.ResetDefaultsRegistries()
	scraperutil.ResetValidators()
	scraperutil.ResetScraperConfigs()
	scraperutil.ResetConfigFactories()
	scraperutil.ResetFlattenFuncs()
	scraperutil.ResetScraperOptions()
	scraperutil.ResetDefaults()
}
