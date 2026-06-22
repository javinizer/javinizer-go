package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestIsValidHTTPURL_Uncovered(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		expect bool
	}{
		{"valid http", "http://example.com", true},
		{"valid https", "https://example.com", true},
		{"valid with path", "https://example.com/path?q=1", true},
		{"invalid ftp", "ftp://example.com", false},
		{"no scheme", "example.com", false},
		{"empty string", "", false},
		{"just scheme", "http://", false},
		{"no host", "http:///path", false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"data scheme", "data:text/html,<h1>hi</h1>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isValidHTTPURL(tt.url))
		})
	}
}

func TestProxyProfilesEqual_Uncovered(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]models.ProxyProfile
		b    map[string]models.ProxyProfile
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "both empty",
			a:    map[string]models.ProxyProfile{},
			b:    map[string]models.ProxyProfile{},
			want: true,
		},
		{
			name: "different lengths",
			a: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy1:8080"},
			},
			b:    map[string]models.ProxyProfile{},
			want: false,
		},
		{
			name: "same profiles",
			a: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy1:8080", Username: "user1"},
			},
			b: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy1:8080", Username: "user1"},
			},
			want: true,
		},
		{
			name: "different profile values",
			a: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy1:8080"},
			},
			b: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy2:8080"},
			},
			want: false,
		},
		{
			name: "different keys",
			a: map[string]models.ProxyProfile{
				"p1": {URL: "http://proxy1:8080"},
			},
			b: map[string]models.ProxyProfile{
				"p2": {URL: "http://proxy1:8080"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, proxyProfilesEqual(tt.a, tt.b))
		})
	}
}

func TestPreserveRedactedSecrets_Uncovered(t *testing.T) {
	t.Run("nil old config", func(t *testing.T) {
		newCfg := &config.Config{}
		preserveRedactedSecrets(nil, newCfg)
		// Should not panic
	})

	t.Run("nil new config", func(t *testing.T) {
		oldCfg := &config.Config{}
		preserveRedactedSecrets(oldCfg, nil)
		// Should not panic
	})

	t.Run("DSN preserved", func(t *testing.T) {
		oldCfg := &config.Config{
			Database: config.DatabaseConfig{
				DSN: "file:test.db",
			},
		}
		newCfg := &config.Config{
			Database: config.DatabaseConfig{
				DSN: models.RedactedValue,
			},
		}
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "file:test.db", newCfg.Database.DSN)
	})

	t.Run("DSN not redacted remains", func(t *testing.T) {
		oldCfg := &config.Config{
			Database: config.DatabaseConfig{
				DSN: "file:old.db",
			},
		}
		newCfg := &config.Config{
			Database: config.DatabaseConfig{
				DSN: "file:new.db",
			},
		}
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "file:new.db", newCfg.Database.DSN)
	})

	t.Run("OpenAI API key preserved", func(t *testing.T) {
		oldCfg := &config.Config{}
		oldCfg.Metadata.Translation.OpenAI.APIKey = "sk-real-key"
		newCfg := &config.Config{}
		newCfg.Metadata.Translation.OpenAI.APIKey = models.RedactedValue
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "sk-real-key", newCfg.Metadata.Translation.OpenAI.APIKey)
	})

	t.Run("DeepL API key preserved", func(t *testing.T) {
		oldCfg := &config.Config{}
		oldCfg.Metadata.Translation.DeepL.APIKey = "deepl-real-key"
		newCfg := &config.Config{}
		newCfg.Metadata.Translation.DeepL.APIKey = models.RedactedValue
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "deepl-real-key", newCfg.Metadata.Translation.DeepL.APIKey)
	})

	t.Run("Google API key preserved", func(t *testing.T) {
		oldCfg := &config.Config{}
		oldCfg.Metadata.Translation.Google.APIKey = "google-real-key"
		newCfg := &config.Config{}
		newCfg.Metadata.Translation.Google.APIKey = models.RedactedValue
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "google-real-key", newCfg.Metadata.Translation.Google.APIKey)
	})

	t.Run("OpenAI Compatible API key preserved", func(t *testing.T) {
		oldCfg := &config.Config{}
		oldCfg.Metadata.Translation.OpenAICompatible.APIKey = "oai-real-key"
		newCfg := &config.Config{}
		newCfg.Metadata.Translation.OpenAICompatible.APIKey = models.RedactedValue
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "oai-real-key", newCfg.Metadata.Translation.OpenAICompatible.APIKey)
	})

	t.Run("Anthropic API key preserved", func(t *testing.T) {
		oldCfg := &config.Config{}
		oldCfg.Metadata.Translation.Anthropic.APIKey = "anthropic-real-key"
		newCfg := &config.Config{}
		newCfg.Metadata.Translation.Anthropic.APIKey = models.RedactedValue
		preserveRedactedSecrets(oldCfg, newCfg)
		assert.Equal(t, "anthropic-real-key", newCfg.Metadata.Translation.Anthropic.APIKey)
	})
}

func TestPreserveRedactedProxyProfiles_Uncovered(t *testing.T) {
	t.Run("nil maps", func(t *testing.T) {
		preserveRedactedProxyProfiles(nil, nil)
		// Should not panic
	})

	t.Run("old nil new not nil", func(t *testing.T) {
		new := map[string]models.ProxyProfile{
			"p1": {Username: models.RedactedValue},
		}
		preserveRedactedProxyProfiles(nil, new)
		assert.Equal(t, models.RedactedValue, new["p1"].Username)
	})

	t.Run("redacted username preserved", func(t *testing.T) {
		old := map[string]models.ProxyProfile{
			"p1": {Username: "real-user", Password: "real-pass"},
		}
		new := map[string]models.ProxyProfile{
			"p1": {Username: models.RedactedValue, Password: models.RedactedValue},
		}
		preserveRedactedProxyProfiles(old, new)
		assert.Equal(t, "real-user", new["p1"].Username)
		assert.Equal(t, "real-pass", new["p1"].Password)
	})

	t.Run("non-redacted values not overwritten", func(t *testing.T) {
		old := map[string]models.ProxyProfile{
			"p1": {Username: "old-user", Password: "old-pass"},
		}
		new := map[string]models.ProxyProfile{
			"p1": {Username: "new-user", Password: "new-pass"},
		}
		preserveRedactedProxyProfiles(old, new)
		assert.Equal(t, "new-user", new["p1"].Username)
		assert.Equal(t, "new-pass", new["p1"].Password)
	})

	t.Run("key not in old map skipped", func(t *testing.T) {
		old := map[string]models.ProxyProfile{
			"p1": {Username: "user1"},
		}
		new := map[string]models.ProxyProfile{
			"p2": {Username: models.RedactedValue},
		}
		preserveRedactedProxyProfiles(old, new)
		assert.Equal(t, models.RedactedValue, new["p2"].Username)
	})
}

func TestValidateTranslationSaveConfig_Uncovered(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		assert.NoError(t, validateTranslationSaveConfig(nil))
	})

	t.Run("translation disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = false
		assert.NoError(t, validateTranslationSaveConfig(cfg))
	})

	t.Run("openai missing api key", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = ""
		err := validateTranslationSaveConfig(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "openai.api_key is required")
	})

	t.Run("openai with api key", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-test"
		assert.NoError(t, validateTranslationSaveConfig(cfg))
	})

	t.Run("deepl missing api key", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "deepl"
		cfg.Metadata.Translation.DeepL.APIKey = ""
		err := validateTranslationSaveConfig(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deepl.api_key is required")
	})

	t.Run("deepl with api key", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "deepl"
		cfg.Metadata.Translation.DeepL.APIKey = "deepl-key"
		assert.NoError(t, validateTranslationSaveConfig(cfg))
	})

	t.Run("google paid missing api key", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "google"
		cfg.Metadata.Translation.Google.Mode = "paid"
		cfg.Metadata.Translation.Google.APIKey = ""
		err := validateTranslationSaveConfig(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google.api_key is required")
	})

	t.Run("google free no api key needed", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "google"
		cfg.Metadata.Translation.Google.Mode = "free"
		cfg.Metadata.Translation.Google.APIKey = ""
		assert.NoError(t, validateTranslationSaveConfig(cfg))
	})

	t.Run("unknown provider rejected", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "unknown"
		err := validateTranslationSaveConfig(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metadata.translation.provider must be one of")
	})
}

func TestFormatDirectProxyError_Uncovered(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		result := formatDirectProxyError(nil)
		assert.Contains(t, result, "direct proxy request failed")
	})

	t.Run("method not allowed", func(t *testing.T) {
		result := formatDirectProxyError(assert.AnError)
		assert.Contains(t, result, "direct proxy request failed")
	})
}

func TestMapConfigErrorToHTTP_Uncovered(t *testing.T) {
	t.Run("nil error returns zero", func(t *testing.T) {
		status, msg := mapConfigErrorToHTTP(nil)
		assert.Equal(t, 0, status)
		assert.Empty(t, msg)
	})

	t.Run("validation error returns 400", func(t *testing.T) {
		err := &validationError{message: "bad config"}
		status, msg := mapConfigErrorToHTTP(err)
		assert.Equal(t, 400, status)
		assert.Equal(t, "bad config", msg)
	})

	t.Run("persist error returns 500", func(t *testing.T) {
		err := &persistError{message: "disk full"}
		status, msg := mapConfigErrorToHTTP(err)
		assert.Equal(t, 500, status)
		assert.Equal(t, "disk full", msg)
	})

	t.Run("reload error returns 500", func(t *testing.T) {
		err := &reloadError{message: "reload failed"}
		status, msg := mapConfigErrorToHTTP(err)
		assert.Equal(t, 500, status)
		assert.Contains(t, msg, "reload failed")
	})

	t.Run("rollback error returns 500", func(t *testing.T) {
		err := &rollbackError{message: "critical failure"}
		status, msg := mapConfigErrorToHTTP(err)
		assert.Equal(t, 500, status)
		assert.Contains(t, msg, "critical failure")
	})

	t.Run("unknown error returns 500", func(t *testing.T) {
		status, msg := mapConfigErrorToHTTP(assert.AnError)
		assert.Equal(t, 500, status)
		assert.NotEmpty(t, msg)
	})
}
