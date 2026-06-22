package aventertainment

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "aventertainment",
		Description: "AV Entertainment",
		Options: []models.ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "en",
				Choices: []models.ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
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
				Description: "AV Entertainment base URL",
				Type:        "string",
			},
			{
				Key:         "scrape_bonus_screens",
				Label:       "Scrape bonus screenshots",
				Description: "Append bonus image files to screenshots",
				Type:        "boolean",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			Language:  "en",
			RateLimit: 1000,
			BaseURL:   "https://www.aventertainments.com",
		},
		Priority: 45,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
