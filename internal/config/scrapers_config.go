package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gopkg.in/yaml.v3"
)

// scraperSettingsYAMLKeys is the set of valid YAML keys for ScraperSettings,
// built at init time via reflection. Used by UnmarshalYAML for unknown-field
// rejection on scraper entries.
var scraperSettingsYAMLKeys map[string]bool

func init() {
	scraperSettingsYAMLKeys = make(map[string]bool)
	t := reflect.TypeOf(models.ScraperSettings{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			scraperSettingsYAMLKeys[name] = true
		}
	}
	// Deprecated aliases: accepted but mapped to canonical fields.
	scraperSettingsYAMLKeys["request_delay"] = true
	scraperSettingsYAMLKeys["max_retries"] = true
}

// ScrapersConfig holds scraper-specific settings.
// PLUGIN-01: No concrete scraper type fields - map-backed storage only.
type ScrapersConfig struct {
	UserAgent             string                                         `yaml:"user_agent" json:"user_agent"`
	Referer               string                                         `yaml:"referer" json:"referer"`                                 // Referer header for CDN compatibility (default: https://www.dmm.co.jp/)
	TimeoutSeconds        int                                            `yaml:"timeout_seconds" json:"timeout_seconds"`                 // HTTP client timeout in seconds (default: 30)
	RequestTimeoutSeconds int                                            `yaml:"request_timeout_seconds" json:"request_timeout_seconds"` // Overall request timeout in seconds (default: 180)
	Priority              []string                                       `yaml:"priority" json:"priority"`                               // Global scraper priority order
	FlareSolverr          models.FlareSolverrConfig                      `yaml:"flaresolverr" json:"flaresolverr"`                       // Global FlareSolverr config for Cloudflare bypass
	ScrapeActress         bool                                           `yaml:"scrape_actress" json:"scrape_actress"`                   // Global scrape_actress default (opt-out, default: true)
	Browser               models.BrowserConfig                           `yaml:"browser" json:"browser"`                                 // Global Browser configuration block
	Proxy                 models.ProxyConfig                             `yaml:"proxy" json:"proxy"`                                     // Default HTTP/SOCKS5 proxy for scraper requests
	Overrides             map[string]*models.ScraperSettings             `yaml:"-" json:"-"`                                             // Canonical per-scraper settings map
	validateFns           map[string]func(*models.ScraperSettings) error `yaml:"-" json:"-"`                                             // Per-scraper validation functions
	resolver              models.ScraperConfigResolverInterface          `yaml:"-" json:"-"`                                             // Injected by Finalize
}

// Finalize stores the resolver and normalizes per-scraper settings.
// Returns an error if resolver is nil. Must be called after loading config
// and before using Overrides.
func (c *ScrapersConfig) Finalize(resolver models.ScraperConfigResolverInterface) error {
	if resolver == nil {
		return fmt.Errorf("scrapers: Finalize called with nil resolver")
	}
	c.resolver = resolver
	c.normalize()
	return nil
}

// Normalize re-normalizes per-scraper settings using the already-stored resolver.
// Returns nothing. If Finalize has not been called (resolver is nil), this is a no-op —
// the CLI startup path calls Normalize before Finalize is available.
// This is idempotent and safe to call multiple times.
func (c *ScrapersConfig) Normalize() {
	if c.resolver == nil {
		return
	}
	c.normalize()
}

// normalize is the shared implementation for Finalize and Normalize.
func (c *ScrapersConfig) normalize() {
	if c.Overrides == nil {
		c.Overrides = make(map[string]*models.ScraperSettings)
	}
	if c.validateFns == nil {
		c.validateFns = make(map[string]func(*models.ScraperSettings) error)
	}

	// Always rebuild validator dispatch from current registry.
	for name := range c.validateFns {
		delete(c.validateFns, name)
	}

	for name := range c.Overrides {
		if validateFn := c.resolver.GetValidateFn(name); validateFn != nil {
			c.validateFns[name] = validateFn
		}
	}

}

// ResolvedSettings returns the effective settings for a scraper by deep-cloning
// the registered module defaults and merging any user override on top. It never
// mutates Overrides or the resolver's defaults.
func (c *ScrapersConfig) ResolvedSettings(name string) models.ScraperSettings {
	if c.resolver == nil {
		if c.Overrides[name] != nil {
			return c.Overrides[name].Clone()
		}
		return models.ScraperSettings{}
	}
	defaults, ok := c.resolver.GetAllDefaults()[name]
	if !ok {
		if c.Overrides[name] != nil {
			return c.Overrides[name].Clone()
		}
		return models.ScraperSettings{}
	}
	if c.Overrides[name] == nil {
		return defaults.Clone()
	}
	resolved := c.Overrides[name].Clone()
	resolved.MergeDefaultsFrom(defaults)
	resolved.MergeEnabledDefault(defaults)
	return resolved
}

func (c *ScrapersConfig) defaultEnabled(name string) bool {
	if c.resolver == nil {
		return false
	}
	defaults, ok := c.resolver.GetAllDefaults()[name]
	if !ok {
		return false
	}
	return defaults.Enabled
}

// getValidateFn returns the scraper-specific validation function for the named scraper.
// Returns nil if no ValidateFn is registered.
func (c *ScrapersConfig) getValidateFn(name string) func(*models.ScraperSettings) error {
	if c.validateFns == nil {
		return nil
	}
	return c.validateFns[name]
}

// Override returns the creating accessor for a scraper's user-supplied settings.
func (c *ScrapersConfig) Override(name string) *models.ScraperSettings {
	if c.Overrides == nil {
		c.Overrides = make(map[string]*models.ScraperSettings)
	}
	if c.Overrides[name] == nil {
		c.Overrides[name] = &models.ScraperSettings{}
	}
	return c.Overrides[name]
}

// ApplyOverride applies the given options to the named scraper's override block.
// With no options it returns immediately without creating an override block.
func (c *ScrapersConfig) ApplyOverride(name string, opts ...models.ScraperOverride) {
	if len(opts) == 0 {
		return
	}
	s := c.Override(name)
	for _, opt := range opts {
		opt(s)
	}
}

// UserOverride returns the named scraper's user-supplied override without creating one.
func (c *ScrapersConfig) UserOverride(name string) (*models.ScraperSettings, bool) {
	if c.Overrides == nil {
		return nil, false
	}
	s, ok := c.Overrides[name]
	return s, ok && s != nil
}

// IsScraperRegistered reports whether a scraper name is known to the resolver.
// Returns true when no resolver is set (permissive default for standalone configs).
func (c *ScrapersConfig) IsScraperRegistered(name string) bool {
	if c.resolver != nil {
		return c.resolver.IsRegistered(name)
	}
	return true
}

// UnmarshalYAML implements custom YAML unmarshaling for ScrapersConfig.
// Uses yaml.Node walk for direct decoding into ScraperSettings — no map[string]any
// intermediary or bytes round-trip.
func (s *ScrapersConfig) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == 0 {
		s.Overrides = make(map[string]*models.ScraperSettings)
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("scrapers config must be a mapping, got kind %d", node.Kind)
	}

	// Always reset map state on unmarshal to avoid stale entries.
	s.Overrides = make(map[string]*models.ScraperSettings)

	// yaml.Node Content is a flat array: keys at even indices, values at odd.
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		key := keyNode.Value

		switch key {
		case "user_agent":
			s.UserAgent = valNode.Value
		case "referer":
			s.Referer = valNode.Value
		case "timeout_seconds":
			var v int
			if err := valNode.Decode(&v); err != nil {
				return fmt.Errorf("timeout_seconds must be an integer: %w", err)
			}
			s.TimeoutSeconds = v
		case "request_timeout_seconds":
			var v int
			if err := valNode.Decode(&v); err != nil {
				return fmt.Errorf("request_timeout_seconds must be an integer: %w", err)
			}
			s.RequestTimeoutSeconds = v
		case "priority":
			if valNode.Kind == yaml.ScalarNode && valNode.Value == "" {
				s.Priority = nil
				continue
			}
			var v []string
			if err := valNode.Decode(&v); err != nil {
				return fmt.Errorf("priority must be an array of strings: %w", err)
			}
			s.Priority = v
		case "proxy":
			if err := valNode.Decode(&s.Proxy); err != nil {
				return fmt.Errorf("failed to decode proxy: %w", err)
			}
		case "flaresolverr":
			if err := valNode.Decode(&s.FlareSolverr); err != nil {
				return fmt.Errorf("failed to decode flaresolverr: %w", err)
			}
		case "scrape_actress":
			var v bool
			if err := valNode.Decode(&v); err != nil {
				return fmt.Errorf("scrape_actress must be a boolean: %w", err)
			}
			s.ScrapeActress = v
		case "browser":
			if err := valNode.Decode(&s.Browser); err != nil {
				return fmt.Errorf("failed to decode browser: %w", err)
			}
		default:
			// Scraper entry — decode directly into ScraperSettings.
			if valNode.Kind == yaml.MappingNode && len(valNode.Content) == 0 {
				continue
			}
			if valNode.Kind == yaml.ScalarNode && valNode.Tag == "!!null" {
				continue
			}

			if s.resolver != nil && !s.resolver.IsRegistered(key) {
				return fmt.Errorf("unknown scraper %q", key)
			}

			var ss models.ScraperSettings
			if err := valNode.Decode(&ss); err != nil {
				return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
			}
			ss.SetEnabledPresence(scraperYAMLHasEnabledKey(valNode))
			// rate_limit presence matters: explicit 0 means "no delay", which
			// differs from omitted (conservative 1s default). The canonical key
			// counts, and so does its deprecated request_delay alias — an alias
			// zero is still an explicit user choice and must survive
			// MergeDefaultsFrom (codex). Canonical value precedence is kept by
			// applyYAMLAliases.
			ss.SetRateLimitPresence(scraperYAMLHasKey(valNode, "rate_limit") || scraperYAMLHasKey(valNode, "request_delay"))

			// Handle deprecated aliases: request_delay → rate_limit, max_retries → retry_count.
			// Walk the node content to find alias keys and apply them if the canonical
			// field is still zero (canonical takes precedence).
			if err := s.applyYAMLAliases(valNode, &ss); err != nil {
				return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
			}

			// Unknown-field checking: walk the node's Content pairs and compare
			// against the known-fields set for ScraperSettings.
			if err := s.validateYAMLScraperKeys(key, valNode); err != nil {
				return err
			}

			s.Overrides[key] = &ss
		}
	}

	return nil
}

// applyYAMLAliases handles deprecated YAML aliases request_delay→rate_limit
// and max_retries→retry_count in a scraper entry's value node.
func (s *ScrapersConfig) applyYAMLAliases(valNode *yaml.Node, ss *models.ScraperSettings) error {
	if valNode.Kind != yaml.MappingNode {
		return nil
	}
	// Canonical presence (not merely a zero decode) wins: an explicit
	// rate_limit: 0 is a real choice; the deprecated alias must not overwrite
	// it (codex P2 round 7).
	// Alias presence now counts as explicit (see caller), so key the
	// precedence decision on the canonical key alone instead of the
	// zero/explicit combo — otherwise an alias-only entry would never apply.
	hasRateKey := scraperYAMLHasKey(valNode, "rate_limit")
	hasRetryKey := scraperYAMLHasKey(valNode, "retry_count")
	for i := 0; i < len(valNode.Content); i += 2 {
		k := valNode.Content[i].Value
		switch k {
		case "request_delay":
			if !hasRateKey {
				// Propagate conversion failures: silently dropping a malformed
				// alias whose presence is already recorded would accept the
				// config AND pin the zero as explicit — MergeDefaultsFrom then
				// skips the default and throttling ends up disabled (codex).
				var v int
				if err := valNode.Content[i+1].Decode(&v); err != nil {
					return fmt.Errorf("request_delay must be an integer: %w", err)
				}
				ss.RateLimit = v
			}
		case "max_retries":
			if !hasRetryKey && ss.RetryCount == 0 {
				var v int
				if err := valNode.Content[i+1].Decode(&v); err != nil {
					return fmt.Errorf("max_retries must be an integer: %w", err)
				}
				ss.RetryCount = v
			}
		}
	}
	return nil
}

func scraperYAMLHasEnabledKey(valNode *yaml.Node) bool {
	return scraperYAMLHasKey(valNode, "enabled")
}

// scraperYAMLHasKey reports whether a mapping node carries any of the given keys.
func scraperYAMLHasKey(valNode *yaml.Node, keys ...string) bool {
	if valNode == nil || valNode.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(valNode.Content); i += 2 {
		for _, key := range keys {
			if valNode.Content[i].Value == key {
				return true
			}
		}
	}
	return false
}

// validateYAMLScraperKeys checks for unknown fields in a scraper entry's YAML node.
func (s *ScrapersConfig) validateYAMLScraperKeys(scraperName string, valNode *yaml.Node) error {
	if valNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(valNode.Content); i += 2 {
		key := valNode.Content[i].Value
		if !scraperSettingsYAMLKeys[key] {
			return fmt.Errorf("unknown field %q in scraper %q", key, scraperName)
		}
	}
	return nil
}
