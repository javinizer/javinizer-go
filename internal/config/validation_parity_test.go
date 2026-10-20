package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cases lock down both entry points before sharing their implementation.
// Each suffix has multiple simultaneous errors, so their order is part of the contract.
func TestValidationParity_StructuralOrder(t *testing.T) {
	cases := []struct {
		name       string
		invalidate func(*Config)
		message    string
	}{
		{"database type", func(c *Config) { c.Database.Type = "postgres" }, "database.type must be 'sqlite' (currently only sqlite is supported)"},
		{"database dsn", func(c *Config) { c.Database.DSN = " " }, "database.dsn is required"},
		{"scraper timeout", func(c *Config) { c.Scrapers.TimeoutSeconds = 0 }, "scrapers.timeout_seconds must be between 1 and 300"},
		{"request timeout", func(c *Config) { c.Scrapers.RequestTimeoutSeconds = 601 }, "scrapers.request_timeout_seconds must be between 1 and 600"},
		{"flaresolverr", func(c *Config) { c.Scrapers.FlareSolverr.Enabled = true; c.Scrapers.FlareSolverr.URL = "" }, "scrapers.flaresolverr.url is required when flaresolverr is enabled"},
		{"browser", func(c *Config) { c.Scrapers.Browser.Enabled = true; c.Scrapers.Browser.Timeout = 0 }, "scrapers.browser.timeout must be between 1 and 300 seconds"},
		{"referer", func(c *Config) { c.Scrapers.Referer = "ftp://example.test" }, "scrapers.referer must be a valid http(s) URL with a host"},
		{"workers", func(c *Config) { c.Performance.MaxWorkers = 0 }, "performance.max_workers must be between 1 and 100"},
		{"worker timeout", func(c *Config) { c.Performance.WorkerTimeout = 9 }, "performance.worker_timeout must be between 10 and 3600"},
		{"update interval", func(c *Config) { c.Performance.UpdateInterval = 9 }, "performance.update_interval must be between 10 and 5000"},
		{"version interval", func(c *Config) { c.System.VersionCheckIntervalHours = 169 }, "system.version_check_interval_hours must be between 1 and 168 (1 week), or 0 for default"},
		{"cache ttl range", func(c *Config) { c.System.ImageCacheTTLHours = -1 }, "system.image_cache_ttl_hours must be between 0 and 87600 (10 years)"},
		{"cache quota", func(c *Config) { c.System.ImageCacheMaxSizeMB = -1 }, "system.image_cache_max_size_mb must be >= 0 (0 disables the quota)"},
		{"log size", func(c *Config) { c.Logging.MaxSizeMB = -1 }, "logging.max_size_mb must be >= 0"},
		{"log backups", func(c *Config) { c.Logging.MaxBackups = -1 }, "logging.max_backups must be >= 0"},
		{"log age", func(c *Config) { c.Logging.MaxAgeDays = -1 }, "logging.max_age_days must be >= 0"},
		{"review view", func(c *Config) { c.WebUI.DefaultReviewView = "invalid" }, "webui.default_review_view must be one of: detail, grid-poster, grid-cover"},
		{"ui language", func(c *Config) { c.UI.Language = "!" }, `ui.language must be "auto" or a valid BCP 47 tag (e.g. "en", "ja", "zh-Hans", "pt-BR"): language: tag is not well-formed`},
		{"operation mode", func(c *Config) { c.Output.Operation.OperationMode = "invalid" }, `output.operation_mode is invalid: invalid operation mode "invalid" (expected one of: organize, in-place, in-place-norenamefolder, metadata-artwork, preview)`},
		{"scraper override", func(c *Config) { c.Scrapers.Overrides = map[string]*models.ScraperSettings{"r18dev": nil} }, "r18dev: config is nil"},
		{"proxy", func(c *Config) { c.Scrapers.Proxy.Enabled = true; c.Scrapers.Proxy.DefaultProfile = "missing" }, `scrapers.proxy.default_profile references unknown profile "missing"`},
		{"translation", func(c *Config) { c.Metadata.Translation.Enabled = true; c.Metadata.Translation.TimeoutSeconds = 4 }, "metadata.translation.timeout_seconds must be between 5 and 300"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, multiple := range []bool{false, true} {
				cfg := DefaultConfig(nil, nil)
				tc.invalidate(cfg)
				if multiple {
					for _, later := range cases[i+1:] {
						later.invalidate(cfg)
					}
				}
				assertValidationParity(t, cfg, tc.message, tc.message)
			}
		})
	}
	t.Run("enabled cache requires ttl before quota", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.System.ImageCacheEnabled = true
		cfg.System.ImageCacheTTLHours = 0
		cfg.System.ImageCacheMaxSizeMB = -1
		msg := "system.image_cache_ttl_hours must be >= 1 when image caching is enabled"
		assertValidationParity(t, cfg, msg, msg)
	})
}

func assertValidationParity(t *testing.T, cfg *Config, structuralError, runtimeError string) {
	t.Helper()
	before := cfg.Clone()
	for _, entry := range []struct {
		name     string
		validate func(*Config) error
		want     string
	}{
		{"persistence", validateConfigExcludingTranslationCredentials, structuralError},
		{"runtime", ValidateConfig, runtimeError},
	} {
		err := entry.validate(cfg)
		if entry.want == "" {
			require.NoError(t, err, entry.name)
		} else {
			require.EqualError(t, err, entry.want, entry.name)
		}
		assert.Equal(t, before, cfg, "%s validation must not mutate its input", entry.name)
	}
}

func TestValidationParity_Translation(t *testing.T) {
	cases := []struct {
		name            string
		setup           func(*TranslationConfig)
		structuralError string
		runtimeError    string
	}{
		{"disabled skips invalid fields", func(c *TranslationConfig) { c.Enabled = false; c.Provider = "invalid"; c.TimeoutSeconds = 301 }, "", ""},
		{"default provider missing key", func(c *TranslationConfig) { c.Provider = " "; c.TimeoutSeconds = 0; c.OpenAI.BaseURL = " " }, "", "metadata.translation.openai.api_key is required when provider=openai"},
		{"timeout before provider", func(c *TranslationConfig) { c.Provider = "invalid"; c.TimeoutSeconds = 4 }, "metadata.translation.timeout_seconds must be between 5 and 300", ""},
		{"unknown provider", func(c *TranslationConfig) { c.Provider = "invalid" }, "metadata.translation.provider must be one of: openai, openai-compatible, anthropic, deepl, google", ""},
		{"openai bad url before key", func(c *TranslationConfig) { c.OpenAI.BaseURL = "ftp://example.test" }, "metadata.translation.openai.base_url must be a valid http(s) URL", ""},
		{"openai malformed url", func(c *TranslationConfig) { c.OpenAI.BaseURL = "http://%" }, "metadata.translation.openai.base_url must be a valid http(s) URL", ""},
		{"openai key whitespace", func(c *TranslationConfig) { c.OpenAI.APIKey = " \t " }, "", "metadata.translation.openai.api_key is required when provider=openai"},
		{"openai normalized provider", func(c *TranslationConfig) {
			c.Provider = " OpenAI "
			c.OpenAI.APIKey = "key"
			c.OpenAI.BaseURL = ""
			c.TimeoutSeconds = -1
		}, "", ""},
		{"deepl default mode key required", func(c *TranslationConfig) { c.Provider = " DeePL "; c.DeepL.Mode = "" }, "", "metadata.translation.deepl.api_key is required when provider=deepl"},
		{"deepl mode before url", func(c *TranslationConfig) { c.Provider = "deepl"; c.DeepL.Mode = "bad"; c.DeepL.BaseURL = "bad" }, "metadata.translation.deepl.mode must be either 'free' or 'pro'", ""},
		{"deepl url before key", func(c *TranslationConfig) { c.Provider = "deepl"; c.DeepL.BaseURL = "bad" }, "metadata.translation.deepl.base_url must be a valid http(s) URL", ""},
		{"deepl normalized pro", func(c *TranslationConfig) {
			c.Provider = "deepl"
			c.DeepL.Mode = " PRO "
			c.DeepL.APIKey = "key"
			c.DeepL.BaseURL = " https://example.test "
		}, "", ""},
		{"google default free no key", func(c *TranslationConfig) { c.Provider = "google"; c.Google.Mode = "" }, "", ""},
		{"google normalized free no key", func(c *TranslationConfig) { c.Provider = " GOOGLE "; c.Google.Mode = " FREE " }, "", ""},
		{"google normalized paid needs key", func(c *TranslationConfig) { c.Provider = " GOOGLE "; c.Google.Mode = " PAID "; c.Google.APIKey = " " }, "", "metadata.translation.google.api_key is required when provider=google and mode=paid"},
		{"google paid with key", func(c *TranslationConfig) {
			c.Provider = "google"
			c.Google.Mode = "paid"
			c.Google.APIKey = "key"
			c.Google.BaseURL = "https://example.test"
		}, "", ""},
		{"google mode before url", func(c *TranslationConfig) { c.Provider = "google"; c.Google.Mode = "bad"; c.Google.BaseURL = "bad" }, "metadata.translation.google.mode must be either 'free' or 'paid'", ""},
		{"google url before key", func(c *TranslationConfig) { c.Provider = "google"; c.Google.Mode = "paid"; c.Google.BaseURL = "bad" }, "metadata.translation.google.base_url must be a valid http(s) URL", ""},
		{"compatible required url first", func(c *TranslationConfig) {
			c.Provider = "openai-compatible"
			c.OpenAICompatible.BaseURL = " "
			c.OpenAICompatible.Model = ""
			c.OpenAICompatible.BackendType = "bad"
		}, "metadata.translation.openai_compatible.base_url is required when provider=openai-compatible", ""},
		{"compatible invalid url before model", func(c *TranslationConfig) {
			c.Provider = "openai-compatible"
			c.OpenAICompatible.BaseURL = "bad"
			c.OpenAICompatible.Model = ""
		}, "metadata.translation.openai_compatible.base_url must be a valid http(s) URL", ""},
		{"compatible required model before backend", func(c *TranslationConfig) {
			c.Provider = "openai-compatible"
			c.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
			c.OpenAICompatible.Model = " "
			c.OpenAICompatible.BackendType = "bad"
		}, "metadata.translation.openai_compatible.model is required when provider=openai-compatible", ""},
		{"compatible invalid backend", func(c *TranslationConfig) {
			c.Provider = "openai-compatible"
			c.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
			c.OpenAICompatible.Model = "model"
			c.OpenAICompatible.BackendType = "bad"
		}, "metadata.translation.openai_compatible.backend_type must be one of: auto, vllm, ollama, llama.cpp, other", ""},
		{"compatible optional key", func(c *TranslationConfig) {
			c.Provider = " OPENAI-COMPATIBLE "
			c.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
			c.OpenAICompatible.Model = "model"
			c.OpenAICompatible.BackendType = "auto"
		}, "", ""},
		{"anthropic required url first", func(c *TranslationConfig) {
			c.Provider = "anthropic"
			c.Anthropic.BaseURL = " "
			c.Anthropic.Model = ""
		}, "metadata.translation.anthropic.base_url is required when provider=anthropic", ""},
		{"anthropic bad url before model", func(c *TranslationConfig) {
			c.Provider = "anthropic"
			c.Anthropic.BaseURL = "bad"
			c.Anthropic.Model = ""
		}, "metadata.translation.anthropic.base_url must be a valid http(s) URL", ""},
		{"anthropic model before key", func(c *TranslationConfig) { c.Provider = "anthropic"; c.Anthropic.Model = " " }, "metadata.translation.anthropic.model is required when provider=anthropic", ""},
		{"anthropic key required", func(c *TranslationConfig) { c.Provider = " ANTHROPIC "; c.Anthropic.APIKey = " " }, "", "metadata.translation.anthropic.api_key is required when provider=anthropic"},
		{"anthropic with key", func(c *TranslationConfig) { c.Provider = "anthropic"; c.Anthropic.APIKey = "key" }, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			cfg.Metadata.Translation.Enabled = true
			tc.setup(&cfg.Metadata.Translation)
			runtimeError := tc.runtimeError
			if tc.structuralError != "" {
				runtimeError = tc.structuralError
			}
			assertValidationParity(t, cfg, tc.structuralError, runtimeError)
			before := cfg.Clone()
			for _, entry := range []struct {
				validate func(*Config) error
				want     string
			}{
				{validateTranslationProviderExcludingCredentials, tc.structuralError},
				{ValidateTranslationProvider, runtimeError},
				{(*Config).validateTranslationConfig, runtimeError},
			} {
				err := entry.validate(cfg)
				if entry.want == "" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, entry.want)
				}
				assert.Equal(t, before, cfg)
			}
		})
	}
}

func TestValidationParity_DefaultsDoNotMutate(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = " "
	cfg.Scrapers.Referer = ""
	cfg.Scrapers.Overrides = nil
	cfg.Output.Operation.OperationMode = ""
	cfg.System.VersionCheckIntervalHours = 0
	cfg.WebUI.DefaultReviewView = ""
	cfg.UI.Language = ""
	assertValidationParity(t, cfg, "", "")
	assert.Nil(t, cfg.Scrapers.Overrides, "delegated proxy validation must normalize only the clone")
}

func TestValidationParity_PersistenceThenRuntimeEnvironment(t *testing.T) {
	for _, tc := range []struct{ provider, mode, keyEnv, missingKey string }{
		{"openai", "", "OPENAI_API_KEY", "metadata.translation.openai.api_key is required when provider=openai"},
		{"deepl", "", "DEEPL_API_KEY", "metadata.translation.deepl.api_key is required when provider=deepl"},
		{"google", "paid", "GOOGLE_TRANSLATE_API_KEY", "metadata.translation.google.api_key is required when provider=google and mode=paid"},
		{"anthropic", "", "ANTHROPIC_API_KEY", "metadata.translation.anthropic.api_key is required when provider=anthropic"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			cfg.Scrapers.Priority = []string{"r18dev"}
			cfg.Database.Type = " SQLITE "
			cfg.Scrapers.Referer = ""
			cfg.Metadata.Translation.Enabled = true
			cfg.Metadata.Translation.Provider = " " + tc.provider + " "
			cfg.Metadata.Translation.TimeoutSeconds = 0
			cfg.Metadata.Translation.Google.Mode = models.GoogleMode(tc.mode)
			changed, err := PrepareForPersistence(cfg)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, "sqlite", cfg.Database.Type)
			assert.Equal(t, dmmBaseURL, cfg.Scrapers.Referer)
			assert.Equal(t, tc.provider, cfg.Metadata.Translation.Provider)
			assert.Equal(t, 60, cfg.Metadata.Translation.TimeoutSeconds)
			changed, err = PrepareForPersistence(cfg)
			require.NoError(t, err)
			assert.False(t, changed, "normalization must be idempotent")
			changed, err = PrepareRuntime(cfg)
			require.EqualError(t, err, "invalid configuration: "+tc.missingKey)
			assert.False(t, changed)
			ApplyEnvironmentOverrides(cfg, func(key string) string {
				if key == tc.keyEnv {
					return " env-key "
				}
				return ""
			})
			changed, err = PrepareRuntime(cfg)
			require.NoError(t, err)
			assert.False(t, changed)
		})
	}
}
