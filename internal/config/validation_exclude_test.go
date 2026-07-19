package config

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func translationCfg(enabled bool, provider string) *Config {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = enabled
	cfg.Metadata.Translation.Provider = provider
	return cfg
}

func TestValidateTranslationProviderExcludingCredentials_NilConfig(t *testing.T) {
	assert.NoError(t, validateTranslationProviderExcludingCredentials(nil))
}

func TestValidateTranslationProviderExcludingCredentials_Disabled(t *testing.T) {
	cfg := translationCfg(false, "openai")
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_OpenAI(t *testing.T) {
	cfg := translationCfg(true, "openai")
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_OpenAIBadURL(t *testing.T) {
	cfg := translationCfg(true, "openai")
	cfg.Metadata.Translation.OpenAI.BaseURL = "not a url"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

func TestValidateTranslationProviderExcludingCredentials_DeepL(t *testing.T) {
	cfg := translationCfg(true, "deepl")
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_DeepLBadMode(t *testing.T) {
	cfg := translationCfg(true, "deepl")
	cfg.Metadata.Translation.DeepL.Mode = "bogus"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deepl.mode")
}

func TestValidateTranslationProviderExcludingCredentials_DeepLBadBaseURL(t *testing.T) {
	cfg := translationCfg(true, "deepl")
	cfg.Metadata.Translation.DeepL.BaseURL = "ftp://bad"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deepl.base_url")
}

func TestValidateTranslationProviderExcludingCredentials_Google(t *testing.T) {
	cfg := translationCfg(true, "google")
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_GoogleBadMode(t *testing.T) {
	cfg := translationCfg(true, "google")
	cfg.Metadata.Translation.Google.Mode = "bogus"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "google.mode")
}

func TestValidateTranslationProviderExcludingCredentials_GoogleBadBaseURL(t *testing.T) {
	cfg := translationCfg(true, "google")
	cfg.Metadata.Translation.Google.BaseURL = "ftp://bad"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "google.base_url")
}

func TestValidateTranslationProviderExcludingCredentials_OpenAICompatible(t *testing.T) {
	cfg := translationCfg(true, "openai-compatible")
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
	cfg.Metadata.Translation.OpenAICompatible.Model = "llama3.1"
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_OpenAICompatibleMissingBaseURL(t *testing.T) {
	cfg := translationCfg(true, "openai-compatible")
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = ""
	cfg.Metadata.Translation.OpenAICompatible.Model = "llama3.1"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url is required")
}

func TestValidateTranslationProviderExcludingCredentials_OpenAICompatibleMissingModel(t *testing.T) {
	cfg := translationCfg(true, "openai-compatible")
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestValidateTranslationProviderExcludingCredentials_OpenAICompatibleBadBackendType(t *testing.T) {
	cfg := translationCfg(true, "openai-compatible")
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
	cfg.Metadata.Translation.OpenAICompatible.Model = "llama3.1"
	cfg.Metadata.Translation.OpenAICompatible.BackendType = "bogus-backend"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend_type")
}

func TestValidateTranslationProviderExcludingCredentials_Anthropic(t *testing.T) {
	cfg := translationCfg(true, "anthropic")
	cfg.Metadata.Translation.Anthropic.BaseURL = "https://api.anthropic.com"
	cfg.Metadata.Translation.Anthropic.Model = "claude-sonnet-4-20250514"
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_AnthropicMissingBaseURL(t *testing.T) {
	cfg := translationCfg(true, "anthropic")
	cfg.Metadata.Translation.Anthropic.BaseURL = ""
	cfg.Metadata.Translation.Anthropic.Model = "claude-sonnet-4-20250514"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url is required")
}

func TestValidateTranslationProviderExcludingCredentials_AnthropicMissingModel(t *testing.T) {
	cfg := translationCfg(true, "anthropic")
	cfg.Metadata.Translation.Anthropic.BaseURL = "https://api.anthropic.com"
	cfg.Metadata.Translation.Anthropic.Model = ""
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestValidateTranslationProviderExcludingCredentials_InvalidProvider(t *testing.T) {
	cfg := translationCfg(true, "bogus")
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider must be one of")
}

func TestValidateTranslationProviderExcludingCredentials_TimeoutOutOfRange(t *testing.T) {
	cfg := translationCfg(true, "openai")
	cfg.Metadata.Translation.TimeoutSeconds = 1
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds")

	cfg.Metadata.Translation.TimeoutSeconds = 9999
	err = validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds")
}

func TestValidateTranslationProviderExcludingCredentials_NoCredentialCheck(t *testing.T) {
	// All providers configured structurally-valid but with NO API keys — must pass.
	cases := []struct {
		name     string
		provider string
		setup    func(*TranslationConfig)
	}{
		{"openai no key", "openai", func(t *TranslationConfig) {}},
		{"deepl no key", "deepl", func(t *TranslationConfig) {}},
		{"google paid no key", "google", func(tc *TranslationConfig) { tc.Google.Mode = "paid" }},
		{"openai-compatible no key", "openai-compatible", func(tc *TranslationConfig) {
			tc.OpenAICompatible.BaseURL = "http://localhost:11434/v1"
			tc.OpenAICompatible.Model = "llama3.1"
		}},
		{"anthropic no key", "anthropic", func(tc *TranslationConfig) {
			tc.Anthropic.BaseURL = "https://api.anthropic.com"
			tc.Anthropic.Model = "claude-sonnet-4-20250514"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := translationCfg(true, tc.provider)
			tc.setup(&cfg.Metadata.Translation)
			assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
		})
	}
}

func TestValidateConfigExcludingTranslationCredentials_DatabaseType(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = "postgres"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.type")
}

func TestValidateConfigExcludingTranslationCredentials_DatabaseDSN(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.DSN = ""
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.dsn")
}

func TestValidateConfigExcludingTranslationCredentials_ScraperTimeout(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.TimeoutSeconds = 0
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds")
}

func TestValidateConfigExcludingTranslationCredentials_RequestTimeout(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.RequestTimeoutSeconds = 9999
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_timeout_seconds")
}

func TestValidateConfigExcludingTranslationCredentials_FlareSolverr(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.FlareSolverr.Enabled = true
	cfg.Scrapers.FlareSolverr.URL = ""
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flaresolverr")
}

func TestValidateConfigExcludingTranslationCredentials_Browser(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Browser.Enabled = true
	cfg.Scrapers.Browser.Headless = false
	cfg.Scrapers.Browser.UserAgent = ""
	err := validateConfigExcludingTranslationCredentials(cfg)
	if err != nil {
		assert.Contains(t, err.Error(), "browser")
	}
}

func TestValidateConfigExcludingTranslationCredentials_Referer(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Referer = "not a url"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "referer")
}

func TestValidateConfigExcludingTranslationCredentials_Performance(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Performance.MaxWorkers = 0
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_workers")

	cfg.Performance.MaxWorkers = 5
	cfg.Performance.WorkerTimeout = 1
	err = validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker_timeout")

	cfg.Performance.WorkerTimeout = 30
	cfg.Performance.UpdateInterval = 1
	err = validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update_interval")
}

func TestValidateConfigExcludingTranslationCredentials_Logging(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.MaxSizeMB = -1
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_size_mb")

	cfg.Logging.MaxSizeMB = 1
	cfg.Logging.MaxBackups = -1
	err = validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_backups")

	cfg.Logging.MaxBackups = 1
	cfg.Logging.MaxAgeDays = -1
	err = validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_age_days")
}

func TestValidateConfigExcludingTranslationCredentials_WebUIView(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.WebUI.DefaultReviewView = "bogus"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_review_view")
}

func TestValidateConfigExcludingTranslationCredentials_OperationMode(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Output.Operation.OperationMode = "bogus-mode"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "operation_mode") || strings.Contains(err.Error(), "invalid"))
}

func TestValidateConfigExcludingTranslationCredentials_Valid(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	assert.NoError(t, validateConfigExcludingTranslationCredentials(cfg))
}

func TestValidateConfigExcludingTranslationCredentials_VersionCheckInterval(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.System.VersionCheckIntervalHours = 9999
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version_check_interval_hours")
}

func TestValidateConfigExcludingTranslationCredentials_UILanguage(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.UI.Language = "not-a-lang!!!"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ui.language")
}

func TestValidateTranslationProviderExcludingCredentials_EmptyProviderDefaultsOpenAI(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = ""
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_ZeroTimeoutNormalized(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.TimeoutSeconds = 0
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_EmptyOpenAIBaseURLDefaulted(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai"
	cfg.Metadata.Translation.OpenAI.BaseURL = ""
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_EmptyDeepLModeDefaulted(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "deepl"
	cfg.Metadata.Translation.DeepL.Mode = ""
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_EmptyGoogleModeDefaulted(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	cfg.Metadata.Translation.Google.Mode = ""
	assert.NoError(t, validateTranslationProviderExcludingCredentials(cfg))
}

func TestValidateTranslationProviderExcludingCredentials_OpenAICompatibleBadURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai-compatible"
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = "not a url"
	cfg.Metadata.Translation.OpenAICompatible.Model = "llama3.1"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

func TestValidateTranslationProviderExcludingCredentials_AnthropicBadURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "anthropic"
	cfg.Metadata.Translation.Anthropic.BaseURL = "not a url"
	cfg.Metadata.Translation.Anthropic.Model = "claude-sonnet-4-20250514"
	err := validateTranslationProviderExcludingCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

func TestValidateTranslationProvider_NilConfig(t *testing.T) {
	assert.NoError(t, ValidateTranslationProvider(nil))
}

func TestValidateTranslationProviderInternal_AnthropicBadURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "anthropic"
	cfg.Metadata.Translation.Anthropic.BaseURL = "not a url"
	cfg.Metadata.Translation.Anthropic.Model = "claude-sonnet-4-20250514"
	cfg.Metadata.Translation.Anthropic.APIKey = "key"
	err := ValidateTranslationProvider(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

func TestValidateConfigExcludingTranslationCredentials_EmptyDBTypeDefaultsSQLite(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = ""
	assert.NoError(t, validateConfigExcludingTranslationCredentials(cfg))
}

func TestValidateConfigExcludingTranslationCredentials_EmptyRefererDefaulted(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Referer = ""
	assert.NoError(t, validateConfigExcludingTranslationCredentials(cfg))
}

func TestValidateConfigExcludingTranslationCredentials_BrowserInvalid(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Browser.Enabled = true
	cfg.Scrapers.Browser.Timeout = 0
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browser")
}

func TestValidateConfigExcludingTranslationCredentials_ScraperOverrideError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, RateLimit: -1},
	}
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
}

func TestValidateConfigExcludingTranslationCredentials_ProxyProfileError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "missing"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_profile")
}
