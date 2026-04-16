package javstash

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	scraperutil.RegisterModule(&scraperModule{})
}

type scraperModule struct{}

func (m *scraperModule) Name() string        { return "javstash" }
func (m *scraperModule) Description() string { return "Javstash" }
func (m *scraperModule) Constructor() any {
	return func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		var globalProxy *config.ProxyConfig
		var globalFlareSolverr config.FlareSolverrConfig
		if globalConfig != nil {
			globalProxy = &globalConfig.Proxy
			globalFlareSolverr = globalConfig.FlareSolverr
		}
		return New(settings, globalProxy, globalFlareSolverr), nil
	}
}
func (m *scraperModule) Validator() any {
	return scraperutil.ValidatorFunc(func(a any) error {
		return (&JavstashConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &JavstashConfig{} })
}
func (m *scraperModule) Options() any {
	return []any{
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
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:  false,
		Language: "en",
	}
}
func (m *scraperModule) Priority() int { return 10 }
func proxyAsConfig(p any) *config.ProxyConfig {
	if p == nil {
		return nil
	}
	return p.(*config.ProxyConfig)
}

func (m *scraperModule) FlattenFunc() any {
	return scraperutil.DefaultFlattenConfigWithRaw(scraperutil.FlattenOverrides{}, func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides, raw any) any {
		s := &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: proxyAsConfig(fc.Proxy), DownloadProxy: proxyAsConfig(fc.DownloadProxy)}
		if jsCfg, ok := raw.(*JavstashConfig); ok {
			s.Language = jsCfg.Language
			s.BaseURL = jsCfg.BaseURL
			s.Extra = map[string]any{"api_key": jsCfg.APIKey}
		}
		return s
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
