package scraper

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperRegistryConfigFromApp constructs a ScraperRegistryConfig from the
// application config at the factory boundary. This is the only function in
// the scraper package that imports *config.Config — the registry itself
// never sees the god-pointer.
//
// Config-bridge reads: cfg.Scrapers.Overrides, cfg.Scrapers.Proxy,
// cfg.Scrapers.FlareSolverr, cfg.Scrapers.Browser,
// cfg.Scrapers.TimeoutSeconds, cfg.Scrapers.ScrapeActress
func ScraperRegistryConfigFromApp(cfg *config.Config) ScraperRegistryConfig {
	overrides := make(map[string]models.ScraperSettings, len(cfg.Scrapers.Overrides))
	for name, override := range cfg.Scrapers.Overrides {
		if override != nil {
			overrides[name] = *override
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
