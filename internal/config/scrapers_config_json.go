package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// UnmarshalJSON implements custom JSON unmarshaling for ScrapersConfig.
// Uses json.RawMessage to preserve original bytes and avoid re-encoding.
func (s *ScrapersConfig) UnmarshalJSON(data []byte) error {
	// Always reset map state on unmarshal.
	s.Overrides = make(map[string]*models.ScraperSettings)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to unmarshal scrapers config: %w", err)
	}

	for key, rawVal := range raw {
		switch key {
		case "user_agent":
			if err := json.Unmarshal(rawVal, &s.UserAgent); err != nil {
				return fmt.Errorf("user_agent must be a string: %w", err)
			}
		case "referer":
			if err := json.Unmarshal(rawVal, &s.Referer); err != nil {
				return fmt.Errorf("referer must be a string: %w", err)
			}
		case "timeout_seconds":
			if err := json.Unmarshal(rawVal, &s.TimeoutSeconds); err != nil {
				return fmt.Errorf("timeout_seconds must be an integer: %w", err)
			}
		case "request_timeout_seconds":
			if err := json.Unmarshal(rawVal, &s.RequestTimeoutSeconds); err != nil {
				return fmt.Errorf("request_timeout_seconds must be an integer: %w", err)
			}
		case "priority":
			if err := json.Unmarshal(rawVal, &s.Priority); err != nil {
				return fmt.Errorf("priority must be an array of strings: %w", err)
			}
		case "proxy":
			if err := json.Unmarshal(rawVal, &s.Proxy); err != nil {
				return fmt.Errorf("failed to unmarshal proxy: %w", err)
			}
		case "flaresolverr":
			if err := json.Unmarshal(rawVal, &s.FlareSolverr); err != nil {
				return fmt.Errorf("failed to unmarshal flaresolverr: %w", err)
			}
		case "scrape_actress":
			if err := json.Unmarshal(rawVal, &s.ScrapeActress); err != nil {
				return fmt.Errorf("scrape_actress must be a boolean: %w", err)
			}
		case "browser":
			if err := json.Unmarshal(rawVal, &s.Browser); err != nil {
				return fmt.Errorf("failed to unmarshal browser: %w", err)
			}
		default:
			trimmed := bytes.TrimSpace(rawVal)
			if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
				continue
			}

			if s.resolver != nil && !s.resolver.IsRegistered(key) {
				return fmt.Errorf("unknown scraper %q", key)
			}

			var ss models.ScraperSettings

			var scraperRaw map[string]json.RawMessage
			if err := json.Unmarshal(rawVal, &scraperRaw); err != nil {
				return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
			}

			// Pre-check for deprecated aliases using the parsed map to avoid
			// unnecessary double-decode in the common (no-alias) case.
			hasAliases := false
			for k := range scraperRaw {
				if k == "request_delay" || k == "max_retries" {
					hasAliases = true
					break
				}
			}

			// Presence BEFORE alias resolution: an explicit canonical zero must
			// already count as "the user chose this" when aliases apply (codex
			// P2 round 7). The deprecated request_delay alias counts as explicit
			// presence too — request_delay: 0 must survive MergeDefaultsFrom
			// defaults (codex) — while canonical precedence is kept by
			// applyJSONAliases.
			_, explicitEnabled := scraperRaw["enabled"]
			ss.SetEnabledPresence(explicitEnabled)
			_, canonicalRate := scraperRaw["rate_limit"]
			_, aliasRate := scraperRaw["request_delay"]
			if canonicalRate || aliasRate {
				ss.SetRateLimitPresence(true)
			}
			_, canonicalRetry := scraperRaw["retry_count"]
			_, aliasRetry := scraperRaw["max_retries"]
			if canonicalRetry || aliasRetry {
				ss.SetRetryCountPresence(true)
			}
			if _, hasTimeout := scraperRaw["timeout"]; hasTimeout {
				ss.SetTimeoutPresence(true)
			}
			if hasAliases {
				// Decode without strict mode, then apply aliases,
				// then validate remaining keys.
				if err := json.Unmarshal(rawVal, &ss); err != nil {
					return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
				}
				if err := s.applyJSONAliases(scraperRaw, &ss); err != nil {
					return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
				}

				// Validate keys manually.
				for k := range scraperRaw {
					if !scraperSettingsYAMLKeys[k] {
						return fmt.Errorf("unknown field %q in scraper %q", k, key)
					}
				}
			} else {
				// Single strict decode (no alias handling needed).
				decoder := json.NewDecoder(bytes.NewReader(rawVal))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&ss); err != nil {
					return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
				}
			}

			s.Overrides[key] = &ss
		}
	}

	return nil
}

// applyJSONAliases handles deprecated JSON aliases request_delay→rate_limit
// and max_retries→retry_count.
func (s *ScrapersConfig) applyJSONAliases(raw map[string]json.RawMessage, ss *models.ScraperSettings) error {
	// Canonical presence beats the alias: explicit rate_limit: 0 is a choice.
	// Alias presence counts as explicit (see caller), so precedence keys on
	// the canonical key alone — otherwise an alias-only entry would never
	// apply now that the alias marks rate_limit explicit.
	_, canonicalRate := raw["rate_limit"]
	if rd, ok := raw["request_delay"]; ok && !canonicalRate {
		// Propagate conversion failures: silently dropping a malformed alias
		// whose presence is already recorded would accept the config AND pin
		// the zero as explicit — MergeDefaultsFrom then skips the default and
		// throttling ends up disabled (codex).
		var v int
		if err := json.Unmarshal(rd, &v); err != nil {
			return fmt.Errorf("request_delay must be an integer: %w", err)
		}
		ss.RateLimit = v
	}
	_, canonicalRetry := raw["retry_count"]
	if mr, ok := raw["max_retries"]; ok && !canonicalRetry && ss.RetryCount == 0 {
		var v int
		if err := json.Unmarshal(mr, &v); err != nil {
			return fmt.Errorf("max_retries must be an integer: %w", err)
		}
		ss.RetryCount = v
	}
	return nil
}

func (s *ScrapersConfig) marshalScrapersMap(effective bool) map[string]any {
	m := make(map[string]any)

	m["user_agent"] = s.UserAgent
	m["referer"] = s.Referer
	m["timeout_seconds"] = s.TimeoutSeconds
	m["request_timeout_seconds"] = s.RequestTimeoutSeconds
	m["priority"] = s.Priority
	m["proxy"] = s.Proxy
	m["flaresolverr"] = s.FlareSolverr
	m["scrape_actress"] = s.ScrapeActress
	m["browser"] = s.Browser

	var defaults map[string]models.ScraperSettings
	if effective && s.resolver != nil {
		defaults = s.resolver.GetAllDefaults()
	}
	for name, settings := range s.Overrides {
		if settings == nil {
			continue
		}
		if effective {
			m[name] = s.effectiveOverrideForMarshal(name, settings, defaults)
		} else {
			m[name] = settings
		}
	}
	return m
}

func (s *ScrapersConfig) effectiveOverrideForMarshal(name string, settings *models.ScraperSettings, defaults map[string]models.ScraperSettings) *models.ScraperSettings {
	if settings == nil {
		return nil
	}
	if defaults == nil {
		return settings
	}
	def, ok := defaults[name]
	if !ok {
		return settings
	}
	resolved := settings.Clone()
	resolved.MergeEnabledDefault(def)
	return &resolved
}

// MarshalJSON implements custom JSON marshaling for ScrapersConfig.
func (s *ScrapersConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshalScrapersMap(true))
}

// MarshalYAML serializes scrapers with full unified ScraperSettings.
func (s *ScrapersConfig) MarshalYAML() (interface{}, error) {
	return s.marshalScrapersMap(false), nil
}
