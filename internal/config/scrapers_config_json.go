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
			// Scraper entry — decode with strict unknown-field checking.
			if s.resolver != nil && !s.resolver.IsRegistered(key) {
				return fmt.Errorf("unknown scraper %q", key)
			}

			var ss models.ScraperSettings

			// Pre-check for deprecated aliases using bytes.Contains to avoid
			// unnecessary double-decode in the common (no-alias) case.
			hasAliases := bytes.Contains(rawVal, []byte(`"request_delay"`)) ||
				bytes.Contains(rawVal, []byte(`"max_retries"`))

			if hasAliases {
				// Decode the raw value into a map for alias handling.
				var scraperRaw map[string]json.RawMessage
				if err := json.Unmarshal(rawVal, &scraperRaw); err != nil {
					return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
				}

				// Decode without strict mode, then apply aliases,
				// then validate remaining keys.
				if err := json.Unmarshal(rawVal, &ss); err != nil {
					return fmt.Errorf("failed to decode config for scraper %q: %w", key, err)
				}
				s.applyJSONAliases(scraperRaw, &ss)

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
func (s *ScrapersConfig) applyJSONAliases(raw map[string]json.RawMessage, ss *models.ScraperSettings) {
	if rd, ok := raw["request_delay"]; ok && ss.RateLimit == 0 {
		var v int
		if err := json.Unmarshal(rd, &v); err == nil {
			ss.RateLimit = v
		}
	}
	if mr, ok := raw["max_retries"]; ok && ss.RetryCount == 0 {
		var v int
		if err := json.Unmarshal(mr, &v); err == nil {
			ss.RetryCount = v
		}
	}
}

// MarshalJSON implements custom JSON marshaling for ScrapersConfig.
func (s *ScrapersConfig) MarshalJSON() ([]byte, error) {
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

	for name, settings := range s.Overrides {
		if settings != nil {
			m[name] = settings
		}
	}

	return json.Marshal(m)
}

// MarshalYAML serializes scrapers with full unified ScraperSettings.
func (s *ScrapersConfig) MarshalYAML() (interface{}, error) {
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

	for name, settings := range s.Overrides {
		if settings != nil {
			m[name] = settings
		}
	}

	return m, nil
}
