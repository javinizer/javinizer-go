package javstash

import (
	"fmt"
	"os"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type JavstashConfig struct {
	Enabled       bool                `yaml:"enabled" json:"enabled"`
	APIKey        string              `yaml:"api_key" json:"api_key"`
	BaseURL       string              `yaml:"base_url" json:"base_url"`
	Language      string              `yaml:"language" json:"language"`
	RequestDelay  int                 `yaml:"request_delay" json:"request_delay"`
	UserAgent     string              `yaml:"user_agent" json:"user_agent"`
	Proxy         *config.ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	DownloadProxy *config.ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"`
}

func (c *JavstashConfig) IsEnabled() bool { return c.Enabled }

func (c *JavstashConfig) GetUserAgent() string { return c.UserAgent }

func (c *JavstashConfig) GetRequestDelay() int { return c.RequestDelay }

func (c *JavstashConfig) GetMaxRetries() int { return 0 }

func (c *JavstashConfig) GetProxy() any { return c.Proxy }

func (c *JavstashConfig) GetDownloadProxy() any { return c.DownloadProxy }

func (c *JavstashConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if sc == nil {
		return fmt.Errorf("javstash: config is nil")
	}
	if !sc.Enabled {
		return nil
	}

	apiKey := ""
	if v, ok := sc.Extra["api_key"].(string); ok {
		apiKey = strings.TrimSpace(v)
	}
	if apiKey == "" {
		apiKey = os.Getenv("JAVSTASH_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("javstash: api_key is required (set in config or JAVSTASH_API_KEY env var)")
	}

	if sc.RateLimit < 0 {
		return fmt.Errorf("javstash: rate_limit must be non-negative, got %d", sc.RateLimit)
	}
	if sc.RetryCount < 0 {
		return fmt.Errorf("javstash: retry_count must be non-negative, got %d", sc.RetryCount)
	}
	if sc.Timeout < 0 {
		return fmt.Errorf("javstash: timeout must be non-negative, got %d", sc.Timeout)
	}
	if err := configutil.ValidateHTTPBaseURL("javstash.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
