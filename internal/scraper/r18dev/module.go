package r18dev

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

func (m *scraperModule) Name() string        { return "r18dev" }
func (m *scraperModule) Description() string { return "R18.dev" }
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
		return (&R18DevConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &R18DevConfig{} })
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
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:  true,
		Language: "en",
	}
}
func (m *scraperModule) Priority() int { return 100 }
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
		if r18Cfg, ok := cfg.(*R18DevConfig); ok {
			_ = r18Cfg
			return &config.ScraperSettings{
				Enabled:       c.IsEnabled(),
				Language:      "",
				RateLimit:     c.GetRequestDelay(),
				Proxy:         proxyVal,
				DownloadProxy: downloadProxyVal,
			}
		}
		return nil
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
