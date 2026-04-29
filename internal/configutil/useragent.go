package configutil

import (
	"encoding/json"
	"fmt"
)

// UserAgentString is a custom User-Agent string type that marshals/unmarshals as a plain string.
type UserAgentString struct {
	Value string
}

// MarshalJSON implements json.Marshaler so UserAgentString marshals as a plain string.
func (u UserAgentString) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.Value)
}

// UnmarshalJSON implements json.Unmarshaler so UserAgentString unmarshals from a
// plain string or an object with a "value" field.
func (u *UserAgentString) UnmarshalJSON(data []byte) error {
	// Try plain string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		u.Value = s
		return nil
	}
	// Only try object form if data starts with '{'
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("useragentstring: cannot unmarshal %q", string(data))
	}
	// Try object form {"value": "..."}
	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		u.Value = obj.Value
		return nil
	}
	return fmt.Errorf("useragentstring: cannot unmarshal %q", string(data))
}

// MarshalYAML implements custom marshaling so the field serializes as a plain string.
func (u UserAgentString) MarshalYAML() (interface{}, error) {
	return u.Value, nil
}

// UnmarshalYAML implements custom unmarshaling so the field accepts a plain string.
func (u *UserAgentString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}
	u.Value = str
	return nil
}

// Scan implements sql.Scanner for database compatibility.
func (u *UserAgentString) Scan(value interface{}) error {
	if value == nil {
		u.Value = ""
		return nil
	}
	switch v := value.(type) {
	case string:
		u.Value = v
	case []byte:
		u.Value = string(v)
	default:
		u.Value = fmt.Sprintf("%v", v)
	}
	return nil
}

const (
	// DefaultUserAgent is the true/identifying UA for Javinizer.
	DefaultUserAgent = "Javinizer (+https://github.com/javinizer/Javinizer)"

	// DefaultScraperUserAgent is a browser-like UA used as the default for scrapers
	// when no scraper-specific or global user_agent is configured.
	DefaultScraperUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)
