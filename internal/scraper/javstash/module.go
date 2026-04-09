package javstash

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
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
		var globalProxy config.ProxyConfig
		var globalFlareSolverr config.FlareSolverrConfig
		if globalConfig != nil {
			globalProxy = globalConfig.Proxy
			globalFlareSolverr = globalConfig.FlareSolverr
		}
		return New(settings, &globalProxy, globalFlareSolverr), nil
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
		contracts.ScraperOption{
			Key:         "api_key",
			Label:       "API Key",
			Description: "API key for Javstash.org authentication",
			Type:        "password",
			Default:     "",
		},
		contracts.ScraperOption{
			Key:         "language",
			Label:       "Language",
			Description: "Language for metadata fields",
			Type:        "select",
			Default:     "en",
			Choices: []contracts.ScraperChoice{
				{Value: "en", Label: "English"},
				{Value: "ja", Label: "Japanese"},
			},
		},
		contracts.ScraperOption{
			Key:         "base_url",
			Label:       "Base URL",
			Description: "GraphQL API endpoint URL",
			Type:        "string",
			Default:     "https://javstash.org/graphql",
		},
		contracts.ScraperOption{
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
func (m *scraperModule) FlattenFunc() any {
	return scraperutil.FlattenFunc(func(cfg any) any {
		c, ok := cfg.(scraperutil.ScraperConfigInterface)
		if !ok {
			return nil
		}
		proxy := c.GetProxy()
		downloadProxy := c.GetDownloadProxy()
		var proxyVal, downloadProxyVal *config.ProxyConfig
		if proxy != nil {
			proxyVal = proxy.(*config.ProxyConfig)
		}
		if downloadProxy != nil {
			downloadProxyVal = downloadProxy.(*config.ProxyConfig)
		}
		if jsCfg, ok := cfg.(*JavstashConfig); ok {
			return &config.ScraperSettings{
				Enabled:       c.IsEnabled(),
				Language:      jsCfg.Language,
				RateLimit:     c.GetRequestDelay(),
				BaseURL:       jsCfg.BaseURL,
				Proxy:         proxyVal,
				DownloadProxy: downloadProxyVal,
				Extra: map[string]any{
					"api_key": jsCfg.APIKey,
				},
			}
		}
		return nil
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
