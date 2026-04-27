package dmm

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

type DMMConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	UseBrowser               bool     `yaml:"use_browser" json:"use_browser"`
	ScrapeActress            bool     `yaml:"scrape_actress" json:"scrape_actress"`
	PlaceholderThresholdKB   int      `yaml:"placeholder_threshold" json:"placeholder_threshold"`
	ExtraPlaceholderHashes   []string `yaml:"extra_placeholder_hashes" json:"extra_placeholder_hashes"`
}

func (c *DMMConfig) ValidateConfig(sc *config.ScraperSettings) error {
	return config.ValidateCommonSettings("dmm", sc)
}

func (c *DMMConfig) ToScraperSettings() *config.ScraperSettings {
	settings := &config.ScraperSettings{
		Enabled:       c.Enabled,
		RateLimit:     c.RequestDelay,
		RetryCount:    c.MaxRetries,
		UserAgent:     c.UserAgent,
		Proxy:         c.Proxy,
		DownloadProxy: c.DownloadProxy,
		UseBrowser:    c.UseBrowser,
		ScrapeActress: &c.ScrapeActress,
	}
	settings.Extra = make(map[string]any)
	if c.PlaceholderThresholdKB > 0 {
		settings.Extra["placeholder_threshold"] = c.PlaceholderThresholdKB
	}
	if len(c.ExtraPlaceholderHashes) > 0 {
		settings.Extra["extra_placeholder_hashes"] = c.ExtraPlaceholderHashes
	}
	return settings
}
