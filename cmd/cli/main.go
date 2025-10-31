package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	cfg          *config.Config
	scrapersFlag []string
	verboseFlag  bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "javinizer",
		Short:   "Javinizer - JAV metadata scraper and organizer",
		Long:    `A metadata scraper and file organizer for Japanese Adult Videos (JAV)`,
		Version: version.Short(),
	}

	// Customize version template to show full build info
	rootCmd.SetVersionTemplate(version.Info() + "\n")

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "enable debug logging")

	// Scrape command
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runScrape),
	}
	scrapeCmd.Flags().StringSliceVarP(&scrapersFlag, "scrapers", "s", nil, "Comma-separated list of scrapers to use (e.g., 'r18dev,dmm' or 'dmm')")
	scrapeCmd.Flags().BoolP("force", "f", false, "Force refresh metadata from scrapers (clear cache)")

	// Info command
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show configuration and scraper information",
		RunE:  runWithConfig(runInfo),
	}

	// Init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration and database",
		RunE:  runWithDeps(runInit),
	}

	// Sort command
	sortCmd := &cobra.Command{
		Use:   "sort [path]",
		Short: "Scan, scrape, and organize video files",
		Long:  `Scans a directory for video files, scrapes metadata, generates NFO files, downloads media, and organizes files`,
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runSort),
	}
	sortCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	sortCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	sortCmd.Flags().StringP("dest", "d", "", "Destination directory (default: same as source)")
	sortCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")
	sortCmd.Flags().BoolP("nfo", "", true, "Generate NFO files")
	sortCmd.Flags().BoolP("download", "", true, "Download media (covers, screenshots, etc.)")
	sortCmd.Flags().Bool("extrafanart", false, "Download extrafanart (screenshots)")
	sortCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority (comma-separated, e.g., 'r18dev,dmm')")
	sortCmd.Flags().BoolP("force-update", "f", false, "Force update existing files")
	sortCmd.Flags().Bool("force-refresh", false, "Force refresh metadata from scrapers (clear cache)")
	sortCmd.Flags().BoolP("update", "u", false, "Update mode: only create/update metadata files without moving video files")

	// Genre command with subcommands
	genreCmd := &cobra.Command{
		Use:   "genre",
		Short: "Manage genre replacements",
		Long:  `Manage genre name replacements for customizing genre names from scrapers`,
	}

	genreAddCmd := &cobra.Command{
		Use:   "add <original> <replacement>",
		Short: "Add a genre replacement",
		Args:  cobra.ExactArgs(2),
		RunE:  runWithDeps(runGenreAdd),
	}

	genreListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all genre replacements",
		RunE:  runWithDeps(runGenreList),
	}

	genreRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a genre replacement",
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runGenreRemove),
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd)

	// Tag command with subcommands
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage per-movie tags",
		Long:  `Manage custom tags for individual movies (stored in database, appear in NFO files)`,
	}

	tagAddCmd := &cobra.Command{
		Use:   "add <movie_id> <tag> [tag2] [tag3]...",
		Short: "Add tag(s) to a movie",
		Long: `Add one or more tags to a specific movie. Tags will appear in the movie's NFO file.

Examples:
  javinizer tag add IPX-535 "Favorite" "Uncensored"
  javinizer tag add ABC-123 "Collection: Summer 2023"`,
		Args: cobra.MinimumNArgs(2),
		RunE: runWithDeps(runTagAdd),
	}

	tagListCmd := &cobra.Command{
		Use:   "list [movie_id]",
		Short: "List tags for a movie or all tag mappings",
		Long: `List tags for a specific movie, or show all tag mappings if no movie ID provided.

Examples:
  javinizer tag list              # Show all tag mappings
  javinizer tag list IPX-535      # Show tags for IPX-535`,
		Args: cobra.MaximumNArgs(1),
		RunE: runWithDeps(runTagList),
	}

	tagRemoveCmd := &cobra.Command{
		Use:   "remove <movie_id> [tag]",
		Short: "Remove tag(s) from a movie",
		Long: `Remove a specific tag from a movie, or all tags if no tag specified.

Examples:
  javinizer tag remove IPX-535 "Favorite"    # Remove one tag
  javinizer tag remove IPX-535               # Remove all tags`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runWithDeps(runTagRemove),
	}

	tagSearchCmd := &cobra.Command{
		Use:   "search <tag>",
		Short: "Find all movies with a specific tag",
		Long: `Search for all movies that have been tagged with the specified tag.

Example:
  javinizer tag search "Favorite"`,
		Args: cobra.ExactArgs(1),
		RunE: runWithDeps(runTagSearch),
	}

	tagAllTagsCmd := &cobra.Command{
		Use:   "tags",
		Short: "List all unique tags in database",
		RunE:  runWithDeps(runTagAllTags),
	}

	tagCmd.AddCommand(tagAddCmd, tagListCmd, tagRemoveCmd, tagSearchCmd, tagAllTagsCmd)

	// History command with subcommands
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View operation history",
		Long:  `View and manage the history of scrape, organize, download, and NFO operations`,
	}

	historyListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent operations",
		RunE:  runWithDeps(runHistoryList),
	}
	historyListCmd.Flags().IntP("limit", "n", 20, "Number of records to show")
	historyListCmd.Flags().StringP("operation", "o", "", "Filter by operation type (scrape, organize, download, nfo)")
	historyListCmd.Flags().StringP("status", "s", "", "Filter by status (success, failed, reverted)")

	historyStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show operation statistics",
		RunE:  runWithDeps(runHistoryStats),
	}

	historyMovieCmd := &cobra.Command{
		Use:   "movie <id>",
		Short: "Show history for a specific movie",
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runHistoryMovie),
	}

	historyCleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean up old history records",
		RunE:  runWithDeps(runHistoryClean),
	}
	historyCleanCmd.Flags().IntP("days", "d", 30, "Delete records older than this many days")

	historyCmd.AddCommand(historyListCmd, historyStatsCmd, historyMovieCmd, historyCleanCmd)

	// TUI command
	tuiCmd := createTUICommand()

	// API command
	apiCmd := newAPICmd()

	rootCmd.AddCommand(scrapeCmd, infoCmd, initCmd, sortCmd, genreCmd, tagCmd, historyCmd, tuiCmd, apiCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func loadConfig() error {
	var err error
	cfg, err = config.LoadOrCreate(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

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

	// Validate proxy configuration
	if cfg.Scrapers.Proxy.Enabled {
		if cfg.Scrapers.Proxy.URL == "" {
			logging.Warn("Scraper proxy is enabled but URL is empty, disabling proxy")
			cfg.Scrapers.Proxy.Enabled = false
		} else {
			// Sanitize URL to avoid logging credentials
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
			// Sanitize URL to avoid logging credentials
			sanitizedURL := cfg.Output.DownloadProxy.URL
			if u, err := url.Parse(sanitizedURL); err == nil && u.User != nil {
				u.User = url.User("[REDACTED]")
				sanitizedURL = u.String()
			}
			logging.Infof("Download proxy enabled: %s", sanitizedURL)
		}
	}

	return nil
}

func runScrape(cmd *cobra.Command, args []string, deps *Dependencies) error {
	id := args[0]

	// Get force flag
	forceRefresh, _ := cmd.Flags().GetBool("force")

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
			printMovie(movie, nil)
			return nil
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
		return fmt.Errorf("no results found from any scraper")
	}

	logging.Infof("✅ Found %d source(s)", len(results))

	// Aggregate results
	movie, err := agg.Aggregate(results)
	if err != nil {
		return fmt.Errorf("failed to aggregate: %w", err)
	}

	movie.OriginalFileName = id

	// Save to database (upsert: create or update)
	if err := movieRepo.Upsert(movie); err != nil {
		logging.Warnf("Failed to save to database: %v", err)
	} else {
		fmt.Println("💾 Saved to database")
	}

	printMovie(movie, results)
	return nil
}

func runInfo(cmd *cobra.Command, args []string, cfg *config.Config) error {
	fmt.Println("=== Javinizer Configuration ===")
	fmt.Printf("Config file: %s\n", cfgFile)
	fmt.Printf("Database: %s (%s)\n", cfg.Database.DSN, cfg.Database.Type)
	fmt.Printf("Server: %s:%d\n\n", cfg.Server.Host, cfg.Server.Port)

	fmt.Println("Scrapers:")
	fmt.Printf("  Priority: %v\n", cfg.Scrapers.Priority)
	fmt.Printf("  - R18.dev: %v\n", cfg.Scrapers.R18Dev.Enabled)
	fmt.Printf("  - DMM: %v (scrape_actress: %v)\n\n", cfg.Scrapers.DMM.Enabled, cfg.Scrapers.DMM.ScrapeActress)

	fmt.Println("Output:")
	fmt.Printf("  - Folder format: %s\n", cfg.Output.FolderFormat)
	fmt.Printf("  - File format: %s\n", cfg.Output.FileFormat)
	fmt.Printf("  - Download cover: %v\n", cfg.Output.DownloadCover)
	fmt.Printf("  - Download extrafanart: %v\n", cfg.Output.DownloadExtrafanart)

	return nil
}

func runInit(cmd *cobra.Command, args []string, deps *Dependencies) error {
	fmt.Println("Initializing Javinizer...")

	// Create data directory
	dataDir := filepath.Dir(deps.Config.Database.DSN)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	fmt.Printf("✅ Created data directory: %s\n", dataDir)

	// Database is already initialized via deps, just run migrations
	if err := deps.DB.AutoMigrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	fmt.Printf("✅ Initialized database: %s\n", deps.Config.Database.DSN)

	// Save config if it was just created
	if err := config.Save(deps.Config, cfgFile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Printf("✅ Saved configuration: %s\n", cfgFile)

	fmt.Println("\n🎉 Initialization complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Run 'javinizer scrape IPX-535' to test scraping")
	fmt.Println("  - Run 'javinizer info' to view configuration")

	return nil
}

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

	// Actresses
	if len(movie.Actresses) > 0 {
		actressNames := []string{}
		for i, actress := range movie.Actresses {
			name := actress.FullName()
			if actress.JapaneseName != "" {
				name += fmt.Sprintf(" (%s)", actress.JapaneseName)
			}
			if i < 3 || len(movie.Actresses) <= 4 {
				actressNames = append(actressNames, name)
			} else if i == 3 {
				actressNames = append(actressNames, fmt.Sprintf("... and %d more", len(movie.Actresses)-3))
				break
			}
		}
		rows = append(rows, []string{"Actresses", strings.Join(actressNames, ", ")})
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
	if results != nil && len(results) > 0 {
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

func runSort(cmd *cobra.Command, args []string, deps *Dependencies) error {
	sourcePath := args[0]

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	recursive, _ := cmd.Flags().GetBool("recursive")
	destPath, _ := cmd.Flags().GetString("dest")
	moveFiles, _ := cmd.Flags().GetBool("move")
	generateNFO, _ := cmd.Flags().GetBool("nfo")
	downloadMedia, _ := cmd.Flags().GetBool("download")
	downloadExtrafanart, _ := cmd.Flags().GetBool("extrafanart")
	scraperPriority, _ := cmd.Flags().GetStringSlice("scrapers")
	forceUpdate, _ := cmd.Flags().GetBool("force-update")
	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	updateMode, _ := cmd.Flags().GetBool("update")

	// Default destination is same as source
	if destPath == "" {
		destPath = sourcePath
	}

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		deps.Config.Output.DownloadExtrafanart = true
	}

	// Override config with flag if scraper priority is provided
	if len(scraperPriority) > 0 {
		deps.Config.Scrapers.Priority = scraperPriority
	}

	// Initialize components
	movieRepo := database.NewMovieRepository(deps.DB)

	registry := deps.ScraperRegistry

	agg := aggregator.NewWithDatabase(deps.Config, deps.DB)

	fileScanner := scanner.NewScanner(&deps.Config.Matching)
	fileMatcher, err := matcher.NewMatcher(&deps.Config.Matching)
	if err != nil {
		return fmt.Errorf("failed to create matcher: %w", err)
	}

	fileOrganizer := organizer.NewOrganizer(&deps.Config.Output)
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&deps.Config.Metadata.NFO, &deps.Config.Output, &deps.Config.Metadata, deps.DB))
	mediaDownloader := downloader.NewDownloader(&deps.Config.Output, deps.Config.Scrapers.UserAgent)

	// Print configuration
	fmt.Println("=== Javinizer Sort ===")
	fmt.Printf("Source: %s\n", sourcePath)
	fmt.Printf("Destination: %s\n", destPath)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[dryRun])
	fmt.Printf("Operation: %s\n", map[bool]string{true: "MOVE", false: "COPY"}[moveFiles])
	fmt.Printf("Generate NFO: %v\n", generateNFO)
	fmt.Printf("Download Media: %v\n\n", downloadMedia)

	// Step 1: Scan for video files
	fmt.Println("📂 Scanning for video files...")
	var scanResult *scanner.ScanResult
	if recursive {
		scanResult, err = fileScanner.Scan(sourcePath)
	} else {
		scanResult, err = fileScanner.ScanSingle(sourcePath)
	}
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	fmt.Printf("   Found %d video file(s)\n", len(scanResult.Files))
	if len(scanResult.Skipped) > 0 {
		fmt.Printf("   Skipped %d file(s)\n", len(scanResult.Skipped))
	}
	if len(scanResult.Errors) > 0 {
		fmt.Printf("   ⚠️  %d error(s) during scan\n", len(scanResult.Errors))
	}

	if len(scanResult.Files) == 0 {
		fmt.Println("\n✅ No files to process")
		return nil
	}

	// Step 2: Match JAV IDs
	fmt.Println("\n🔍 Extracting JAV IDs...")
	matches := fileMatcher.Match(scanResult.Files)
	fmt.Printf("   Matched %d file(s)\n", len(matches))

	if len(matches) == 0 {
		fmt.Println("\n⚠️  No JAV IDs found in filenames")
		return nil
	}

	// Group by ID
	grouped := matcher.GroupByID(matches)
	fmt.Printf("   Found %d unique ID(s)\n", len(grouped))

	// Step 3: Scrape metadata
	fmt.Println("\n🌐 Scraping metadata...")
	movies := make(map[string]*models.Movie)
	scrapedCount := 0
	cachedCount := 0

	for id := range grouped {
		fmt.Printf("   %s... ", id)

		// Force refresh - clear cache if requested
		if forceRefresh {
			if err := movieRepo.Delete(id); err != nil {
				logging.Debugf("Failed to delete %s from cache (may not exist): %v", id, err)
			} else {
				logging.Debugf("[%s] Cache cleared successfully", id)
			}
		}

		// Check cache first (skip if force refresh)
		if !forceRefresh {
			if movie, err := movieRepo.FindByID(id); err == nil {
				movies[id] = movie
				cachedCount++
				fmt.Println("✅ (cached)")
				logging.Debugf("[%s] Found in cache: Title=%s, Maker=%s, Actresses=%d",
					id, movie.Title, movie.Maker, len(movie.Actresses))
				continue
			}
		}

		logging.Debugf("[%s] Not found in cache, scraping from sources", id)

		// Scrape from sources
		results := []*models.ScraperResult{}
		scrapers := registry.GetByPriority(deps.Config.Scrapers.Priority)
		logging.Debugf("[%s] Initialized %d scrapers in priority order", id, len(scrapers))

		for _, scraper := range scrapers {
			logging.Debugf("[%s] Querying scraper: %s", id, scraper.Name())
			if result, err := scraper.Search(id); err == nil {
				logging.Debugf("[%s] Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
					id, scraper.Name(), result.Title, result.Language, len(result.Actresses), len(result.Genres))
				results = append(results, result)
			} else {
				logging.Debugf("[%s] Scraper %s failed: %v", id, scraper.Name(), err)
			}
		}

		if len(results) == 0 {
			fmt.Println("❌ (not found)")
			logging.Debugf("[%s] No results from any scraper", id)
			continue
		}

		logging.Debugf("[%s] Collected %d results from scrapers, starting aggregation", id, len(results))

		// Aggregate and save
		movie, err := agg.Aggregate(results)
		if err != nil {
			fmt.Printf("❌ (aggregate error: %v)\n", err)
			logging.Debugf("[%s] Aggregation failed: %v", id, err)
			continue
		}

		// Log aggregated metadata details
		logging.Debugf("[%s] Aggregation complete - Final metadata:", id)
		logging.Debugf("[%s]   Title: %s", id, movie.Title)
		logging.Debugf("[%s]   Maker: %s", id, movie.Maker)
		logging.Debugf("[%s]   Release Date: %v", id, movie.ReleaseDate)
		logging.Debugf("[%s]   Runtime: %d min", id, movie.Runtime)
		logging.Debugf("[%s]   Actresses: %d", id, len(movie.Actresses))
		if len(movie.Actresses) > 0 {
			actressNames := make([]string, len(movie.Actresses))
			for i, a := range movie.Actresses {
				actressNames[i] = a.FullName()
			}
			logging.Debugf("[%s]   Actress Names: %v", id, actressNames)
		}
		logging.Debugf("[%s]   Genres: %d", id, len(movie.Genres))
		if len(movie.Genres) > 0 {
			genreNames := make([]string, len(movie.Genres))
			for i, g := range movie.Genres {
				genreNames[i] = g.Name
			}
			logging.Debugf("[%s]   Genre Names: %v", id, genreNames)
		}
		logging.Debugf("[%s]   Screenshots: %d", id, len(movie.Screenshots))
		logging.Debugf("[%s]   Cover URL: %s", id, movie.CoverURL)
		logging.Debugf("[%s]   Trailer URL: %s", id, movie.TrailerURL)

		if err := movieRepo.Upsert(movie); err != nil {
			logging.Infof("Warning: Failed to save %s to database: %v", id, err)
		}

		movies[id] = movie
		scrapedCount++
		fmt.Println("✅ (scraped)")
	}

	fmt.Printf("   Scraped: %d, Cached: %d, Failed: %d\n", scrapedCount, cachedCount, len(grouped)-len(movies))

	if len(movies) == 0 {
		fmt.Println("\n⚠️  No metadata found")
		return nil
	}

	// Step 4: Generate NFO files
	if generateNFO && deps.Config.Metadata.NFO.Enabled {
		fmt.Println("\n📝 Generating NFO files...")
		nfoCount := 0

		for id, movie := range movies {
			// Find all matches for this ID
			var idMatches []matcher.MatchResult
			for _, m := range matches {
				if m.ID == id {
					idMatches = append(idMatches, m)
				}
			}

			// Determine output directory: either organized folder or source directory
			var outputDir string
			if deps.Config.Output.MoveToFolder {
				// Create destination folder for this movie (use first match for planning)
				plan, err := fileOrganizer.Plan(idMatches[0], movie, destPath, forceUpdate)
				if err != nil {
					logging.Infof("Failed to plan for %s: %v", id, err)
					continue
				}
				outputDir = plan.TargetDir
			} else {
				// Use source directory (directory of the first file)
				outputDir = idMatches[0].File.Dir
			}

			// If per_file is enabled and this is multi-part, generate NFO for each part
			if deps.Config.Metadata.NFO.PerFile && len(idMatches) > 1 {
				for _, match := range idMatches {
					partSuffix := ""
					if match.IsMultiPart {
						partSuffix = match.PartSuffix
					}

					if dryRun {
						fmt.Printf("   %s%s.nfo (would generate)\n", id, partSuffix)
					} else {
						if err := nfoGenerator.Generate(movie, outputDir, partSuffix, ""); err != nil {
							logging.Infof("Failed to generate NFO for %s%s: %v", id, partSuffix, err)
						} else {
							nfoCount++
							fmt.Printf("   %s%s.nfo ✅\n", id, partSuffix)
						}
					}
				}
			} else {
				// Single NFO for all parts (or single file)
				if dryRun {
					fmt.Printf("   %s.nfo (would generate)\n", id)
				} else {
					if err := nfoGenerator.Generate(movie, outputDir, "", ""); err != nil {
						logging.Infof("Failed to generate NFO for %s: %v", id, err)
					} else {
						nfoCount++
						fmt.Printf("   %s.nfo ✅\n", id)
					}
				}
			}
		}

		if dryRun {
			fmt.Printf("   Would generate %d NFO file(s)\n", len(movies))
		} else {
			fmt.Printf("   Generated %d NFO file(s)\n", nfoCount)
		}
	}

	// Step 5: Download media
	if downloadMedia {
		fmt.Println("\n📥 Downloading media...")
		downloadCount := 0

		for id, movie := range movies {
			// Find all matches for this ID
			var idMatches []matcher.MatchResult
			for _, m := range matches {
				if m.ID == id {
					idMatches = append(idMatches, m)
				}
			}
			if len(idMatches) == 0 {
				continue
			}
			firstMatch := idMatches[0]

			// Determine output directory: either organized folder or source directory
			var downloadDir string
			if deps.Config.Output.MoveToFolder {
				plan, err := fileOrganizer.Plan(firstMatch, movie, destPath, forceUpdate)
				if err != nil {
					continue
				}
				downloadDir = plan.TargetDir
			} else {
				// Use source directory
				downloadDir = firstMatch.File.Dir
			}

			if dryRun {
				count := 0
				if deps.Config.Output.DownloadCover {
					count++
					logging.Debugf("[%s] Would download cover from: %s", id, movie.CoverURL)
				}
				if deps.Config.Output.DownloadExtrafanart {
					count += len(movie.Screenshots)
					logging.Debugf("[%s] Would download %d screenshots", id, len(movie.Screenshots))
				}
				fmt.Printf("   %s: would download ~%d file(s)\n", id, count)
			} else {
				logging.Debugf("[%s] Starting download to: %s", id, downloadDir)
				// Use PartNumber for deduplication (0 for single file, 1+ for multi-part)
				// Find the lowest part number to determine if we should download shared media
				partNumber := 0
				if firstMatch.IsMultiPart {
					// For multi-part, find the lowest part number among all matches
					minPartNumber := idMatches[0].PartNumber
					for _, m := range idMatches {
						if m.PartNumber < minPartNumber {
							minPartNumber = m.PartNumber
						}
					}
					// Clamp to 1 so even if only later segments exist (e.g., only pt2),
					// we still download shared media once
					if minPartNumber > 1 {
						minPartNumber = 1
					}
					partNumber = minPartNumber
				}
				results, err := mediaDownloader.DownloadAll(movie, downloadDir, partNumber)
				if err != nil {
					logging.Infof("Download error for %s: %v", id, err)
				}

				downloaded := 0
				skipped := 0
				failed := 0
				for _, r := range results {
					if r.Downloaded {
						downloaded++
						logging.Debugf("[%s] Downloaded %s: %s (%d bytes in %v)", id, r.Type, r.LocalPath, r.Size, r.Duration)
					} else if r.Error != nil {
						failed++
						logging.Debugf("[%s] Failed to download %s: %v", id, r.Type, r.Error)
					} else {
						skipped++
						logging.Debugf("[%s] Skipped %s (already exists): %s", id, r.Type, r.LocalPath)
					}
				}
				logging.Debugf("[%s] Download summary: %d downloaded, %d skipped, %d failed", id, downloaded, skipped, failed)
				if downloaded > 0 {
					downloadCount += downloaded
					fmt.Printf("   %s: %d file(s) ✅\n", id, downloaded)
				}
			}
		}

		if !dryRun {
			fmt.Printf("   Downloaded %d file(s)\n", downloadCount)
		}
	}

	// Step 6: Organize files (skip in update mode or if move_to_folder is disabled)
	organizedCount := 0
	if !updateMode && deps.Config.Output.MoveToFolder {
		fmt.Println("\n📦 Organizing files...")

		for _, match := range matches {
			movie, exists := movies[match.ID]
			if !exists {
				continue
			}

			logging.Debugf("[%s] Starting organize for: %s", match.ID, match.File.Path)
			logging.Debugf("[%s] Destination: %s, Move: %v, ForceUpdate: %v, DryRun: %v",
				match.ID, destPath, moveFiles, forceUpdate, dryRun)

			plan, err := fileOrganizer.Plan(match, movie, destPath, forceUpdate)
			if err != nil {
				logging.Infof("Failed to plan %s: %v", match.File.Name, err)
				logging.Debugf("[%s] Planning failed: %v", match.ID, err)
				continue
			}

			logging.Debugf("[%s] Organization plan created:", match.ID)
			logging.Debugf("[%s]   Source: %s", match.ID, plan.SourcePath)
			logging.Debugf("[%s]   Target Dir: %s", match.ID, plan.TargetDir)
			logging.Debugf("[%s]   Target File: %s", match.ID, plan.TargetFile)
			logging.Debugf("[%s]   Target Path: %s", match.ID, plan.TargetPath)
			logging.Debugf("[%s]   Will Move: %v", match.ID, plan.WillMove)
			logging.Debugf("[%s]   Conflicts: %d", match.ID, len(plan.Conflicts))

			// Validate plan (skip if force update)
			if !forceUpdate {
				if issues := organizer.ValidatePlan(plan); len(issues) > 0 {
					fmt.Printf("   ⚠️  %s: %v\n", match.File.Name, issues)
					logging.Debugf("[%s] Validation failed with %d issues: %v", match.ID, len(issues), issues)
					continue
				}
			}
			logging.Debugf("[%s] Plan validated successfully", match.ID)

			var result *organizer.OrganizeResult
			operation := "COPY"
			if moveFiles {
				operation = "MOVE"
				logging.Debugf("[%s] Executing MOVE operation", match.ID)
				result, err = fileOrganizer.Execute(plan, dryRun)
			} else {
				logging.Debugf("[%s] Executing COPY operation", match.ID)
				result, err = fileOrganizer.Copy(plan, dryRun)
			}

			if err != nil {
				fmt.Printf("   ❌ %s: %v\n", match.File.Name, err)
				logging.Debugf("[%s] Organize execution failed: %v", match.ID, err)
				continue
			}

			if result.Error != nil {
				logging.Debugf("[%s] Organize result contains error: %v", match.ID, result.Error)
			}

			if result.Moved || dryRun {
				organizedCount++
				status := "✅"
				if dryRun {
					status = "→"
					logging.Debugf("[%s] DRY RUN mode - would %s file to %s", match.ID, operation, plan.TargetPath)
				} else {
					logging.Debugf("[%s] File organized successfully to: %s", match.ID, result.NewPath)
				}
				fmt.Printf("   %s %s\n      %s\n", status, match.File.Name, plan.TargetPath)
			}
		}
	} else {
		fmt.Println("\n📝 Update mode: Files will remain in their original locations")
		fmt.Printf("   Source directory: %s\n", sourcePath)
	}

	if !updateMode {
		if dryRun {
			fmt.Printf("\n   Would organize %d file(s)\n", organizedCount)
		} else {
			fmt.Printf("\n   Organized %d file(s)\n", organizedCount)
		}
	}

	// Summary
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Files scanned: %d\n", len(scanResult.Files))
	fmt.Printf("IDs matched: %d\n", len(matches))
	fmt.Printf("Metadata found: %d\n", len(movies))
	if generateNFO {
		fmt.Printf("NFOs generated: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", len(movies)), false: fmt.Sprintf("%d", len(movies))}[dryRun])
	}
	if !updateMode {
		fmt.Printf("Files organized: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", organizedCount), false: fmt.Sprintf("%d", organizedCount)}[dryRun])
	} else {
		fmt.Printf("Mode: Update (metadata only, files remain in place)\n")
	}

	if dryRun {
		fmt.Println("\n💡 Run without --dry-run to apply changes")
	} else {
		if updateMode {
			fmt.Println("\n✅ Update complete!")
		} else {
			fmt.Println("\n✅ Sort complete!")
		}
	}

	return nil
}

func runGenreAdd(cmd *cobra.Command, args []string, deps *Dependencies) error {
	original := args[0]
	replacement := args[1]

	repo := database.NewGenreReplacementRepository(deps.DB)

	genreReplacement := &models.GenreReplacement{
		Original:    original,
		Replacement: replacement,
	}

	if err := repo.Upsert(genreReplacement); err != nil {
		return fmt.Errorf("Failed to add genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement added: '%s' → '%s'\n", original, replacement)

	return nil
}

func runGenreList(cmd *cobra.Command, args []string, deps *Dependencies) error {
	repo := database.NewGenreReplacementRepository(deps.DB)

	replacements, err := repo.List()
	if err != nil {
		return fmt.Errorf("Failed to list genre replacements: %v", err)
	}

	if len(replacements) == 0 {
		fmt.Println("No genre replacements configured")
		return nil
	}

	fmt.Println("=== Genre Replacements ===")
	fmt.Printf("%-30s → %s\n", "Original", "Replacement")
	fmt.Println(strings.Repeat("-", 65))

	for _, r := range replacements {
		fmt.Printf("%-30s → %s\n", r.Original, r.Replacement)
	}

	fmt.Printf("\nTotal: %d replacements\n", len(replacements))

	return nil
}

func runGenreRemove(cmd *cobra.Command, args []string, deps *Dependencies) error {
	original := args[0]

	repo := database.NewGenreReplacementRepository(deps.DB)

	if err := repo.Delete(original); err != nil {
		return fmt.Errorf("Failed to remove genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement removed: '%s'\n", original)

	return nil
}

// Tag command handlers
func runTagAdd(cmd *cobra.Command, args []string, deps *Dependencies) error {
	movieID := args[0]
	tags := args[1:]

	repo := database.NewMovieTagRepository(deps.DB)

	addedTags := []string{}
	for _, tag := range tags {
		if err := repo.AddTag(movieID, tag); err != nil {
			// Check if it's a duplicate error (UNIQUE constraint)
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
				logging.Warnf("Tag '%s' already exists for %s, skipping", tag, movieID)
				continue
			}
			return fmt.Errorf("Failed to add tag '%s': %v", tag, err)
		}
		addedTags = append(addedTags, tag)
	}

	if len(addedTags) == 1 {
		fmt.Printf("✅ Added tag '%s' to %s\n", addedTags[0], movieID)
	} else if len(addedTags) > 1 {
		fmt.Printf("✅ Added %d tags to %s: %v\n", len(addedTags), movieID, addedTags)
	} else {
		fmt.Println("ℹ️  No new tags added (all already exist)")
	}

	return nil
}

func runTagList(cmd *cobra.Command, args []string, deps *Dependencies) error {
	repo := database.NewMovieTagRepository(deps.DB)

	// List tags for specific movie
	if len(args) == 1 {
		movieID := args[0]
		tags, err := repo.GetTagsForMovie(movieID)
		if err != nil {
			return fmt.Errorf("Failed to get tags: %v", err)
		}

		if len(tags) == 0 {
			fmt.Printf("No tags for %s\n", movieID)
			return nil
		}

		fmt.Printf("=== Tags for %s ===\n", movieID)
		for _, tag := range tags {
			fmt.Printf("  - %s\n", tag)
		}
		fmt.Printf("\nTotal: %d tags\n", len(tags))
		return nil
	}

	// List all tag mappings
	allTags, err := repo.ListAll()
	if err != nil {
		return fmt.Errorf("Failed to list tags: %v", err)
	}

	if len(allTags) == 0 {
		fmt.Println("No tag mappings configured")
		return nil
	}

	fmt.Println("=== Movie Tag Mappings ===")
	fmt.Printf("%-20s → Tags\n", "Movie ID")
	fmt.Println(strings.Repeat("-", 70))

	for movieID, tags := range allTags {
		fmt.Printf("%-20s → %s\n", movieID, strings.Join(tags, ", "))
	}

	fmt.Printf("\nTotal: %d movies tagged\n", len(allTags))

	return nil
}

func runTagRemove(cmd *cobra.Command, args []string, deps *Dependencies) error {
	movieID := args[0]
	repo := database.NewMovieTagRepository(deps.DB)

	// Remove specific tag
	if len(args) == 2 {
		tag := args[1]
		if err := repo.RemoveTag(movieID, tag); err != nil {
			return fmt.Errorf("Failed to remove tag: %v", err)
		}
		fmt.Printf("✅ Removed tag '%s' from %s\n", tag, movieID)
		return nil
	}

	// Remove all tags
	if err := repo.RemoveAllTags(movieID); err != nil {
		return fmt.Errorf("Failed to remove tags: %v", err)
	}
	fmt.Printf("✅ Removed all tags from %s\n", movieID)

	return nil
}

func runTagSearch(cmd *cobra.Command, args []string, deps *Dependencies) error {
	tag := args[0]
	repo := database.NewMovieTagRepository(deps.DB)

	movieIDs, err := repo.GetMoviesWithTag(tag)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	if len(movieIDs) == 0 {
		fmt.Printf("No movies found with tag '%s'\n", tag)
		return nil
	}

	fmt.Printf("=== Movies with tag '%s' ===\n", tag)
	for _, id := range movieIDs {
		fmt.Printf("  - %s\n", id)
	}
	fmt.Printf("\nTotal: %d movies\n", len(movieIDs))
	return nil
}

func runTagAllTags(cmd *cobra.Command, args []string, deps *Dependencies) error {
	repo := database.NewMovieTagRepository(deps.DB)

	tags, err := repo.GetUniqueTagsList()
	if err != nil {
		return fmt.Errorf("failed to list tags: %w", err)
	}

	if len(tags) == 0 {
		fmt.Println("No tags in database")
		return nil
	}

	fmt.Println("=== All Tags ===")
	for _, tag := range tags {
		// Count movies with this tag
		movies, err := repo.GetMoviesWithTag(tag)
		if err != nil {
			logging.Warnf("Failed to count movies for tag '%s': %v", tag, err)
			fmt.Printf("  - %-30s (error)\n", tag)
			continue
		}
		fmt.Printf("  - %-30s (%d movies)\n", tag, len(movies))
	}
	fmt.Printf("\nTotal: %d unique tags\n", len(tags))
	return nil
}

func runHistoryList(cmd *cobra.Command, args []string, deps *Dependencies) error {
	logger := history.NewLogger(deps.DB)

	// Get flags
	limit, _ := cmd.Flags().GetInt("limit")
	operation, _ := cmd.Flags().GetString("operation")
	status, _ := cmd.Flags().GetString("status")

	var records []models.History
	var err error

	// Apply filters
	if operation != "" {
		records, err = logger.GetByOperation(operation, limit)
	} else if status != "" {
		records, err = logger.GetByStatus(status, limit)
	} else {
		records, err = logger.GetRecent(limit)
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve history: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("No history records found")
		return nil
	}

	fmt.Println("=== Operation History ===")
	fmt.Printf("%-6s %-10s %-12s %-10s %-8s %-20s %s\n",
		"ID", "Operation", "Movie ID", "Status", "Dry Run", "Time", "Path")
	fmt.Println(strings.Repeat("-", 120))

	for _, record := range records {
		dryRunStr := " "
		if record.DryRun {
			dryRunStr = "✓"
		}

		path := record.NewPath
		if path == "" {
			path = record.OriginalPath
		}
		if len(path) > 40 {
			path = "..." + path[len(path)-37:]
		}

		timeStr := record.CreatedAt.Format("2006-01-02 15:04:05")

		statusIcon := "✅"
		if record.Status == "failed" {
			statusIcon = "❌"
		} else if record.Status == "reverted" {
			statusIcon = "↩️"
		}

		fmt.Printf("%-6d %-10s %-12s %s %-9s %-8s %-20s %s\n",
			record.ID,
			record.Operation,
			record.MovieID,
			statusIcon,
			record.Status,
			dryRunStr,
			timeStr,
			path,
		)

		if record.ErrorMessage != "" {
			fmt.Printf("       Error: %s\n", record.ErrorMessage)
		}
	}

	fmt.Printf("\nShowing %d record(s)\n", len(records))

	return nil
}

func runHistoryStats(cmd *cobra.Command, args []string, deps *Dependencies) error {
	logger := history.NewLogger(deps.DB)

	stats, err := logger.GetStats()
	if err != nil {
		return fmt.Errorf("failed to retrieve stats: %w", err)
	}

	fmt.Println("=== History Statistics ===")
	fmt.Printf("\nTotal Operations: %d\n", stats.Total)

	fmt.Println("\nBy Status:")
	fmt.Printf("  ✅ Success:  %d (%.1f%%)\n", stats.Success, percentage(stats.Success, stats.Total))
	fmt.Printf("  ❌ Failed:   %d (%.1f%%)\n", stats.Failed, percentage(stats.Failed, stats.Total))
	fmt.Printf("  ↩️  Reverted: %d (%.1f%%)\n", stats.Reverted, percentage(stats.Reverted, stats.Total))

	fmt.Println("\nBy Operation:")
	fmt.Printf("  🌐 Scrape:   %d (%.1f%%)\n", stats.Scrape, percentage(stats.Scrape, stats.Total))
	fmt.Printf("  📦 Organize: %d (%.1f%%)\n", stats.Organize, percentage(stats.Organize, stats.Total))
	fmt.Printf("  📥 Download: %d (%.1f%%)\n", stats.Download, percentage(stats.Download, stats.Total))
	fmt.Printf("  📝 NFO:      %d (%.1f%%)\n", stats.NFO, percentage(stats.NFO, stats.Total))

	return nil
}

func runHistoryMovie(cmd *cobra.Command, args []string, deps *Dependencies) error {
	movieID := args[0]

	logger := history.NewLogger(deps.DB)

	records, err := logger.GetByMovieID(movieID)
	if err != nil {
		return fmt.Errorf("failed to retrieve history: %w", err)
	}

	if len(records) == 0 {
		fmt.Printf("No history found for movie: %s\n", movieID)
		return nil
	}

	fmt.Printf("=== History for %s ===\n\n", movieID)

	for _, record := range records {
		statusIcon := "✅"
		if record.Status == "failed" {
			statusIcon = "❌"
		} else if record.Status == "reverted" {
			statusIcon = "↩️"
		}

		fmt.Printf("%s %s - %s (%s)\n",
			statusIcon,
			record.CreatedAt.Format("2006-01-02 15:04:05"),
			record.Operation,
			record.Status,
		)

		if record.OriginalPath != "" {
			fmt.Printf("   From: %s\n", record.OriginalPath)
		}
		if record.NewPath != "" {
			fmt.Printf("   To:   %s\n", record.NewPath)
		}
		if record.DryRun {
			fmt.Println("   (Dry Run)")
		}
		if record.ErrorMessage != "" {
			fmt.Printf("   Error: %s\n", record.ErrorMessage)
		}
		if record.Metadata != "" && record.Metadata != "{}" {
			fmt.Printf("   Metadata: %s\n", record.Metadata)
		}
		fmt.Println()
	}

	fmt.Printf("Total: %d operation(s)\n", len(records))

	return nil
}

func runHistoryClean(cmd *cobra.Command, args []string, deps *Dependencies) error {
	logger := history.NewLogger(deps.DB)

	days, _ := cmd.Flags().GetInt("days")

	// Get count before deletion
	totalBefore, err := logger.GetRecent(0) // Get all
	if err != nil {
		return fmt.Errorf("failed to count records: %w", err)
	}

	// Perform cleanup
	if err := logger.CleanupOldRecords(time.Duration(days) * 24 * time.Hour); err != nil {
		return fmt.Errorf("failed to clean up history: %w", err)
	}

	// Get count after deletion
	totalAfter, err := logger.GetRecent(0)
	if err != nil {
		return fmt.Errorf("failed to count records: %w", err)
	}

	deleted := len(totalBefore) - len(totalAfter)

	if deleted == 0 {
		fmt.Printf("No records older than %d days found\n", days)
	} else {
		fmt.Printf("✅ Cleaned up %d record(s) older than %d days\n", deleted, days)
		fmt.Printf("Remaining: %d record(s)\n", len(totalAfter))
	}

	return nil
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// Wrapper functions that handle dependency initialization

// runWithDeps is a higher-order function that wraps any run* function with
// dependency injection. It handles config loading, dependency creation,
// cleanup, and error handling in one place.
//
// This wrapper uses Cobra's RunE to properly propagate errors and ensure
// resources are cleaned up even on error paths.
//
// Usage:
//
//	scrapeCmd := &cobra.Command{
//	    Use:  "scrape [id]",
//	    RunE: runWithDeps(runScrape),
//	}
func runWithDeps(fn func(*cobra.Command, []string, *Dependencies) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
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

// runWithConfig is a lightweight wrapper for commands that only need config
// (no database or scrapers). Use this for diagnostic commands like 'info'
// that should work even when the database is unavailable.
//
// Usage:
//
//	infoCmd := &cobra.Command{
//	    Use:  "info",
//	    RunE: runWithConfig(runInfo),
//	}
func runWithConfig(fn func(*cobra.Command, []string, *config.Config) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return fn(cmd, args, cfg)
	}
}
