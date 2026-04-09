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
	// NEW: Per-scraper browser and scrape_actress settings
	UseBrowser    bool `yaml:"use_browser" json:"use_browser"`
	ScrapeActress bool `yaml:"scrape_actress" json:"scrape_actress"`
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
	// Note: Browser and scrape_actress settings are now global, not scraper-specific
	return nil
}
