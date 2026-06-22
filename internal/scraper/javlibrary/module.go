package javlibrary

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "javlibrary",
		Description: "JavLibrary",
		Options: []models.ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "ja",
				Choices: []models.ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
					{Value: "cn", Label: "Chinese (Simplified)"},
					{Value: "tw", Label: "Chinese (Traditional)"},
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
				Description: "JavLibrary base URL (leave default unless you need a mirror)",
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
			Language:  "en",
			RateLimit: 1000,
			BaseURL:   "http://www.javlibrary.com",
		},
		Priority: 80,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
