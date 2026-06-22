package r18dev

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        "r18dev",
		Description: "R18.dev",
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
				Key:         "placeholder_threshold",
				Label:       "Placeholder Threshold",
				Description: "File size threshold in KB for detecting placeholder screenshots. Files smaller than this are checked against known placeholder hashes.",
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
			{
				Key:         "respect_retry_after",
				Label:       "Respect Retry-After",
				Description: "Respect the Retry-After header from Cloudflare on 429 rate-limit responses. When enabled, waits the server-specified duration before retrying instead of using only exponential backoff.",
				Type:        "boolean",
				Default:     true,
			},
		},
		Defaults: models.ScraperSettings{
			RespectRetryAfter: scraperutil.BoolPtr(true),
			Enabled:           true,
			Language:          "en",
			UserAgent:         config.DefaultUserAgent,
			Proxy:             &models.ProxyConfig{Enabled: false}, // r18.dev doesn't need a proxy (cd591290)
		},
		Priority: 100,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
