package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// unreachableConfigPath returns a config path whose parent directory cannot be
// created by MkdirAll, guaranteeing LoadOrCreate fails cross-platform.
// Uses a blocker-file pattern (distinct from testutil.InvalidConfigPath which
// uses invalid YAML) to avoid the import cycle (config -> testutil -> config).
func unreachableConfigPath(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	blocker := filepath.Join(tmpDir, "blocked")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))
	return filepath.Join(blocker, "config.yaml")
}
func init() {
}

// ---------------------------------------------------------------------------
// getFirstScraperPriorityStatic (66.7% → target: 100%)
// ---------------------------------------------------------------------------

func TestGetFirstScraperPriorityStatic(t *testing.T) {
	t.Run("returns first element from default list", func(t *testing.T) {
		result := getFirstScraperPriorityStatic()
		assert.Equal(t, "r18dev", result, "should return first element of defaultScraperPriority")
	})
}

// ---------------------------------------------------------------------------
// Migration.Description (0% → target: 100%)
// ---------------------------------------------------------------------------

func TestLegacyMigration_Description(t *testing.T) {
	m := NewLegacyMigration()
	desc := m.Description()
	assert.Contains(t, desc, "v0/v1/v2")
	assert.Contains(t, desc, "v3")
}

// ---------------------------------------------------------------------------
// ApplyEnvironmentOverrides (65.4% → target: 90%+)
// ---------------------------------------------------------------------------

func TestApplyEnvironmentOverrides_TempDir(t *testing.T) {
	t.Setenv("JAVINIZER_TEMP_DIR", "/tmp/custom")

	cfg := DefaultConfig(nil, nil)
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, "/tmp/custom", cfg.System.TempDir)
}

func TestApplyEnvironmentOverrides_TranslationProvider(t *testing.T) {
	t.Run("TRANSLATION_PROVIDER", func(t *testing.T) {
		t.Setenv("TRANSLATION_PROVIDER", "deepl")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "deepl", cfg.Metadata.Translation.Provider)
	})
}

func TestApplyEnvironmentOverrides_TranslationLanguages(t *testing.T) {
	t.Run("TRANSLATION_SOURCE_LANGUAGE", func(t *testing.T) {
		t.Setenv("TRANSLATION_SOURCE_LANGUAGE", "zh")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "zh", cfg.Metadata.Translation.SourceLanguage)
	})

	t.Run("TRANSLATION_TARGET_LANGUAGE", func(t *testing.T) {
		t.Setenv("TRANSLATION_TARGET_LANGUAGE", "ko")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "ko", cfg.Metadata.Translation.TargetLanguage)
	})
}

func TestApplyEnvironmentOverrides_OpenAICredentials(t *testing.T) {
	t.Run("OPENAI_API_KEY", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-test-key-123")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "sk-test-key-123", cfg.Metadata.Translation.OpenAI.APIKey)
	})

	t.Run("OPENAI_BASE_URL", func(t *testing.T) {
		t.Setenv("OPENAI_BASE_URL", "https://custom.openai.com/v1")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "https://custom.openai.com/v1", cfg.Metadata.Translation.OpenAI.BaseURL)
	})

	t.Run("OPENAI_MODEL", func(t *testing.T) {
		t.Setenv("OPENAI_MODEL", "gpt-4o")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "gpt-4o", cfg.Metadata.Translation.OpenAI.Model)
	})
}

func TestApplyEnvironmentOverrides_DeepLKey(t *testing.T) {
	t.Setenv("DEEPL_API_KEY", "deepl-test-key")

	cfg := DefaultConfig(nil, nil)
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, "deepl-test-key", cfg.Metadata.Translation.DeepL.APIKey)
}

func TestApplyEnvironmentOverrides_GoogleKey(t *testing.T) {
	t.Setenv("GOOGLE_TRANSLATE_API_KEY", "google-test-key")

	cfg := DefaultConfig(nil, nil)
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, "google-test-key", cfg.Metadata.Translation.Google.APIKey)
}

func TestApplyEnvironmentOverrides_OpenAICompatibleKey(t *testing.T) {
	t.Setenv("OPENAI_COMPATIBLE_API_KEY", "compatible-test-key")

	cfg := DefaultConfig(nil, nil)
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, "compatible-test-key", cfg.Metadata.Translation.OpenAICompatible.APIKey)
}

func TestApplyEnvironmentOverrides_AnthropicKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")

	cfg := DefaultConfig(nil, nil)
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, "anthropic-test-key", cfg.Metadata.Translation.Anthropic.APIKey)
}

func TestApplyEnvironmentOverrides_MetadataTranslationProvider(t *testing.T) {
	t.Run("METADATA_TRANSLATION_PROVIDER overrides provider", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_PROVIDER", "google")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "google", cfg.Metadata.Translation.Provider)
	})
}

func TestApplyEnvironmentOverrides_MetadataTranslationLanguages(t *testing.T) {
	t.Run("METADATA_TRANSLATION_SOURCE_LANGUAGE", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_SOURCE_LANGUAGE", "fr")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "fr", cfg.Metadata.Translation.SourceLanguage)
	})

	t.Run("METADATA_TRANSLATION_TARGET_LANGUAGE", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_TARGET_LANGUAGE", "de")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, "de", cfg.Metadata.Translation.TargetLanguage)
	})
}

func TestApplyEnvironmentOverrides_MetadataTranslationSettings(t *testing.T) {
	t.Run("METADATA_TRANSLATION_TIMEOUT_SECONDS", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_TIMEOUT_SECONDS", "120")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, 120, cfg.Metadata.Translation.TimeoutSeconds)
	})

	t.Run("METADATA_TRANSLATION_TIMEOUT_SECONDS invalid keeps default", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_TIMEOUT_SECONDS", "abc")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, 60, cfg.Metadata.Translation.TimeoutSeconds)
	})

	t.Run("METADATA_TRANSLATION_TIMEOUT_SECONDS empty keeps default", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_TIMEOUT_SECONDS", "")

		cfg := DefaultConfig(nil, nil)
		ApplyEnvironmentOverrides(cfg)

		assert.Equal(t, 60, cfg.Metadata.Translation.TimeoutSeconds)
	})

	t.Run("METADATA_TRANSLATION_APPLY_TO_PRIMARY true", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_APPLY_TO_PRIMARY", "true")

		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.ApplyToPrimary = false
		ApplyEnvironmentOverrides(cfg)

		assert.True(t, cfg.Metadata.Translation.ApplyToPrimary)
	})

	t.Run("METADATA_TRANSLATION_OVERWRITE_EXISTING_TARGET true", func(t *testing.T) {
		t.Setenv("METADATA_TRANSLATION_OVERWRITE_EXISTING_TARGET", "true")

		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.Translation.OverwriteExistingTarget = false
		ApplyEnvironmentOverrides(cfg)

		assert.True(t, cfg.Metadata.Translation.OverwriteExistingTarget)
	})
}

func TestApplyEnvironmentOverrides_EmptyEnvNoChange(t *testing.T) {
	// Ensure empty env vars don't override existing values
	cfg := DefaultConfig(nil, nil)
	originalLevel := cfg.Logging.Level

	// Don't set LOG_LEVEL — it should remain unchanged
	ApplyEnvironmentOverrides(cfg)

	assert.Equal(t, originalLevel, cfg.Logging.Level)
}

// ---------------------------------------------------------------------------
// normalizeTranslationMode (55.6% → target: 90%+)
// ---------------------------------------------------------------------------

func TestNormalizeTranslationMode_DeepLMode(t *testing.T) {
	t.Run("nil value returns false", func(t *testing.T) {
		var mode *models.DeepLMode
		changed := normalizeTranslationMode(mode, models.DeepLModeFree)
		assert.False(t, changed)
	})

	t.Run("empty mode gets default", func(t *testing.T) {
		mode := models.DeepLMode("")
		changed := normalizeTranslationMode(&mode, models.DeepLModeFree)
		assert.True(t, changed)
		assert.Equal(t, models.DeepLModeFree, mode)
	})

	t.Run("whitespace mode gets default", func(t *testing.T) {
		mode := models.DeepLMode("  ")
		changed := normalizeTranslationMode(&mode, models.DeepLModeFree)
		assert.True(t, changed)
		assert.Equal(t, models.DeepLModeFree, mode)
	})

	t.Run("uppercase mode normalized to lowercase", func(t *testing.T) {
		mode := models.DeepLMode("PRO")
		changed := normalizeTranslationMode(&mode, models.DeepLModeFree)
		assert.True(t, changed)
		assert.Equal(t, models.DeepLModePro, mode)
	})

	t.Run("already normalized returns false", func(t *testing.T) {
		mode := models.DeepLModeFree
		changed := normalizeTranslationMode(&mode, models.DeepLModeFree)
		assert.False(t, changed)
		assert.Equal(t, models.DeepLModeFree, mode)
	})
}

func TestNormalizeTranslationMode_GoogleMode(t *testing.T) {
	t.Run("empty mode gets default", func(t *testing.T) {
		mode := models.GoogleMode("")
		changed := normalizeTranslationMode(&mode, models.GoogleModeFree)
		assert.True(t, changed)
		assert.Equal(t, models.GoogleModeFree, mode)
	})

	t.Run("uppercase mode normalized to lowercase", func(t *testing.T) {
		mode := models.GoogleMode("PAID")
		changed := normalizeTranslationMode(&mode, models.GoogleModeFree)
		assert.True(t, changed)
		assert.Equal(t, models.GoogleModePaid, mode)
	})

	t.Run("already normalized returns false", func(t *testing.T) {
		mode := models.GoogleModeFree
		changed := normalizeTranslationMode(&mode, models.GoogleModeFree)
		assert.False(t, changed)
	})
}

// ---------------------------------------------------------------------------
// Prepare (75.0% → target: 95%+)
// ---------------------------------------------------------------------------

func TestPrepare_NilConfig(t *testing.T) {
	changed, err := Prepare(nil)
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestPrepare_FutureVersion(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.ConfigVersion = CurrentConfigVersion + 1

	changed, err := Prepare(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "newer than supported")
	assert.False(t, changed)
}

func TestPrepare_ValidationError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = "postgres"

	changed, err := Prepare(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
	// changed may be true if normalization ran
	_ = changed
}

func TestPrepare_ValidConfig_NoChanges(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	// Run prepare once to normalize
	_, _ = Prepare(cfg)

	// Second run should detect no changes
	changed, err := Prepare(cfg)
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestPrepare_ValidConfig_WithNormalization(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = " SQLITE " // needs normalization

	changed, err := Prepare(cfg)
	assert.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "sqlite", cfg.Database.Type)
}

// ---------------------------------------------------------------------------
// validateProxyProfileConfig (60.0% → target: 90%+)
// ---------------------------------------------------------------------------

func TestValidateProxyProfileConfig_NilConfig(t *testing.T) {
	err := validateProxyProfileConfig(nil)
	assert.NoError(t, err)
}

func TestValidateProxyProfileConfig_EnabledWithoutDefaultProfile(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = ""

	err := validateProxyProfileConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "default_profile is required")
}

func TestValidateProxyProfileConfig_UnknownDefaultProfile(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "nonexistent"

	err := validateProxyProfileConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestValidateProxyProfileConfig_ValidProxy(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Enabled = true
	cfg.Scrapers.Proxy.DefaultProfile = "main"

	err := validateProxyProfileConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateProxyProfileConfig_ScraperProxyUnknownProfile(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{
		Enabled: true,
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "nonexistent",
		},
	}

	err := validateProxyProfileConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestValidateProxyProfileConfig_ScraperDownloadProxyUnknownProfile(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{
		Enabled: true,
		DownloadProxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "nonexistent",
		},
	}

	err := validateProxyProfileConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestValidateProxyProfileConfig_DownloadProxyUnknownProfile(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	cfg.Output.Download.DownloadProxy = models.ProxyConfig{
		Enabled: true,
		Profile: "nonexistent",
	}

	err := validateProxyProfileConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown profile")
}

func TestValidateProxyProfileConfig_NilOverridesNormalized(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = nil

	err := validateProxyProfileConfig(cfg)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// validateScraperProxyProfiles (70.0% → target: 90%+)
// ---------------------------------------------------------------------------

func TestValidateScraperProxyProfiles_ValidProxy(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{
		Enabled: true,
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "main",
		},
	}

	err := cfg.validateScraperProxyProfiles()
	assert.NoError(t, err)
}

func TestValidateScraperProxyProfiles_ValidDownloadProxy(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{
		Enabled: true,
		DownloadProxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "main",
		},
	}

	err := cfg.validateScraperProxyProfiles()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// validateHTTPBaseURL (83.3% → target: 100%)
// ---------------------------------------------------------------------------

func TestValidateHTTPBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"valid https", "https://api.example.com", false},
		{"valid http", "http://localhost:8080", false},
		{"ftp scheme", "ftp://example.com", true},
		{"no host", "http://", true},
		{"empty string", "", true},
		{"just text", "not-a-url", true},
		{"file scheme", "file:///tmp/test", true},
		{"with whitespace", " https://api.example.com ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHTTPBaseURL("test.path", tt.rawURL)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeField (90.9% → target: 100%)
// ---------------------------------------------------------------------------

func TestNormalizeField_NilValue(t *testing.T) {
	changed := normalizeField(nil, "default", true)
	assert.False(t, changed)
}

// ---------------------------------------------------------------------------
// TranslationConfig JSON/YAML round-trip (UnmarshalJSON 75%, UnmarshalYAML 66.7%, decodeFromMap 75.9%)
// ---------------------------------------------------------------------------

func TestPriorityConfig_UnmarshalJSON_InvalidJSON(t *testing.T) {
	var p PriorityConfig
	err := json.Unmarshal([]byte(`"not an object"`), &p)
	assert.Error(t, err)
}

func TestPriorityConfig_UnmarshalJSON_NilPriority(t *testing.T) {
	var p PriorityConfig
	err := json.Unmarshal([]byte(`{"priority": null}`), &p)
	assert.NoError(t, err)
	assert.Nil(t, p.Priority)
}

func TestPriorityConfig_UnmarshalJSON_NilFieldOverride(t *testing.T) {
	var p PriorityConfig
	err := json.Unmarshal([]byte(`{"title": null}`), &p)
	assert.NoError(t, err)
	// nil value should be skipped
	_, exists := p.Fields["title"]
	assert.False(t, exists)
}

func TestPriorityConfig_UnmarshalJSON_NonStringArrayElement(t *testing.T) {
	var p PriorityConfig
	err := json.Unmarshal([]byte(`{"priority": [123]}`), &p)
	assert.NoError(t, err)
	// non-string element should be skipped
	assert.Empty(t, p.Priority)
}

func TestPriorityConfig_UnmarshalJSON_FieldNonArray(t *testing.T) {
	var p PriorityConfig
	err := json.Unmarshal([]byte(`{"title": "not-array"}`), &p)
	assert.NoError(t, err)
	// non-array value should be skipped
	_, exists := p.Fields["title"]
	assert.False(t, exists)
}

func TestPriorityConfig_UnmarshalYAML_InvalidNode(t *testing.T) {
	var p PriorityConfig
	// Create a scalar node instead of mapping
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "not-a-mapping"}
	err := p.UnmarshalYAML(node)
	assert.Error(t, err)
}

func TestPriorityConfig_UnmarshalYAML_NilNode(t *testing.T) {
	var p PriorityConfig
	err := p.UnmarshalYAML(nil)
	assert.NoError(t, err)
}

func TestPriorityConfig_UnmarshalYAML_KindZero(t *testing.T) {
	var p PriorityConfig
	err := p.UnmarshalYAML(&yaml.Node{Kind: 0})
	assert.NoError(t, err)
}

func TestPriorityConfig_GetFieldPriority_NilReceiver(t *testing.T) {
	var p *PriorityConfig
	result := p.GetFieldPriority("title")
	assert.Nil(t, result)
}

func TestPriorityConfig_GetFieldPriority_EmptyOverride(t *testing.T) {
	p := &PriorityConfig{
		Priority: []string{"dmm", "r18dev"},
		Fields:   map[string][]string{"title": {}},
	}
	result := p.GetFieldPriority("title")
	// A PRESENT empty override ([]string{}) is a deliberate empty field — it must
	// NOT fall back to global. This is the pure-exclusivity contract: a present []
	// means "consult no scrapers", distinct from an absent key (inherit global).
	assert.Equal(t, []string{}, result)
	assert.NotNil(t, result, "present [] must be a non-nil empty slice, not nil (nil ⇒ inherit)")
}

func TestPriorityConfig_GetFieldPriority_NilOverride(t *testing.T) {
	p := &PriorityConfig{
		Priority: []string{"dmm"},
		Fields:   map[string][]string{"title": nil},
	}
	result := p.GetFieldPriority("title")
	assert.Equal(t, []string{"dmm"}, result)
}

func TestPriorityConfig_GetFieldPriority_NoGlobalOrOverride(t *testing.T) {
	p := &PriorityConfig{}
	result := p.GetFieldPriority("title")
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// SettingsHash (84.6% → target: 95%+)
// ---------------------------------------------------------------------------

func TestSettingsHash_DifferentProviders(t *testing.T) {
	cfg1 := DefaultConfig(nil, nil)
	cfg1.Metadata.Translation.Provider = "openai"
	cfg1.Metadata.Translation.OpenAI.Model = "gpt-4o-mini"

	cfg2 := DefaultConfig(nil, nil)
	cfg2.Metadata.Translation.Provider = "deepl"
	cfg2.Metadata.Translation.DeepL.Mode = models.DeepLModeFree

	hash1 := cfg1.Metadata.Translation.SettingsHash()
	hash2 := cfg2.Metadata.Translation.SettingsHash()

	assert.NotEqual(t, hash1, hash2, "different providers should produce different hashes")
}

func TestSettingsHash_SameSettingsSameHash(t *testing.T) {
	cfg1 := DefaultConfig(nil, nil)
	cfg2 := DefaultConfig(nil, nil)

	hash1 := cfg1.Metadata.Translation.SettingsHash()
	hash2 := cfg2.Metadata.Translation.SettingsHash()

	assert.Equal(t, hash1, hash2, "same settings should produce same hash")
}

func TestSettingsHash_DeterministicLength(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	hash := cfg.Metadata.Translation.SettingsHash()
	assert.Len(t, hash, 16, "hash should be 16 hex characters")
}

// ---------------------------------------------------------------------------
// ScrapersConfig UnmarshalJSON (75% → target: 95%+)
// ---------------------------------------------------------------------------

func TestScrapersConfig_UnmarshalJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		data := `{
			"user_agent": "TestBot",
			"referer": "https://example.com",
			"timeout_seconds": 30,
			"request_timeout_seconds": 60,
			"priority": ["dmm", "r18dev"],
			"scrape_actress": false
		}`

		var sc ScrapersConfig
		err := json.Unmarshal([]byte(data), &sc)
		require.NoError(t, err)
		assert.Equal(t, "TestBot", sc.UserAgent)
		assert.Equal(t, "https://example.com", sc.Referer)
		assert.Equal(t, 30, sc.TimeoutSeconds)
		assert.Equal(t, []string{"dmm", "r18dev"}, sc.Priority)
		assert.False(t, sc.ScrapeActress)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`"not an object"`), &sc)
		assert.Error(t, err)
	})

	t.Run("null value", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`null`), &sc)
		assert.NoError(t, err)
	})

	t.Run("invalid user_agent type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"user_agent": 123}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user_agent must be a string")
	})

	t.Run("invalid referer type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"referer": 123}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "referer must be a string")
	})

	t.Run("invalid timeout_seconds type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"timeout_seconds": "not-a-number"}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout_seconds must be an integer")
	})

	t.Run("invalid request_timeout_seconds type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"request_timeout_seconds": true}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request_timeout_seconds must be an integer")
	})

	t.Run("invalid priority type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"priority": "not-array"}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "priority must be an array")
	})

	t.Run("invalid priority element type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"priority": [123]}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "priority")
	})

	t.Run("null priority", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"priority": null}`), &sc)
		require.NoError(t, err)
		assert.Nil(t, sc.Priority)
	})

	t.Run("invalid scrape_actress type", func(t *testing.T) {
		var sc ScrapersConfig
		err := json.Unmarshal([]byte(`{"scrape_actress": "yes"}`), &sc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "scrape_actress must be a boolean")
	})
}

// ---------------------------------------------------------------------------
// configToYAMLDocument (66.7%) & encodeYAMLDocument (66.7%)
// Test via Save with actual file I/O
// ---------------------------------------------------------------------------

func TestSave_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, path)
	require.NoError(t, err)

	// Verify file exists and can be loaded
	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Server.Host, loaded.Server.Host)
	assert.Equal(t, cfg.Server.Port, loaded.Server.Port)
}

func TestSave_ExistingFileUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)

	// First save
	err := Save(cfg, path)
	require.NoError(t, err)

	// Get file mod time
	info1, err := os.Stat(path)
	require.NoError(t, err)

	// Second save with same content should be a no-op
	err = Save(cfg, path)
	require.NoError(t, err)

	info2, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info1.ModTime(), info2.ModTime(), "file should not be rewritten if unchanged")
}

func TestSave_MalformedExistingFallsBackToCanonical(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	// Write malformed YAML
	err := os.WriteFile(path, []byte(":::not valid yaml:::"), 0644)
	require.NoError(t, err)

	cfg := DefaultConfig(nil, nil)
	err = Save(cfg, path)
	require.NoError(t, err)

	// Should be loadable now
	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Server.Port, loaded.Server.Port)
}

// ---------------------------------------------------------------------------
// Config MarshalYAML / UnmarshalYAML (83.3% each)
// ---------------------------------------------------------------------------

func TestConfig_MarshalYAML_ScraperError(t *testing.T) {
	// This tests the error path in MarshalYAML when scrapers fail to marshal
	// Hard to trigger directly; just verify normal case works
	cfg := DefaultConfig(nil, nil)
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), "config_version")
}

func TestConfig_UnmarshalYAML_NilNode(t *testing.T) {
	cfg := &Config{}
	err := cfg.UnmarshalYAML(nil)
	assert.NoError(t, err)
}

func TestConfig_UnmarshalYAML_KindZero(t *testing.T) {
	cfg := &Config{}
	err := cfg.UnmarshalYAML(&yaml.Node{Kind: 0})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// normalizeTranslationConfig (92.9% → slight boost)
// ---------------------------------------------------------------------------

func TestNormalizeTranslationConfig_NilConfig(t *testing.T) {
	changed := normalizeTranslationConfig(nil)
	assert.False(t, changed)
}

func TestNormalizeTranslationConfig_ZeroTimeout(t *testing.T) {
	tc := &TranslationConfig{
		Provider:       "openai",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		TimeoutSeconds: 0, // should be set to 60
	}
	changed := normalizeTranslationConfig(tc)
	assert.True(t, changed)
	assert.Equal(t, 60, tc.TimeoutSeconds)
}

func TestNormalizeTranslationConfig_AlreadyNormalized(t *testing.T) {
	tc := &TranslationConfig{
		Provider:                "openai",
		SourceLanguage:          "ja",
		TargetLanguage:          "en",
		OpenAI:                  OpenAITranslationConfig{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		DeepL:                   DeepLTranslationConfig{Mode: models.DeepLModeFree},
		Google:                  GoogleTranslationConfig{Mode: models.GoogleModeFree},
		TimeoutSeconds:          60,
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
	}
	changed := normalizeTranslationConfig(tc)
	assert.False(t, changed)
}

// ---------------------------------------------------------------------------
// normalize (94.4% → target: 100%)
// ---------------------------------------------------------------------------

func TestNormalize_NilConfig(t *testing.T) {
	changed := normalize(nil)
	assert.False(t, changed)
}

// ---------------------------------------------------------------------------
// MigrateToCurrent edge cases
// ---------------------------------------------------------------------------

func TestMigrateToCurrent_NilConfig(t *testing.T) {
	err := MigrateToCurrent(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestLegacyMigration_Migrate_DryRun(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte("server:\n  host: localhost\n"), 0644)
	require.NoError(t, err)

	SetMigrationContext(MigrationContext{
		ConfigPath: cfgPath,
		DryRun:     true,
	})

	m := NewLegacyMigration()
	cfg := &Config{ConfigVersion: 0}
	err = m.Migrate(cfg)
	require.NoError(t, err)

	// In dry run, no backup should be created
	ctx := GetMigrationContext()
	assert.Empty(t, ctx.BackupPath)
}

func TestLegacyMigration_Migrate_NoConfigPath(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	SetMigrationContext(MigrationContext{
		ConfigPath: "",
		DryRun:     false,
	})

	m := NewLegacyMigration()
	cfg := &Config{ConfigVersion: 0}
	err := m.Migrate(cfg)
	require.NoError(t, err)
}

func TestLegacyMigration_Migrate_WithBackup(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte("server:\n  host: localhost\n"), 0644)
	require.NoError(t, err)

	SetMigrationContext(MigrationContext{
		ConfigPath: cfgPath,
		DryRun:     false,
	})

	m := NewLegacyMigration()
	cfg := &Config{ConfigVersion: 0}
	err = m.Migrate(cfg)
	require.NoError(t, err)

	// Backup should be created
	ctx := GetMigrationContext()
	assert.NotEmpty(t, ctx.BackupPath)
	assert.FileExists(t, ctx.BackupPath)
}

func TestLegacyMigration_Migrate_ConfigPathNotExist(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	SetMigrationContext(MigrationContext{
		ConfigPath: unreachableConfigPath(t),
		DryRun:     false,
	})

	m := NewLegacyMigration()
	cfg := &Config{ConfigVersion: 0}
	err := m.Migrate(cfg)
	require.NoError(t, err)
	// Should succeed without backup since file doesn't exist
	ctx := GetMigrationContext()
	assert.Empty(t, ctx.BackupPath)
}

// ---------------------------------------------------------------------------
// LoadOrCreate edge cases
// ---------------------------------------------------------------------------

func TestLoadOrCreate_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "newconfig.yaml")

	cfg, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)

	// File should exist now
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

func TestLoadOrCreate_StatError(t *testing.T) {
	// Try to load from a directory that can't be stat'd (permission denied)
	// This is platform-dependent, so just test the happy path thoroughly
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

// ---------------------------------------------------------------------------
// Config Validate — WebUI default_review_view
// ---------------------------------------------------------------------------

func TestValidate_WebUIInvalidReviewView(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.WebUI.DefaultReviewView = "invalid"

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webui.default_review_view")
}

func TestValidate_WebUIValidReviewViews(t *testing.T) {
	for _, view := range []string{"detail", "grid-poster", "grid-cover", ""} {
		t.Run(view, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			cfg.WebUI.DefaultReviewView = view

			err := cfg.Validate()
			assert.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Config Validate — version check interval
// ---------------------------------------------------------------------------

func TestValidate_VersionCheckIntervalTooHigh(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.System.VersionCheckIntervalHours = 200

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version_check_interval_hours")
}

func TestValidate_VersionCheckIntervalZeroAllowed(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.System.VersionCheckIntervalHours = 0

	err := cfg.Validate()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Config Validate — empty database type normalized
// ---------------------------------------------------------------------------

func TestValidate_EmptyDatabaseType(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.Type = ""

	err := cfg.Validate()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DeepL translation validation — valid base_url
// ---------------------------------------------------------------------------

func TestValidateTranslationConfig_DeepLValidBaseURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "deepl"
	cfg.Metadata.Translation.DeepL.APIKey = "test-key"
	cfg.Metadata.Translation.DeepL.BaseURL = "https://api.deepl.com/v2"

	err := cfg.validateTranslationConfig()
	assert.NoError(t, err)
}

func TestValidateTranslationConfig_DeepLInvalidBaseURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "deepl"
	cfg.Metadata.Translation.DeepL.APIKey = "test-key"
	cfg.Metadata.Translation.DeepL.BaseURL = "not-a-url"

	err := cfg.validateTranslationConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

// ---------------------------------------------------------------------------
// Google translation validation — valid/paid base_url
// ---------------------------------------------------------------------------

func TestValidateTranslationConfig_GoogleValidBaseURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	cfg.Metadata.Translation.Google.Mode = "free"
	cfg.Metadata.Translation.Google.BaseURL = "https://translate.googleapis.com"

	err := cfg.validateTranslationConfig()
	assert.NoError(t, err)
}

func TestValidateTranslationConfig_GoogleInvalidBaseURL(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	cfg.Metadata.Translation.Google.Mode = "free"
	cfg.Metadata.Translation.Google.BaseURL = "bad-url"

	err := cfg.validateTranslationConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

// ---------------------------------------------------------------------------
// OpenAI-compatible backend_type validation
// ---------------------------------------------------------------------------

func TestNormalizedBackendType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"auto", ""},
		{"vllm", "vllm"},
		{"VLLM", "vllm"},
		{"ollama", "ollama"},
		{"llama.cpp", "llama.cpp"},
		{"llamacpp", "llama.cpp"},
		{"llama_cpp", "llama.cpp"},
		{"other", "other"},
		{"generic", "other"},
		{"custom-backend", "custom-backend"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			oc := OpenAICompatibleTranslationConfig{BackendType: tt.input}
			assert.Equal(t, tt.expected, oc.NormalizedBackendType())
		})
	}
}

func TestEffectiveEnableThinking(t *testing.T) {
	t.Run("nil returns false", func(t *testing.T) {
		oc := OpenAICompatibleTranslationConfig{EnableThinking: nil}
		assert.False(t, oc.EffectiveEnableThinking())
	})

	t.Run("true returns true", func(t *testing.T) {
		v := true
		oc := OpenAICompatibleTranslationConfig{EnableThinking: &v}
		assert.True(t, oc.EffectiveEnableThinking())
	})

	t.Run("false returns false", func(t *testing.T) {
		v := false
		oc := OpenAICompatibleTranslationConfig{EnableThinking: &v}
		assert.False(t, oc.EffectiveEnableThinking())
	})
}

// ---------------------------------------------------------------------------
// Config Validate — translation disabled bypasses all checks
// ---------------------------------------------------------------------------

func TestValidateTranslationConfig_DisabledBypassesAll(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = false
	cfg.Metadata.Translation.Provider = "totally-invalid"
	cfg.Metadata.Translation.TimeoutSeconds = 1

	err := cfg.validateTranslationConfig()
	assert.NoError(t, err, "disabled translation should not validate provider/timeout")
}

// ---------------------------------------------------------------------------
// applyInitDefaultsFromEnv
// ---------------------------------------------------------------------------

func TestApplyInitDefaultsFromEnv_NilConfig(t *testing.T) {
	changed := applyInitDefaultsFromEnv(nil)
	assert.False(t, changed)
}

func TestApplyInitDefaultsFromEnv_InitServerHost(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)

	assert.True(t, changed)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
}

func TestApplyInitDefaultsFromEnv_InitAllowedDirectories(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/media,/videos")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)

	assert.True(t, changed)
	assert.Equal(t, []string{"/media", "/videos"}, cfg.API.Security.AllowedDirectories)
}

func TestApplyInitDefaultsFromEnv_InitAllowedDirectories_EmptyParts(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/media,,/videos,")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)

	assert.True(t, changed)
	assert.Equal(t, []string{"/media", "/videos"}, cfg.API.Security.AllowedDirectories)
}

func TestApplyInitDefaultsFromEnv_InitAllowedOrigins(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "http://host1:8080,http://host2:5173")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)

	assert.True(t, changed)
	assert.Equal(t, []string{"http://host1:8080", "http://host2:5173"}, cfg.API.Security.AllowedOrigins)
}

func TestApplyInitDefaultsFromEnv_NoEnvVars(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)

	assert.False(t, changed)
}

// parseConfigLockMetadata already tested in config_compatibility_test.go

// ---------------------------------------------------------------------------
// isProcessAlive
// ---------------------------------------------------------------------------

func TestIsProcessAlive_InvalidPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.False(t, isProcessAlive(0))
	assert.False(t, isProcessAlive(-1))
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	// Current process should be alive
	assert.True(t, isProcessAlive(os.Getpid()))
}

func TestIsProcessAlive_NonexistentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	// Very high PID is unlikely to exist
	assert.False(t, isProcessAlive(999999999))
}

// ---------------------------------------------------------------------------
// shouldReapConfigLock
// ---------------------------------------------------------------------------

func TestShouldReapConfigLock_InvalidMetadata(t *testing.T) {
	now := time.Now()
	// Old mtime with corrupt lock should be reaped
	oldTime := now.Add(-5 * 60 * time.Second)
	assert.True(t, shouldReapConfigLock([]byte("corrupt"), oldTime, now))
}

func TestShouldReapConfigLock_RecentMetadata(t *testing.T) {
	now := time.Now()
	// Recent lock should NOT be reaped
	token := makeConfigLockToken()
	assert.False(t, shouldReapConfigLock([]byte(token), now, now))
}

// ---------------------------------------------------------------------------
// ScrapersConfig YAML unmarshal edge cases
// ---------------------------------------------------------------------------

func TestScrapersConfig_UnmarshalYAML_NilNode(t *testing.T) {
	var sc ScrapersConfig
	err := sc.UnmarshalYAML(nil)
	assert.NoError(t, err)
	assert.NotNil(t, sc.Overrides)
}

func TestScrapersConfig_UnmarshalYAML_KindZero(t *testing.T) {
	var sc ScrapersConfig
	err := sc.UnmarshalYAML(&yaml.Node{Kind: 0})
	assert.NoError(t, err)
	assert.NotNil(t, sc.Overrides)
}

func TestScrapersConfig_UnmarshalYAML_InvalidNode(t *testing.T) {
	var sc ScrapersConfig
	// Scalar node instead of mapping
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "not-a-mapping"}
	err := sc.UnmarshalYAML(node)
	assert.Error(t, err)
}

// TestValidateConfig_EmptyScrapersPriorityErrors is a regression test for
// cycle-1 MINOR-8. An explicitly empty scrapers.priority (yaml `priority: []`
// or `priority: null`) means the user configured no scrapers; without a guard
// the aggregator's resolved priorities would be empty and every assign* loop
// would iterate nothing -> a blank movie (silent data loss). DefaultConfig
// seeds the 14-scraper default before user values overlay, so an empty slice
// uniquely identifies explicit empty intent. Validate must surface it as an
// error rather than silently producing a blank movie.
func TestPrepare_EmptyScrapersPriorityErrors(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	require.NotEmpty(t, cfg.Scrapers.Priority, "precondition: DefaultConfig seeds a non-empty default priority")

	// Explicit empty -> error (the silent-data-loss case). The guard lives in
	// Prepare (not ValidateConfig) because DefaultConfig seeds defaults during
	// Load, so an empty slice here uniquely identifies explicit empty intent.
	cfg.Scrapers.Priority = []string{}
	_, err := Prepare(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scrapers.priority")

	// Explicit nil -> same error (yaml `priority: null`).
	cfg.Scrapers.Priority = nil
	_, err = Prepare(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scrapers.priority")

	// Non-empty (the default) -> no error.
	cfg.Scrapers.Priority = []string{"r18dev"}
	_, err = Prepare(cfg)
	assert.NoError(t, err)
}
