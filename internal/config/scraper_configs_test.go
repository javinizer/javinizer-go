package config

import "github.com/javinizer/javinizer-go/internal/models"

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// NewTestScraperConfigResolverInterface creates a models.ScraperConfigResolverInterface for testing.
func NewTestScraperConfigResolverInterface() models.ScraperConfigResolverInterface {
	return &staticTestConfigResolver{
		registered: map[string]bool{
			"r18dev": true, "dmm": true, "libredmm": true, "mgstage": true,
			"javlibrary": true, "javdb": true, "javbus": true, "jav321": true,
			"tokyohot": true, "aventertainment": true, "dlgetchu": true,
			"caribbeancom": true, "fc2": true, "javstash": true,
		},
		defaults: map[string]models.ScraperSettings{
			"r18dev":          {Enabled: true, Language: "en", UserAgent: DefaultUserAgent, RespectRetryAfter: boolPtr(true)},
			"dmm":             {Enabled: false},
			"libredmm":        {Enabled: false, RateLimit: 1000, BaseURL: "https://www.libredmm.com"},
			"mgstage":         {Enabled: false, RateLimit: 1000},
			"javlibrary":      {Enabled: false, Language: "en", RateLimit: 1000, BaseURL: "http://www.javlibrary.com"},
			"javdb":           {Enabled: false, RateLimit: 1000, BaseURL: "https://javdb.com"},
			"javbus":          {Enabled: false, Language: "ja", RateLimit: 1000, BaseURL: "https://www.javbus.com"},
			"jav321":          {Enabled: false, Language: "ja", RateLimit: 1000, BaseURL: "https://jp.jav321.com"},
			"tokyohot":        {Enabled: false, Language: "ja", RateLimit: 1000, BaseURL: "https://www.tokyo-hot.com"},
			"aventertainment": {Enabled: false, Language: "en", RateLimit: 1000, BaseURL: "https://www.aventertainments.com"},
			"dlgetchu":        {Enabled: false, RateLimit: 1000, BaseURL: "http://dl.getchu.com"},
			"caribbeancom":    {Enabled: false, Language: "ja", RateLimit: 1000, BaseURL: "https://www.caribbeancom.com"},
			"fc2":             {Enabled: false, RateLimit: 1000, BaseURL: "https://adult.contents.fc2.com"},
			"javstash":        {Enabled: false, Language: "en", RateLimit: 1000, BaseURL: "https://javstash.org/graphql"},
		},
		priorities: map[string]int{
			"r18dev": 100, "libredmm": 95, "dmm": 90, "javlibrary": 80,
			"javdb": 75, "javbus": 70, "jav321": 65, "mgstage": 55,
			"tokyohot": 50, "aventertainment": 45, "caribbeancom": 40,
			"dlgetchu": 40, "fc2": 35, "javstash": 10,
		},
	}
}

type staticTestConfigResolver struct {
	registered map[string]bool
	defaults   map[string]models.ScraperSettings
	priorities map[string]int
}

func (r *staticTestConfigResolver) IsRegistered(name string) bool {
	return r.registered[name]
}

func (r *staticTestConfigResolver) GetAllDefaults() map[string]models.ScraperSettings {
	result := make(map[string]models.ScraperSettings, len(r.defaults))
	for k, v := range r.defaults {
		result[k] = v
	}
	return result
}

func (r *staticTestConfigResolver) GetValidateFn(name string) func(*models.ScraperSettings) error {
	return nil
}
