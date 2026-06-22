package config

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
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

// normalizeTranslationMode normalizes a typed mode field (DeepLMode or GoogleMode).
// It trims whitespace, lowercases, and sets a default if empty.
func normalizeTranslationMode[T ~string](value *T, defaultValue T) bool {
	if value == nil {
		return false
	}
	normalized := T(strings.ToLower(strings.TrimSpace(string(*value))))
	if normalized == "" {
		normalized = defaultValue
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
	changed = normalizeTranslationMode(&t.DeepL.Mode, models.DeepLModeFree) || changed
	changed = normalizeTranslationMode(&t.Google.Mode, models.GoogleModeFree) || changed

	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = 60
		changed = true
	}

	return changed
}

// normalize applies idempotent value normalization to config data.
func normalize(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	// Ensure Overrides is populated before accessing it.
	cfg.Scrapers.Normalize()

	changed := false
	changed = normalizeField(&cfg.Database.Type, "sqlite", true) || changed

	// Logging.Output: default to the standard dual-output target if empty. A config
	// saved via the API (JSON) without an explicit output would otherwise leave
	// InitLogger with no valid targets, which now errors instead of silently
	// falling back to stdout.
	if strings.TrimSpace(cfg.Logging.Output) == "" {
		cfg.Logging.Output = DefaultConfig(nil, nil).Logging.Output
		changed = true
	}

	languageDefaults := map[string]string{
		"r18dev":          "en",
		"javlibrary":      "en",
		"javbus":          "ja",
		"tokyohot":        "ja",
		"caribbeancom":    "ja",
		"aventertainment": "en",
	}

	for name, defaultLang := range languageDefaults {
		if _, ok := cfg.Scrapers.Overrides[name]; ok {
			if scraper := cfg.Scrapers.Overrides[name]; scraper != nil {
				changed = normalizeField(&scraper.Language, defaultLang, true) || changed
			}
		}
	}
	if strings.TrimSpace(cfg.Scrapers.Referer) == "" {
		cfg.Scrapers.Referer = "https://www.dmm.co.jp/"
		changed = true
	}

	changed = normalizeTranslationConfig(&cfg.Metadata.Translation) || changed

	// Backward-compat shim: pre-existing configs may use the legacy
	// `output.delimiter` key (renamed to `output.actress_delimiter`). Carry the
	// legacy value over to the renamed field when it differs from what was
	// already loaded. yaml.v3 doesn't tell us which key was explicitly present,
	// so the shim is conservative: only carry over when the legacy value is
	// non-empty AND the new field still holds the default ', '. This preserves
	// pre-rename user settings without clobbering an explicitly-set new key
	// that happens to equal the default.
	defaultDelim := ", " // matches defaultOutputConfig().Output.Template.ActressDelimiter
	if cfg.Output.Template.LegacyDelimiter != "" && cfg.Output.Template.ActressDelimiter == defaultDelim && cfg.Output.Template.LegacyDelimiter != defaultDelim {
		cfg.Output.Template.ActressDelimiter = cfg.Output.Template.LegacyDelimiter
		cfg.Output.Template.LegacyDelimiter = ""
		changed = true
	}

	if cfg.Output.Operation.OperationMode == "" {
		cfg.Output.Operation.OperationMode = operationmode.OperationModeOrganize
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

	normalized := normalize(cfg)

	if err := cfg.Validate(); err != nil {
		return normalized, fmt.Errorf("invalid configuration: %w", err)
	}

	return normalized, nil
}
