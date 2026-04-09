package aventertainment

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

// Config holds AVEntertainment scraper configuration.
// YAML tags are defined here for unmarshaling via config.ScrapersConfig.
type AVEntertainmentConfig struct {
	Enabled            bool                      `yaml:"enabled" json:"enabled"`
	Language           string                    `yaml:"language" json:"language"`
	RequestDelay       int                       `yaml:"request_delay" json:"request_delay"`
	MaxRetries         int                       `yaml:"max_retries" json:"max_retries"`
	BaseURL            string                    `yaml:"base_url" json:"base_url"`
	ScrapeBonusScreens bool                      `yaml:"scrape_bonus_screens" json:"scrape_bonus_screens"`
	UserAgent          string                    `yaml:"user_agent" json:"user_agent"`
	Proxy              *config.ProxyConfig       `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	DownloadProxy      *config.ProxyConfig       `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"`
	Priority           int                       `yaml:"priority" json:"priority"` // Scraper's priority (higher = higher priority)
	FlareSolverr       config.FlareSolverrConfig `yaml:"flaresolverr" json:"flaresolverr"`
}

// IsEnabled implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) IsEnabled() bool { return c.Enabled }

// GetUserAgent implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) GetUserAgent() string { return c.UserAgent }

// GetRequestDelay implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) GetRequestDelay() int { return c.RequestDelay }

// GetMaxRetries implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) GetMaxRetries() int { return c.MaxRetries }

// GetProxy implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) GetProxy() any { return c.Proxy }

// GetDownloadProxy implements scraperutil.ScraperConfigInterface.
func (c *AVEntertainmentConfig) GetDownloadProxy() any { return c.DownloadProxy }

// ValidateConfig implements config.ConfigValidator for AVEntertainmentConfig.
func (c *AVEntertainmentConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if sc == nil {
		return fmt.Errorf("aventertainment: config is nil")
	}
	if !sc.Enabled {
		return nil // Disabled is valid
	}
	// Validate language if set
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en", "ja":
		// Valid
	default:
		return fmt.Errorf("aventertainment: language must be 'en' or 'ja', got %q", sc.Language)
	}
	// Validate rate limit
	if sc.RateLimit < 0 {
		return fmt.Errorf("aventertainment: rate_limit must be non-negative, got %d", sc.RateLimit)
	}
	// Validate retry count
	if sc.RetryCount < 0 {
		return fmt.Errorf("aventertainment: retry_count must be non-negative, got %d", sc.RetryCount)
	}
	// Validate timeout
	if sc.Timeout < 0 {
		return fmt.Errorf("aventertainment: timeout must be non-negative, got %d", sc.Timeout)
	}
	// Validate base URL if set
	if err := configutil.ValidateHTTPBaseURL("aventertainment.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
