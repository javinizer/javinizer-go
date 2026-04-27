package dmm

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        "dmm",
		ScraperDescription: "DMM/Fanza",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "use_browser",
				Label:       "Use Browser",
				Description: "Enable browser automation for this scraper. Requires global 'Use Browser' to be enabled.",
				Type:        "boolean",
			},
			models.ScraperOption{
				Key:         "scrape_actress",
				Label:       "Scrape Actress Information",
				Description: "Override global setting: Extract actress names and IDs. Requires global 'Scrape Actress Information' to be enabled.",
				Type:        "boolean",
			},
			models.ScraperOption{
				Key:         "placeholder_threshold",
				Label:       "Placeholder Threshold",
				Description: "File size threshold in KB for detecting placeholder images. Files smaller than this are considered potential placeholders.",
				Type:        "number",
				Default:     10,
				Min:         scraperutil.IntPtr(1),
				Max:         scraperutil.IntPtr(1000),
				Unit:        "KB",
			},
			models.ScraperOption{
				Key:         "extra_placeholder_hashes",
				Label:       "Extra Placeholder Hashes",
				Description: "Additional SHA256 hashes of known placeholder images. Each hash is a 64-character hex string.",
				Type:        "string",
			},
		},
		ScraperDefaults: config.ScraperSettings{
			Enabled: false,
		},
		ScraperPriority: 90,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &DMMConfig{} },
		FlatBuilderRaw: func(fc *scraperutil.FlattenedConfig, _ scraperutil.FlattenOverrides, raw any) any {
			s := &config.ScraperSettings{Enabled: fc.Enabled, RateLimit: fc.RateLimit, Proxy: config.ProxyAsConfig(fc.Proxy), DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy)}
			if dmmCfg, ok := raw.(*DMMConfig); ok {
				s.UseBrowser = dmmCfg.UseBrowser
				if dmmCfg.ScrapeActress {
					s.ScrapeActress = &dmmCfg.ScrapeActress
				}
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

func (m *scraperModule) Constructor() any {
	return func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		contentIDRepo := database.NewContentIDMappingRepository(db)
		return New(settings, globalConfig, contentIDRepo), nil
	}
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
