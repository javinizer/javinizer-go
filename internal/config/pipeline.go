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

	// Logging.Output: default to the standard dual-output target if empty. A config
	// saved via the API (JSON) without an explicit output would otherwise leave
	// InitLogger with no valid targets, which now errors instead of silently
	// falling back to stdout.
	if strings.TrimSpace(cfg.Logging.Output) == "" {
		cfg.Logging.Output = DefaultConfig().Logging.Output
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

	// Backward-compat shim: pre-existing configs may use the legacy
	// `output.delimiter` key (renamed to `output.actress_delimiter`). Carry the
	// legacy value over to the renamed field when it differs from what was
	// already loaded. yaml.v3 doesn't tell us which key was explicitly present,
	// so the shim is conservative: only carry over when the legacy value is
	// non-empty AND the new field still holds the default ', '. This preserves
	// pre-rename user settings without clobbering an explicitly-set new key
	// that happens to equal the default.
	defaultDelim := DefaultConfig().Output.ActressDelimiter
	if cfg.Output.LegacyDelimiter != "" && cfg.Output.ActressDelimiter == defaultDelim && cfg.Output.LegacyDelimiter != defaultDelim {
		cfg.Output.ActressDelimiter = cfg.Output.LegacyDelimiter
		cfg.Output.LegacyDelimiter = ""
		changed = true
	}

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
