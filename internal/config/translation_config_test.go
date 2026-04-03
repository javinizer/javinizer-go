package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranslationConfig_SettingsHash(t *testing.T) {
	t.Run("deterministic hash for same config", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
			OpenAI: OpenAITranslationConfig{
				Model: "gpt-4",
			},
		}

		cfg2 := TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
			OpenAI: OpenAITranslationConfig{
				Model: "gpt-4",
			},
		}

		hash1 := cfg1.SettingsHash()
		hash2 := cfg2.SettingsHash()

		assert.Equal(t, hash1, hash2, "same config should produce same hash")
		assert.Len(t, hash1, 16, "hash should be 16 characters")
	})

	t.Run("different hash for different provider", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-4"},
		}

		cfg2 := TranslationConfig{
			Provider:       "deepl",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			DeepL:          DeepLTranslationConfig{Mode: "pro"},
		}

		hash1 := cfg1.SettingsHash()
		hash2 := cfg2.SettingsHash()

		assert.NotEqual(t, hash1, hash2, "different provider should produce different hash")
	})

	t.Run("different hash for different model", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-3.5-turbo"},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-4"},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different model should produce different hash")
	})

	t.Run("different hash for different target language", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "zh",
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different target language should produce different hash")
	})

	t.Run("same hash for different api_key", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{APIKey: "key1"},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{APIKey: "key2"},
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "api_key change should not affect hash")
	})

	t.Run("same hash for different timeout", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			TimeoutSeconds: 30,
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			TimeoutSeconds: 60,
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "timeout change should not affect hash")
	})

	t.Run("different hash for different fields", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title: true,
			},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different fields should produce different hash")
	})

	t.Run("different hash for apply_to_primary change", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			ApplyToPrimary: false,
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			ApplyToPrimary: true,
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "apply_to_primary change should produce different hash")
	})

	t.Run("different hash for different google mode", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			Google:         GoogleTranslationConfig{Mode: "free"},
		}

		cfg2 := TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			Google:         GoogleTranslationConfig{Mode: "paid"},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different google mode should produce different hash")
	})

	t.Run("same hash for language case variations", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "JA",
			TargetLanguage: "EN",
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "language case should not affect hash")
	})
}
