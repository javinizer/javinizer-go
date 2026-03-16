package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"syscall"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	initcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/init"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tui"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	verboseFlag bool
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
		api.NewCommand(),
		genre.NewCommand(),
		history.NewCommand(),
		info.NewCommand(),
		initcmd.NewCommand(),
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

	// Override config values with environment variables (Docker-friendly)
	config.ApplyEnvironmentOverrides(cfg)

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
		_, _ = fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
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
}
