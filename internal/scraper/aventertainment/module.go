package aventertainment

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "aventertainment",
		ScraperDescription: "AV Entertainment",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "en",
				Choices: []models.ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
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
				Description: "AV Entertainment base URL",
				Type:        "string",
			},
			models.ScraperOption{
				Key:         "scrape_bonus_screens",
				Label:       "Scrape bonus screenshots",
				Description: "Append bonus image files to screenshots",
				Type:        "boolean",
			},
		},
		ScraperDefaults: config.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   "https://www.aventertainments.com",
		},
		ScraperPriority: 45,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &AVEntertainmentConfig{} },
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
		FlatBuilderRaw: func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides, raw any) any {
			s := &config.ScraperSettings{Enabled: fc.Enabled, Language: "", RateLimit: fc.RateLimit, BaseURL: "https://www.aventertainments.com", Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
			if aventCfg, ok := raw.(*AVEntertainmentConfig); ok {
				if aventCfg.ScrapeBonusScreens {
					s.Extra = map[string]any{"scrape_bonus_screens": aventCfg.ScrapeBonusScreens}
				}
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
