package aventertainment

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

func (m *scraperModule) Name() string        { return "aventertainment" }
func (m *scraperModule) Description() string { return "AV Entertainment" }
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
		return (&AVEntertainmentConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &AVEntertainmentConfig{} })
}
func (m *scraperModule) Options() any {
	return []any{
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
			Key:         "request_delay",
			Label:       "Request Delay",
			Description: "Delay between requests to avoid rate limiting",
			Type:        "number",
			Min:         scraperutil.IntPtr(0),
			Max:         scraperutil.IntPtr(5000),
			Unit:        "ms",
		},
		contracts.ScraperOption{
			Key:         "base_url",
			Label:       "Base URL",
			Description: "AV Entertainment base URL",
			Type:        "string",
		},
		contracts.ScraperOption{
			Key:         "scrape_bonus_screens",
			Label:       "Scrape bonus screenshots",
			Description: "Append bonus image files to screenshots",
			Type:        "boolean",
		},
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:   false,
		RateLimit: 1000,
		BaseURL:   "https://www.aventertainments.com",
	}
}
func (m *scraperModule) Priority() int { return 45 }
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
		aventCfg, ok := cfg.(*AVEntertainmentConfig)
		if !ok {
			return nil
		}

		extra := make(map[string]any)
		if aventCfg.ScrapeBonusScreens {
			extra["scrape_bonus_screens"] = aventCfg.ScrapeBonusScreens
		}

		return &config.ScraperSettings{
			Enabled:       c.IsEnabled(),
			Language:      "",
			RateLimit:     c.GetRequestDelay(),
			BaseURL:       "https://www.aventertainments.com",
			Proxy:         proxyVal,
			DownloadProxy: downloadProxyVal,
			Extra:         extra,
		}
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
