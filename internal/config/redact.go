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
			s.Proxy.Profiles = redactProxyProfiles(s.Proxy.Profiles)
		}
		if s.DownloadProxy != nil {
			s.DownloadProxy.Profiles = redactProxyProfiles(s.DownloadProxy.Profiles)
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

func redactProxyProfiles(profiles map[string]models.ProxyProfile) map[string]models.ProxyProfile {
	if profiles == nil {
		return nil
	}
	result := make(map[string]models.ProxyProfile, len(profiles))
	for k, v := range profiles {
		result[k] = v.Redact()
	}
	return result
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
