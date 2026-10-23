package scraperconfig

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// numericSettings is also the wire format for deprecated numeric aliases.
// A null numeric value is treated as omitted, not as an explicit zero.
type numericSettings struct {
	Timeout      *int `json:"timeout" yaml:"timeout"`
	RateLimit    *int `json:"rate_limit" yaml:"rate_limit"`
	RetryCount   *int `json:"retry_count" yaml:"retry_count"`
	RequestDelay *int `json:"request_delay" yaml:"request_delay"`
	MaxRetries   *int `json:"max_retries" yaml:"max_retries"`
}

// UnmarshalJSON records numeric presence independently of the integer value.
func (s *ScraperSettings) UnmarshalJSON(data []byte) error {
	// Validate object shape before typed decoding; later probes cannot add
	// structural errors after the full settings have already decoded.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	type plain ScraperSettings
	decoded := plain(s.Clone())
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var numeric numericSettings
	if err := json.Unmarshal(data, &numeric); err != nil {
		return err
	}
	// Canonical keys win even when their value is zero or null.
	present := make(map[string]bool, len(keys))
	for key := range keys {
		present[strings.ToLower(key)] = true
	}
	decodedSettings := ScraperSettings(decoded)
	decodedSettings.applyNumericPresence(numeric, present)
	*s = decodedSettings
	return nil
}

// UnmarshalYAML records numeric presence, including aliases and merged mappings.
func (s *ScraperSettings) UnmarshalYAML(node *yaml.Node) error {
	// Resolve merge structure first, before decoding either typed view.
	keys, err := effectiveMappingKeys(node)
	if err != nil {
		return err
	}
	type plain ScraperSettings
	decoded := plain(s.Clone())
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	var numeric numericSettings
	if err := node.Decode(&numeric); err != nil {
		return err
	}
	present := make(map[string]bool, len(keys))
	for key := range keys {
		present[key] = true
	}
	decodedSettings := ScraperSettings(decoded)
	decodedSettings.applyNumericPresence(numeric, present)
	*s = decodedSettings
	return nil
}

func (s *ScraperSettings) applyNumericPresence(n numericSettings, present map[string]bool) {
	if present["timeout"] {
		s.Timeout, s.timeoutExplicit = numericValue(n.Timeout)
	}
	if present["rate_limit"] {
		s.RateLimit, s.rateLimitExplicit = numericValue(n.RateLimit)
	} else if present["request_delay"] {
		s.RateLimit, s.rateLimitExplicit = numericValue(n.RequestDelay)
	}
	if present["retry_count"] {
		s.RetryCount, s.retryCountExplicit = numericValue(n.RetryCount)
	} else if present["max_retries"] {
		s.RetryCount, s.retryCountExplicit = numericValue(n.MaxRetries)
	}
}

func numericValue(value *int) (int, bool) {
	if value == nil {
		return 0, false
	}
	return *value, true
}

// HasExplicitRetryCount reports whether decoding supplied a non-null retry_count
// (or its legacy alias). This lets HTTP consumers distinguish disabled retries
// from a zero-valued programmatic literal, which retains its historical default.
func (s *ScraperSettings) HasExplicitRetryCount() bool {
	return s != nil && s.retryCountExplicit
}
