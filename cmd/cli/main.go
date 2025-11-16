package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/cobra"
)

// Global vars moved to root.go

func main() {
	// Root command and all setup moved to root.go
	// Commands wired via root.go's init() function
	if err := Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Command definitions - will be extracted to separate files in Phase 2

func newSortCmd() *cobra.Command {
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
	return sortCmd
}

func newUpdateCmd() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:   "update [path]",
		Short: "Update metadata for existing files in place",
		Long:  `Scans files, scrapes metadata, and updates NFO files and media without moving video files`,
		Args:  cobra.ExactArgs(1),
		RunE:  runWithDeps(runUpdate),
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

// Helper functions and runXXX implementations below
// These will be extracted to separate files in Phase 2

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

	// Default destination is same as source
	// If source is a file, use its directory as destination
	if destPath == "" {
		fileInfo, err := os.Stat(sourcePath)
		if err == nil && !fileInfo.IsDir() {
			destPath = filepath.Dir(sourcePath)
		} else {
			destPath = sourcePath
		}
	}

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		deps.Config.Output.DownloadExtrafanart = true
	}

	// Determine scraper priority (use flag override if provided, otherwise use config)
	effectiveScraperPriority := deps.Config.Scrapers.Priority
	if len(scraperPriority) > 0 {
		effectiveScraperPriority = scraperPriority
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
	mediaDownloader := downloader.NewDownloaderWithNFOConfig(&deps.Config.Output, deps.Config.Scrapers.UserAgent, deps.Config.Metadata.NFO.ActressLanguageJA, deps.Config.Metadata.NFO.FirstNameOrder)

	// Print configuration
	fmt.Println("=== Javinizer Sort ===")
	fmt.Printf("Source: %s\n", sourcePath)
	fmt.Printf("Destination: %s\n", destPath)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[dryRun])
	fmt.Printf("Operation: %s\n", map[bool]string{true: "MOVE", false: "COPY"}[moveFiles])
	fmt.Printf("Generate NFO: %v\n", generateNFO)
	fmt.Printf("Download Media: %v\n\n", downloadMedia)

	// Step 1 & 2: Scan and match
	matches, scanResult, err := scanAndMatch(sourcePath, recursive, fileScanner, fileMatcher)
	if err != nil {
		return err
	}
	if matches == nil || len(matches) == 0 {
		return nil
	}

	// Step 3: Scrape metadata
	movies, _, _, err := scrapeMetadata(matches, movieRepo, registry, agg, effectiveScraperPriority, forceRefresh)
	if err != nil {
		return err
	}
	if movies == nil || len(movies) == 0 {
		return nil
	}

	// Step 4: Generate NFO files
	if generateNFO {
		_, err = generateNFOs(movies, matches, nfoGenerator, fileOrganizer,
			deps.Config.Metadata.NFO.Enabled, deps.Config.Output.MoveToFolder,
			deps.Config.Metadata.NFO.PerFile, destPath, forceUpdate, dryRun)
		if err != nil {
			return err
		}
	}

	// Step 5: Download media
	if downloadMedia {
		_, err = downloadMediaFiles(movies, matches, mediaDownloader, fileOrganizer,
			deps.Config.Output.DownloadCover, deps.Config.Output.DownloadExtrafanart,
			deps.Config.Output.MoveToFolder, destPath, forceUpdate, dryRun)
		if err != nil {
			return err
		}
	}

	// Step 6: Organize files (skip if move_to_folder is disabled)
	organizedCount := 0
	if deps.Config.Output.MoveToFolder {
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
	fmt.Printf("Files organized: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", organizedCount), false: fmt.Sprintf("%d", organizedCount)}[dryRun])

	if dryRun {
		fmt.Println("\n💡 Run without --dry-run to apply changes")
	} else {
		fmt.Println("\n✅ Sort complete!")
	}

	return nil
}

func runUpdate(cmd *cobra.Command, args []string, deps *Dependencies) error {
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

	// Destination is the source directory (or parent if source is a file)
	destPath := sourcePath
	fileInfo, err := os.Stat(sourcePath)
	if err == nil && !fileInfo.IsDir() {
		destPath = filepath.Dir(sourcePath)
	}

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		deps.Config.Output.DownloadExtrafanart = true
	}

	// Determine scraper priority (use flag override if provided, otherwise use config)
	effectiveScraperPriority := deps.Config.Scrapers.Priority
	if len(scraperPriority) > 0 {
		effectiveScraperPriority = scraperPriority
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
	mediaDownloader := downloader.NewDownloaderWithNFOConfig(&deps.Config.Output, deps.Config.Scrapers.UserAgent, deps.Config.Metadata.NFO.ActressLanguageJA, deps.Config.Metadata.NFO.FirstNameOrder)

	// Print configuration
	fmt.Println("=== Javinizer Update ===")
	fmt.Printf("Source: %s\n", sourcePath)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[dryRun])
	fmt.Printf("Generate NFO: %v\n", generateNFO)
	fmt.Printf("Download Media: %v\n\n", downloadMedia)

	// Step 1 & 2: Scan and match
	matches, scanResult, err := scanAndMatch(sourcePath, recursive, fileScanner, fileMatcher)
	if err != nil {
		return err
	}
	if matches == nil || len(matches) == 0 {
		return nil
	}

	// Step 3: Scrape metadata
	movies, _, _, err := scrapeMetadata(matches, movieRepo, registry, agg, effectiveScraperPriority, forceRefresh)
	if err != nil {
		return err
	}
	if movies == nil || len(movies) == 0 {
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
			nfoPath := constructNFOPath(*firstMatch, scrapedMovie, deps.Config.Metadata.NFO.PerFile)

			// Check if NFO exists
			if _, err := os.Stat(nfoPath); err == nil {
				// Parse existing NFO
				parseResult, parseErr := nfo.ParseNFO(nfoPath)
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
	nfoCount, err := generateNFOs(movies, matches, nfoGenerator, fileOrganizer,
		true, false, // nfoEnabled = true (always in update mode), moveToFolder = false (files stay in place)
		deps.Config.Metadata.NFO.PerFile, destPath, forceUpdate, dryRun)
	if err != nil {
		return err
	}

	// Step 5: Download media (if requested)
	if downloadMedia {
		_, err = downloadMediaFiles(movies, matches, mediaDownloader, fileOrganizer,
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

// constructNFOPath constructs the expected path to an NFO file for a movie
func constructNFOPath(match matcher.MatchResult, movie *models.Movie, perFile bool) string {
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
