package dmm

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
)

// Config holds DMM/Fanza scraper configuration.
// YAML tags are defined here for unmarshaling via config.ScrapersConfig.
type DMMConfig struct {
	Enabled       bool                `yaml:"enabled" json:"enabled"`
	RequestDelay  int                 `yaml:"request_delay" json:"request_delay"`
	MaxRetries    int                 `yaml:"max_retries" json:"max_retries"`
	UserAgent     string              `yaml:"user_agent" json:"user_agent"`
	Proxy         *config.ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	DownloadProxy *config.ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"`
	Priority      int                 `yaml:"priority" json:"priority"` // Scraper's priority (higher = higher priority)
	// Per-scraper browser and scrape_actress settings
	UseBrowser    bool `yaml:"use_browser" json:"use_browser"`
	ScrapeActress bool `yaml:"scrape_actress" json:"scrape_actress"`
	// Placeholder detection settings
	PlaceholderThresholdKB int      `yaml:"placeholder_threshold" json:"placeholder_threshold"`
	ExtraPlaceholderHashes []string `yaml:"extra_placeholder_hashes" json:"extra_placeholder_hashes"`
}

// IsEnabled implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) IsEnabled() bool { return c.Enabled }

// GetUserAgent implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) GetUserAgent() string { return c.UserAgent }

// GetRequestDelay implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) GetRequestDelay() int { return c.RequestDelay }

// GetMaxRetries implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) GetMaxRetries() int { return c.MaxRetries }

// GetProxy implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) GetProxy() any { return c.Proxy }

// GetDownloadProxy implements scraperutil.ScraperConfigInterface.
func (c *DMMConfig) GetDownloadProxy() any { return c.DownloadProxy }

// ValidateConfig implements config.ConfigValidator for DMMConfig.
func (c *DMMConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if sc == nil {
		return fmt.Errorf("dmm: config is nil")
	}
	if !sc.Enabled {
		return nil // Disabled is valid
	}
	// Validate rate limit
	if sc.RateLimit < 0 {
		return fmt.Errorf("dmm: rate_limit must be non-negative, got %d", sc.RateLimit)
	}
	// Validate retry count
	if sc.RetryCount < 0 {
		return fmt.Errorf("dmm: retry_count must be non-negative, got %d", sc.RetryCount)
	}
	// Validate timeout
	if sc.Timeout < 0 {
		return fmt.Errorf("dmm: timeout must be non-negative, got %d", sc.Timeout)
	}
	return nil
}

// ToScraperSettings converts DMMConfig to ScraperSettings, flowing placeholder
// settings to Extra map for runtime access by placeholder detection functions.
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
