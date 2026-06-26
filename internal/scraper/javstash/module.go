package javstash

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "javstash",
		Description: "Javstash",
		Options: []models.ScraperOption{
			{
				Key:         "api_key",
				Label:       "API Key",
				Description: "API key for Javstash.org authentication",
				Type:        "password",
				Default:     "",
			},
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
				Key:         "base_url",
				Label:       "Base url",
				Description: "GraphQL API endpoint url",
				Type:        "string",
				Default:     "https://javstash.org/graphql",
			},
			{
				Key:         "rate_limit",
				Label:       "Rate Limit",
				Description: "Delay between requests in milliseconds",
				Type:        "number",
				Default:     1000,
				Min:         scraperutil.IntPtr(0),
				Max:         scraperutil.IntPtr(60000),
				Unit:        "ms",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			Language:  "en",
			BaseURL:   "https://javstash.org/graphql",
			RateLimit: 1000,
		},
		Priority: 10,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
