package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	configcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/config"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	initcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/init"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/logs"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tui"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	"github.com/javinizer/javinizer-go/internal/config"
	_ "github.com/javinizer/javinizer-go/internal/config/migrations"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile           string
	verboseFlag       bool
	originalLogOutput string
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

	// Initialize configuration for commands that need it.
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if shouldSkipConfigInit(cmd) {
			return
		}
		initConfig()
	}

	// Add all subcommands
	rootCmd.AddCommand(
		actress.NewCommand(),
		api.NewCommand(),
		configcmd.NewCommand(),
		genre.NewCommand(),
		history.NewCommand(),
		info.NewCommand(),
		initcmd.NewCommand(),
		logs.NewCommand(),
		scrape.NewCommand(),
		sort.NewCommand(),
		tag.NewCommand(),
		tui.NewCommand(),
		update.NewCommand(),
		versioncmd.NewCommand(),
	)
}

func shouldSkipConfigInit(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	// Built-in/help/version paths should not require config or logger setup.
	if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" {
		return true
	}

	// `javinizer --version` should stay lightweight and side-effect free.
	versionFlag := cmd.Flags().Lookup("version")
	return versionFlag != nil && versionFlag.Changed
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

// initConfig reads in config file and ENV variables
func initConfig() {
	// Check for JAVINIZER_CONFIG environment variable (Docker override)
	if envConfig := os.Getenv("JAVINIZER_CONFIG"); envConfig != "" {
		cfgFile = envConfig
	}

	cfg, err := config.LoadOrCreate(cfgFile)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	originalLogOutput = cfg.Logging.Output

	// Apply environment variable overrides FIRST (UMASK, JAVINIZER_LOG_DIR, etc.)
	config.ApplyEnvironmentOverrides(cfg)
	if _, err := config.Prepare(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to apply environment overrides: %v\n", err)
		os.Exit(1)
	}

	// Apply umask AFTER env overrides, BEFORE creating log files
	if cfg.System.Umask != "" {
		umaskValue, err := strconv.ParseUint(cfg.System.Umask, 8, 32)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Invalid umask value '%s': %v\n", cfg.System.Umask, err)
		} else {
			oldUmask, applied := applyUmask(int(umaskValue))
			if applied {
				defer func() { _, _ = applyUmask(oldUmask) }()
			}
		}
	}

	// Initialize logger
	logCfg := &logging.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}

	// Override level to debug if --verbose flag is set
	if verboseFlag {
		logCfg.Level = "debug"
	}

	if err := logging.InitLogger(logCfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Log file output location (INFO level for visibility)
	if logPaths := logging.GetFileOutputs(cfg.Logging.Output); len(logPaths) > 0 {
		for _, path := range logPaths {
			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}
			logging.Infof("Log file: %s", absPath)
		}
	}

	logging.Debugf("Loaded configuration from: %s", cfgFile)

	// Log environment variable overrides
	if os.Getenv("LOG_LEVEL") != "" {
		logging.Debugf("Log level overridden by LOG_LEVEL: %s", cfg.Logging.Level)
	}
	if os.Getenv("JAVINIZER_DB") != "" {
		logging.Debugf("Database DSN overridden by JAVINIZER_DB: %s", cfg.Database.DSN)
	}
	if envLogDir := os.Getenv("JAVINIZER_LOG_DIR"); envLogDir != "" {
		if cfg.Logging.Output != originalLogOutput {
			logging.Debugf("Log file outputs relocated by JAVINIZER_LOG_DIR=%s: %s", envLogDir, cfg.Logging.Output)
		} else {
			logging.Debugf("JAVINIZER_LOG_DIR=%s set, but logging.output has no file target; output remains: %s", envLogDir, cfg.Logging.Output)
		}
	}
	if os.Getenv("JAVINIZER_HOME") != "" {
		logging.Debugf("JAVINIZER_HOME is set to: %s (reserved for future use)", os.Getenv("JAVINIZER_HOME"))
	}

	// Validate proxy configuration
	if cfg.Scrapers.Proxy.Enabled {
		resolvedScraperProxy := config.ResolveGlobalProxy(cfg.Scrapers.Proxy)
		if resolvedScraperProxy.URL == "" {
			logging.Warn("Scraper proxy is enabled but resolved profile URL is empty, disabling proxy")
			cfg.Scrapers.Proxy.Enabled = false
		} else {
			logging.Infof("Scraper proxy enabled: %s", sanitizeProxyURL(resolvedScraperProxy.URL))
		}
	}

	if cfg.Output.DownloadProxy.Enabled {
		resolvedDownloadProxy := config.ResolveScraperProxy(cfg.Scrapers.Proxy, &cfg.Output.DownloadProxy)
		if resolvedDownloadProxy.URL == "" {
			logging.Warn("Download proxy is enabled but resolved profile URL is empty, disabling proxy")
			cfg.Output.DownloadProxy.Enabled = false
		} else {
			logging.Infof("Download proxy enabled: %s", sanitizeProxyURL(resolvedDownloadProxy.URL))
		}
	}
}

func sanitizeProxyURL(raw string) string {
	sanitized := raw
	if u, err := url.Parse(sanitized); err == nil && u.User != nil {
		u.User = url.User("[REDACTED]")
		sanitized = u.String()
	}
	return sanitized
}
