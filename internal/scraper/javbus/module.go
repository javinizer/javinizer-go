package javbus

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "javbus",
		Description: "JavBus",
		Options: []models.ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "ja",
				Choices: []models.ScraperChoice{
					{Value: "ja", Label: "Japanese"},
					{Value: "en", Label: "English"},
					{Value: "zh", Label: "Chinese"},
				},
			},
			{
				Key:         "rate_limit",
				Label:       "Rate Limit",
				Description: "Delay between requests in milliseconds to avoid rate limiting",
				Type:        "number",
				Min:         scraperutil.IntPtr(0),
				Max:         scraperutil.IntPtr(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "JavBus base URL (leave default unless you need a mirror)",
				Type:        "string",
			},
			{
				Key:         "use_flaresolverr",
				Label:       "Use FlareSolverr",
				Description: "Route requests through FlareSolverr to bypass Cloudflare protection",
				Type:        "boolean",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			Language:  "ja",
			RateLimit: 1000,
			BaseURL:   "https://www.javbus.com",
		},
		Priority: 70,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
