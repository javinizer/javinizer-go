package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestRedact(t *testing.T) {
	t.Run("redacts OpenAI API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-secret-key-123"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Metadata.Translation.OpenAI.APIKey)
		assert.Equal(t, "sk-secret-key-123", cfg.Metadata.Translation.OpenAI.APIKey)
	})

	t.Run("redacts DeepL API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.DeepL.APIKey = "deepl-secret"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Metadata.Translation.DeepL.APIKey)
		assert.Equal(t, "deepl-secret", cfg.Metadata.Translation.DeepL.APIKey)
	})

	t.Run("redacts Google API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.Google.APIKey = "google-secret"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Metadata.Translation.Google.APIKey)
		assert.Equal(t, "google-secret", cfg.Metadata.Translation.Google.APIKey)
	})

	t.Run("redacts Anthropic API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.Anthropic.APIKey = "anthropic-secret"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Metadata.Translation.Anthropic.APIKey)
		assert.Equal(t, "anthropic-secret", cfg.Metadata.Translation.Anthropic.APIKey)
	})

	t.Run("redacts OpenAI-compatible API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAICompatible.APIKey = "compatible-secret"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Metadata.Translation.OpenAICompatible.APIKey)
		assert.Equal(t, "compatible-secret", cfg.Metadata.Translation.OpenAICompatible.APIKey)
	})

	t.Run("redacts Database DSN", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Database.DSN = "user:password@tcp(localhost:3306)/db"
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Database.DSN)
		assert.Equal(t, "user:password@tcp(localhost:3306)/db", cfg.Database.DSN)
	})

	t.Run("redacts proxy profile password", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
		}
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Proxy.Profiles["main"].Username)
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Proxy.Profiles["main"].Password)
		assert.Equal(t, "http://proxy:8080", redacted.Scrapers.Proxy.Profiles["main"].URL)
		assert.Equal(t, "admin", cfg.Scrapers.Proxy.Profiles["main"].Username)
		assert.Equal(t, "s3cret", cfg.Scrapers.Proxy.Profiles["main"].Password)
	})

	t.Run("redacts download proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
			"dl": {URL: "http://dlproxy:8080", Username: "dluser", Password: "dlpass"},
		}
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Output.Download.DownloadProxy.Profiles["dl"].Username)
		assert.Equal(t, models.RedactedValue, redacted.Output.Download.DownloadProxy.Profiles["dl"].Password)
		assert.Equal(t, "http://dlproxy:8080", redacted.Output.Download.DownloadProxy.Profiles["dl"].URL)
	})

	t.Run("does not modify non-secret fields", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
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
		cfg := DefaultConfig(nil, nil)
		cfg.Database.DSN = "original-dsn"
		cfg.Metadata.Translation.OpenAI.APIKey = "original-key"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
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
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAI.APIKey = ""
		cfg.Database.DSN = ""
		redacted := cfg.Redact()
		assert.Equal(t, "", redacted.Metadata.Translation.OpenAI.APIKey)
		assert.Equal(t, "", redacted.Database.DSN)
	})

	t.Run("redacts scraper override proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javbus": {
				Enabled: true,
				Proxy: &models.ProxyConfig{
					Enabled: true,
					Profiles: map[string]models.ProxyProfile{
						"custom": {URL: "http://custom:8080", Username: "customuser", Password: "custompass"},
					},
				},
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Username)
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Password)
		assert.Equal(t, "http://custom:8080", redacted.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].URL)
		assert.Equal(t, "customuser", cfg.Scrapers.Overrides["javbus"].Proxy.Profiles["custom"].Username)
	})

	t.Run("nil config returns nil", func(t *testing.T) {
		var cfg *Config
		assert.Nil(t, cfg.Redact())
	})

	t.Run("deep copy of priority fields map", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
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
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Priority.Fields = map[string][]string{
			"empty": nil,
		}
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Metadata.Priority.Fields["empty"])
	})

	t.Run("deep copy of priority fields nil map", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Priority.Fields = nil
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Metadata.Priority.Fields)
	})

	t.Run("redacts scraper override download proxy profiles", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javdb": {
				Enabled: true,
				DownloadProxy: &models.ProxyConfig{
					Enabled: true,
					Profiles: map[string]models.ProxyProfile{
						"dlproxy": {URL: "http://dl:8080", Username: "dluser", Password: "dlpass"},
					},
				},
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].Username)
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].Password)
		assert.Equal(t, "http://dl:8080", redacted.Scrapers.Overrides["javdb"].DownloadProxy.Profiles["dlproxy"].URL)
	})

	t.Run("handles nil scraper override value", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"nilscraper": nil,
		}
		redacted := cfg.Redact()
		assert.Nil(t, redacted.Scrapers.Overrides["nilscraper"])
	})

	t.Run("redacts scraper override API key", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				Enabled: true,
				APIKey:  "super-secret-api-key",
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, models.RedactedValue, redacted.Scrapers.Overrides["javstash"].APIKey)
		assert.Equal(t, "super-secret-api-key", cfg.Scrapers.Overrides["javstash"].APIKey)
	})

	t.Run("empty scraper override API key is not redacted", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javbus": {
				Enabled: true,
				APIKey:  "",
			},
		}
		redacted := cfg.Redact()
		assert.Equal(t, "", redacted.Scrapers.Overrides["javbus"].APIKey)
	})

	t.Run("redacts scraper override with nil proxy", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"noproxy": {
				Enabled: true,
			},
		}
		redacted := cfg.Redact()
		_, exists := redacted.Scrapers.Overrides["noproxy"]
		assert.True(t, exists)
	})

	t.Run("deep copy of scraper override reference-type fields", func(t *testing.T) {
		// Verifies that Clone() is used instead of shallow copy — mutations
		// to Cookies, ExtraPlaceholderHashes, and ScrapeActress in the
		// redacted copy must NOT leak back to the original Config.
		scrapeActress := true
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"test": {
				Enabled:                true,
				Cookies:                map[string]string{"session": "original-session"},
				ExtraPlaceholderHashes: []string{"hash1", "hash2"},
				ScrapeActress:          &scrapeActress,
			},
		}

		redacted := cfg.Redact()

		// Mutate redacted copy's reference-type fields
		redacted.Scrapers.Overrides["test"].Cookies["session"] = "tampered"
		redacted.Scrapers.Overrides["test"].ExtraPlaceholderHashes[0] = "tampered"
		*redacted.Scrapers.Overrides["test"].ScrapeActress = false

		// Original must be unchanged
		assert.Equal(t, "original-session", cfg.Scrapers.Overrides["test"].Cookies["session"],
			"Cookies map should be deep-copied, not shared")
		assert.Equal(t, "hash1", cfg.Scrapers.Overrides["test"].ExtraPlaceholderHashes[0],
			"ExtraPlaceholderHashes slice should be deep-copied, not shared")
		assert.True(t, *cfg.Scrapers.Overrides["test"].ScrapeActress,
			"ScrapeActress pointer should be deep-copied, not shared")
	})

	t.Run("deep copy of EnableThinking *bool pointer", func(t *testing.T) {
		thinking := true
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAICompatible.EnableThinking = &thinking

		redacted := cfg.Redact()

		// Mutate the redacted copy's EnableThinking pointer
		*redacted.Metadata.Translation.OpenAICompatible.EnableThinking = false

		// Original must be unchanged
		assert.True(t, *cfg.Metadata.Translation.OpenAICompatible.EnableThinking,
			"EnableThinking *bool pointer should be deep-copied, not shared")
	})

	t.Run("deep copy of slice fields", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Matching.Extensions = []string{".mp4", ".mkv"}
		cfg.Matching.ExcludePatterns = []string{"*-trailer*"}
		cfg.Output.Template.SubfolderFormat = []string{"<ID>"}
		cfg.Metadata.IgnoreGenres = []string{"genre1"}
		cfg.Metadata.RequiredFields = []string{"title"}
		cfg.Metadata.NFO.Extra.Tag = []string{"tag1"}
		cfg.Metadata.NFO.Extra.Credits = []string{"credit1"}
		cfg.API.Security.AllowedDirectories = []string{"/media"}
		cfg.Scrapers.Priority = []string{"dmm", "r18dev"}

		redacted := cfg.Redact()

		// Mutate all slice fields in the redacted copy
		redacted.Matching.Extensions[0] = ".tampered"
		redacted.Matching.ExcludePatterns[0] = "tampered"
		redacted.Output.Template.SubfolderFormat[0] = "tampered"
		redacted.Metadata.IgnoreGenres[0] = "tampered"
		redacted.Metadata.RequiredFields[0] = "tampered"
		redacted.Metadata.NFO.Extra.Tag[0] = "tampered"
		redacted.Metadata.NFO.Extra.Credits[0] = "tampered"
		redacted.API.Security.AllowedDirectories[0] = "tampered"
		redacted.Scrapers.Priority[0] = "tampered"

		// Original must be unchanged
		assert.Equal(t, ".mp4", cfg.Matching.Extensions[0],
			"Matching.Extensions slice should be deep-copied, not shared")
		assert.Equal(t, "*-trailer*", cfg.Matching.ExcludePatterns[0],
			"Matching.ExcludePatterns slice should be deep-copied, not shared")
		assert.Equal(t, "<ID>", cfg.Output.Template.SubfolderFormat[0],
			"Output.SubfolderFormat slice should be deep-copied, not shared")
		assert.Equal(t, "genre1", cfg.Metadata.IgnoreGenres[0],
			"Metadata.IgnoreGenres slice should be deep-copied, not shared")
		assert.Equal(t, "title", cfg.Metadata.RequiredFields[0],
			"Metadata.RequiredFields slice should be deep-copied, not shared")
		assert.Equal(t, "tag1", cfg.Metadata.NFO.Extra.Tag[0],
			"Metadata.NFO.Extra.Tag slice should be deep-copied, not shared")
		assert.Equal(t, "credit1", cfg.Metadata.NFO.Extra.Credits[0],
			"Metadata.NFO.Extra.Credits slice should be deep-copied, not shared")
		assert.Equal(t, "/media", cfg.API.Security.AllowedDirectories[0],
			"API.Security.AllowedDirectories slice should be deep-copied, not shared")
		assert.Equal(t, "dmm", cfg.Scrapers.Priority[0],
			"Scrapers.Priority slice should be deep-copied, not shared")
	})

	t.Run("deep copy of Completeness tier Fields slices", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Completeness.Tiers.Essential.Fields = []string{"title", "poster_url"}
		cfg.Metadata.Completeness.Tiers.Important.Fields = []string{"description"}
		cfg.Metadata.Completeness.Tiers.NiceToHave.Fields = []string{"label"}

		redacted := cfg.Redact()

		redacted.Metadata.Completeness.Tiers.Essential.Fields[0] = "tampered"
		redacted.Metadata.Completeness.Tiers.Important.Fields[0] = "tampered"
		redacted.Metadata.Completeness.Tiers.NiceToHave.Fields[0] = "tampered"

		assert.Equal(t, "title", cfg.Metadata.Completeness.Tiers.Essential.Fields[0],
			"Completeness.Essential.Fields slice should be deep-copied, not shared")
		assert.Equal(t, "description", cfg.Metadata.Completeness.Tiers.Important.Fields[0],
			"Completeness.Important.Fields slice should be deep-copied, not shared")
		assert.Equal(t, "label", cfg.Metadata.Completeness.Tiers.NiceToHave.Fields[0],
			"Completeness.NiceToHave.Fields slice should be deep-copied, not shared")
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
		assert.Equal(t, models.RedactedValue, redactString("secret"))
	})
}

func TestRedactProxyProfiles(t *testing.T) {
	// redactProxyProfiles helper was removed in favor of ProxyConfig.Redact()
	// (which redacts profiles via ProxyProfile.Redact internally). Verify the
	// same per-profile redaction behavior is preserved through Redact().
	t.Run("Redact redacts username and password across profiles", func(t *testing.T) {
		pc := models.ProxyConfig{
			Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
				"noauth": {URL: "http://proxy:8080", Username: "", Password: ""},
			},
		}
		result := pc.Redact()
		assert.Equal(t, models.RedactedValue, result.Profiles["main"].Username)
		assert.Equal(t, models.RedactedValue, result.Profiles["main"].Password)
		assert.Equal(t, "http://proxy:8080", result.Profiles["main"].URL)
		assert.Equal(t, "", result.Profiles["noauth"].Username)
		assert.Equal(t, "", result.Profiles["noauth"].Password)
	})
}

func TestClone(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		var cfg *Config
		assert.Nil(t, cfg.Clone())
	})

	t.Run("EnableThinking nil preserved", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAICompatible.EnableThinking = nil
		cloned := cfg.Clone()
		assert.Nil(t, cloned.Metadata.Translation.OpenAICompatible.EnableThinking)
	})

	t.Run("EnableThinking pointer deep-copied", func(t *testing.T) {
		thinking := true
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OpenAICompatible.EnableThinking = &thinking
		cloned := cfg.Clone()
		*cloned.Metadata.Translation.OpenAICompatible.EnableThinking = false
		assert.True(t, *cfg.Metadata.Translation.OpenAICompatible.EnableThinking)
	})

	t.Run("slice fields deep-copied", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Matching.Extensions = []string{".mp4", ".mkv"}
		cfg.Scrapers.Priority = []string{"dmm", "r18dev"}

		cloned := cfg.Clone()
		cloned.Matching.Extensions[0] = ".tampered"
		cloned.Scrapers.Priority[0] = "tampered"

		assert.Equal(t, ".mp4", cfg.Matching.Extensions[0])
		assert.Equal(t, "dmm", cfg.Scrapers.Priority[0])
	})

	t.Run("nil slice preserved", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Matching.Extensions = nil
		cloned := cfg.Clone()
		assert.Nil(t, cloned.Matching.Extensions)
	})

	t.Run("Overrides map deep-copied", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"test": {Enabled: true, APIKey: "secret"},
		}

		cloned := cfg.Clone()
		cloned.Scrapers.Overrides["test"].APIKey = "tampered"

		assert.Equal(t, "secret", cfg.Scrapers.Overrides["test"].APIKey)
	})

	t.Run("proxy profiles map deep-copied", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: "http://proxy:8080", Username: "admin", Password: "s3cret"},
		}

		cloned := cfg.Clone()
		p := cloned.Scrapers.Proxy.Profiles["main"]
		p.Username = "tampered"
		cloned.Scrapers.Proxy.Profiles["main"] = p

		assert.Equal(t, "admin", cfg.Scrapers.Proxy.Profiles["main"].Username)
	})
}
