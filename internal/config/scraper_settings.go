package config

import "encoding/json"

// ScraperSettings holds common scraper configuration fields used by the Scraper interface.
// Individual scraper configs embed this and add scraper-specific fields.
// CONF-01: All fields are present: Enabled, Timeout, RateLimit, RetryCount,
// UseFlareSolverr, UserAgent, Cookies.
type ScraperSettings struct {
	Enabled         bool         `yaml:"enabled" json:"enabled"`
	Language        string       `yaml:"language" json:"language"`                                 // Language code varies by scraper
	Timeout         int          `yaml:"timeout" json:"timeout"`                                   // HTTP client timeout in seconds
	RateLimit       int          `yaml:"rate_limit" json:"rate_limit"`                             // Request delay in milliseconds (mirrors RequestDelay)
	RetryCount      int          `yaml:"retry_count" json:"retry_count"`                           // Max retries (mirrors MaxRetries)
	UserAgent       string       `yaml:"user_agent" json:"user_agent"`                             // Custom User-Agent; if empty, configutil.DefaultScraperUserAgent is used
	Proxy           *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy   *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
	BaseURL         string       `yaml:"base_url,omitempty" json:"base_url,omitempty"`             // Base URL for the scraper
	UseFlareSolverr bool         `yaml:"use_flaresolverr" json:"use_flaresolverr"`                 // Whether to use FlareSolverr for this scraper

	// NEW: Per-scraper toggle for browser (mirrors use_flaresolverr pattern)
	UseBrowser bool `yaml:"use_browser" json:"use_browser"`

	// NEW: Per-scraper override for scrape_actress (nil = inherit global)
	ScrapeActress *bool `yaml:"scrape_actress,omitempty" json:"scrape_actress,omitempty"`

	Cookies map[string]string `yaml:"cookies,omitempty" json:"cookies,omitempty"` // CONF-06: scraper-specific cookies
	Extra   map[string]any    `yaml:"-" json:"-"`                                 // Scraper-specific fields, flattened on marshal
}

// MarshalYAML preserves the full unified scraper settings shape so config
// save/load round-trips do not drop scraper-specific data.
// Extra fields are flattened to the top level for clean YAML output.
func (s *ScraperSettings) MarshalYAML() (interface{}, error) {
	// Nil receiver guard
	if s == nil {
		return nil, nil
	}

	// Create result map
	result := make(map[string]any)

	// Add standard fields
	result["enabled"] = s.Enabled
	result["language"] = s.Language
	result["timeout"] = s.Timeout
	result["rate_limit"] = s.RateLimit
	result["retry_count"] = s.RetryCount
	result["user_agent"] = s.UserAgent
	if s.Proxy != nil {
		result["proxy"] = s.Proxy
	}
	if s.DownloadProxy != nil {
		result["download_proxy"] = s.DownloadProxy
	}
	if s.BaseURL != "" {
		result["base_url"] = s.BaseURL
	}
	result["use_flaresolverr"] = s.UseFlareSolverr

	// NEW: Include use_browser and scrape_actress
	result["use_browser"] = s.UseBrowser
	if s.ScrapeActress != nil {
		result["scrape_actress"] = *s.ScrapeActress
	}

	if len(s.Cookies) > 0 {
		result["cookies"] = s.Cookies
	}

	// Flatten Extra fields at top level
	for k, v := range s.Extra {
		result[k] = v
	}

	return result, nil
}

// MarshalJSON preserves the full unified scraper settings shape for JSON serialization.
// Extra fields are flattened to the top level, matching MarshalYAML behavior.
func (s *ScraperSettings) MarshalJSON() ([]byte, error) {
	// Reuse MarshalYAML logic
	result, err := s.MarshalYAML()
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// ToScraperSettings implements ScraperSettingsAdapter.
func (s *ScraperSettings) ToScraperSettings() *ScraperSettings {
	return s
}

// GetBaseURL returns the base URL for the scraper, or an empty string if not set.
func (sc *ScraperSettings) GetBaseURL() string {
	return sc.BaseURL
}

// DeepCopy creates a deep copy of ScraperSettings, ensuring that pointer fields
// (Proxy, DownloadProxy) and map fields (Cookies) are properly isolated from the original.
// This prevents mutation leaks when settings are shared between config instances.
func (s *ScraperSettings) DeepCopy() *ScraperSettings {
	if s == nil {
		return nil
	}
	copy := &ScraperSettings{
		Enabled:         s.Enabled,
		Language:        s.Language,
		Timeout:         s.Timeout,
		RateLimit:       s.RateLimit,
		RetryCount:      s.RetryCount,
		UserAgent:       s.UserAgent,
		UseFlareSolverr: s.UseFlareSolverr,
		UseBrowser:      s.UseBrowser, // NEW
		BaseURL:         s.BaseURL,
	}

	// Deep copy Proxy if not nil (profile-based only)
	if s.Proxy != nil {
		copy.Proxy = &ProxyConfig{
			Enabled:        s.Proxy.Enabled,
			Profile:        s.Proxy.Profile,
			DefaultProfile: s.Proxy.DefaultProfile,
			Profiles:       deepCopyProxyProfiles(s.Proxy.Profiles),
		}
	}

	// Deep copy DownloadProxy if not nil (profile-based only)
	if s.DownloadProxy != nil {
		copy.DownloadProxy = &ProxyConfig{
			Enabled:        s.DownloadProxy.Enabled,
			Profile:        s.DownloadProxy.Profile,
			DefaultProfile: s.DownloadProxy.DefaultProfile,
			Profiles:       deepCopyProxyProfiles(s.DownloadProxy.Profiles),
		}
	}

	// Deep copy Cookies map
	if s.Cookies != nil {
		copy.Cookies = make(map[string]string, len(s.Cookies))
		for k, v := range s.Cookies {
			copy.Cookies[k] = v
		}
	}

	// Deep copy Extra map
	if s.Extra != nil {
		copy.Extra = make(map[string]any, len(s.Extra))
		for k, v := range s.Extra {
			copy.Extra[k] = v
		}
	}

	// Deep copy ScrapeActress pointer if set
	if s.ScrapeActress != nil {
		val := *s.ScrapeActress
		copy.ScrapeActress = &val
	}

	return copy
}

// ShouldScrapeActress returns whether actress scraping is enabled for this scraper.
// Returns per-scraper override if set, otherwise falls back to global default.
func (s *ScraperSettings) ShouldScrapeActress(globalDefault bool) bool {
	if s == nil {
		return globalDefault
	}
	if s.ScrapeActress != nil {
		return *s.ScrapeActress // Per-scraper override wins
	}
	return globalDefault // Fall back to global ScrapersConfig.ScrapeActress
}

// SetScrapeActress sets the per-scraper scrape_actress override.
func (s *ScraperSettings) SetScrapeActress(value bool) {
	if s == nil {
		return
	}
	s.ScrapeActress = &value
}

// ShouldUseBrowser returns whether browser automation is enabled for this scraper.
// Checks global enabled first, then per-scraper toggle.
func (s *ScraperSettings) ShouldUseBrowser(globalEnabled bool) bool {
	if s == nil {
		return false
	}
	if !globalEnabled {
		return false // Global disabled = no browser for anyone
	}
	return s.UseBrowser // Per-scraper toggle (default: false)
}

// deepCopyProxyProfiles creates a deep copy of the proxy profiles map
func deepCopyProxyProfiles(profiles map[string]ProxyProfile) map[string]ProxyProfile {
	if profiles == nil {
		return nil
	}

	copy := make(map[string]ProxyProfile, len(profiles))
	for k, v := range profiles {
		copy[k] = ProxyProfile{
			URL:      v.URL,
			Username: v.Username,
			Password: v.Password,
		}
	}
	return copy
}

// ScraperCommonConfig holds common scraper configuration fields used by ScraperConfigInterface.
// Embed this struct in all scraper-specific configs with `yaml:",inline"` to automatically
// satisfy the interface without boilerplate wrapper methods.
type ScraperCommonConfig struct {
	Enabled       bool            `yaml:"enabled"`
	RequestDelay  int             `yaml:"request_delay"`
	MaxRetries    int             `yaml:"max_retries"`
	UserAgent     UserAgentString `yaml:"user_agent"`
	Proxy         *ProxyConfig    `yaml:"proxy,omitempty"`
	DownloadProxy *ProxyConfig    `yaml:"download_proxy,omitempty"`
}

// IsEnabled implements ScraperConfigInterface.
func (c ScraperCommonConfig) IsEnabled() bool { return c.Enabled }

// GetUserAgent implements ScraperConfigInterface.
func (c ScraperCommonConfig) GetUserAgent() string { return c.UserAgent.Value }

// GetRequestDelay implements ScraperConfigInterface.
func (c ScraperCommonConfig) GetRequestDelay() int { return c.RequestDelay }

// GetMaxRetries implements ScraperConfigInterface.
func (c ScraperCommonConfig) GetMaxRetries() int { return c.MaxRetries }

// GetProxy implements ScraperConfigInterface.
func (c ScraperCommonConfig) GetProxy() any { return c.Proxy }

// GetDownloadProxy implements ScraperConfigInterface.
func (c ScraperCommonConfig) GetDownloadProxy() any { return c.DownloadProxy }
