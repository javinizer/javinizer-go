package dmm

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "dmm",
		Description: "DMM/Fanza",
		Options: []models.ScraperOption{
			{
				Key:         "use_browser",
				Label:       "Use Browser",
				Description: "Enable browser automation for this scraper. Requires global 'Use Browser' to be enabled.",
				Type:        "boolean",
			},
			{
				Key:         "scrape_actress",
				Label:       "Scrape Actress Information",
				Description: "Override global setting: Extract actress names and IDs. Requires global 'Scrape Actress Information' to be enabled.",
				Type:        "boolean",
			},
			{
				Key:         "placeholder_threshold",
				Label:       "Placeholder Threshold",
				Description: "File size threshold in KB for detecting placeholder images. Files smaller than this are considered potential placeholders.",
				Type:        "number",
				Default:     10,
				Min:         scraperutil.IntPtr(1),
				Max:         scraperutil.IntPtr(1000),
				Unit:        "KB",
			},
			{
				Key:         "extra_placeholder_hashes",
				Label:       "Extra Placeholder Hashes",
				Description: "Additional SHA256 hashes of known placeholder images. Each hash is a 64-character hex string.",
				Type:        "string",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled: false,
		},
		Priority: 90,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			opts := dmmOptions{
				TimeoutSeconds: deps.TimeoutSeconds,
				ScrapeActress:  deps.ScrapeActress,
				Browser:        deps.Browser,
				ContentIDRepo:  deps.ContentIDRepo,
			}
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr, opts), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
