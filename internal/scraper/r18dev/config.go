package r18dev

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
)

// R18DevConfig holds R18.dev scraper configuration.
// YAML tags are defined here for unmarshaling via config.ScrapersConfig.
type R18DevConfig struct {
	Enabled           bool                      `yaml:"enabled" json:"enabled"`
	Language          string                    `yaml:"language" json:"language"`                                 // Language code: en, ja (default: en)
	RequestDelay      int                       `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	MaxRetries        int                       `yaml:"max_retries" json:"max_retries"`                           // Maximum number of retry attempts for rate-limited requests
	RespectRetryAfter bool                      `yaml:"respect_retry_after" json:"respect_retry_after"`           // Whether to respect Retry-After header from server
	UserAgent         string                    `yaml:"user_agent" json:"user_agent"`                             // Custom User-Agent for this scraper
	Proxy             *config.ProxyConfig       `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy     *config.ProxyConfig       `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
	Priority          int                       `yaml:"priority" json:"priority"`                                 // Scraper's priority (higher = higher priority)
	FlareSolverr      config.FlareSolverrConfig `yaml:"flaresolverr" json:"flaresolverr"`                         // FlareSolverr config for Cloudflare bypass
}

// IsEnabled implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) IsEnabled() bool { return c.Enabled }

// GetUserAgent implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) GetUserAgent() string { return c.UserAgent }

// GetRequestDelay implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) GetRequestDelay() int { return c.RequestDelay }

// GetMaxRetries implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) GetMaxRetries() int { return 0 }

// GetProxy implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) GetProxy() any { return c.Proxy }

// GetDownloadProxy implements scraperutil.ScraperConfigInterface.
func (c *R18DevConfig) GetDownloadProxy() any { return c.DownloadProxy }

// ValidateConfig implements config.ConfigValidator for R18DevConfig.
func (c *R18DevConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if sc == nil {
		return fmt.Errorf("r18dev: config is nil")
	}
	if !sc.Enabled {
		return nil // Disabled is valid
	}
	// Validate language if set
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en":
		// Valid
	case "ja":
		// Valid
	default:
		return fmt.Errorf("r18dev: language must be 'en' or 'ja', got %q", sc.Language)
	}
	// Validate rate limit
	if sc.RateLimit < 0 {
		return fmt.Errorf("r18dev: rate_limit must be non-negative, got %d", sc.RateLimit)
	}
	// Validate retry count
	if sc.RetryCount < 0 {
		return fmt.Errorf("r18dev: retry_count must be non-negative, got %d", sc.RetryCount)
	}
	// Validate timeout
	if sc.Timeout < 0 {
		return fmt.Errorf("r18dev: timeout must be non-negative, got %d", sc.Timeout)
	}
	return nil
}
