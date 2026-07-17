package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/formatter"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/timeout"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/cobra"
)

// NewCommand creates the scrape CLI subcommand that fetches metadata for a single movie ID.
func NewCommand() *cobra.Command {
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				output, _ := cmd.Flags().GetString("output")
				if output == "json" {
					cmd.SilenceUsage = true
					cmd.SilenceErrors = true
					writeJSONError(cmd, unknownErrorEnvelope("exactly 1 argument (movie ID) is required"))
					return ErrJSONExit
				}
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runScrape(cmd, args, configFile)
		},
	}
	scrapeCmd.Flags().StringSliceP("scrapers", "s", nil, "Comma-separated subset of enabled scrapers to use (e.g., 'r18dev,dmm'); scraper must be enabled in config.yaml")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers (clear cache)")
	scrapeCmd.Flags().String("output", "text", "Output format: 'text' (default) or 'json'. JSON mode requires --scrapers with exactly one scraper and is incompatible with --force. In JSON mode, stdout contains only the raw ScraperResult or error envelope; all logs go to stderr.")
	scrapeCmd.Flags().Bool("scrape-actress", false, "Enable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("no-scrape-actress", false, "Disable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("browser", false, "Enable browser mode for DMM video pages (overrides config)")
	scrapeCmd.Flags().Bool("no-browser", false, "Disable browser mode for DMM video pages (overrides config)")
	scrapeCmd.Flags().Int("browser-timeout", 0, "Browser timeout in seconds (overrides config, 0=use config)")
	scrapeCmd.Flags().Bool("actress-db", false, "Enable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("no-actress-db", false, "Disable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("genre-replacement", false, "Enable genre replacement (overrides config)")
	scrapeCmd.Flags().Bool("no-genre-replacement", false, "Disable genre replacement (overrides config)")
	return scrapeCmd
}

// ApplyFlagOverrides applies CLI flag overrides to the config. Exported for testability.
func ApplyFlagOverrides(cmd *cobra.Command, cfg *config.Config) {
	cfg.Scrapers.Normalize()
	if cmd.Flags().Changed("scrape-actress") || cmd.Flags().Changed("no-scrape-actress") || cmd.Flags().Changed("browser") || cmd.Flags().Changed("no-browser") {
		if cfg.Scrapers.Overrides == nil {
			cfg.Scrapers.Overrides = make(map[string]*models.ScraperSettings)
		}
		if cfg.Scrapers.Overrides["dmm"] == nil {
			cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{}
		}
	}
	if cmd.Flags().Changed("scrape-actress") {
		if val, _ := cmd.Flags().GetBool("scrape-actress"); val {
			cfg.Scrapers.Overrides["dmm"].ScrapeActress = scraperutil.BoolPtr(true)
		}
	}
	if cmd.Flags().Changed("no-scrape-actress") {
		if val, _ := cmd.Flags().GetBool("no-scrape-actress"); val {
			cfg.Scrapers.Overrides["dmm"].ScrapeActress = scraperutil.BoolPtr(false)
		}
	}
	if cmd.Flags().Changed("browser") {
		if val, _ := cmd.Flags().GetBool("browser"); val {
			cfg.Scrapers.Overrides["dmm"].UseBrowser = true
		}
	}
	if cmd.Flags().Changed("no-browser") {
		if val, _ := cmd.Flags().GetBool("no-browser"); val {
			cfg.Scrapers.Overrides["dmm"].UseBrowser = false
		}
	}
	if cmd.Flags().Changed("browser-timeout") {
		if val, _ := cmd.Flags().GetInt("browser-timeout"); val > 0 {
			cfg.Scrapers.Browser.Timeout = val
		}
	}
	if cmd.Flags().Changed("actress-db") {
		if val, _ := cmd.Flags().GetBool("actress-db"); val {
			cfg.Metadata.ActressDatabase.Enabled = true
		}
	}
	if cmd.Flags().Changed("no-actress-db") {
		if val, _ := cmd.Flags().GetBool("no-actress-db"); val {
			cfg.Metadata.ActressDatabase.Enabled = false
		}
	}
	if cmd.Flags().Changed("genre-replacement") {
		if val, _ := cmd.Flags().GetBool("genre-replacement"); val {
			cfg.Metadata.GenreReplacement.Enabled = true
		}
	}
	if cmd.Flags().Changed("no-genre-replacement") {
		if val, _ := cmd.Flags().GetBool("no-genre-replacement"); val {
			cfg.Metadata.GenreReplacement.Enabled = false
		}
	}
}

// Run executes the scrape command business logic and returns the scraped movie and results.
// Exported for testing — allows testing business logic without console output.
func Run(ctx context.Context, cmd *cobra.Command, args []string, configFile string, deps *commandutil.CoreDeps) (movie *models.Movie, results []*models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicutil.HandleRecoverWithStack(r)
		}
	}()

	id := args[0]
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}
	config.ApplyEnvironmentOverrides(cfg)
	ApplyFlagOverrides(cmd, cfg)
	if _, err := config.Prepare(cfg); err != nil {
		return nil, nil, fmt.Errorf("invalid configuration after CLI overrides: %w", err)
	}

	var ownDeps bool
	var wf workflow.WorkflowInterface
	if deps == nil {
		bs, bootstrapErr := commandutil.BootstrapScrapeOnly(cfg)
		if bootstrapErr != nil {
			return nil, nil, fmt.Errorf("failed to bootstrap: %w", bootstrapErr)
		}
		deps = bs.CoreDeps
		wf = bs.Workflow
		ownDeps = true
	} else {
		fc, fcErr := workflow.NewFactoryConfigFromRepos(cfg, deps.ScraperRegistry, deps.DB.Repositories())
		if fcErr != nil {
			return nil, nil, fmt.Errorf("failed to create factory config: %w", fcErr)
		}
		factory, factoryErr := workflow.NewWorkflowFactory(fc)
		if factoryErr != nil {
			return nil, nil, fmt.Errorf("failed to create workflow factory: %w", factoryErr)
		}
		wf, err = factory.NewScrapeOnlyWorkflow()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create scraper: %w", err)
		}
	}
	if ownDeps {
		defer func() { _ = deps.Close() }()
	}

	forceRefresh, _ := cmd.Flags().GetBool("force")
	scrapersFlag, _ := cmd.Flags().GetStringSlice("scrapers")
	scrapeCtx := ctx
	if cfg.Scrapers.RequestTimeoutSeconds > 0 {
		resolved := timeout.FromConfig("scrapers.request_timeout_seconds", cfg.Scrapers.RequestTimeoutSeconds, 0)
		var cancel context.CancelFunc
		scrapeCtx, cancel = context.WithTimeout(ctx, resolved.Duration)
		defer cancel()
	}
	result, _, err := wf.Scrape(scrapeCtx, scrape.ScrapeCmd{
		MovieID:          id,
		ForceRefresh:     forceRefresh,
		SelectedScrapers: scrapersFlag,
	})
	if scrapeCtx.Err() != nil {
		return nil, nil, fmt.Errorf("scrape timed out")
	}
	if err != nil {
		if result != nil && result.Movie != nil {
			return result.Movie, result.ScraperResults, err
		}
		return nil, nil, err
	}
	if result.Status == scrape.StatusFailed {
		errMsg := "scrape failed"
		if result.Message != "" {
			errMsg = result.Message
		}
		return nil, nil, fmt.Errorf("%s", errMsg)
	}
	return result.Movie, result.ScraperResults, nil
}

func runScrape(cmd *cobra.Command, args []string, configFile string) error {
	outputFlag, _ := cmd.Flags().GetString("output")
	if outputFlag == "json" {
		return runScrapeJSON(cmd, args, configFile)
	}
	if outputFlag != "text" && outputFlag != "" {
		return fmt.Errorf("invalid output value: must be 'text' or 'json'")
	}
	movie, results, err := Run(cmd.Context(), cmd, args, configFile, nil)
	if err != nil {
		return err
	}
	formatter.WriteMovie(cmd.OutOrStdout(), movie, results)
	return nil
}

func runScrapeJSON(cmd *cobra.Command, args []string, configFile string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	scrapersFlag, _ := cmd.Flags().GetStringSlice("scrapers")
	forceRefresh, _ := cmd.Flags().GetBool("force")
	if err := validateJSONMode(scrapersFlag, forceRefresh); err != nil {
		writeJSONError(cmd, unknownErrorEnvelope(err.Error()))
		return ErrJSONExit
	}

	id := args[0]

	// B3: Respect JAVINIZER_CONFIG env var like initConfig() does.
	if envConfig := os.Getenv("JAVINIZER_CONFIG"); envConfig != "" {
		configFile = envConfig
	}

	// B1: Initialize logger to stderr-only before any code that might log,
	// preventing stdout contamination of the JSON output.
	// Honor the --verbose flag like initConfig() does.
	logLevel := "warn"
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		logLevel = "debug"
	}
	loggingCfg := &logging.Config{Output: "stderr", Level: logLevel}
	if err := logging.InitLogger(loggingCfg); err != nil {
		_ = logging.SetOutput(os.Stderr)
	}

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		writeJSONError(cmd, unknownErrorEnvelope(fmt.Sprintf("failed to load config: %v", err)))
		return ErrJSONExit
	}
	config.ApplyEnvironmentOverrides(cfg)
	ApplyFlagOverrides(cmd, cfg)
	if _, err := config.Prepare(cfg); err != nil {
		writeJSONError(cmd, unknownErrorEnvelope(fmt.Sprintf("invalid configuration: %v", err)))
		return ErrJSONExit
	}

	// Apply configured umask (normally done by initConfig, which JSON mode skips)
	if cfg.System.Umask != "" {
		if mask, err := strconv.ParseUint(cfg.System.Umask, 8, 32); err == nil {
			applyUmask(int(mask))
		}
	}

	// Re-init logger with the user's config, but remove any stdout target
	// to keep stdout clean for JSON output. Append stderr so diagnostics are
	// still visible. Preserve format and rotation settings.
	logOutput := removeStdoutFromLogOutput(cfg.Logging.Output)
	if logOutput == "" {
		logOutput = "stderr"
	} else if !hasOutputToken(logOutput, "stderr") {
		logOutput = logOutput + ",stderr"
	}
	logLevel = cfg.Logging.Level
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		logLevel = "debug"
	}
	loggingCfg = &logging.Config{
		Output:     logOutput,
		Level:      logLevel,
		Format:     cfg.Logging.Format,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}
	_ = logging.InitLogger(loggingCfg)

	deps, err := commandutil.NewQueryOnlyDependencies(cfg)
	if err != nil {
		writeJSONError(cmd, unknownErrorEnvelope(fmt.Sprintf("failed to bootstrap: %v", err)))
		return ErrJSONExit
	}
	defer func() { _ = deps.Close() }()

	engine := scrape.NewQueryOnly(deps.ScraperRegistry)

	scrapeCtx := cmd.Context()
	if cfg.Scrapers.RequestTimeoutSeconds > 0 {
		resolved := timeout.FromConfig("scrapers.request_timeout_seconds", cfg.Scrapers.RequestTimeoutSeconds, 0)
		var cancel context.CancelFunc
		scrapeCtx, cancel = context.WithTimeout(scrapeCtx, resolved.Duration)
		defer cancel()
	}

	result, scraperErr := engine.QueryRaw(scrapeCtx, id, scrapersFlag[0])

	if scrapeCtx.Err() != nil && scraperErr == nil {
		scraperErr = &models.ScraperError{
			Kind:      models.ScraperErrorKindUnavailable,
			Message:   "context deadline exceeded",
			Retryable: true,
			Temporary: true,
		}
	}

	if scraperErr != nil {
		writeJSONError(cmd, jsonErrorWrapper{Error: scraperErrorToEnvelope(scraperErr)})
		return ErrJSONExit
	}

	// F1: Handle nil result (scraper returned nil, nil)
	if result == nil {
		writeJSONError(cmd, unknownErrorEnvelope("scraper returned no result"))
		return ErrJSONExit
	}

	data, err := json.Marshal(result)
	if err != nil {
		writeJSONError(cmd, unknownErrorEnvelope(fmt.Sprintf("failed to marshal result: %v", err)))
		return ErrJSONExit
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// hasOutputToken checks if a comma-separated output string contains an exact
// token match (e.g. "stderr"), avoiding false positives on substrings like
// "/var/log/javinizer-stderr.log".
func hasOutputToken(output, token string) bool {
	for _, part := range strings.Split(output, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

// removeStdoutFromLogOutput parses a comma-separated log output string and
// removes any "stdout" entries, returning the remaining outputs joined.
func removeStdoutFromLogOutput(output string) string {
	if output == "" {
		return ""
	}
	parts := strings.Split(output, ",")
	var filtered []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "stdout" {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, ",")
}

func validateJSONMode(scrapersFlag []string, forceRefresh bool) error {
	if len(scrapersFlag) != 1 {
		return fmt.Errorf("json output requires --scrapers with exactly one scraper")
	}
	if strings.TrimSpace(scrapersFlag[0]) == "" {
		return fmt.Errorf("json output requires --scrapers with a non-empty scraper name")
	}
	if forceRefresh {
		return fmt.Errorf("json output is incompatible with --force")
	}
	return nil
}

// ErrJSONExit is a sentinel error that signals the JSON path already wrote
// its error envelope to stdout; the caller should exit 1 without printing
// anything else.
// ErrJSONExit is a sentinel error that signals the JSON path already wrote
// its error envelope to stdout; the caller should exit 1 without printing
// anything else.
var ErrJSONExit = fmt.Errorf("json error already emitted")

// writeJSONError marshals and writes a JSON error envelope to the command's stdout.
func writeJSONError(cmd *cobra.Command, wrap jsonErrorWrapper) {
	data, _ := json.Marshal(wrap)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
}
