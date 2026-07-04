package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/app"
	configcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/config"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/dump"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	initcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/init"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/logs"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/token"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tui"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/upgrade"
	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
	"github.com/javinizer/javinizer-go/internal/config"
	_ "github.com/javinizer/javinizer-go/internal/config/migrations"
	"github.com/javinizer/javinizer-go/internal/desktop"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile           string
	verboseFlag       bool
	originalLogOutput string
	currentCmd        *cobra.Command
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:     "javinizer",
	Short:   "Javinizer - JAV metadata scraper and organizer",
	Long:    `A metadata scraper and file organizer for Japanese Adult Videos (JAV)`,
	Version: version.Short(),
}

// initDesktopDefault wires no-arg → GUI for desktop builds only. In CLI builds
// (BuildDesktop="0") rootCmd keeps no Run, so no-args prints help as before.
func initDesktopDefault() {
	if !desktop.IsDesktopBuild() {
		return
	}
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		return desktop.Run(desktop.Options{ConfigFile: cfgFile})
	}
}

func init() {
	// Customize version template
	rootCmd.SetVersionTemplate(version.Info() + "\n")

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "enable debug logging")

	// Desktop builds (injected via -X internal/desktop.BuildDesktop=1) run with
	// a portable user-data dir so config/db/logs land in a writable,
	// CWD-independent location regardless of how the app was launched
	// (Finder/Explorer set CWD to "/" or the bundle dir). This must run before
	// config init so ApplyEnvironmentOverrides picks the portable paths up.
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if desktop.IsDesktopBuild() {
			// Only set up the portable env when the user did not pass a custom
			// --config. With a custom config, the user owns their data layout;
			// injecting JAVINIZER_DB/LOG_DIR here would have ApplyEnvironmentOverrides
			// override that file's DB/log settings with the portable paths.
			if cfgFile == "" || cfgFile == "configs/config.yaml" {
				if err := desktop.SetupPortableEnv(); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "desktop: %v\n", err)
				}
				cfgFile = desktop.DefaultConfigPath()
			}
			// Write the resolved path back to the flag so subcommands that read
			// cmd.Flags().GetString("config") (api, app, tui, …) see the portable
			// path, not the default "configs/config.yaml".
			_ = cmd.Flags().Set("config", cfgFile)
		}
		if shouldSkipConfigInit(cmd) {
			return
		}
		currentCmd = cmd
		initConfig()
	}

	// Add all subcommands
	rootCmd.AddCommand(
		actress.NewCommand(),
		api.NewCommand(),
		app.NewCommand(),
		configcmd.NewCommand(),
		dump.NewCommand(),
		genre.NewCommand(),
		history.NewCommand(),
		info.NewCommand(),
		initcmd.NewCommand(),
		logs.NewCommand(),
		scrape.NewCommand(),
		sort.NewCommand(),
		token.NewCommand(),
		tag.NewCommand(),
		tui.NewCommand(),
		update.NewCommand(),
		upgrade.NewCommand(),
		word.NewCommand(),
		versioncmd.NewCommand(),
	)

	initDesktopDefault()
}

func shouldSkipConfigInit(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	// Built-in/help/version paths should not require config or logger setup.
	// `upgrade` also runs without config: it only talks to GitHub and replaces
	// the binary, and forcing config init would create a config file as a side
	// effect of a self-update.
	// `app` on a non-desktop build always errors (the desktop runner is a
	// build-tagged stub), so initConfig would create config/DB/log files before
	// the command fails — the same side-effect concern as `upgrade`.
	if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "upgrade" {
		return true
	}
	if cmd.Name() == "app" && !desktop.IsDesktopBuild() {
		return true
	}

	// `javinizer --version` should stay lightweight and side-effect free.
	versionFlag := cmd.Flags().Lookup("version")
	return versionFlag != nil && versionFlag.Changed
}

// isTUICommand reports whether cmd (or any ancestor) is the tui subcommand.
// Used to suppress stdout logging during initial setup so startup messages don't
// leak to the terminal before the TUI's AltScreen activates.
func isTUICommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "tui" {
			return true
		}
	}
	return false
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
			_, applied := applyUmask(int(umaskValue))
			if applied {
				config.StoreUmask(int(umaskValue))
			} else {
				_, _ = fmt.Fprintf(os.Stderr, "Umask not supported on this platform\n")
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

	// For the TUI subcommand, strip stdout/stderr from the FIRST logger so startup
	// messages (e.g. "Log file: ...") don't leak to the terminal before AltScreen
	// activates. Only logCfg.Output is modified; cfg.Logging.Output is left intact
	// so the JAVINIZER_LOG_DIR relocation check below still compares correctly.
	// When the config has no file target at all (e.g. pure "stdout"), the fallback
	// path honors JAVINIZER_LOG_DIR so logs still land in the env-configured dir.
	// The TUI's run() reinitializes the logger via configureTUILogging afterwards.
	if isTUICommand(currentCmd) {
		defaultTUILog := "data/logs/javinizer-tui.log"
		if len(logging.GetFileOutputs(logCfg.Output)) == 0 {
			if envLogDir := os.Getenv("JAVINIZER_LOG_DIR"); envLogDir != "" {
				defaultTUILog = filepath.Join(envLogDir, "javinizer-tui.log")
			}
		}
		logCfg.Output = logging.FileOnlyOutput(logCfg.Output, defaultTUILog)
	}
	actualLogOutput := logCfg.Output

	if err := logging.InitLogger(logCfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Log file output location (INFO level for visibility). Use the actual logger
	// output (which for the TUI is the stripped/relocated file-only path) rather than
	// the original cfg.Logging.Output, so the reported path matches what is written.
	if logPaths := logging.GetFileOutputs(actualLogOutput); len(logPaths) > 0 {
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
		resolvedScraperProxy := models.ResolveGlobalProxy(cfg.Scrapers.Proxy)
		if resolvedScraperProxy.URL == "" {
			logging.Warn("Scraper proxy is enabled but resolved profile URL is empty, disabling proxy")
			cfg.Scrapers.Proxy.Enabled = false
		} else {
			logging.Infof("Scraper proxy enabled: %s", sanitizeProxyURL(resolvedScraperProxy.URL))
		}
	}

	if cfg.Output.Download.DownloadProxy.Enabled {
		resolvedDownloadProxy := models.ResolveScraperProxy(cfg.Scrapers.Proxy, &cfg.Output.Download.DownloadProxy)
		if resolvedDownloadProxy.URL == "" {
			logging.Warn("Download proxy is enabled but resolved profile URL is empty, disabling proxy")
			cfg.Output.Download.DownloadProxy.Enabled = false
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
