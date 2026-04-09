package javbus

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

func (m *scraperModule) Name() string        { return "javbus" }
func (m *scraperModule) Description() string { return "JavBus" }
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
		return (&JavBusConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &JavBusConfig{} })
}
func (m *scraperModule) Options() any {
	return []any{
		contracts.ScraperOption{
			Key:         "language",
			Label:       "Language",
			Description: "Language for metadata fields",
			Type:        "select",
			Default:     "ja",
			Choices: []contracts.ScraperChoice{
				{Value: "ja", Label: "Japanese"},
				{Value: "en", Label: "English"},
				{Value: "zh", Label: "Chinese"},
			},
		},
		contracts.ScraperOption{
			Key:         "request_delay",
			Label:       "Request Delay",
			Description: "Delay between requests to avoid rate limiting",
			Type:        "number",
			Min:         intPtr(0),
			Max:         intPtr(5000),
			Unit:        "ms",
		},
		contracts.ScraperOption{
			Key:         "base_url",
			Label:       "Base URL",
			Description: "JavBus base URL (leave default unless you need a mirror)",
			Type:        "string",
		},
		contracts.ScraperOption{
			Key:         "use_flaresolverr",
			Label:       "Use FlareSolverr",
			Description: "Route requests through FlareSolverr to bypass Cloudflare protection",
			Type:        "boolean",
		},
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:   false,
		Language:  "ja",
		RateLimit: 1000,
	}
}
func (m *scraperModule) Priority() int { return 70 }
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
		return &config.ScraperSettings{
			Enabled:       c.IsEnabled(),
			Language:      "",
			RateLimit:     c.GetRequestDelay(),
			BaseURL:       "https://www.javbus.com",
			Proxy:         proxyVal,
			DownloadProxy: downloadProxyVal,
		}
	})
}

func intPtr(i int) *int { return &i }

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
