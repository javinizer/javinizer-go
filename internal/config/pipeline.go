package config

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/types"
)

func normalizeField(value *string, defaultValue string, toLower bool) bool {
	if value == nil {
		return false
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		normalized = defaultValue
	}
	if toLower {
		normalized = strings.ToLower(normalized)
	}
	if *value == normalized {
		return false
	}
	*value = normalized
	return true
}

func normalizeTranslationConfig(t *TranslationConfig) bool {
	if t == nil {
		return false
	}

	changed := false
	changed = normalizeField(&t.Provider, "openai", true) || changed
	changed = normalizeField(&t.SourceLanguage, "auto", false) || changed
	changed = normalizeField(&t.TargetLanguage, "ja", false) || changed
	changed = normalizeField(&t.OpenAI.BaseURL, "https://api.openai.com/v1", false) || changed
	changed = normalizeField(&t.OpenAI.Model, "gpt-4o-mini", false) || changed
	changed = normalizeField(&t.DeepL.Mode, "free", true) || changed
	changed = normalizeField(&t.Google.Mode, "free", true) || changed

	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = 60
		changed = true
	}

	return changed
}

// Normalize applies idempotent value normalization to config data.
func Normalize(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	// Ensure Overrides is populated before accessing it.
	// This handles the case where a config was loaded via JSON (which doesn't
	// call NormalizeScraperConfigs like Load() does for YAML).
	cfg.Scrapers.NormalizeScraperConfigs()

	changed := false
	changed = normalizeField(&cfg.Database.Type, "sqlite", true) || changed

	languageDefaults := map[string]string{
		"r18dev":          "en",
		"javlibrary":      "en",
		"javbus":          "ja",
		"tokyohot":        "ja",
		"caribbeancom":    "ja",
		"aventertainment": "en",
	}

	for name, defaultLang := range languageDefaults {
		if _, registered := scraperutil.GetDefaultScraperSettings()[name]; registered {
			if scraper, ok := cfg.Scrapers.Overrides[name]; ok && scraper != nil {
				changed = normalizeField(&scraper.Language, defaultLang, true) || changed
			}
		}
	}

	if strings.TrimSpace(cfg.Scrapers.Referer) == "" {
		cfg.Scrapers.Referer = "https://www.dmm.co.jp/"
		changed = true
	}

	changed = normalizeTranslationConfig(&cfg.Metadata.Translation) || changed

	if cfg.Output.OperationMode == "" {
		cfg.Output.OperationMode = types.OperationModeOrganize
		changed = true
	}

	return changed
}

// Prepare runs compatibility migrations, normalization, and strict validation.
// Returns true when config data was changed during preparation.
func Prepare(cfg *Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}

	if cfg.ConfigVersion > CurrentConfigVersion {
		return false, fmt.Errorf(
			"config version %d is newer than supported version %d; please update Javinizer",
			cfg.ConfigVersion,
			CurrentConfigVersion,
		)
	}

	normalized := Normalize(cfg)

	if err := cfg.Validate(); err != nil {
		return normalized, fmt.Errorf("invalid configuration: %w", err)
	}

	return normalized, nil
}
