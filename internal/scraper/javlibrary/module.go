package javlibrary

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "javlibrary",
		ScraperDescription: "JavLibrary",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "ja",
				Choices: []models.ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
					{Value: "cn", Label: "Chinese (Simplified)"},
					{Value: "tw", Label: "Chinese (Traditional)"},
				},
			},
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
				Description: "JavLibrary base URL (leave default unless you need a mirror)",
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
			Language:  "en",
			RateLimit: 1000,
		},
		ScraperPriority: 80,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &JavLibraryConfig{} },
		NewScraperFunc: func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			var globalProxy *config.ProxyConfig
			var globalFlareSolverr config.FlareSolverrConfig
			if globalConfig != nil {
				globalProxy = &globalConfig.Proxy
				globalFlareSolverr = globalConfig.FlareSolverr
			}
			return New(settings, globalProxy, globalFlareSolverr), nil
		},
		FlatBuilderRaw: func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides, raw any) any {
			s := &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
			if jlCfg, ok := raw.(*JavLibraryConfig); ok {
				s.Language = jlCfg.Language
				s.BaseURL = jlCfg.BaseURL
				s.Cookies = jlCfg.Cookies
			}
			return s
		},
		UseRawBuilder: true,
	}
	scraperutil.RegisterModule(m)
}

type scraperModule struct {
	scraperutil.StandardModule
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
