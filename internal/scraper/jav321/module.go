package jav321

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "jav321",
		ScraperDescription: "Jav321",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "request_delay",
				Label:       "Request Delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         scraperutil.IntPtr(0),
				Max:         scraperutil.IntPtr(5000),
				Unit:        "ms",
			},
			models.ScraperOption{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "Jav321 base URL",
				Type:        "string",
			},
			models.ScraperOption{
				Key:         "use_flaresolverr",
				Label:       "Use FlareSolverr",
				Description: "Route requests through FlareSolverr to bypass Cloudflare protection",
				Type:        "boolean",
			},
		},
		ScraperDefaults: config.ScraperSettings{
			Enabled:   false,
			Language:  "ja",
			RateLimit: 1000,
		},
		ScraperPriority: 65,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &Jav321Config{} },
		NewScraperFunc: func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			_ = db
			var globalProxy *config.ProxyConfig
			var globalFlareSolverr config.FlareSolverrConfig
			if globalConfig != nil {
				globalProxy = &globalConfig.Proxy
				globalFlareSolverr = globalConfig.FlareSolverr
			}
			return New(settings, globalProxy, globalFlareSolverr), nil
		},
		FlatBuilder: func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides) any {
			return &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
		},
	}
	scraperutil.RegisterModule(m)
}

type scraperModule struct {
	scraperutil.StandardModule
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
