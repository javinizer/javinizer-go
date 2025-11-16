package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	cfg          *config.Config
	scrapersFlag []string
	verboseFlag  bool
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:     "javinizer",
	Short:   "Javinizer - JAV metadata scraper and organizer",
	Long:    `A metadata scraper and file organizer for Japanese Adult Videos (JAV)`,
	Version: version.Short(),
}

func init() {
	// Customize version template
	rootCmd.SetVersionTemplate(version.Info() + "\n")

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "enable debug logging")

	// Add all subcommands
	rootCmd.AddCommand(
		newScrapeCmd(),
		newInfoCmd(),
		newInitCmd(),
		newSortCmd(),
		newUpdateCmd(),
		newGenreCmd(),
		newTagCmd(),
		newHistoryCmd(),
		createTUICommand(), // Already exists in tui.go
		newAPICmd(),        // Already exists in api.go
	)
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

// loadConfig loads configuration from file (moved from main.go lines 271-363)
func loadConfig() error {
	// Check for JAVINIZER_CONFIG environment variable (Docker override)
	if envConfig := os.Getenv("JAVINIZER_CONFIG"); envConfig != "" {
		cfgFile = envConfig
	}

	var err error
	cfg, err = config.LoadOrCreate(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override config values with environment variables (Docker-friendly)
	applyEnvironmentOverrides(cfg)

	// Initialize logger
	logCfg := &logging.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	}

	// Override level to debug if --verbose flag is set
	if verboseFlag {
		logCfg.Level = "debug"
	}

	if err := logging.InitLogger(logCfg); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	logging.Debugf("Loaded configuration from: %s", cfgFile)

	// Log environment variable overrides
	if os.Getenv("LOG_LEVEL") != "" {
		logging.Debugf("Log level overridden by LOG_LEVEL: %s", cfg.Logging.Level)
	}
	if os.Getenv("JAVINIZER_DB") != "" {
		logging.Debugf("Database DSN overridden by JAVINIZER_DB: %s", cfg.Database.DSN)
	}
	if os.Getenv("JAVINIZER_LOG_DIR") != "" {
		logging.Debugf("Log output overridden by JAVINIZER_LOG_DIR: %s", cfg.Logging.Output)
	}
	if os.Getenv("JAVINIZER_HOME") != "" {
		logging.Debugf("JAVINIZER_HOME is set to: %s (reserved for future use)", os.Getenv("JAVINIZER_HOME"))
	}

	// Validate proxy configuration
	if cfg.Scrapers.Proxy.Enabled {
		if cfg.Scrapers.Proxy.URL == "" {
			logging.Warn("Scraper proxy is enabled but URL is empty, disabling proxy")
			cfg.Scrapers.Proxy.Enabled = false
		} else {
			sanitizedURL := cfg.Scrapers.Proxy.URL
			if u, err := url.Parse(sanitizedURL); err == nil && u.User != nil {
				u.User = url.User("[REDACTED]")
				sanitizedURL = u.String()
			}
			logging.Infof("Scraper proxy enabled: %s", sanitizedURL)
		}
	}

	if cfg.Output.DownloadProxy.Enabled {
		if cfg.Output.DownloadProxy.URL == "" {
			logging.Warn("Download proxy is enabled but URL is empty, disabling proxy")
			cfg.Output.DownloadProxy.Enabled = false
		} else {
			sanitizedURL := cfg.Output.DownloadProxy.URL
			if u, err := url.Parse(sanitizedURL); err == nil && u.User != nil {
				u.User = url.User("[REDACTED]")
				sanitizedURL = u.String()
			}
			logging.Infof("Download proxy enabled: %s", sanitizedURL)
		}
	}

	// Apply umask if configured
	if cfg.System.Umask != "" {
		umaskValue, err := strconv.ParseUint(cfg.System.Umask, 8, 32)
		if err != nil {
			logging.Warnf("Invalid umask value '%s', using default: %v", cfg.System.Umask, err)
		} else {
			oldUmask := syscall.Umask(int(umaskValue))
			logging.Debugf("Applied umask: %s (previous: %04o)", cfg.System.Umask, oldUmask)
		}
	}

	return nil
}

// applyEnvironmentOverrides applies environment variable overrides (moved from main.go lines 368-420)
func applyEnvironmentOverrides(cfg *config.Config) {
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

	// Docker auto-detection
	if len(cfg.API.Security.AllowedDirectories) == 0 {
		if _, err := os.Stat("/media"); err == nil {
			cfg.API.Security.AllowedDirectories = []string{"/media"}
			logging.Debugf("Auto-detected Docker environment, setting allowed directories to [/media]")
		}
	}
}

// applyScrapeFlagOverrides applies CLI flag overrides (moved from main.go lines 424-474)
func applyScrapeFlagOverrides(cmd *cobra.Command, cfg *config.Config) {
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

	if cmd.Flags().Changed("headless") {
		headless, _ := cmd.Flags().GetBool("headless")
		cfg.Scrapers.DMM.EnableHeadless = headless
		logging.Debugf("CLI override: enable_headless = %v", headless)
	}
	if cmd.Flags().Changed("no-headless") {
		cfg.Scrapers.DMM.EnableHeadless = false
		logging.Debugf("CLI override: enable_headless = false")
	}

	if cmd.Flags().Changed("headless-timeout") {
		timeout, _ := cmd.Flags().GetInt("headless-timeout")
		if timeout > 0 {
			cfg.Scrapers.DMM.HeadlessTimeout = timeout
			logging.Debugf("CLI override: headless_timeout = %d", timeout)
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

// runWithDeps wrapper (moved from main.go lines 1763-1787)
func runWithDeps(fn func(*cobra.Command, []string, *Dependencies) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Apply scrape command overrides BEFORE creating dependencies
		if cmd.Name() == "scrape" {
			applyScrapeFlagOverrides(cmd, cfg)
		}

		deps, err := NewDependencies(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize dependencies: %w", err)
		}
		defer func() {
			if closeErr := deps.Close(); closeErr != nil {
				logging.Warnf("Failed to close dependencies: %v", closeErr)
			}
		}()

		return fn(cmd, args, deps)
	}
}

// runWithConfig wrapper (moved from main.go lines 1799-1806)
func runWithConfig(fn func(*cobra.Command, []string, *config.Config) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return fn(cmd, args, cfg)
	}
}
