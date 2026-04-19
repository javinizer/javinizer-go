package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/cobra"
)

// ApplyEnvironmentOverrides applies environment variable overrides to config
func ApplyEnvironmentOverrides(cfg *Config) {
	// LOG_LEVEL - Override log level
	if envLogLevel := os.Getenv("LOG_LEVEL"); envLogLevel != "" {
		cfg.Logging.Level = strings.ToLower(envLogLevel)
	}

	// UMASK - Override file creation mask
	if envUmask := os.Getenv("UMASK"); envUmask != "" {
		cfg.System.Umask = envUmask
	}

	// JAVINIZER_TEMP_DIR - Override temp directory
	if envTempDir := os.Getenv("JAVINIZER_TEMP_DIR"); envTempDir != "" {
		cfg.System.TempDir = envTempDir
	}

	// JAVINIZER_DB - Override database DSN path
	if envDB := os.Getenv("JAVINIZER_DB"); envDB != "" {
		cfg.Database.DSN = envDB
	}

	// JAVINIZER_LOG_DIR - Override log output directory
	if envLogDir := os.Getenv("JAVINIZER_LOG_DIR"); envLogDir != "" {
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
	if provider := os.Getenv("TRANSLATION_PROVIDER"); provider != "" {
		cfg.Metadata.Translation.Provider = strings.ToLower(strings.TrimSpace(provider))
	}
	if srcLang := os.Getenv("TRANSLATION_SOURCE_LANGUAGE"); srcLang != "" {
		cfg.Metadata.Translation.SourceLanguage = strings.TrimSpace(srcLang)
	}
	if targetLang := os.Getenv("TRANSLATION_TARGET_LANGUAGE"); targetLang != "" {
		cfg.Metadata.Translation.TargetLanguage = strings.TrimSpace(targetLang)
	}
	if openAIKey := os.Getenv("OPENAI_API_KEY"); openAIKey != "" {
		cfg.Metadata.Translation.OpenAI.APIKey = strings.TrimSpace(openAIKey)
	}
	if openAIBaseURL := os.Getenv("OPENAI_BASE_URL"); openAIBaseURL != "" {
		cfg.Metadata.Translation.OpenAI.BaseURL = strings.TrimSpace(openAIBaseURL)
	}
	if openAIModel := os.Getenv("OPENAI_MODEL"); openAIModel != "" {
		cfg.Metadata.Translation.OpenAI.Model = strings.TrimSpace(openAIModel)
	}
	if deeplKey := os.Getenv("DEEPL_API_KEY"); deeplKey != "" {
		cfg.Metadata.Translation.DeepL.APIKey = strings.TrimSpace(deeplKey)
	}
	if googleKey := os.Getenv("GOOGLE_TRANSLATE_API_KEY"); googleKey != "" {
		cfg.Metadata.Translation.Google.APIKey = strings.TrimSpace(googleKey)
	}
	if openAICompatibleKey := os.Getenv("OPENAI_COMPATIBLE_API_KEY"); openAICompatibleKey != "" {
		cfg.Metadata.Translation.OpenAICompatible.APIKey = strings.TrimSpace(openAICompatibleKey)
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		cfg.Metadata.Translation.Anthropic.APIKey = strings.TrimSpace(anthropicKey)
	}

	// Translation provider settings (separate from credentials)
	if provider := os.Getenv("METADATA_TRANSLATION_PROVIDER"); provider != "" {
		cfg.Metadata.Translation.Provider = strings.ToLower(strings.TrimSpace(provider))
	}
	if srcLang := os.Getenv("METADATA_TRANSLATION_SOURCE_LANGUAGE"); srcLang != "" {
		cfg.Metadata.Translation.SourceLanguage = strings.TrimSpace(srcLang)
	}
	if targetLang := os.Getenv("METADATA_TRANSLATION_TARGET_LANGUAGE"); targetLang != "" {
		cfg.Metadata.Translation.TargetLanguage = strings.TrimSpace(targetLang)
	}
	if timeout := os.Getenv("METADATA_TRANSLATION_TIMEOUT_SECONDS"); timeout != "" {
		cfg.Metadata.Translation.TimeoutSeconds = 60
	}
	if applyPrimary := os.Getenv("METADATA_TRANSLATION_APPLY_TO_PRIMARY"); applyPrimary != "" {
		cfg.Metadata.Translation.ApplyToPrimary = applyPrimary == "true"
	}
	if overwrite := os.Getenv("METADATA_TRANSLATION_OVERWRITE_EXISTING_TARGET"); overwrite != "" {
		cfg.Metadata.Translation.OverwriteExistingTarget = overwrite == "true"
	}
	if provider := os.Getenv("TRANSLATION_PROVIDER"); provider != "" {
		cfg.Metadata.Translation.Provider = strings.ToLower(strings.TrimSpace(provider))
	}
}

// ApplyScrapeFlagOverrides applies CLI flag overrides to config
func ApplyScrapeFlagOverrides(cmd *cobra.Command, cfg *Config) {
	// Ensure Overrides are populated if not already
	if cfg.Scrapers.Overrides == nil {
		cfg.Scrapers.NormalizeScraperConfigs()
	}

	// Metadata configuration overrides
	if cmd.Flags().Changed("actress-db") {
		actressDB, _ := cmd.Flags().GetBool("actress-db")
		cfg.Metadata.ActressDatabase.Enabled = actressDB
		logging.Debugf("CLI override: actress_database.enabled = %v", actressDB)
	}
	if cmd.Flags().Changed("no-actress-db") {
		cfg.Metadata.ActressDatabase.Enabled = false
		logging.Debugf("CLI override: actress_database.enabled = false")
	}

	if cmd.Flags().Changed("genre-replacement") {
		genreRepl, _ := cmd.Flags().GetBool("genre-replacement")
		cfg.Metadata.GenreReplacement.Enabled = genreRepl
		logging.Debugf("CLI override: genre_replacement.enabled = %v", genreRepl)
	}
	if cmd.Flags().Changed("no-genre-replacement") {
		cfg.Metadata.GenreReplacement.Enabled = false
		logging.Debugf("CLI override: genre_replacement.enabled = false")
	}
}
