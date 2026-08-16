package minnanoav

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Register ...
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "minnanoav",
		Description: "Minnano-AV (actress metadata only)",
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
				Description: "Minnano-AV base URL (leave default unless you need a mirror)",
				Type:        "string",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   defaultBaseURL,
			Proxy:     &models.ProxyConfig{Enabled: false},
		},
		Priority: 10,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
