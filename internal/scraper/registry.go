// Package scraper serves as the container directory for individual source scraper plugin
// implementations — one sub-package per website (r18dev, dmm, javbus, javlibrary, javdb,
// mgstage, fc2, jav321, javstash, aventertainment, caribbeancom, dlgetchu, libredmm, tokyohot).
// Each sub-package implements the models.Scraper interface and self-registers into the
// scraperutil.ScraperRegistry via init() using the scraperutil module pattern.
//
// The top-level scraper package provides bootstrap glue:
//   - RegisterAll(reg) calls each sub-package's Register function to populate a registry
//   - NewDefaultScraperRegistry(cfg, db) creates and populates a registry with all built-in scrapers
//
// For the core scraping orchestration engine, see the scrape package.
// For the registry infrastructure that scrapers register with, see scraperutil.
package scraper

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// ScraperRegistryConfig carries the narrow set of fields the scraper registry
// needs from the application config. Constructed at the factory boundary from
// *config.Config via ScraperRegistryConfigFromApp — the registry never sees
// the god-pointer.
type ScraperRegistryConfig struct {
	Overrides      map[string]models.ScraperSettings // per-scraper settings
	GlobalProxy    models.ProxyConfig
	FlareSolverr   models.FlareSolverrConfig
	Browser        models.BrowserConfig
	TimeoutSeconds int
	ScrapeActress  bool
}

// initFromRegistration populates a scraperutil.ScraperRegistry with scraper instances
// by building ScraperDeps from ScraperRegistryConfig and calling constructors from
// registration metadata.
func initFromRegistration(reg *scraperutil.ScraperRegistry, cfg ScraperRegistryConfig, contentIDRepo models.ContentIDMappingRepositoryInterface) (*scraperutil.ScraperRegistry, error) {
	depsMap := make(map[string]scraperutil.ScraperDeps, len(cfg.Overrides))
	for name, settings := range cfg.Overrides {
		deps := scraperutil.ScraperDeps{
			Settings:       settings,
			GlobalProxy:    &cfg.GlobalProxy,
			FlareSolverr:   cfg.FlareSolverr,
			TimeoutSeconds: cfg.TimeoutSeconds,
			ScrapeActress:  cfg.ScrapeActress,
			Browser:        cfg.Browser,
		}
		if contentIDRepo != nil {
			deps.ContentIDRepo = contentIDRepo
		}
		depsMap[name] = deps
	}

	if err := reg.InitInstances(depsMap); err != nil {
		return nil, err
	}
	return reg, nil
}

// NewDefaultScraperRegistryFrom initializes scrapers in an existing registry from
// the provided config. Used for hot-reload scenarios where the registry metadata
// (registration entries) is already populated.
func NewDefaultScraperRegistryFrom(reg *scraperutil.ScraperRegistry, cfg ScraperRegistryConfig, contentIDRepo models.ContentIDMappingRepositoryInterface) (*scraperutil.ScraperRegistry, error) {
	return initFromRegistration(reg, cfg, contentIDRepo)
}
