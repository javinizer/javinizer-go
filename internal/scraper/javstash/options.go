package javstash

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	scraperutil.RegisterScraperOptions("javstash", scraperutil.ScraperOptionsProvider{
		DisplayName: "Javstash",
		Options: []any{
			contracts.ScraperOption{
				Key:         "api_key",
				Label:       "API Key",
				Description: "API key for Javstash.org authentication",
				Type:        "password",
				Default:     "",
			},
			contracts.ScraperOption{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields",
				Type:        "select",
				Default:     "en",
				Choices: []contracts.ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
				},
			},
			contracts.ScraperOption{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "GraphQL API endpoint URL",
				Type:        "string",
				Default:     "https://javstash.org/graphql",
			},
			contracts.ScraperOption{
				Key:         "request_delay",
				Label:       "Request Delay",
				Description: "Delay between requests in milliseconds",
				Type:        "number",
				Default:     "1000",
			},
		},
	})
}
