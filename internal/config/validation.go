package config

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ConfigWarning represents a non-blocking validation warning about a
// potentially misconfigured setting. Warnings are surfaced via the API
// and WebUI but do not block config loading or scraping.
type ConfigWarning struct {
	Field    string   `json:"field"`
	Scrapers []string `json:"scrapers"`
	Message  string   `json:"message"`
}

// ValidatePriorityOverrides checks per-field metadata priority overrides
// and returns warnings when an override points exclusively at scrapers
// that are all disabled. A warning is generated only when ALL known
// scrapers in the override are disabled (if at least one is enabled, the
// field can still get data). Unknown scrapers (not in Overrides) are
// skipped — they may be registered later. The __skip__ sentinel and
// empty [] overrides are also skipped (intentional suppression / inherit
// global). Results are sorted by field name for deterministic ordering.
func ValidatePriorityOverrides(cfg *Config) []ConfigWarning {
	if cfg == nil || cfg.Metadata.Priority.Fields == nil {
		return nil
	}

	var fields []string
	for field := range cfg.Metadata.Priority.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	var warnings []ConfigWarning
	for _, field := range fields {
		scrapers := cfg.Metadata.Priority.Fields[field]
		if len(scrapers) == 0 {
			continue
		}
		// Skip exact __skip__ sentinel.
		if len(scrapers) == 1 && scrapers[0] == "__skip__" {
			continue
		}

		var unqueryable []string
		hasQueryable := false
		for _, name := range scrapers {
			settings, ok := cfg.Scrapers.Overrides[name]
			if !ok || settings == nil {
				continue
			}
			if settings.Enabled && scraperInPriority(cfg.Scrapers.Priority, name) {
				hasQueryable = true
			} else {
				unqueryable = append(unqueryable, name)
			}
		}

		if !hasQueryable && len(unqueryable) > 0 {
			var reasons []string
			for _, name := range unqueryable {
				settings := cfg.Scrapers.Overrides[name]
				if settings != nil && !settings.Enabled {
					reasons = append(reasons, fmt.Sprintf("%s is disabled", name))
				} else {
					reasons = append(reasons, fmt.Sprintf("%s is not in scrapers.priority", name))
				}
			}
			warnings = append(warnings, ConfigWarning{
				Field:    field,
				Scrapers: unqueryable,
				Message:  fmt.Sprintf("metadata.priority.%s is set to [%s] but all listed scrapers are unqueryable (%s) — this field will be empty", field, strings.Join(scrapers, ", "), strings.Join(reasons, ", ")),
			})
		}
	}
	return warnings
}

// validateHTTPBaseURL validates that a URL has an http or https scheme and a host.
func validateHTTPBaseURL(path, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s must be a valid http(s) URL", path)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid http(s) URL", path)
	}
	return nil
}

// validateProxyProfileConfig is the internal implementation for ValidateProxyProfiles.
func validateProxyProfileConfig(c *Config) error {
	if c == nil {
		return nil
	}

	// Ensure Overrides is populated before validation.
	// This must be called here (not just in Validate()) so that direct calls
	// to validateProxyProfileConfig pick up any flat config modifications.
	if c.Scrapers.Overrides == nil {
		c.Scrapers.Normalize()
	}

	profiles := c.Scrapers.Proxy.Profiles

	if c.Scrapers.Proxy.Enabled && c.Scrapers.Proxy.DefaultProfile == "" {
		return fmt.Errorf("scrapers.proxy.default_profile is required when scrapers.proxy.enabled is true")
	}

	if c.Scrapers.Proxy.DefaultProfile != "" {
		if _, ok := profiles[c.Scrapers.Proxy.DefaultProfile]; !ok {
			return fmt.Errorf("scrapers.proxy.default_profile references unknown profile %q", c.Scrapers.Proxy.DefaultProfile)
		}
	}

	// CONF-04: Generic scraper proxy profile validation — iterates Overrides map.
	// NO hardcoded scraper-name branches.
	if err := c.validateScraperProxyProfiles(); err != nil {
		return err
	}

	// Validate output.download_proxy (not a scraper, special case)
	if err := validateProxyProfileRef("output.download_proxy", &c.Output.Download.DownloadProxy, profiles); err != nil {
		return err
	}

	return nil
}

// validateScraperProxyProfiles validates per-scraper proxy configuration.
// Uses c.Scrapers.Overrides map — NO hardcoded scraper-name branches.
func (c *Config) validateScraperProxyProfiles() error {
	// Always re-normalize to pick up any modifications.
	c.Scrapers.Normalize()

	for name, sc := range c.Scrapers.Overrides {
		path := "scrapers." + name

		if sc.Proxy != nil {
			if err := validateProxyProfileRef(path+".proxy", sc.Proxy, c.Scrapers.Proxy.Profiles); err != nil {
				return err
			}
		}

		if sc.DownloadProxy != nil {
			if err := validateProxyProfileRef(path+".download_proxy", sc.DownloadProxy, c.Scrapers.Proxy.Profiles); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProxyProfileRef(path string, proxyCfg *models.ProxyConfig, profiles map[string]models.ProxyProfile) error {
	if proxyCfg == nil {
		return nil
	}

	// enabled=true with empty profile means "inherit" mode - valid, no profile needed
	// enabled=true with non-empty profile means "specific" mode - profile is required
	if proxyCfg.Enabled && proxyCfg.Profile != "" {
		if _, ok := profiles[proxyCfg.Profile]; !ok {
			return fmt.Errorf("%s.profile references unknown profile %q", path, proxyCfg.Profile)
		}
	}
	return nil
}

// ValidateProxyProfiles validates proxy profile configuration including global and
// per-scraper proxy settings. It checks that enabled proxies reference existing profiles,
// and that per-scraper proxy and download_proxy references are valid.
func ValidateProxyProfiles(c *Config) error {
	return validateProxyProfileConfig(c)
}

// ValidateScraperOverrides validates per-scraper configuration overrides.
// It checks each scraper's base settings and runs scraper-specific validation
// via getValidateFn for enabled scrapers. Disabled scrapers skip specific checks.
func ValidateScraperOverrides(c *Config) error {
	if c == nil {
		return nil
	}

	// Ensure Overrides is populated before validation.
	if c.Scrapers.Overrides == nil {
		c.Scrapers.Normalize()
	}

	// CONF-04: Generic scraper config validation — uses getValidateFn for dispatch.
	// NO hardcoded scraper-name branches.
	for name, sc := range c.Scrapers.Overrides {
		// Base check: ScraperSettings.Validate handles nil, enabled, rate_limit, retry_count, timeout.
		if err := sc.Validate(name); err != nil {
			return err
		}
		// Skip scraper-specific validation for disabled scrapers (DRY: one guard here
		// vs adding enabled checks to all 13 individual ValidateFn closures).
		if !sc.Enabled {
			continue
		}

		// Scraper-specific check via ValidateFn (no switch on scraper name)
		if validateFn := c.Scrapers.getValidateFn(name); validateFn != nil {
			if err := validateFn(sc); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateTranslationProvider validates cross-field translation provider configuration.
// It checks provider-specific requirements (base URLs, API keys, modes) based on the
// selected provider. When translation is disabled, all checks are skipped.
func ValidateTranslationProvider(c *Config) error {
	if c == nil {
		return nil
	}
	return validateTranslationProviderInternal(c)
}

// validateTranslationProviderInternal contains the translation provider validation logic.
func validateTranslationProviderInternal(c *Config) error {
	t := c.Metadata.Translation

	provider := strings.ToLower(strings.TrimSpace(t.Provider))
	if provider == "" {
		provider = translationProviderOpenAI
	}

	timeoutSeconds := t.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}

	openAIBaseURL := strings.TrimSpace(t.OpenAI.BaseURL)
	if openAIBaseURL == "" {
		openAIBaseURL = "https://api.openai.com/v1"
	}

	deepLMode := models.DeepLMode(strings.ToLower(strings.TrimSpace(string(t.DeepL.Mode))))
	if deepLMode == "" {
		deepLMode = models.DeepLModeFree
	}

	googleMode := models.GoogleMode(strings.ToLower(strings.TrimSpace(string(t.Google.Mode))))
	if googleMode == "" {
		googleMode = models.GoogleModeFree
	}

	if !t.Enabled {
		return nil
	}

	if timeoutSeconds < 5 || timeoutSeconds > 300 {
		return fmt.Errorf("metadata.translation.timeout_seconds must be between 5 and 300")
	}

	switch provider {
	case translationProviderOpenAI:
		if err := validateHTTPBaseURL("metadata.translation.openai.base_url", openAIBaseURL); err != nil {
			return err
		}
	case "deepl":
		if deepLMode != models.DeepLModeFree && deepLMode != models.DeepLModePro {
			return fmt.Errorf("metadata.translation.deepl.mode must be either 'free' or 'pro'")
		}
		if strings.TrimSpace(t.DeepL.BaseURL) != "" {
			if err := validateHTTPBaseURL("metadata.translation.deepl.base_url", t.DeepL.BaseURL); err != nil {
				return err
			}
		}
	case "google":
		if googleMode != models.GoogleModeFree && googleMode != models.GoogleModePaid {
			return fmt.Errorf("metadata.translation.google.mode must be either 'free' or 'paid'")
		}
		if strings.TrimSpace(t.Google.BaseURL) != "" {
			if err := validateHTTPBaseURL("metadata.translation.google.base_url", t.Google.BaseURL); err != nil {
				return err
			}
		}
	case "openai-compatible":
		if strings.TrimSpace(t.OpenAICompatible.BaseURL) == "" {
			return fmt.Errorf("metadata.translation.openai_compatible.base_url is required when provider=openai-compatible")
		}
		if err := validateHTTPBaseURL("metadata.translation.openai_compatible.base_url", t.OpenAICompatible.BaseURL); err != nil {
			return err
		}
		if strings.TrimSpace(t.OpenAICompatible.Model) == "" {
			return fmt.Errorf("metadata.translation.openai_compatible.model is required when provider=openai-compatible")
		}
		switch t.OpenAICompatible.NormalizedBackendType() {
		case "", "vllm", "ollama", "llama.cpp", "other":
		default:
			return fmt.Errorf("metadata.translation.openai_compatible.backend_type must be one of: auto, vllm, ollama, llama.cpp, other")
		}
	case "anthropic":
		if strings.TrimSpace(t.Anthropic.BaseURL) == "" {
			return fmt.Errorf("metadata.translation.anthropic.base_url is required when provider=anthropic")
		}
		if err := validateHTTPBaseURL("metadata.translation.anthropic.base_url", t.Anthropic.BaseURL); err != nil {
			return err
		}
		if strings.TrimSpace(t.Anthropic.Model) == "" {
			return fmt.Errorf("metadata.translation.anthropic.model is required when provider=anthropic")
		}
	default:
		return fmt.Errorf("metadata.translation.provider must be one of: openai, openai-compatible, anthropic, deepl, google")
	}

	// REGV-04: Validate API key presence at config time
	switch provider {
	case translationProviderOpenAI:
		if strings.TrimSpace(t.OpenAI.APIKey) == "" {
			return fmt.Errorf("metadata.translation.openai.api_key is required when provider=openai")
		}
	case "deepl":
		if strings.TrimSpace(t.DeepL.APIKey) == "" {
			return fmt.Errorf("metadata.translation.deepl.api_key is required when provider=deepl")
		}
	case "google":
		// Google free mode doesn't require API key; paid mode does
		if googleMode == models.GoogleModePaid && strings.TrimSpace(t.Google.APIKey) == "" {
			return fmt.Errorf("metadata.translation.google.api_key is required when provider=google and mode=paid")
		}
	case "openai-compatible":
		// API key is optional for self-hosted endpoints
	case "anthropic":
		if strings.TrimSpace(t.Anthropic.APIKey) == "" {
			return fmt.Errorf("metadata.translation.anthropic.api_key is required when provider=anthropic")
		}
	}

	return nil
}

// scraperInPriority checks if a scraper name is in the scraper priority list.
// If the priority list is empty, all scrapers are considered in priority
// (the default order is applied by the registry at runtime).
func scraperInPriority(priority []string, name string) bool {
	if len(priority) == 0 {
		return true
	}
	for _, p := range priority {
		if p == name {
			return true
		}
	}
	return false
}
