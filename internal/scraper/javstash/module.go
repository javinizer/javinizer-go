package javstash

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "javstash",
		ScraperDescription: "Javstash",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "api_key",
				Label:       "API Key",
				Description: "API key for Javstash.org authentication",
				Type:        "password",
				Default:     "",
			},
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
				Key:         "base_url",
				Label:       "Base URL",
				Description: "GraphQL API endpoint URL",
				Type:        "string",
				Default:     "https://javstash.org/graphql",
			},
			models.ScraperOption{
				Key:         "request_delay",
				Label:       "Request Delay",
				Description: "Delay between requests in milliseconds",
				Type:        "number",
				Default:     "1000",
			},
		},
		ScraperDefaults: config.ScraperSettings{
			Enabled:  false,
			Language: "en",
		},
		ScraperPriority: 10,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &JavstashConfig{} },
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
			s := &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
			if jsCfg, ok := raw.(*JavstashConfig); ok {
				s.Language = jsCfg.Language
				s.BaseURL = jsCfg.BaseURL
				s.Extra = map[string]any{"api_key": jsCfg.APIKey}
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
