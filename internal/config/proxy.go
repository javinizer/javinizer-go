package config

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/configutil"
	"gopkg.in/yaml.v3"
)

// ProxyProfile holds reusable proxy connection settings.
type ProxyProfile struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

// ScraperProxyMode represents how a scraper should use proxy
type ScraperProxyMode string

const (
	ScraperProxyModeDirect   ScraperProxyMode = "direct"
	ScraperProxyModeInherit  ScraperProxyMode = "inherit"
	ScraperProxyModeSpecific ScraperProxyMode = "specific"
)

// ProxyConfig holds HTTP/SOCKS5 proxy configuration
// All proxy settings are managed through profiles (scrapers.proxy.profiles).
// Use Profile field to reference a named profile for scraper-specific overrides.
type ProxyConfig struct {
	Enabled        bool                    `yaml:"enabled" json:"enabled"`                                     // Enable proxy for HTTP requests
	Profile        string                  `yaml:"profile,omitempty" json:"profile,omitempty"`                 // Named profile to use (for scraper-specific overrides)
	DefaultProfile string                  `yaml:"default_profile,omitempty" json:"default_profile,omitempty"` // Default profile name (for global scrapers.proxy)
	Profiles       map[string]ProxyProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`               // Named proxy profiles (global scrapers.proxy)
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

// ResolveScraperUserAgent resolves the effective User-Agent for a scraper.
// If the scraper-specific userAgent is non-empty, it is used; otherwise DefaultScraperUserAgent is returned.
func ResolveScraperUserAgent(userAgent string) string {
	if ua := strings.TrimSpace(userAgent); ua != "" {
		return ua
	}
	return configutil.DefaultScraperUserAgent
}

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
