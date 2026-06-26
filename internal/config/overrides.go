package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ApplyEnvironmentOverrides applies environment variable overrides to config.
// envLookup, when provided, replaces os.Getenv for testability.
// When omitted or nil, os.Getenv is used.
func ApplyEnvironmentOverrides(cfg *Config, envLookup ...func(key string) string) {
	lookup := os.Getenv
	if len(envLookup) > 0 && envLookup[0] != nil {
		lookup = envLookup[0]
	}

	// LOG_LEVEL - Override log level
	if envLogLevel := lookup("LOG_LEVEL"); envLogLevel != "" {
		cfg.Logging.Level = strings.ToLower(envLogLevel)
	}

	// UMASK - Override file creation mask
	if envUmask := lookup("UMASK"); envUmask != "" {
		cfg.System.Umask = envUmask
	}

	// JAVINIZER_TEMP_DIR - Override temp directory
	if envTempDir := lookup("JAVINIZER_TEMP_DIR"); envTempDir != "" {
		cfg.System.TempDir = envTempDir
	}

	// JAVINIZER_DB - Override database DSN path
	if envDB := lookup("JAVINIZER_DB"); envDB != "" {
		cfg.Database.DSN = envDB
	}

	// JAVINIZER_LOG_DIR - Override log output directory
	if envLogDir := lookup("JAVINIZER_LOG_DIR"); envLogDir != "" {
		outputs := strings.Split(cfg.Logging.Output, ",")
		newOutputs := make([]string, 0, len(outputs))

		for _, output := range outputs {
			output = strings.TrimSpace(output)
			if output != "stdout" && output != "stderr" && output != "" {
				filename := filepath.Base(output)
				newOutputs = append(newOutputs, filepath.ToSlash(filepath.Join(envLogDir, filename)))
			} else {
				newOutputs = append(newOutputs, output)
			}
		}

		cfg.Logging.Output = strings.Join(newOutputs, ",")
	}

	// Translation provider credentials/settings
	if provider := lookup("TRANSLATION_PROVIDER"); provider != "" {
		cfg.Metadata.Translation.Provider = strings.ToLower(strings.TrimSpace(provider))
	}
	if srcLang := lookup("TRANSLATION_SOURCE_LANGUAGE"); srcLang != "" {
		cfg.Metadata.Translation.SourceLanguage = strings.TrimSpace(srcLang)
	}
	if targetLang := lookup("TRANSLATION_TARGET_LANGUAGE"); targetLang != "" {
		cfg.Metadata.Translation.TargetLanguage = strings.TrimSpace(targetLang)
	}
	if openAIKey := lookup("OPENAI_API_KEY"); openAIKey != "" {
		cfg.Metadata.Translation.OpenAI.APIKey = strings.TrimSpace(openAIKey)
	}
	if openAIBaseURL := lookup("OPENAI_BASE_URL"); openAIBaseURL != "" {
		cfg.Metadata.Translation.OpenAI.BaseURL = strings.TrimSpace(openAIBaseURL)
	}
	if openAIModel := lookup("OPENAI_MODEL"); openAIModel != "" {
		cfg.Metadata.Translation.OpenAI.Model = strings.TrimSpace(openAIModel)
	}
	if deeplKey := lookup("DEEPL_API_KEY"); deeplKey != "" {
		cfg.Metadata.Translation.DeepL.APIKey = strings.TrimSpace(deeplKey)
	}
	if googleKey := lookup("GOOGLE_TRANSLATE_API_KEY"); googleKey != "" {
		cfg.Metadata.Translation.Google.APIKey = strings.TrimSpace(googleKey)
	}
	if openAICompatibleKey := lookup("OPENAI_COMPATIBLE_API_KEY"); openAICompatibleKey != "" {
		cfg.Metadata.Translation.OpenAICompatible.APIKey = strings.TrimSpace(openAICompatibleKey)
	}
	if anthropicKey := lookup("ANTHROPIC_API_KEY"); anthropicKey != "" {
		cfg.Metadata.Translation.Anthropic.APIKey = strings.TrimSpace(anthropicKey)
	}

	// Translation provider settings (separate from credentials)
	if provider := lookup("METADATA_TRANSLATION_PROVIDER"); provider != "" {
		cfg.Metadata.Translation.Provider = strings.ToLower(strings.TrimSpace(provider))
	}
	if srcLang := lookup("METADATA_TRANSLATION_SOURCE_LANGUAGE"); srcLang != "" {
		cfg.Metadata.Translation.SourceLanguage = strings.TrimSpace(srcLang)
	}
	if targetLang := lookup("METADATA_TRANSLATION_TARGET_LANGUAGE"); targetLang != "" {
		cfg.Metadata.Translation.TargetLanguage = strings.TrimSpace(targetLang)
	}
	if timeout := lookup("METADATA_TRANSLATION_TIMEOUT_SECONDS"); timeout != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(timeout)); err == nil && n > 0 {
			cfg.Metadata.Translation.TimeoutSeconds = n
		}
	}
	if applyPrimary := lookup("METADATA_TRANSLATION_APPLY_TO_PRIMARY"); applyPrimary != "" {
		cfg.Metadata.Translation.ApplyToPrimary = applyPrimary == "true"
	}
	if overwrite := lookup("METADATA_TRANSLATION_OVERWRITE_EXISTING_TARGET"); overwrite != "" {
		cfg.Metadata.Translation.OverwriteExistingTarget = overwrite == "true"
	}
	// Note: TRANSLATION_PROVIDER is intentionally NOT re-read here. It is
	// already applied as a legacy alias at the top of this function (before the
	// METADATA_TRANSLATION_* block), so the canonical METADATA_TRANSLATION_PROVIDER
	// above takes precedence. Re-reading TRANSLATION_PROVIDER here would silently
	// override METADATA_TRANSLATION_PROVIDER, inverting the documented precedence.
}
