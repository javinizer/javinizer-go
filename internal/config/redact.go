package config

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

func (c *Config) Redact() *Config {
	if c == nil {
		return nil
	}

	// Start from a deep copy so pointer/slice/map fields are independent.
	copy := c.Clone()

	copy.Database.DSN = redactString(copy.Database.DSN)

	copy.Metadata.Translation.OpenAI.APIKey = redactString(copy.Metadata.Translation.OpenAI.APIKey)
	copy.Metadata.Translation.DeepL.APIKey = redactString(copy.Metadata.Translation.DeepL.APIKey)
	copy.Metadata.Translation.Google.APIKey = redactString(copy.Metadata.Translation.Google.APIKey)
	copy.Metadata.Translation.OpenAICompatible.APIKey = redactString(copy.Metadata.Translation.OpenAICompatible.APIKey)
	copy.Metadata.Translation.Anthropic.APIKey = redactString(copy.Metadata.Translation.Anthropic.APIKey)

	copy.Scrapers.Proxy = copy.Scrapers.Proxy.Redact()
	copy.Output.Download.DownloadProxy = copy.Output.Download.DownloadProxy.Redact()

	for _, s := range copy.Scrapers.Overrides {
		if s == nil {
			continue
		}
		if s.Proxy != nil {
			redacted := s.Proxy.Redact()
			s.Proxy = &redacted
		}
		if s.DownloadProxy != nil {
			redacted := s.DownloadProxy.Redact()
			s.DownloadProxy = &redacted
		}
		if s.APIKey != "" {
			s.APIKey = models.RedactedValue
		}
	}

	return copy
}

func redactString(s string) string {
	if s == "" {
		return ""
	}
	return models.RedactedValue
}

func deepCopyFieldsMap(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	result := make(map[string][]string, len(m))
	for k, v := range m {
		if v == nil {
			result[k] = nil
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		result[k] = cp
	}
	return result
}
