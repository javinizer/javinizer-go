package scraper

import (
	"reflect"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperRegistryConfigFromApp constructs a ScraperRegistryConfig from the
// application config at the factory boundary. This is the only function in
// the scraper package that imports *config.Config — the registry itself
// never sees the god-pointer.
//
// names and defaults come from the scraper registry (scraperutil cannot be
// imported here without an import cycle), so each registered scraper is
// resolved by merging the user override over the module defaults.
//
// Config-bridge reads: cfg.Scrapers.Overrides, cfg.Scrapers.Proxy,
// cfg.Scrapers.FlareSolverr, cfg.Scrapers.Browser,
// cfg.Scrapers.TimeoutSeconds, cfg.Scrapers.ScrapeActress
func ScraperRegistryConfigFromApp(cfg *config.Config, names []string, defaults map[string]models.ScraperSettings) ScraperRegistryConfig {
	overrides := make(map[string]models.ScraperSettings, len(names))
	for _, name := range names {
		if override := cfg.Scrapers.Overrides[name]; override != nil {
			resolved := override.Clone()
			if def, ok := defaults[name]; ok {
				resolved.MergeDefaultsFrom(def)
				if !resolved.Enabled && def.Enabled {
					hasOtherFields := !reflect.DeepEqual(*override, models.ScraperSettings{}) &&
						!reflect.DeepEqual(*override, models.ScraperSettings{Enabled: false})
					if hasOtherFields {
						resolved.Enabled = def.Enabled
					}
				}
			}
			overrides[name] = resolved
		} else {
			if def, ok := defaults[name]; ok {
				overrides[name] = def.Clone()
			} else {
				overrides[name] = models.ScraperSettings{}
			}
		}
	}
	return ScraperRegistryConfig{
		Overrides:      overrides,
		GlobalProxy:    cfg.Scrapers.Proxy,
		FlareSolverr:   cfg.Scrapers.FlareSolverr,
		Browser:        cfg.Scrapers.Browser,
		TimeoutSeconds: cfg.Scrapers.TimeoutSeconds,
		ScrapeActress:  cfg.Scrapers.ScrapeActress,
	}
}
