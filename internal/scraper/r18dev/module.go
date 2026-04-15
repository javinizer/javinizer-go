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
		Enabled:  true,
		Language: "en",
	}
}

func (m *scraperModule) Priority() int { return 100 }
func proxyAsConfig(p any) *config.ProxyConfig {
	if p == nil {
		return nil
	}
	return p.(*config.ProxyConfig)
}

func (m *scraperModule) FlattenFunc() any {
	return scraperutil.DefaultFlattenConfig(scraperutil.FlattenOverrides{}, func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides) any {
		return &config.ScraperSettings{Enabled: fc.Enabled, Language: "", RateLimit: fc.RateLimit, Proxy: proxyAsConfig(fc.Proxy), DownloadProxy: proxyAsConfig(fc.DownloadProxy)}
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
