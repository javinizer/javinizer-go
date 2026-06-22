// Package scraperconfig holds configuration-infrastructure types that describe
// how scrapers are configured, how proxies are resolved, and how browser/FlareSolverr
// automation is set up. These types were extracted from internal/models so that
// models remains a pure domain-model package.
package scraperconfig

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProxyProfile holds reusable proxy connection settings.
type ProxyProfile struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

// ProxyConfig holds HTTP/SOCKS5 proxy configuration.
type ProxyConfig struct {
	Enabled        bool                    `yaml:"enabled" json:"enabled"`
	Profile        string                  `yaml:"profile,omitempty" json:"profile,omitempty"`
	DefaultProfile string                  `yaml:"default_profile,omitempty" json:"default_profile,omitempty"`
	Profiles       map[string]ProxyProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for ProxyConfig.
// It validates that no legacy proxy fields (url, username, password, use_main_proxy)
// are present at the YAML level, then decodes into the struct.
func (p *ProxyConfig) UnmarshalYAML(node *yaml.Node) error {
	if err := rejectUnknownProxyFields(node, "proxy"); err != nil {
		return err
	}
	type plain ProxyConfig
	return node.Decode((*plain)(p))
}

// rejectUnknownProxyFields checks if the raw YAML node contains legacy proxy fields
// that are no longer supported. Returns an error if any are found.
func rejectUnknownProxyFields(node *yaml.Node, context string) error {
	if node == nil {
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return nil
	}

	legacyFields := []string{"url", "username", "password", "use_main_proxy"}

	for i := 0; i < len(node.Content); i += 2 {
		if i < len(node.Content) {
			keyNode := node.Content[i]
			key := strings.ToLower(keyNode.Value)

			for _, legacy := range legacyFields {
				if key == legacy {
					return fmt.Errorf(
						"%s: field '%s' is no longer supported. "+
							"Use 'profile: <name>' to reference a proxy profile from scrapers.proxy.profiles instead",
						context, keyNode.Value,
					)
				}
			}
		}
	}

	return nil
}

// ScraperProxyMode represents how a scraper should use proxy
type ScraperProxyMode string

const (
	ScraperProxyModeDirect   ScraperProxyMode = "direct"
	ScraperProxyModeInherit  ScraperProxyMode = "inherit"
	ScraperProxyModeSpecific ScraperProxyMode = "specific"
)

// ResolveScraperProxy returns the effective proxy profile for a scraper based on
// the three proxy modes: direct (no proxy), inherit (use global default), or
// specific (use named profile with optional credential inheritance).
//
// Mode resolution follows this priority:
// 1. If global proxy is disabled → direct mode for all scrapers
// 2. If scraper override is disabled → direct mode for this scraper
// 3. If scraper override enabled with profile → specific mode (profile + inherit missing creds)
// 4. If scraper override enabled without profile → inherit mode (use global default)
// 5. If no scraper override → inherit mode (use global default)
//
// Note: FlareSolverr is handled separately via ScraperSettings.FlareSolverr and
// ScrapersConfig.FlareSolverr (global), not via ProxyConfig.
func ResolveScraperProxy(global ProxyConfig, scraperOverride *ProxyConfig) *ProxyProfile {
	mode := ResolveScraperProxyMode(global, scraperOverride)

	switch mode {
	case ScraperProxyModeDirect:
		return &ProxyProfile{} // Empty = no proxy
	case ScraperProxyModeInherit:
		return ResolveGlobalProxy(global)
	case ScraperProxyModeSpecific:
		// Look up the profile
		if scraperOverride != nil && scraperOverride.Profile != "" {
			if profile, ok := global.Profiles[scraperOverride.Profile]; ok {
				resolved := profile
				// Inherit credentials from global if omitted
				globalProfile := ResolveGlobalProxy(global)
				if resolved.URL == "" {
					resolved.URL = globalProfile.URL
				}
				if resolved.Username == "" {
					resolved.Username = globalProfile.Username
				}
				if resolved.Password == "" {
					resolved.Password = globalProfile.Password
				}
				return &resolved
			}
		}
		// Profile not found → fallback to inherit
		return ResolveGlobalProxy(global)
	}
	return &ProxyProfile{}
}

// ResolveGlobalProxy returns the effective global proxy profile, including the
// selected default profile when configured. Returns empty profile if proxy is disabled.
func ResolveGlobalProxy(global ProxyConfig) *ProxyProfile {
	if !global.Enabled {
		return &ProxyProfile{}
	}
	if global.DefaultProfile != "" {
		if profile, ok := global.Profiles[global.DefaultProfile]; ok {
			return &profile
		}
	}
	return &ProxyProfile{}
}

// ResolveScraperProxyMode determines the effective proxy mode for a scraper.
//
// Logic:
//   - If global proxy disabled → Direct (circuit breaker)
//   - If global proxy enabled + scraper override missing → Inherit
//   - If global proxy enabled + scraper override disabled → Direct (user opted out)
//   - If global proxy enabled + scraper override enabled + profile → Specific
//   - If global proxy enabled + scraper override enabled + no profile → Inherit
func ResolveScraperProxyMode(global ProxyConfig, scraperOverride *ProxyConfig) ScraperProxyMode {
	// Circuit breaker: global proxy disabled means all scrapers use Direct
	if !global.Enabled {
		return ScraperProxyModeDirect
	}

	// No scraper-specific config → Inherit global
	if scraperOverride == nil {
		return ScraperProxyModeInherit
	}

	// Scraper explicitly disabled → Direct (user wants no proxy for this scraper)
	if !scraperOverride.Enabled {
		return ScraperProxyModeDirect
	}

	// Scraper enabled with profile → Specific
	if strings.TrimSpace(scraperOverride.Profile) != "" {
		return ScraperProxyModeSpecific
	}

	// Scraper enabled without profile → Inherit global default
	return ScraperProxyModeInherit
}

// ScraperSettings holds unified scraper configuration fields used by the Scraper interface.
type ScraperSettings struct {
	Enabled         bool         `yaml:"enabled" json:"enabled"`
	Language        string       `yaml:"language" json:"language"`
	Timeout         int          `yaml:"timeout" json:"timeout"`
	RateLimit       int          `yaml:"rate_limit" json:"rate_limit"`
	RetryCount      int          `yaml:"retry_count" json:"retry_count"`
	UserAgent       string       `yaml:"user_agent" json:"user_agent"`
	Proxy           *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	DownloadProxy   *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"`
	BaseURL         string       `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	UseFlareSolverr bool         `yaml:"use_flaresolverr" json:"use_flaresolverr"`
	UseBrowser      bool         `yaml:"use_browser" json:"use_browser"`
	// *bool fields (ScrapeActress, RespectRetryAfter) represent tri-state semantics:
	// nil = inherit from global default (resolved via Should* helpers),
	// non-nil = explicit override. Plain bool fields (UseFlareSolverr, UseBrowser,
	// ScrapeBonusScreens) have no inheritance — their zero value IS the default.
	ScrapeActress          *bool             `yaml:"scrape_actress,omitempty" json:"scrape_actress,omitempty"`
	Cookies                map[string]string `yaml:"cookies,omitempty" json:"cookies,omitempty"`
	PlaceholderThresholdKB int               `yaml:"placeholder_threshold,omitempty" json:"placeholder_threshold,omitempty"`
	ExtraPlaceholderHashes []string          `yaml:"extra_placeholder_hashes,omitempty" json:"extra_placeholder_hashes,omitempty"`
	ScrapeBonusScreens     bool              `yaml:"scrape_bonus_screens,omitempty" json:"scrape_bonus_screens,omitempty"`
	APIKey                 string            `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	RespectRetryAfter      *bool             `yaml:"respect_retry_after,omitempty" json:"respect_retry_after,omitempty"`
}

// MarshalYAML preserves the full unified scraper settings shape so config
// save/load round-trips do not drop scraper-specific data.
func (s *ScraperSettings) MarshalYAML() (interface{}, error) {
	if s == nil {
		return nil, nil
	}

	result := make(map[string]any)

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
	result["use_browser"] = s.UseBrowser
	if s.ScrapeActress != nil {
		result["scrape_actress"] = *s.ScrapeActress
	}
	if len(s.Cookies) > 0 {
		result["cookies"] = s.Cookies
	}
	if s.PlaceholderThresholdKB > 0 {
		result["placeholder_threshold"] = s.PlaceholderThresholdKB
	}
	if len(s.ExtraPlaceholderHashes) > 0 {
		result["extra_placeholder_hashes"] = s.ExtraPlaceholderHashes
	}
	if s.ScrapeBonusScreens {
		result["scrape_bonus_screens"] = s.ScrapeBonusScreens
	}
	if s.APIKey != "" {
		result["api_key"] = s.APIKey
	}
	if s.RespectRetryAfter != nil {
		result["respect_retry_after"] = *s.RespectRetryAfter
	}

	return result, nil
}

// MarshalJSON preserves the full unified scraper settings shape for JSON serialization.
func (s *ScraperSettings) MarshalJSON() ([]byte, error) {
	// Reuse MarshalYAML logic
	result, err := s.MarshalYAML()
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// Clone returns a deep copy of the ScraperSettings.
// Pointer, map, and slice fields are cloned so mutations to the copy do not affect the original.
func (s *ScraperSettings) Clone() ScraperSettings {
	cp := *s
	if cp.Proxy != nil {
		p := *cp.Proxy
		p.Profiles = maps.Clone(p.Profiles)
		cp.Proxy = &p
	}
	if cp.DownloadProxy != nil {
		p := *cp.DownloadProxy
		p.Profiles = maps.Clone(p.Profiles)
		cp.DownloadProxy = &p
	}
	if cp.ScrapeActress != nil {
		val := *cp.ScrapeActress
		cp.ScrapeActress = &val
	}
	cp.Cookies = maps.Clone(cp.Cookies)
	if cp.ExtraPlaceholderHashes != nil {
		slc := make([]string, len(cp.ExtraPlaceholderHashes))
		copy(slc, cp.ExtraPlaceholderHashes)
		cp.ExtraPlaceholderHashes = slc
	}
	if cp.RespectRetryAfter != nil {
		val := *cp.RespectRetryAfter
		cp.RespectRetryAfter = &val
	}
	return cp
}

// MergeDefaultsFrom copies non-zero-value fields from defaults into s,
// but only where s has the Go zero value. This is used by
// Finalize/Normalize to merge module defaults (e.g. r18dev's
// UserAgent, caribbeancom's BaseURL) into existing user overrides
// that have those fields empty.
//
// Enabled is intentionally excluded — the user's explicit
// enabled/disabled choice must always be preserved.
// Proxy and DownloadProxy are excluded because they are complex
// structs where "nil means inherit global" is the desired behavior.
// Cookies, ExtraPlaceholderHashes, and boolean fields with zero-value
// semantics (UseFlareSolverr, UseBrowser, ScrapeBonusScreens) are
// also excluded because their zero values are meaningful.
func (s *ScraperSettings) MergeDefaultsFrom(defaults ScraperSettings) {
	if defaults.Language != "" && s.Language == "" {
		s.Language = defaults.Language
	}
	if defaults.UserAgent != "" && s.UserAgent == "" {
		s.UserAgent = defaults.UserAgent
	}
	if defaults.BaseURL != "" && s.BaseURL == "" {
		s.BaseURL = defaults.BaseURL
	}
	if defaults.APIKey != "" && s.APIKey == "" {
		s.APIKey = defaults.APIKey
	}
	if defaults.RateLimit != 0 && s.RateLimit == 0 {
		s.RateLimit = defaults.RateLimit
	}
	if defaults.Timeout != 0 && s.Timeout == 0 {
		s.Timeout = defaults.Timeout
	}
	if defaults.RetryCount != 0 && s.RetryCount == 0 {
		s.RetryCount = defaults.RetryCount
	}
	if defaults.PlaceholderThresholdKB != 0 && s.PlaceholderThresholdKB == 0 {
		s.PlaceholderThresholdKB = defaults.PlaceholderThresholdKB
	}
	if defaults.ScrapeActress != nil && s.ScrapeActress == nil {
		val := *defaults.ScrapeActress
		s.ScrapeActress = &val
	}
	if defaults.RespectRetryAfter != nil && s.RespectRetryAfter == nil {
		val := *defaults.RespectRetryAfter
		s.RespectRetryAfter = &val
	}
}

// ShouldScrapeActress returns whether actress scraping is enabled for this scraper.
// Per-scraper override wins; otherwise falls back to the global default.
func (s *ScraperSettings) ShouldScrapeActress(globalDefault bool) bool {
	if s == nil {
		return globalDefault
	}
	if s.ScrapeActress != nil {
		return *s.ScrapeActress
	}
	return globalDefault
}

// ShouldRespectRetryAfter resolves the tri-state RespectRetryAfter field.
// If explicitly set, returns the set value. Otherwise returns globalDefault.
func (s *ScraperSettings) ShouldRespectRetryAfter(globalDefault bool) bool {
	if s == nil {
		return globalDefault
	}
	if s.RespectRetryAfter != nil {
		return *s.RespectRetryAfter
	}
	return globalDefault
}

// ShouldUseBrowser returns whether browser automation is enabled for this scraper.
// Checks global enabled first, then per-scraper toggle.
func (s *ScraperSettings) ShouldUseBrowser(globalEnabled bool) bool {
	if s == nil {
		return false
	}
	if !globalEnabled {
		return false
	}
	return s.UseBrowser
}

// Validate checks that common scraper settings are valid.
// Returns an error if the settings are nil, or if any enabled scraper
// has negative values for rate_limit or retry_count, or a negative timeout.
// A timeout of 0 is valid — it means "use the global timeout setting".
func (s *ScraperSettings) Validate(scraperName string) error {
	if s == nil {
		return fmt.Errorf("%s: config is nil", scraperName)
	}

	// Normalize language: trim whitespace and lowercase.
	s.Language = strings.ToLower(strings.TrimSpace(s.Language))

	if !s.Enabled {
		return nil
	}
	if s.RateLimit < 0 {
		return fmt.Errorf("%s: rate_limit must be non-negative, got %d", scraperName, s.RateLimit)
	}
	if s.RetryCount < 0 {
		return fmt.Errorf("%s: retry_count must be non-negative, got %d", scraperName, s.RetryCount)
	}
	if s.Timeout < 0 {
		return fmt.Errorf("%s: timeout must be non-negative, got %d", scraperName, s.Timeout)
	}
	return nil
}

// FlareSolverrConfig holds FlareSolverr configuration for bypassing Cloudflare.
type FlareSolverrConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	URL        string `yaml:"url" json:"url"`
	Timeout    int    `yaml:"timeout" json:"timeout"`
	MaxRetries int    `yaml:"max_retries" json:"max_retries"`
	SessionTTL int    `yaml:"session_ttl" json:"session_ttl"`
}

// Validate validates FlareSolverr configuration fields.
func (c *FlareSolverrConfig) Validate(path string) error {
	if !c.Enabled {
		return nil
	}
	if c.URL == "" {
		return fmt.Errorf("%s.url is required when flaresolverr is enabled", path)
	}
	if c.Timeout < 1 || c.Timeout > 300 {
		return fmt.Errorf("%s.timeout must be between 1 and 300", path)
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("%s.max_retries must be between 0 and 10", path)
	}
	if c.SessionTTL < 60 || c.SessionTTL > 3600 {
		return fmt.Errorf("%s.session_ttl must be between 60 and 3600", path)
	}
	return nil
}

// BrowserConfig holds browser automation configuration.
type BrowserConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	BinaryPath   string `yaml:"binary_path" json:"binary_path"`
	Timeout      int    `yaml:"timeout" json:"timeout"`
	MaxRetries   int    `yaml:"max_retries" json:"max_retries"`
	Headless     bool   `yaml:"headless" json:"headless"`
	StealthMode  bool   `yaml:"stealth_mode" json:"stealth_mode"`
	WindowWidth  int    `yaml:"window_width" json:"window_width"`
	WindowHeight int    `yaml:"window_height" json:"window_height"`
	SlowMo       int    `yaml:"slow_mo" json:"slow_mo"`
	BlockImages  bool   `yaml:"block_images" json:"block_images"`
	BlockCSS     bool   `yaml:"block_css" json:"block_css"`
	UserAgent    string `yaml:"user_agent" json:"user_agent"`
	DebugVisible bool   `yaml:"debug_visible" json:"debug_visible"`
}

// Validate validates Browser configuration fields.
func (c *BrowserConfig) Validate(path string) error {
	if !c.Enabled {
		return nil // Disabled is valid
	}

	if c.Timeout < 1 || c.Timeout > 300 {
		return fmt.Errorf("%s.timeout must be between 1 and 300 seconds", path)
	}

	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("%s.max_retries must be between 0 and 10", path)
	}

	if c.WindowWidth < 640 || c.WindowWidth > 3840 {
		return fmt.Errorf("%s.window_width must be between 640 and 3840", path)
	}

	if c.WindowHeight < 480 || c.WindowHeight > 2160 {
		return fmt.Errorf("%s.window_height must be between 480 and 2160", path)
	}

	if c.SlowMo < 0 || c.SlowMo > 5000 {
		return fmt.Errorf("%s.slow_mo must be between 0 and 5000", path)
	}

	// If binary_path is set, validate it exists
	if c.BinaryPath != "" {
		if _, err := os.Stat(c.BinaryPath); err != nil {
			return fmt.Errorf("%s.binary_path does not exist: %s", path, c.BinaryPath)
		}
	}

	return nil
}
