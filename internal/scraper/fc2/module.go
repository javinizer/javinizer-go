package fc2

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

func (m *scraperModule) Name() string        { return "fc2" }
func (m *scraperModule) Description() string { return "FC2" }
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
		return (&FC2Config{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &FC2Config{} })
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
			Description: "FC2 base URL",
			Type:        "string",
		},
	}
}
func (m *scraperModule) Defaults() any {
	return config.ScraperSettings{
		Enabled:   false,
		RateLimit: 1000,
		BaseURL:   "https://adult.contents.fc2.com",
	}
}
func (m *scraperModule) Priority() int { return 35 }
func proxyAsConfig(p any) *config.ProxyConfig {
	if p == nil {
		return nil
	}
	return p.(*config.ProxyConfig)
}

func (m *scraperModule) FlattenFunc() any {
	return scraperutil.DefaultFlattenConfig(scraperutil.FlattenOverrides{BaseURL: "https://adult.contents.fc2.com"}, func(fc *scraperutil.FlattenedConfig, o scraperutil.FlattenOverrides) any {
		return &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, BaseURL: o.BaseURL, Proxy: proxyAsConfig(fc.Proxy), DownloadProxy: proxyAsConfig(fc.DownloadProxy)}
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
