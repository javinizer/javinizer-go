package scrape

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/formatter"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runScrape(cmd, args, configFile)
		},
	}
	scrapeCmd.Flags().StringSliceP("scrapers", "s", nil, "Comma-separated subset of enabled scrapers to use (e.g., 'r18dev,dmm'); scraper must be enabled in config.yaml")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers (clear cache)")
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

	// DMM-specific CLI flags
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

	// Actress database flags
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

	// Genre replacement flags
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
		// Build the factory from the effective config (cfg), which has env/CLI
		// overrides applied above — not deps.GetConfig(), which may hold a stale
		// snapshot and would drop the overrides for injected-dependency callers.
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

	result, _, err := wf.Scrape(ctx, scrape.ScrapeCmd{
		MovieID:          id,
		ForceRefresh:     forceRefresh,
		SelectedScrapers: scrapersFlag,
	}, nil)
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

// runScrape is the Cobra handler that calls Run() and formats output.
func runScrape(cmd *cobra.Command, args []string, configFile string) error {
	movie, results, err := Run(cmd.Context(), cmd, args, configFile, nil)
	if err != nil {
		return err
	}
	formatter.WriteMovie(cmd.OutOrStdout(), movie, results)
	return nil
}
