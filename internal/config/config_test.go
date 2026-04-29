package config

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/configutil"
	"github.com/stretchr/testify/assert"
)

func TestLoadNewConfigFormat(t *testing.T) {
	// Test with new config format
	newConfig := `
metadata:
  actress_database:
    enabled: true
    auto_add: false
  genre_replacement:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_new_config.yaml"
	if err := os.WriteFile(tmpFile, []byte(newConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	// Load config
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify actress_database loaded correctly
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should be true")
	}
	if cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should be false")
	}

	// Verify genre_replacement loaded correctly
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should be true")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should be true")
	}
}

func TestLoadOldConfigFormatFails(t *testing.T) {
	// Test with old config format (should not work anymore)
	oldConfig := `
metadata:
  thumb_csv:
    enabled: true
    auto_add: false
  genre_csv:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_old_config_fails.yaml"
	if err := os.WriteFile(tmpFile, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	// Load config - old field names should be ignored, defaults used
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Old config fields should be ignored, so we get defaults
	// Default has both enabled=true and auto_add=true
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should use default (true)")
	}
	if !cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should use default (true)")
	}
}

// TestLoadEmptyConfig tests loading empty config file (defaults should apply)
func TestLoadEmptyConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"completely empty", ""},
		{"empty yaml", "---"},
		{"only comments", "# This is a comment\n# Another comment"},
		{"empty sections", "scrapers:\nmetadata:\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := t.TempDir() + "/empty.yaml"
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cfg, err := Load(tmpFile)
			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			// Verify defaults were applied
			defaultCfg := DefaultConfig()
			if cfg.Scrapers.UserAgent != defaultCfg.Scrapers.UserAgent {
				t.Errorf("Expected default user agent %q, got %q", defaultCfg.Scrapers.UserAgent, cfg.Scrapers.UserAgent)
			}
			if cfg.Performance.MaxWorkers != defaultCfg.Performance.MaxWorkers {
				t.Errorf("Expected default max workers %d, got %d", defaultCfg.Performance.MaxWorkers, cfg.Performance.MaxWorkers)
			}
			if cfg.Server.Port != defaultCfg.Server.Port {
				t.Errorf("Expected default port %d, got %d", defaultCfg.Server.Port, cfg.Server.Port)
			}
		})
	}
}

// TestLoadPartialConfig tests loading config with only some fields set
func TestLoadPartialConfig(t *testing.T) {
	partialConfig := `
scrapers:
  priority: ["dmm"]
  dmm:
    enabled: true
`

	tmpFile := t.TempDir() + "/partial.yaml"
	if err := os.WriteFile(tmpFile, []byte(partialConfig), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify specified fields
	if len(cfg.Scrapers.Priority) != 1 || cfg.Scrapers.Priority[0] != "dmm" {
		t.Errorf("Expected priority ['dmm'], got %v", cfg.Scrapers.Priority)
	}
	if !cfg.Scrapers.Overrides["dmm"].Enabled {
		t.Error("Expected DMM enabled")
	}

	// Verify unspecified fields use defaults
	defaultCfg := DefaultConfig()
	if cfg.Scrapers.UserAgent != defaultCfg.Scrapers.UserAgent {
		t.Error("Expected default user agent for unspecified field")
	}
	if cfg.Performance.MaxWorkers != defaultCfg.Performance.MaxWorkers {
		t.Error("Expected default max workers for unspecified field")
	}
}

func TestResolveScraperUserAgent(t *testing.T) {
	tests := []struct {
		name       string
		userAgent  string
		expectedUA string
	}{
		{
			name:       "scraper user-agent takes precedence",
			userAgent:  "Custom-UA",
			expectedUA: "Custom-UA",
		},
		{
			name:       "default fake user-agent when scraper UA empty",
			userAgent:  "",
			expectedUA: configutil.DefaultScraperUserAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveScraperUserAgent(tt.userAgent)
			if got != tt.expectedUA {
				t.Errorf("expected user-agent %q, got %q", tt.expectedUA, got)
			}
		})
	}
}

func TestValidateTranslationConfig_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Metadata.Translation.Enabled = false
	err := cfg.validateTranslationConfig()
	assert.NoError(t, err)
}

func TestValidateTranslationConfig_OpenAI(t *testing.T) {
	t.Run("valid openai config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-test-key"
		cfg.Metadata.Translation.OpenAI.Model = "gpt-4o-mini"
		err := cfg.validateTranslationConfig()
		assert.NoError(t, err)
	})

	t.Run("missing openai api key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_key")
	})

	t.Run("invalid openai base url", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-test"
		cfg.Metadata.Translation.OpenAI.BaseURL = "not-a-url"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})
}

func TestValidateTranslationConfig_DeepL(t *testing.T) {
	t.Run("valid deepl config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "deepl"
		cfg.Metadata.Translation.DeepL.APIKey = "test-key"
		err := cfg.validateTranslationConfig()
		assert.NoError(t, err)
	})

	t.Run("invalid deepl mode", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "deepl"
		cfg.Metadata.Translation.DeepL.APIKey = "test-key"
		cfg.Metadata.Translation.DeepL.Mode = "invalid"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mode")
	})

	t.Run("missing deepl api key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "deepl"
		cfg.Metadata.Translation.DeepL.APIKey = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_key")
	})
}

func TestValidateTranslationConfig_Google(t *testing.T) {
	t.Run("free mode no api key needed", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "google"
		cfg.Metadata.Translation.Google.Mode = "free"
		err := cfg.validateTranslationConfig()
		assert.NoError(t, err)
	})

	t.Run("paid mode requires api key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "google"
		cfg.Metadata.Translation.Google.Mode = "paid"
		cfg.Metadata.Translation.Google.APIKey = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_key")
	})

	t.Run("invalid google mode", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "google"
		cfg.Metadata.Translation.Google.Mode = "invalid"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mode")
	})
}

func TestValidateTranslationConfig_OpenAICompatible(t *testing.T) {
	t.Run("missing base url", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai-compatible"
		cfg.Metadata.Translation.OpenAICompatible.BaseURL = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})

	t.Run("missing model", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai-compatible"
		cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434"
		cfg.Metadata.Translation.OpenAICompatible.Model = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model")
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai-compatible"
		cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434"
		cfg.Metadata.Translation.OpenAICompatible.Model = "llama3"
		err := cfg.validateTranslationConfig()
		assert.NoError(t, err)
	})

	t.Run("invalid base url", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai-compatible"
		cfg.Metadata.Translation.OpenAICompatible.BaseURL = "not-a-url"
		cfg.Metadata.Translation.OpenAICompatible.Model = "llama3"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})

	t.Run("invalid backend type", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai-compatible"
		cfg.Metadata.Translation.OpenAICompatible.BaseURL = "http://localhost:11434"
		cfg.Metadata.Translation.OpenAICompatible.Model = "llama3"
		cfg.Metadata.Translation.OpenAICompatible.BackendType = "invalid-backend"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backend_type")
	})
}

func TestValidateTranslationConfig_Anthropic(t *testing.T) {
	t.Run("valid anthropic config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "anthropic"
		cfg.Metadata.Translation.Anthropic.APIKey = "sk-ant-test"
		cfg.Metadata.Translation.Anthropic.BaseURL = "https://api.anthropic.com"
		cfg.Metadata.Translation.Anthropic.Model = "claude-3-haiku"
		err := cfg.validateTranslationConfig()
		assert.NoError(t, err)
	})

	t.Run("missing base url", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "anthropic"
		cfg.Metadata.Translation.Anthropic.APIKey = "sk-ant-test"
		cfg.Metadata.Translation.Anthropic.Model = "claude-3-haiku"
		cfg.Metadata.Translation.Anthropic.BaseURL = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})

	t.Run("missing model", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "anthropic"
		cfg.Metadata.Translation.Anthropic.APIKey = "sk-ant-test"
		cfg.Metadata.Translation.Anthropic.BaseURL = "https://api.anthropic.com"
		cfg.Metadata.Translation.Anthropic.Model = ""
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model")
	})

	t.Run("missing api key", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "anthropic"
		cfg.Metadata.Translation.Anthropic.APIKey = ""
		cfg.Metadata.Translation.Anthropic.BaseURL = "https://api.anthropic.com"
		cfg.Metadata.Translation.Anthropic.Model = "claude-3"
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_key")
	})
}

func TestValidateTranslationConfig_InvalidProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "nonexistent"
	err := cfg.validateTranslationConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider")
}

func TestValidateTranslationConfig_Timeout(t *testing.T) {
	t.Run("timeout too low", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-test"
		cfg.Metadata.Translation.TimeoutSeconds = 1
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("timeout too high", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = "sk-test"
		cfg.Metadata.Translation.TimeoutSeconds = 500
		err := cfg.validateTranslationConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})
}
