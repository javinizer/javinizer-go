package update

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:   "update [path]",
		Short: "Update metadata for existing files in place",
		Long:  `Scans files, scrapes metadata, and updates NFO files and media without moving video files`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return Run(cmd, args, configFile)
		},
	}
	updateCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	updateCmd.Flags().BoolP("download", "", true, "Download media (covers, screenshots, etc.)")
	updateCmd.Flags().Bool("extrafanart", false, "Download extrafanart (screenshots)")
	updateCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority (comma-separated, e.g., 'r18dev,dmm')")
	updateCmd.Flags().Bool("force-refresh", false, "Force refresh metadata from scrapers (clear cache)")
	updateCmd.Flags().Bool("force-overwrite", false, "Ignore existing NFO, use only scraper data (destructive)")
	updateCmd.Flags().Bool("preserve-nfo", false, "Never overwrite NFO fields, only add missing data (conservative)")
	updateCmd.Flags().Bool("show-merge-stats", false, "Display detailed merge statistics for each file")
	updateCmd.Flags().String("preset", "", "Merge strategy preset: conservative, gap-fill, or aggressive (overrides scalar/array strategies)")
	updateCmd.Flags().String("scalar-strategy", "prefer-nfo", "Scalar field merge strategy: prefer-nfo, prefer-scraper, preserve-existing, or fill-missing-only")
	updateCmd.Flags().String("array-strategy", "merge", "Array field merge strategy: merge or replace")
	return updateCmd
}

// Run executes the update command with the given arguments and config file.
// Exported for testing purposes (Epic 6 Story 6.3).
func Run(cmd *cobra.Command, args []string, configFile string) error {
	sourcePath := args[0]

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	downloadMedia, _ := cmd.Flags().GetBool("download")
	downloadExtrafanart, _ := cmd.Flags().GetBool("extrafanart")
	scraperPriority, _ := cmd.Flags().GetStringSlice("scrapers")
	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	showMergeStats, _ := cmd.Flags().GetBool("show-merge-stats")
	preset, _ := cmd.Flags().GetString("preset")
	scalarStrategyStr, _ := cmd.Flags().GetString("scalar-strategy")
	arrayStrategyStr, _ := cmd.Flags().GetString("array-strategy")

	// Apply preset if specified (overrides individual strategy flags)
	if preset != "" {
		var err error
		scalarStrategyStr, arrayStrategyStr, err = nfo.ApplyPreset(preset, scalarStrategyStr, arrayStrategyStr)
		if err != nil {
			return err
		}
		fmt.Printf("Using preset: %s (%s + %s)\n", preset, scalarStrategyStr, arrayStrategyStr)
	}

	// In update mode: always generate NFO, never move files, force update enabled
	generateNFO := true
	forceUpdate := true
	recursive := true // Always scan recursively

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Destination is the source directory (or parent if source is a file)
	destPath := sourcePath
	fileInfo, err := os.Stat(sourcePath)
	if err == nil && !fileInfo.IsDir() {
		destPath = filepath.Dir(sourcePath)
	}

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		cfg.Output.DownloadExtrafanart = true
	}

	// Determine scraper priority (use flag override if provided, otherwise use config)
	effectiveScraperPriority := cfg.Scrapers.Priority
	if len(scraperPriority) > 0 {
		effectiveScraperPriority = scraperPriority
	}

	// Initialize dependencies
	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	// Initialize components
	movieRepo := database.NewMovieRepository(deps.DB)
	registry := deps.ScraperRegistry
	agg := aggregator.NewWithDatabase(deps.Config, deps.DB)
	fileScanner := scanner.NewScanner(afero.NewOsFs(), &deps.Config.Matching)
	fileMatcher, err := matcher.NewMatcher(&deps.Config.Matching)
	if err != nil {
		return fmt.Errorf("failed to create matcher: %w", err)
	}
	fileOrganizer := organizer.NewOrganizer(afero.NewOsFs(), &deps.Config.Output)
	nfoGenerator := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&deps.Config.Metadata.NFO, &deps.Config.Output, &deps.Config.Metadata, deps.DB))

	// Initialize HTTP client for downloader
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(deps.Config, registry)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	mediaDownloader := downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &deps.Config.Output, deps.Config.Scrapers.UserAgent, deps.Config.Metadata.NFO.ActressLanguageJA, deps.Config.Metadata.NFO.FirstNameOrder)

	// Print configuration
	fmt.Println("=== Javinizer Update ===")
	fmt.Printf("Source: %s\n", sourcePath)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[dryRun])
	fmt.Printf("Generate NFO: %v\n", generateNFO)
	fmt.Printf("Download Media: %v\n\n", downloadMedia)

	// Step 1 & 2: Scan and match
	matches, scanResult, err := commandutil.ScanAndMatch(sourcePath, recursive, fileScanner, fileMatcher)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}

	// Step 3: Scrape metadata
	movies, _, _, err := commandutil.ScrapeMetadata(matches, movieRepo, registry, agg, effectiveScraperPriority, forceRefresh)
	if err != nil {
		return err
	}
	if len(movies) == 0 {
		return nil
	}

	// Step 3.5: Merge with existing NFO data (if not force-overwrite)
	if !forceOverwrite {
		// Determine merge strategies
		var scalarStrategy nfo.MergeStrategy
		var mergeArrays bool

		if preserveNFO {
			scalarStrategy = nfo.PreferNFO
		} else {
			scalarStrategy = nfo.ParseScalarStrategy(scalarStrategyStr)
		}

		mergeArrays = nfo.ParseArrayStrategy(arrayStrategyStr)

		totalMerged := 0
		totalPreservedFromNFO := 0
		totalFromScraper := 0

		for id, scrapedMovie := range movies {
			// Find first match for this ID
			var firstMatch *matcher.MatchResult
			for _, m := range matches {
				if m.ID == id {
					firstMatch = &m
					break
				}
			}
			if firstMatch == nil {
				continue
			}

			// Construct NFO path for this movie
			nfoPath := ConstructNFOPath(*firstMatch, scrapedMovie, deps.Config.Metadata.NFO.PerFile)

			// Check if NFO exists
			if _, err := os.Stat(nfoPath); err == nil {
				// Parse existing NFO
				parseResult, parseErr := nfo.ParseNFO(afero.NewOsFs(), nfoPath)
				if parseErr != nil {
					logging.Warnf("[%s] Failed to parse existing NFO: %v (using scraper data only)", id, parseErr)
					continue
				}

				// Merge scraped data with NFO data using new two-parameter strategy
				mergeResult, mergeErr := nfo.MergeMovieMetadataWithOptions(scrapedMovie, parseResult.Movie, scalarStrategy, mergeArrays)
				if mergeErr != nil {
					logging.Warnf("[%s] Failed to merge NFO data: %v (using scraper data only)", id, mergeErr)
					continue
				}

				// Replace with merged movie
				movies[id] = mergeResult.Merged
				totalMerged++
				totalPreservedFromNFO += mergeResult.Stats.FromNFO
				totalFromScraper += mergeResult.Stats.FromScraper

				// Display merge stats if requested
				if showMergeStats {
					fmt.Printf("\n[%s] Merge Statistics:\n", id)
					fmt.Printf("  Total fields: %d\n", mergeResult.Stats.TotalFields)
					fmt.Printf("  From scraper: %d\n", mergeResult.Stats.FromScraper)
					fmt.Printf("  From NFO: %d\n", mergeResult.Stats.FromNFO)
					if mergeResult.Stats.MergedArrays > 0 {
						fmt.Printf("  Merged arrays: %d\n", mergeResult.Stats.MergedArrays)
					}
					if mergeResult.Stats.ConflictsResolved > 0 {
						fmt.Printf("  Conflicts resolved: %d\n", mergeResult.Stats.ConflictsResolved)
					}
				}
			}
		}

		// Display aggregate merge stats
		if totalMerged > 0 {
			fmt.Printf("\n=== NFO Merge Summary ===\n")
			fmt.Printf("Movies merged with existing NFO: %d\n", totalMerged)
			fmt.Printf("Total fields from scraper: %d\n", totalFromScraper)
			fmt.Printf("Total fields preserved from NFO: %d\n", totalPreservedFromNFO)
			fmt.Printf("Scalar strategy: %s\n", scalarStrategyStr)
			fmt.Printf("Array strategy: %s\n", arrayStrategyStr)
		}
	}

	// Step 4: Generate NFO files (always enabled in update mode)
	// Note: In update mode, we always generate NFOs regardless of config setting
	// because that's the primary purpose of the update command
	nfoCount, err := commandutil.GenerateNFOs(movies, matches, nfoGenerator, fileOrganizer,
		true, false, // nfoEnabled = true (always in update mode), moveToFolder = false (files stay in place)
		deps.Config.Metadata.NFO.PerFile, destPath, forceUpdate, dryRun)
	if err != nil {
		return err
	}

	// Step 5: Download media (if requested)
	if downloadMedia {
		_, err = commandutil.DownloadMediaFiles(movies, matches, mediaDownloader, fileOrganizer,
			deps.Config.Output.DownloadCover, deps.Config.Output.DownloadExtrafanart,
			false, // moveToFolder = false (files stay in place)
			destPath, forceUpdate, dryRun)
		if err != nil {
			return err
		}
	}

	// Summary
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Files scanned: %d\n", len(scanResult.Files))
	fmt.Printf("IDs matched: %d\n", len(matches))
	fmt.Printf("Metadata found: %d\n", len(movies))
	if dryRun {
		fmt.Printf("NFOs generated: %d (dry-run)\n", nfoCount)
	} else {
		fmt.Printf("NFOs generated: %d\n", nfoCount)
	}
	fmt.Printf("Mode: Update (metadata only, files remain in place)\n")

	if dryRun {
		fmt.Println("\n💡 Run without --dry-run to apply changes")
	} else {
		fmt.Println("\n✅ Update complete!")
	}

	return nil
}

// ConstructNFOPath constructs the expected path to an NFO file for a movie.
// Exported for testing purposes (Epic 6 Story 6.3).
func ConstructNFOPath(match matcher.MatchResult, movie *models.Movie, perFile bool) string {
	// Use source directory (where the video file is)
	outputDir := match.File.Dir

	// Construct NFO filename based on ID with sanitization
	basename := movie.ID

	// Add part suffix if per_file is enabled and this is multi-part
	if perFile && match.IsMultiPart {
		basename += match.PartSuffix
	}

	// Sanitize filename to prevent path traversal
	sanitized := template.SanitizeFilename(basename)
	if sanitized == "" {
		// Fallback to just ID if sanitization results in empty string
		sanitized = template.SanitizeFilename(movie.ID)
		if sanitized == "" {
			sanitized = "metadata"
		}
	}

	filename := sanitized + ".nfo"

	return filepath.Join(outputDir, filename)
}
