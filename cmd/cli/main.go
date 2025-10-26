package main

import (
	"fmt"
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
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
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
		Use:   "javinizer",
		Short: "Javinizer - JAV metadata scraper and organizer",
		Long:  `A metadata scraper and file organizer for Japanese Adult Videos (JAV)`,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "enable debug logging")

	// Scrape command
	scrapeCmd := &cobra.Command{
		Use:   "scrape [id]",
		Short: "Scrape metadata for a movie ID",
		Args:  cobra.ExactArgs(1),
		Run:   runScrape,
	}
	scrapeCmd.Flags().StringSliceVarP(&scrapersFlag, "scrapers", "s", nil, "Comma-separated list of scrapers to use (e.g., 'r18dev,dmm' or 'dmm')")

	// Info command
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show configuration and scraper information",
		Run:   runInfo,
	}

	// Init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration and database",
		Run:   runInit,
	}

	// Sort command
	sortCmd := &cobra.Command{
		Use:   "sort [path]",
		Short: "Scan, scrape, and organize video files",
		Long:  `Scans a directory for video files, scrapes metadata, generates NFO files, downloads media, and organizes files`,
		Args:  cobra.ExactArgs(1),
		Run:   runSort,
	}
	sortCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	sortCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	sortCmd.Flags().StringP("dest", "d", "", "Destination directory (default: same as source)")
	sortCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")
	sortCmd.Flags().BoolP("nfo", "", true, "Generate NFO files")
	sortCmd.Flags().BoolP("download", "", true, "Download media (covers, screenshots, etc.)")

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
		Run:   runGenreAdd,
	}

	genreListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all genre replacements",
		Run:   runGenreList,
	}

	genreRemoveCmd := &cobra.Command{
		Use:   "remove <original>",
		Short: "Remove a genre replacement",
		Args:  cobra.ExactArgs(1),
		Run:   runGenreRemove,
	}

	genreCmd.AddCommand(genreAddCmd, genreListCmd, genreRemoveCmd)

	// History command with subcommands
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View operation history",
		Long:  `View and manage the history of scrape, organize, download, and NFO operations`,
	}

	historyListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent operations",
		Run:   runHistoryList,
	}
	historyListCmd.Flags().IntP("limit", "n", 20, "Number of records to show")
	historyListCmd.Flags().StringP("operation", "o", "", "Filter by operation type (scrape, organize, download, nfo)")
	historyListCmd.Flags().StringP("status", "s", "", "Filter by status (success, failed, reverted)")

	historyStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show operation statistics",
		Run:   runHistoryStats,
	}

	historyMovieCmd := &cobra.Command{
		Use:   "movie <id>",
		Short: "Show history for a specific movie",
		Args:  cobra.ExactArgs(1),
		Run:   runHistoryMovie,
	}

	historyCleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean up old history records",
		Run:   runHistoryClean,
	}
	historyCleanCmd.Flags().IntP("days", "d", 30, "Delete records older than this many days")

	historyCmd.AddCommand(historyListCmd, historyStatsCmd, historyMovieCmd, historyCleanCmd)

	// TUI command
	tuiCmd := createTUICommand()

	rootCmd.AddCommand(scrapeCmd, infoCmd, initCmd, sortCmd, genreCmd, historyCmd, tuiCmd)

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
	return nil
}

func runScrape(cmd *cobra.Command, args []string) {
	id := args[0]

	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.AutoMigrate(); err != nil {
		logging.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize scrapers
	registry := models.NewScraperRegistry()
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg))

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)

	// Initialize aggregator with database support
	agg := aggregator.NewWithDatabase(cfg, db)

	logging.Infof("Scraping metadata for: %s", id)

	// Determine which scrapers to use: CLI flag overrides config
	scrapersToUse := cfg.Scrapers.Priority
	usingCustomScrapers := len(scrapersFlag) > 0
	if usingCustomScrapers {
		scrapersToUse = scrapersFlag
		logging.Infof("Using scrapers from CLI flag: %v", scrapersFlag)
	}

	// Check cache first (skip cache if user specified custom scrapers)
	if !usingCustomScrapers {
		if movie, err := movieRepo.FindByID(id); err == nil {
			logging.Info("✅ Found in cache!")
			printMovie(movie)
			return
		}
	}

	// Scrape from sources in priority order
	results := []*models.ScraperResult{}

	for _, scraper := range registry.GetByPriority(scrapersToUse) {
		logging.Infof("Scraping %s...", scraper.Name())
		result, err := scraper.Search(id)
		if err != nil {
			logging.Warnf("❌ %s: %v", scraper.Name(), err)
			continue
		}
		logging.Info("✅")
		results = append(results, result)
	}

	if len(results) == 0 {
		logging.Error("❌ No results found from any scraper")
		return
	}

	logging.Infof("✅ Found %d source(s)", len(results))

	// Aggregate results
	movie, err := agg.Aggregate(results)
	if err != nil {
		logging.Fatalf("Failed to aggregate: %v", err)
	}

	movie.OriginalFileName = id

	// Save to database (upsert: create or update)
	if err := movieRepo.Upsert(movie); err != nil {
		logging.Warnf("Failed to save to database: %v", err)
	} else {
		fmt.Println("💾 Saved to database")
	}

	printMovie(movie)
}

func runInfo(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

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
	fmt.Printf("  - Download screenshots: %v\n", cfg.Output.DownloadScreenshots)
}

func runInit(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	fmt.Println("Initializing Javinizer...")

	// Create data directory
	dataDir := filepath.Dir(cfg.Database.DSN)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logging.Fatalf("Failed to create data directory: %v", err)
	}
	fmt.Printf("✅ Created data directory: %s\n", dataDir)

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.AutoMigrate(); err != nil {
		logging.Fatalf("Failed to run migrations: %v", err)
	}
	fmt.Printf("✅ Initialized database: %s\n", cfg.Database.DSN)

	// Save config if it was just created
	if err := config.Save(cfg, cfgFile); err != nil {
		logging.Fatalf("Failed to save config: %v", err)
	}
	fmt.Printf("✅ Saved configuration: %s\n", cfgFile)

	fmt.Println("\n🎉 Initialization complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Run 'javinizer scrape IPX-535' to test scraping")
	fmt.Println("  - Run 'javinizer info' to view configuration")
}

func printMovie(movie *models.Movie) {
	fmt.Println("=== Movie Details ===")
	fmt.Printf("ID: %s\n", movie.ID)
	fmt.Printf("Content ID: %s\n", movie.ContentID)
	fmt.Printf("Title: %s\n", movie.Title)
	if movie.AlternateTitle != "" {
		fmt.Printf("Alt Title: %s\n", movie.AlternateTitle)
	}

	// Display available translations
	if len(movie.Translations) > 0 {
		fmt.Printf("\nTranslations (%d):\n", len(movie.Translations))
		for _, trans := range movie.Translations {
			fmt.Printf("  [%s] %s (from %s)\n", trans.Language, trans.Title, trans.SourceName)
		}
		fmt.Println()
	}

	fmt.Printf("Description: %s\n", movie.Description)

	if movie.ReleaseDate != nil {
		fmt.Printf("Release Date: %s\n", movie.ReleaseDate.Format("2006-01-02"))
	}
	fmt.Printf("Runtime: %d min\n", movie.Runtime)
	fmt.Printf("Director: %s\n", movie.Director)
	fmt.Printf("Maker: %s\n", movie.Maker)
	fmt.Printf("Label: %s\n", movie.Label)
	fmt.Printf("Series: %s\n", movie.Series)

	if movie.Rating != nil {
		fmt.Printf("Rating: %.1f/10 (%d votes)\n", movie.Rating.Score, movie.Rating.Votes)
	}

	if len(movie.Actresses) > 0 {
		fmt.Printf("\nActresses (%d):\n", len(movie.Actresses))
		for _, actress := range movie.Actresses {
			name := actress.FullName()
			if actress.JapaneseName != "" {
				name += fmt.Sprintf(" (%s)", actress.JapaneseName)
			}
			fmt.Printf("  - %s\n", name)
		}
	}

	if len(movie.Genres) > 0 {
		fmt.Printf("\nGenres (%d):\n", len(movie.Genres))
		for _, genre := range movie.Genres {
			fmt.Printf("  - %s\n", genre.Name)
		}
	}

	if movie.CoverURL != "" {
		fmt.Printf("\nCover: %s\n", movie.CoverURL)
	}
	if len(movie.Screenshots) > 0 {
		fmt.Printf("Screenshots: %d\n", len(movie.Screenshots))
	}
	if movie.TrailerURL != "" {
		fmt.Printf("Trailer: %s\n", movie.TrailerURL)
	}

	fmt.Printf("\nSource: %s\n", movie.SourceName)
}

func runSort(cmd *cobra.Command, args []string) {
	sourcePath := args[0]

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	recursive, _ := cmd.Flags().GetBool("recursive")
	destPath, _ := cmd.Flags().GetString("dest")
	moveFiles, _ := cmd.Flags().GetBool("move")
	generateNFO, _ := cmd.Flags().GetBool("nfo")
	downloadMedia, _ := cmd.Flags().GetBool("download")

	// Default destination is same as source
	if destPath == "" {
		destPath = sourcePath
	}

	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.AutoMigrate(); err != nil {
		logging.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize components
	registry := models.NewScraperRegistry()
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg))

	movieRepo := database.NewMovieRepository(db)
	agg := aggregator.NewWithDatabase(cfg, db)

	fileScanner := scanner.NewScanner(&cfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		logging.Fatalf("Failed to create matcher: %v", err)
	}

	fileOrganizer := organizer.NewOrganizer(&cfg.Output)
	nfoGenerator := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO))
	mediaDownloader := downloader.NewDownloader(&cfg.Output, cfg.Scrapers.UserAgent)

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
		logging.Fatalf("Scan failed: %v", err)
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
		return
	}

	// Step 2: Match JAV IDs
	fmt.Println("\n🔍 Extracting JAV IDs...")
	matches := fileMatcher.Match(scanResult.Files)
	fmt.Printf("   Matched %d file(s)\n", len(matches))

	if len(matches) == 0 {
		fmt.Println("\n⚠️  No JAV IDs found in filenames")
		return
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

		// Check cache first
		if movie, err := movieRepo.FindByID(id); err == nil {
			movies[id] = movie
			cachedCount++
			fmt.Println("✅ (cached)")
			continue
		}

		// Scrape from sources
		results := []*models.ScraperResult{}
		for _, scraper := range registry.GetByPriority(cfg.Scrapers.Priority) {
			if result, err := scraper.Search(id); err == nil {
				results = append(results, result)
			}
		}

		if len(results) == 0 {
			fmt.Println("❌ (not found)")
			continue
		}

		// Aggregate and save
		movie, err := agg.Aggregate(results)
		if err != nil {
			fmt.Printf("❌ (aggregate error: %v)\n", err)
			continue
		}

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
		return
	}

	// Step 4: Generate NFO files
	if generateNFO && cfg.Metadata.NFO.Enabled {
		fmt.Println("\n📝 Generating NFO files...")
		nfoCount := 0

		for id, movie := range movies {
			// Create destination folder for this movie
			plan, err := fileOrganizer.Plan(matches[0], movie, destPath) // Use first match for folder planning
			if err != nil {
				logging.Infof("Failed to plan for %s: %v", id, err)
				continue
			}

			if dryRun {
				fmt.Printf("   %s.nfo (would generate)\n", id)
			} else {
				if err := nfoGenerator.Generate(movie, plan.TargetDir); err != nil {
					logging.Infof("Failed to generate NFO for %s: %v", id, err)
				} else {
					nfoCount++
					fmt.Printf("   %s.nfo ✅\n", id)
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
			// Find first match for this ID
			var firstMatch matcher.MatchResult
			for _, m := range matches {
				if m.ID == id {
					firstMatch = m
					break
				}
			}

			plan, err := fileOrganizer.Plan(firstMatch, movie, destPath)
			if err != nil {
				continue
			}

			if dryRun {
				count := 0
				if cfg.Output.DownloadCover {
					count++
				}
				if cfg.Output.DownloadScreenshots {
					count += len(movie.Screenshots)
				}
				fmt.Printf("   %s: would download ~%d file(s)\n", id, count)
			} else {
				results, err := mediaDownloader.DownloadAll(movie, plan.TargetDir)
				if err != nil {
					logging.Infof("Download error for %s: %v", id, err)
				}

				downloaded := 0
				for _, r := range results {
					if r.Downloaded {
						downloaded++
					}
				}
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

	// Step 6: Organize files
	fmt.Println("\n📦 Organizing files...")
	organizedCount := 0

	for _, match := range matches {
		movie, exists := movies[match.ID]
		if !exists {
			continue
		}

		plan, err := fileOrganizer.Plan(match, movie, destPath)
		if err != nil {
			logging.Infof("Failed to plan %s: %v", match.File.Name, err)
			continue
		}

		// Validate plan
		if issues := organizer.ValidatePlan(plan); len(issues) > 0 {
			fmt.Printf("   ⚠️  %s: %v\n", match.File.Name, issues)
			continue
		}

		var result *organizer.OrganizeResult
		if moveFiles {
			result, err = fileOrganizer.Execute(plan, dryRun)
		} else {
			result, err = fileOrganizer.Copy(plan, dryRun)
		}

		if err != nil {
			fmt.Printf("   ❌ %s: %v\n", match.File.Name, err)
			continue
		}

		if result.Moved || dryRun {
			organizedCount++
			status := "✅"
			if dryRun {
				status = "→"
			}
			fmt.Printf("   %s %s\n      %s\n", status, match.File.Name, plan.TargetPath)
		}
	}

	if dryRun {
		fmt.Printf("\n   Would organize %d file(s)\n", organizedCount)
	} else {
		fmt.Printf("\n   Organized %d file(s)\n", organizedCount)
	}

	// Summary
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Files scanned: %d\n", len(scanResult.Files))
	fmt.Printf("IDs matched: %d\n", len(matches))
	fmt.Printf("Metadata found: %d\n", len(movies))
	if generateNFO {
		fmt.Printf("NFOs generated: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", len(movies)), false: fmt.Sprintf("%d", organizedCount)}[dryRun])
	}
	fmt.Printf("Files organized: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", organizedCount), false: fmt.Sprintf("%d", organizedCount)}[dryRun])

	if dryRun {
		fmt.Println("\n💡 Run without --dry-run to apply changes")
	} else {
		fmt.Println("\n✅ Sort complete!")
	}
}

func runGenreAdd(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	original := args[0]
	replacement := args[1]

	repo := database.NewGenreReplacementRepository(db)

	genreReplacement := &models.GenreReplacement{
		Original:    original,
		Replacement: replacement,
	}

	if err := repo.Upsert(genreReplacement); err != nil {
		logging.Fatalf("Failed to add genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement added: '%s' → '%s'\n", original, replacement)
}

func runGenreList(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repo := database.NewGenreReplacementRepository(db)

	replacements, err := repo.List()
	if err != nil {
		logging.Fatalf("Failed to list genre replacements: %v", err)
	}

	if len(replacements) == 0 {
		fmt.Println("No genre replacements configured")
		return
	}

	fmt.Println("=== Genre Replacements ===")
	fmt.Printf("%-30s → %s\n", "Original", "Replacement")
	fmt.Println(strings.Repeat("-", 65))

	for _, r := range replacements {
		fmt.Printf("%-30s → %s\n", r.Original, r.Replacement)
	}

	fmt.Printf("\nTotal: %d replacements\n", len(replacements))
}

func runGenreRemove(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	original := args[0]

	repo := database.NewGenreReplacementRepository(db)

	if err := repo.Delete(original); err != nil {
		logging.Fatalf("Failed to remove genre replacement: %v", err)
	}

	fmt.Printf("✅ Genre replacement removed: '%s'\n", original)
}

func runHistoryList(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	logger := history.NewLogger(db)

	// Get flags
	limit, _ := cmd.Flags().GetInt("limit")
	operation, _ := cmd.Flags().GetString("operation")
	status, _ := cmd.Flags().GetString("status")

	var records []models.History

	// Apply filters
	if operation != "" {
		records, err = logger.GetByOperation(operation, limit)
	} else if status != "" {
		records, err = logger.GetByStatus(status, limit)
	} else {
		records, err = logger.GetRecent(limit)
	}

	if err != nil {
		logging.Fatalf("Failed to retrieve history: %v", err)
	}

	if len(records) == 0 {
		fmt.Println("No history records found")
		return
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
}

func runHistoryStats(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	logger := history.NewLogger(db)

	stats, err := logger.GetStats()
	if err != nil {
		logging.Fatalf("Failed to retrieve stats: %v", err)
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
}

func runHistoryMovie(cmd *cobra.Command, args []string) {
	movieID := args[0]

	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	logger := history.NewLogger(db)

	records, err := logger.GetByMovieID(movieID)
	if err != nil {
		logging.Fatalf("Failed to retrieve history: %v", err)
	}

	if len(records) == 0 {
		fmt.Printf("No history found for movie: %s\n", movieID)
		return
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
}

func runHistoryClean(cmd *cobra.Command, args []string) {
	if err := loadConfig(); err != nil {
		logging.Fatal(err)
	}

	db, err := database.New(cfg)
	if err != nil {
		logging.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	logger := history.NewLogger(db)

	days, _ := cmd.Flags().GetInt("days")

	// Get count before deletion
	totalBefore, err := logger.GetRecent(0) // Get all
	if err != nil {
		logging.Fatalf("Failed to count records: %v", err)
	}

	// Perform cleanup
	if err := logger.CleanupOldRecords(time.Duration(days) * 24 * time.Hour); err != nil {
		logging.Fatalf("Failed to clean up history: %v", err)
	}

	// Get count after deletion
	totalAfter, err := logger.GetRecent(0)
	if err != nil {
		logging.Fatalf("Failed to count records: %v", err)
	}

	deleted := len(totalBefore) - len(totalAfter)

	if deleted == 0 {
		fmt.Printf("No records older than %d days found\n", days)
	} else {
		fmt.Printf("✅ Cleaned up %d record(s) older than %d days\n", deleted, days)
		fmt.Printf("Remaining: %d record(s)\n", len(totalAfter))
	}
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
