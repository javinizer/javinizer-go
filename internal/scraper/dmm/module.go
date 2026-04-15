package dmm

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

func (m *scraperModule) Name() string        { return "dmm" }
func (m *scraperModule) Description() string { return "DMM/Fanza" }
func (m *scraperModule) Constructor() any {
	return func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		contentIDRepo := database.NewContentIDMappingRepository(db)
		return New(settings, globalConfig, contentIDRepo), nil
	}
}
func (m *scraperModule) Validator() any {
	return scraperutil.ValidatorFunc(func(a any) error {
		return (&DMMConfig{}).ValidateConfig(a.(*config.ScraperSettings))
	})
}
func (m *scraperModule) ConfigFactory() any {
	return scraperutil.ConfigFactory(func() any { return &DMMConfig{} })
}
func (m *scraperModule) Options() any {
	return []any{
		contracts.ScraperOption{
			Key:         "use_browser",
			Label:       "Use Browser",
			Description: "Enable browser automation for this scraper. Requires global 'Use Browser' to be enabled.",
			Type:        "boolean",
		},
		contracts.ScraperOption{
			Key:         "scrape_actress",
			Label:       "Scrape Actress Information",
			Description: "Override global setting: Extract actress names and IDs. Requires global 'Scrape Actress Information' to be enabled.",
			Type:        "boolean",
		},
		contracts.ScraperOption{
			Key:         "placeholder_threshold",
			Label:       "Placeholder Threshold",
			Description: "File size threshold in KB for detecting placeholder images. Files smaller than this are considered potential placeholders.",
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
		Enabled: false,
	}
}
func (m *scraperModule) Priority() int { return 90 }
func proxyAsConfig(p any) *config.ProxyConfig {
	if p == nil {
		return nil
	}
	return p.(*config.ProxyConfig)
}

func (m *scraperModule) FlattenFunc() any {
	return scraperutil.DefaultFlattenConfigWithRaw(scraperutil.FlattenOverrides{}, func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides, raw any) any {
		s := &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: proxyAsConfig(fc.Proxy), DownloadProxy: proxyAsConfig(fc.DownloadProxy)}
		if dmmCfg, ok := raw.(*DMMConfig); ok {
			s.UseBrowser = dmmCfg.UseBrowser
			if dmmCfg.ScrapeActress {
				s.ScrapeActress = &dmmCfg.ScrapeActress
			}
		}
		return s
	})
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
