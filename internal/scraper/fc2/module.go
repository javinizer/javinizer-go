package fc2

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "fc2",
		ScraperDescription: "FC2",
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
				Description: "FC2 base URL",
				Type:        "string",
			},
		},
		ScraperDefaults: config.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   "https://adult.contents.fc2.com",
		},
		ScraperPriority: 35,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &FC2Config{} },
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
		FlatOverrides: scraperutil.FlattenOverrides{BaseURL: "https://adult.contents.fc2.com"},
		FlatBuilder: func(fc *scraperutil.FlattenedConfig, o scraperutil.FlattenOverrides) any {
			return &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, BaseURL: o.BaseURL, Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
		},
	}
	scraperutil.RegisterModule(m)
}

type scraperModule struct {
	scraperutil.StandardModule
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
