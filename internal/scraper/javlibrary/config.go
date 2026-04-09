package javlibrary

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

// Config holds JavLibrary scraper configuration.
// YAML tags are defined here for unmarshaling via config.ScrapersConfig.
type JavLibraryConfig struct {
	Enabled       bool                `yaml:"enabled" json:"enabled"`
	Language      string              `yaml:"language" json:"language"`
	RequestDelay  int                 `yaml:"request_delay" json:"request_delay"`
	BaseURL       string              `yaml:"base_url" json:"base_url"`
	Cookies       map[string]string   `yaml:"cookies,omitempty" json:"cookies,omitempty"` // For cf_clearance, cf_bm, etc.
	UserAgent     string              `yaml:"user_agent" json:"user_agent"`
	Proxy         *config.ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	DownloadProxy *config.ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"`
	Priority      int                 `yaml:"priority" json:"priority"` // Scraper's priority (higher = higher priority)
}

// IsEnabled implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) IsEnabled() bool { return c.Enabled }

// GetUserAgent implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) GetUserAgent() string { return c.UserAgent }

// GetRequestDelay implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) GetRequestDelay() int { return c.RequestDelay }

// GetMaxRetries implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) GetMaxRetries() int { return 0 }

// GetProxy implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) GetProxy() any { return c.Proxy }

// GetDownloadProxy implements scraperutil.ScraperConfigInterface.
func (c *JavLibraryConfig) GetDownloadProxy() any { return c.DownloadProxy }

// ValidateConfig implements config.ConfigValidator for JavLibraryConfig.
func (c *JavLibraryConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if sc == nil {
		return fmt.Errorf("javlibrary: config is nil")
	}
	if !sc.Enabled {
		return nil // Disabled is valid
	}
	// Validate language
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en":
		// Valid
	case "ja":
		// Valid
	case "cn":
		// Valid
	case "tw":
		// Valid
	default:
		return fmt.Errorf("javlibrary: language must be 'en', 'ja', 'cn', or 'tw', got %q", sc.Language)
	}
	// Validate rate limit
	if sc.RateLimit < 0 {
		return fmt.Errorf("javlibrary: rate_limit must be non-negative, got %d", sc.RateLimit)
	}
	// Validate timeout
	if sc.Timeout < 0 {
		return fmt.Errorf("javlibrary: timeout must be non-negative, got %d", sc.Timeout)
	}
	// Validate base URL if set
	if err := configutil.ValidateHTTPBaseURL("javlibrary.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
