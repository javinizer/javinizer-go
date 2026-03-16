package scrape

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
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
	scrapeCmd.Flags().StringSliceP("scrapers", "s", nil, "Comma-separated list of scrapers to use (e.g., 'r18dev,dmm' or 'dmm')")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers (clear cache)")
	scrapeCmd.Flags().Bool("scrape-actress", false, "Enable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("no-scrape-actress", false, "Disable actress scraping (overrides config)")
	scrapeCmd.Flags().Bool("browser", false, "Enable browser mode for DMM video pages (overrides config)")
	scrapeCmd.Flags().Bool("no-browser", false, "Disable browser mode for DMM video pages (overrides config)")
	scrapeCmd.Flags().Int("browser-timeout", 0, "Browser timeout in seconds (overrides config, 0=use config)")

	// Deprecated flags (backward compatibility)
	scrapeCmd.Flags().Bool("headless", false, "DEPRECATED: Use --browser instead")
	scrapeCmd.Flags().Bool("no-headless", false, "DEPRECATED: Use --no-browser instead")
	scrapeCmd.Flags().Int("headless-timeout", 0, "DEPRECATED: Use --browser-timeout instead")
	_ = scrapeCmd.Flags().MarkDeprecated("headless", "use --browser instead")
	_ = scrapeCmd.Flags().MarkDeprecated("no-headless", "use --no-browser instead")
	_ = scrapeCmd.Flags().MarkDeprecated("headless-timeout", "use --browser-timeout instead")
	scrapeCmd.Flags().Bool("actress-db", false, "Enable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("no-actress-db", false, "Disable actress database lookup (overrides config)")
	scrapeCmd.Flags().Bool("genre-replacement", false, "Enable genre replacement (overrides config)")
	scrapeCmd.Flags().Bool("no-genre-replacement", false, "Disable genre replacement (overrides config)")
	return scrapeCmd
}

// ApplyFlagOverrides applies CLI flag overrides to the config.
// This is extracted for testability (Story 5.4 - Epic 5).
// Exported to enable comprehensive testing of flag parsing logic.
func ApplyFlagOverrides(cmd *cobra.Command, cfg *config.Config) {
	// Scrape actress flags
	if cmd.Flags().Changed("scrape-actress") {
		if val, _ := cmd.Flags().GetBool("scrape-actress"); val {
			cfg.Scrapers.DMM.ScrapeActress = true
		}
	}
	if cmd.Flags().Changed("no-scrape-actress") {
		if val, _ := cmd.Flags().GetBool("no-scrape-actress"); val {
			cfg.Scrapers.DMM.ScrapeActress = false
		}
	}

	// Browser mode flags (new) - take precedence over deprecated flags
	if cmd.Flags().Changed("browser") {
		if val, _ := cmd.Flags().GetBool("browser"); val {
			cfg.Scrapers.DMM.EnableBrowser = true
		}
	} else if cmd.Flags().Changed("headless") {
		// Backward compatibility
		if val, _ := cmd.Flags().GetBool("headless"); val {
			cfg.Scrapers.DMM.EnableBrowser = true
		}
	}
	if cmd.Flags().Changed("no-browser") {
		if val, _ := cmd.Flags().GetBool("no-browser"); val {
			cfg.Scrapers.DMM.EnableBrowser = false
		}
	} else if cmd.Flags().Changed("no-headless") {
		// Backward compatibility
		if val, _ := cmd.Flags().GetBool("no-headless"); val {
			cfg.Scrapers.DMM.EnableBrowser = false
		}
	}
	if cmd.Flags().Changed("browser-timeout") {
		if val, _ := cmd.Flags().GetInt("browser-timeout"); val > 0 {
			cfg.Scrapers.DMM.BrowserTimeout = val
		}
	} else if cmd.Flags().Changed("headless-timeout") {
		// Backward compatibility
		if val, _ := cmd.Flags().GetInt("headless-timeout"); val > 0 {
			cfg.Scrapers.DMM.BrowserTimeout = val
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
// Exported for testing purposes - allows testing business logic without console output.
// Following Epic 7 pattern from Story 7.1 (API command refactoring).
//
// Parameters:
//   - cmd: Cobra command for flag access
//   - args: Command arguments (JAV ID at args[0])
//   - configFile: Path to configuration file
//   - deps: Optional injected dependencies (for testing, pass nil for production use)
//
// Returns:
//   - *models.Movie: Scraped and aggregated movie metadata
//   - []*models.ScraperResult: Raw results from each scraper source
//   - error: Any error encountered during scraping process
func Run(cmd *cobra.Command, args []string, configFile string, deps *commandutil.Dependencies) (*models.Movie, []*models.ScraperResult, error) {
	id := args[0]

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply CLI flag overrides to config
	ApplyFlagOverrides(cmd, cfg)

	// Initialize or use injected dependencies
	var ownDeps bool
	if deps == nil {
		deps, err = commandutil.NewDependencies(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize dependencies: %w", err)
		}
		ownDeps = true
	}
	if ownDeps {
		defer func() { _ = deps.Close() }()
	}

	// Get force flag and scrapers override
	forceRefresh, _ := cmd.Flags().GetBool("force")
	scrapersFlag, _ := cmd.Flags().GetStringSlice("scrapers")

	// Initialize repositories
	movieRepo := database.NewMovieRepository(deps.DB)

	// Use injected scraper registry
	registry := deps.ScraperRegistry

	// Initialize aggregator with database support
	agg := aggregator.NewWithDatabase(deps.Config, deps.DB)

	logging.Infof("Scraping metadata for: %s", id)

	// Determine which scrapers to use: CLI flag overrides config
	scrapersToUse := deps.Config.Scrapers.Priority
	usingCustomScrapers := len(scrapersFlag) > 0
	if usingCustomScrapers {
		scrapersToUse = scrapersFlag
		logging.Infof("Using scrapers from CLI flag: %v", scrapersFlag)
	}

	// Force refresh - clear cache if requested
	if forceRefresh {
		if err := movieRepo.Delete(id); err != nil {
			logging.Debugf("Failed to delete %s from cache (may not exist): %v", id, err)
		} else {
			logging.Infof("🔄 Cache cleared for %s", id)
		}
	}

	// Check cache first (skip cache if user specified custom scrapers or force refresh)
	if !usingCustomScrapers && !forceRefresh {
		if movie, err := movieRepo.FindByID(id); err == nil {
			logging.Info("✅ Found in cache!")
			return movie, nil, nil
		}
	}

	// Phase 1: Content-ID Resolution using DMM
	logging.Info("🔍 Resolving content-ID using DMM...")
	var resolvedID string
	dmmScraper, exists := registry.Get("dmm")
	if exists {
		if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
			contentID, err := dmmScraperTyped.ResolveContentID(id)
			if err != nil {
				logging.Debugf("DMM content-ID resolution failed: %v, will use original ID", err)
				resolvedID = id // Fallback to original ID
			} else {
				resolvedID = contentID
				logging.Infof("✅ Resolved content-ID: %s", resolvedID)
			}
		} else {
			logging.Debug("DMM scraper type assertion failed, using original ID")
			resolvedID = id
		}
	} else {
		logging.Debug("DMM scraper not available, using original ID")
		resolvedID = id
	}

	// Phase 2: Scrape from sources in priority order
	results := []*models.ScraperResult{}

	for _, scraper := range registry.GetByPriority(scrapersToUse) {
		logging.Infof("Scraping %s...", scraper.Name())
		result, err := scraper.Search(resolvedID)
		if err != nil {
			logging.Warnf("❌ %s: %v", scraper.Name(), err)
			// If scraping with resolved ID fails, try with original ID before giving up
			if resolvedID != id {
				logging.Debugf("Retrying %s with original ID: %s", scraper.Name(), id)
				result, err = scraper.Search(id)
				if err != nil {
					logging.Warnf("❌ %s (with original ID): %v", scraper.Name(), err)
					continue
				}
			} else {
				continue
			}
		}
		logging.Info("✅")
		results = append(results, result)
	}

	if len(results) == 0 {
		logging.Error("❌ No results found from any scraper")
		return nil, nil, fmt.Errorf("no results found from any scraper")
	}

	logging.Infof("✅ Found %d source(s)", len(results))

	// Aggregate results
	// When users provide --scrapers, honor that order for all fields so
	// metadata priorities don't accidentally exclude the selected source(s).
	var movie *models.Movie
	if usingCustomScrapers {
		movie, err = agg.AggregateWithPriority(results, scrapersToUse)
	} else {
		movie, err = agg.Aggregate(results)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to aggregate: %w", err)
	}

	movie.OriginalFileName = id

	// Save to database (upsert: create or update)
	if err := movieRepo.Upsert(movie); err != nil {
		logging.Warnf("Failed to save to database: %v", err)
	} else {
		fmt.Println("💾 Saved to database")
	}

	return movie, results, nil
}

// runScrape is the private Cobra handler that calls Run() and handles console output.
// This isolates the testable business logic (Run) from the non-testable console formatting.
func runScrape(cmd *cobra.Command, args []string, configFile string) error {
	movie, results, err := Run(cmd, args, configFile, nil)
	if err != nil {
		return err
	}

	printMovie(movie, results)
	return nil
}

// printMovie displays movie metadata in a formatted table
func printMovie(movie *models.Movie, results []*models.ScraperResult) {
	fmt.Println()

	// Build table rows
	rows := [][]string{}

	// ID and Content ID
	rows = append(rows, []string{"ID", movie.ID})
	if movie.ContentID != "" && movie.ContentID != movie.ID {
		rows = append(rows, []string{"ContentID", movie.ContentID})
	}

	// Title
	if movie.Title != "" {
		rows = append(rows, []string{"Title", movie.Title})
	}

	// Release Date
	if movie.ReleaseDate != nil {
		rows = append(rows, []string{"ReleaseDate", movie.ReleaseDate.Format("2006-01-02")})
	}

	// Runtime
	if movie.Runtime > 0 {
		rows = append(rows, []string{"Runtime", fmt.Sprintf("%d min", movie.Runtime)})
	}

	// Director
	if movie.Director != "" {
		rows = append(rows, []string{"Director", movie.Director})
	}

	// Maker
	if movie.Maker != "" {
		rows = append(rows, []string{"Maker", movie.Maker})
	}

	// Label
	if movie.Label != "" {
		rows = append(rows, []string{"Label", movie.Label})
	}

	// Series
	if movie.Series != "" {
		rows = append(rows, []string{"Series", movie.Series})
	}

	// Rating
	if movie.RatingScore > 0 {
		rows = append(rows, []string{"Rating", fmt.Sprintf("%.1f/10 (%d votes)", movie.RatingScore, movie.RatingVotes)})
	}

	// Actresses - show detailed information
	if len(movie.Actresses) > 0 {
		actressHeader := fmt.Sprintf("Actresses (%d)", len(movie.Actresses))
		rows = append(rows, []string{actressHeader, ""})

		for i, actress := range movie.Actresses {
			// Build actress name with Japanese
			name := actress.FullName()
			if actress.JapaneseName != "" {
				name += fmt.Sprintf(" (%s)", actress.JapaneseName)
			}

			// Build actress info line with number and DMM ID
			actressLine := fmt.Sprintf("  [%d] %s", i+1, name)
			if actress.DMMID > 0 {
				actressLine += fmt.Sprintf(" - ID: %d", actress.DMMID)
			}
			rows = append(rows, []string{"", actressLine})

			// Add thumbnail URL on separate line if available
			if actress.ThumbURL != "" {
				rows = append(rows, []string{"", fmt.Sprintf("      Thumb: %s", actress.ThumbURL)})
			}
		}
	}

	// Genres
	if len(movie.Genres) > 0 {
		genreNames := make([]string, 0, len(movie.Genres))
		for i, genre := range movie.Genres {
			if i < 8 || len(movie.Genres) <= 9 {
				genreNames = append(genreNames, genre.Name)
			} else if i == 8 {
				genreNames = append(genreNames, fmt.Sprintf("... and %d more", len(movie.Genres)-8))
				break
			}
		}
		rows = append(rows, []string{"Genres", strings.Join(genreNames, ", ")})
	}

	// Translations
	if len(movie.Translations) > 1 {
		langNames := []string{}
		for _, trans := range movie.Translations {
			langName := map[string]string{
				"en": "English",
				"ja": "Japanese",
				"zh": "Chinese",
				"ko": "Korean",
			}[trans.Language]
			if langName == "" {
				langName = trans.Language
			}
			langNames = append(langNames, fmt.Sprintf("%s (%s)", langName, trans.SourceName))
		}
		rows = append(rows, []string{"Translations", strings.Join(langNames, ", ")})
	}

	// Sources - collect unique sources from translations
	sourcesMap := make(map[string]bool)
	var sources []string

	// Add sources from translations
	for _, trans := range movie.Translations {
		if trans.SourceName != "" && !sourcesMap[trans.SourceName] {
			sourcesMap[trans.SourceName] = true
			sources = append(sources, trans.SourceName)
		}
	}

	// If no translations, fall back to movie.SourceName
	if len(sources) == 0 && movie.SourceName != "" {
		sources = append(sources, movie.SourceName)
	}

	// Display sources (names only in the main table)
	if len(sources) > 0 {
		rows = append(rows, []string{"Sources", strings.Join(sources, ", ")})
	}

	// Calculate column widths
	maxLabelWidth := 0
	for _, row := range rows {
		if len(row[0]) > maxLabelWidth {
			maxLabelWidth = len(row[0])
		}
	}

	// Terminal width for wrapping (default 120, can be adjusted)
	terminalWidth := 120
	valueWidth := terminalWidth - maxLabelWidth - 5 // Account for label, " : ", and padding

	// Helper function to wrap text to specified width
	wrapText := func(text string, width int) []string {
		if width <= 0 {
			width = 80
		}
		words := strings.Fields(text)
		if len(words) == 0 {
			return []string{""}
		}

		var lines []string
		currentLine := ""

		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
		return lines
	}

	// Print table header
	fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))

	// Print rows with proper wrapping
	for _, row := range rows {
		label := row[0]
		value := row[1]

		// For multi-line values (description, media URLs), wrap them
		lines := wrapText(value, valueWidth)

		for i, line := range lines {
			if i == 0 {
				// First line: show label
				paddedLabel := label + strings.Repeat(" ", maxLabelWidth-len(label))
				fmt.Printf("%-*s : %s\n", maxLabelWidth, paddedLabel, line)
			} else {
				// Continuation lines: indent to align with first line's value
				fmt.Printf("%*s   %s\n", maxLabelWidth, "", line)
			}
		}
	}

	// Print Source URLs section (if we have scraperResults from fresh scrape)
	if len(results) > 0 {
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Source URLs:")
		fmt.Println()

		for _, result := range results {
			fmt.Printf("  %-12s : %s\n", result.Source, result.SourceURL)
		}
	}

	// Now print expanded media section
	if movie.CoverURL != "" || movie.PosterURL != "" || len(movie.Screenshots) > 0 || movie.TrailerURL != "" {
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Media URLs:")
		fmt.Println()

		if movie.CoverURL != "" {
			fmt.Printf("  Cover URL    : %s\n", movie.CoverURL)
		}
		if movie.PosterURL != "" && movie.PosterURL != movie.CoverURL {
			fmt.Printf("  Poster URL   : %s\n", movie.PosterURL)
		}
		if movie.TrailerURL != "" {
			fmt.Printf("  Trailer URL  : %s\n", movie.TrailerURL)
		}
		if len(movie.Screenshots) > 0 {
			fmt.Printf("  Screenshots  : %d total\n", len(movie.Screenshots))
			for i, url := range movie.Screenshots {
				fmt.Printf("    [%2d] %s\n", i+1, url)
			}
		}
	}

	// Description section (full text, properly wrapped)
	if movie.Description != "" {
		fmt.Println()
		fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
		fmt.Println()
		fmt.Println("Description:")
		fmt.Println()

		// Wrap description to terminal width with some padding
		descLines := wrapText(movie.Description, terminalWidth-4)
		for _, line := range descLines {
			fmt.Printf("  %s\n", line)
		}
	}

	// Print table footer
	fmt.Println()
	fmt.Println(strings.Repeat("-", maxLabelWidth+2) + " " + strings.Repeat("-", 100))
	fmt.Println()
}
