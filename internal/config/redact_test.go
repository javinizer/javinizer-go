package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedact(t *testing.T) {
	t.Run("redacts OpenAI API key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-secret-key-123"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Metadata.Translation.OpenAI.APIKey)
		assert.Equal(t, "sk-secret-key-123", cfg.Metadata.Translation.OpenAI.APIKey)
	})

	t.Run("redacts DeepL API key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.DeepL.APIKey = "deepl-secret"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Metadata.Translation.DeepL.APIKey)
		assert.Equal(t, "deepl-secret", cfg.Metadata.Translation.DeepL.APIKey)
	})

	t.Run("redacts Google API key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Google.APIKey = "google-secret"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Metadata.Translation.Google.APIKey)
		assert.Equal(t, "google-secret", cfg.Metadata.Translation.Google.APIKey)
	})

	t.Run("redacts Anthropic API key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Anthropic.APIKey = "anthropic-secret"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Metadata.Translation.Anthropic.APIKey)
		assert.Equal(t, "anthropic-secret", cfg.Metadata.Translation.Anthropic.APIKey)
	})

	t.Run("redacts OpenAI-compatible API key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.OpenAICompatible.APIKey = "compatible-secret"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Metadata.Translation.OpenAICompatible.APIKey)
		assert.Equal(t, "compatible-secret", cfg.Metadata.Translation.OpenAICompatible.APIKey)
	})

	t.Run("redacts Database DSN", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Database.DSN = "user:password@tcp(localhost:3306)/db"
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Database.DSN)
		assert.Equal(t, "user:password@tcp(localhost:3306)/db", cfg.Database.DSN)
	})

	t.Run("redacts proxy profile password", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Scrapers.Proxy.Profiles = map[string]ProxyProfile{
			"main": {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
		}
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Scrapers.Proxy.Profiles["main"].Username)
		assert.Equal(t, RedactedValue, redacted.Scrapers.Proxy.Profiles["main"].Password)
		assert.Equal(t, "http://proxy:8080", redacted.Scrapers.Proxy.Profiles["main"].URL)
		assert.Equal(t, "admin", cfg.Scrapers.Proxy.Profiles["main"].Username)
		assert.Equal(t, "s3cret", cfg.Scrapers.Proxy.Profiles["main"].Password)
	})

	t.Run("redacts download proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.DownloadProxy.Profiles = map[string]ProxyProfile{
			"dl": {URL: "http://dlproxy:8080", Username: "dluser", Password: "dlpass"},
		}
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Output.DownloadProxy.Profiles["dl"].Username)
		assert.Equal(t, RedactedValue, redacted.Output.DownloadProxy.Profiles["dl"].Password)
		assert.Equal(t, "http://dlproxy:8080", redacted.Output.DownloadProxy.Profiles["dl"].URL)
	})

	t.Run("does not modify non-secret fields", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Server.Host = "0.0.0.0"
		cfg.Server.Port = 9090
		cfg.Scrapers.TimeoutSeconds = 60
		cfg.Logging.Level = "debug"
		redacted := cfg.Redact()
		assert.Equal(t, "0.0.0.0", redacted.Server.Host)
		assert.Equal(t, 9090, redacted.Server.Port)
		assert.Equal(t, 60, redacted.Scrapers.TimeoutSeconds)
		assert.Equal(t, "debug", redacted.Logging.Level)
	})

	t.Run("returns deep copy without mutating original", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Database.DSN = "original-dsn"
		cfg.Metadata.Translation.OpenAI.APIKey = "original-key"
		cfg.Scrapers.Proxy.Profiles = map[string]ProxyProfile{
			"main": {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
		}

		redacted := cfg.Redact()

		redacted.Database.DSN = "tampered"
		redacted.Metadata.Translation.OpenAI.APIKey = "tampered"
		p := redacted.Scrapers.Proxy.Profiles["main"]
		p.Username = "tampered"
		redacted.Scrapers.Proxy.Profiles["main"] = p

		assert.Equal(t, "original-dsn", cfg.Database.DSN)
		assert.Equal(t, "original-key", cfg.Metadata.Translation.OpenAI.APIKey)
		assert.Equal(t, "admin", cfg.Scrapers.Proxy.Profiles["main"].Username)
	})

	t.Run("empty API keys are not redacted", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.OpenAI.APIKey = ""
		cfg.Database.DSN = ""
		redacted := cfg.Redact()
		assert.Equal(t, "", redacted.Metadata.Translation.OpenAI.APIKey)
		assert.Equal(t, "", redacted.Database.DSN)
	})

	t.Run("redacts scraper override proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Scrapers.Overrides = map[string]*ScraperSettings{
			"javbus": {
				Enabled: true,
				Proxy: &ProxyConfig{
					Enabled: true,
					Profiles: map[string]ProxyProfile{
						"custom": {URL: "http://custom:8080", Username: "customuser", Password: "custompass"},
					},
				},
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Username)
		assert.Equal(t, RedactedValue, redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Password)
		assert.Equal(t, "http://custom:8080", redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].URL)
		assert.Equal(t, "customuser", cfg.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Username)
	})

	t.Run("nil config returns nil", func(t *testing.T) {
		var cfg *Config
		assert.Nil(t, cfg.Redact())
	})

	t.Run("deep copy of priority fields map", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Priority.Fields = map[string][]string{
			"javbus": {"title", "actresses"},
			"dmm":    {"cover_url"},
		}
		redacted := cfg.Redact()

		redacted.Metadata.Priority.Fields["javbus"][0] = "tampered"
		delete(redacted.Metadata.Priority.Fields, "dmm")

		assert.Equal(t, "title", cfg.Metadata.Priority.Fields["javbus"][0])
		assert.Contains(t, cfg.Metadata.Priority.Fields, "dmm")
	})

	t.Run("deep copy of priority fields nil values", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Priority.Fields = map[string][]string{
			"empty": nil,
		}
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Metadata.Priority.Fields["empty"])
	})

	t.Run("deep copy of priority fields nil map", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Priority.Fields = nil
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Metadata.Priority.Fields)
	})

	t.Run("redacts scraper override download proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Scrapers.Overrides = map[string]*ScraperSettings{
			"javdb": {
				Enabled: true,
				DownloadProxy: &ProxyConfig{
					Enabled: true,
					Profiles: map[string]ProxyProfile{
						"dlproxy": {URL: "http://dl:8080", Username: "dluser", Password: "dlpass"},
					},
				},
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, RedactedValue, redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].Username)
		assert.Equal(t, RedactedValue, redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].Password)
		assert.Equal(t, "http://dl:8080", redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].URL)
	})

	t.Run("handles nil scraper override value", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Scrapers.Overrides = map[string]*ScraperSettings{
			"nilscraper": nil,
		}
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Scrapers.Overrides["nilscraper"])
	})

	t.Run("redacts scraper override with nil proxy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Scrapers.Overrides = map[string]*ScraperSettings{
			"noproxy": {
				Enabled: true,
			},
		}
		redacted := cfg.Redact()
		_, exists := redacted.Scrapers.Overrides["noproxy"]
		assert.True(t, exists)
	})
}

func TestDeepCopyFieldsMap(t *testing.T) {
	t.Run("nil map returns nil", func(t *testing.T) {
		assert.Nil(t, deepCopyFieldsMap(nil))
	})

	t.Run("empty map returns empty map", func(t *testing.T) {
		result := deepCopyFieldsMap(map[string][]string{})
		assert.Empty(t, result)
	})

	t.Run("copies all values", func(t *testing.T) {
		original := map[string][]string{
			"key1": {"a", "b"},
			"key2": {"c"},
		}
		result := deepCopyFieldsMap(original)
		assert.Equal(t, original, result)
	})

	t.Run("deep copy is independent", func(t *testing.T) {
		original := map[string][]string{
			"key1": {"a", "b"},
		}
		result := deepCopyFieldsMap(original)
		result["key1"][0] = "modified"
		assert.Equal(t, "a", original["key1"][0])
	})

	t.Run("nil slice values preserved", func(t *testing.T) {
		original := map[string][]string{
			"key1": nil,
		}
		result := deepCopyFieldsMap(original)
		assert.Nil(t, result["key1"])
	})
}

func TestRedactString(t *testing.T) {
	t.Run("empty string returns empty", func(t *testing.T) {
		assert.Equal(t, "", redactString(""))
	})

	t.Run("non-empty string returns redacted", func(t *testing.T) {
		assert.Equal(t, RedactedValue, redactString("secret"))
	})
}

func TestRedactProxyProfiles(t *testing.T) {
	t.Run("nil profiles returns nil", func(t *testing.T) {
		assert.Nil(t, redactProxyProfiles(nil))
	})

	t.Run("redacts username and password", func(t *testing.T) {
		profiles := map[string]ProxyProfile{
			"main": {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
		}
		result := redactProxyProfiles(profiles)
		assert.Equal(t, RedactedValue, result["main"].Username)
		assert.Equal(t, RedactedValue, result["main"].Password)
		assert.Equal(t, "http://proxy:8080", result["main"].URL)
	})

	t.Run("empty username and password not redacted", func(t *testing.T) {
		profiles := map[string]ProxyProfile{
			"noauth": {URL: "http://proxy:8080", Username: "", Password: ""},
		}
		result := redactProxyProfiles(profiles)
		assert.Equal(t, "", result["noauth"].Username)
		assert.Equal(t, "", result["noauth"].Password)
	})
}
