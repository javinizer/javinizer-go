package fc2

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "fc2",
		Description: "FC2",
		Options: []models.ScraperOption{
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
				Description: "FC2 base URL",
				Type:        "string",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   "https://adult.contents.fc2.com",
		},
		Priority: 35,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
