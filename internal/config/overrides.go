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
				newOutputs = append(newOutputs, filepath.Join(envLogDir, filename))
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

	// Docker auto-detection
	if len(cfg.API.Security.AllowedDirectories) == 0 {
		if _, err := os.Stat("/media"); err == nil {
			cfg.API.Security.AllowedDirectories = []string{"/media"}
			logging.Debugf("Auto-detected Docker environment, setting allowed directories to [/media]")
		}
	}
}

// ApplyScrapeFlagOverrides applies CLI flag overrides to config
func ApplyScrapeFlagOverrides(cmd *cobra.Command, cfg *Config) {
	// DMM scraper overrides
	if cmd.Flags().Changed("scrape-actress") {
		scrapeActress, _ := cmd.Flags().GetBool("scrape-actress")
		cfg.Scrapers.DMM.ScrapeActress = scrapeActress
		logging.Debugf("CLI override: scrape_actress = %v", scrapeActress)
	}
	if cmd.Flags().Changed("no-scrape-actress") {
		cfg.Scrapers.DMM.ScrapeActress = false
		logging.Debugf("CLI override: scrape_actress = false")
	}

	// Browser mode flags (new flags take precedence over deprecated ones)
	if cmd.Flags().Changed("browser") {
		browser, _ := cmd.Flags().GetBool("browser")
		cfg.Scrapers.DMM.EnableBrowser = browser
		logging.Debugf("CLI override: enable_browser = %v", browser)
	} else if cmd.Flags().Changed("headless") {
		// Backward compatibility
		headless, _ := cmd.Flags().GetBool("headless")
		cfg.Scrapers.DMM.EnableBrowser = headless
		logging.Debugf("CLI override: enable_browser = %v (via deprecated --headless)", headless)
	}
	if cmd.Flags().Changed("no-browser") {
		cfg.Scrapers.DMM.EnableBrowser = false
		logging.Debugf("CLI override: enable_browser = false")
	} else if cmd.Flags().Changed("no-headless") {
		// Backward compatibility
		cfg.Scrapers.DMM.EnableBrowser = false
		logging.Debugf("CLI override: enable_browser = false (via deprecated --no-headless)")
	}

	if cmd.Flags().Changed("browser-timeout") {
		timeout, _ := cmd.Flags().GetInt("browser-timeout")
		if timeout > 0 {
			cfg.Scrapers.DMM.BrowserTimeout = timeout
			logging.Debugf("CLI override: browser_timeout = %d", timeout)
		}
	} else if cmd.Flags().Changed("headless-timeout") {
		// Backward compatibility
		timeout, _ := cmd.Flags().GetInt("headless-timeout")
		if timeout > 0 {
			cfg.Scrapers.DMM.BrowserTimeout = timeout
			logging.Debugf("CLI override: browser_timeout = %d (via deprecated --headless-timeout)", timeout)
		}
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
