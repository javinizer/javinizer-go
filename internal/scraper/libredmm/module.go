package libredmm

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

func (m *scraperModule) Name() string        { return "libredmm" }
func (m *scraperModule) Description() string { return "LibreDMM" }
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
		return (&LibreDMMConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &LibreDMMConfig{} })
}
func (m *scraperModule) Options() any {
	return []any{
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
			Description: "LibreDMM base URL",
			Type:        "string",
		},
		contracts.ScraperOption{
			Key:         "placeholder_threshold",
			Label:       "Placeholder Threshold",
			Description: "File size threshold in KB for detecting placeholder screenshots. Files smaller than this are checked against known placeholder hashes.",
			Type:        "number",
			Default:     10,
			Min:         scraperutil.IntPtr(1),
			Max:         scraperutil.IntPtr(1000),
			Unit:        "KB",
		},
		contracts.ScraperOption{
			Key:         "extra_placeholder_hashes",
			Label:       "Extra Placeholder Hashes",
			Description: "Additional SHA256 hashes of known placeholder images. Each hash is a 64-character hex string.",
			Type:        "string",
		},
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:   false,
		RateLimit: 1000,
		BaseURL:   "https://www.libredmm.com",
	}
}
func (m *scraperModule) Priority() int { return 95 }
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
			RateLimit:     c.GetRequestDelay(),
			BaseURL:       "https://www.libredmm.com",
			Proxy:         proxyVal,
			DownloadProxy: downloadProxyVal,
		}
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
