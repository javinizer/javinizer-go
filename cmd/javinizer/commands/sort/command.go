package sort

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
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	sortCmd := &cobra.Command{
		Use:   "sort [path]",
		Short: "Scan, scrape, and organize video files",
		Long:  `Scans a directory for video files, scrapes metadata, generates NFO files, downloads media, and organizes files`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return Run(cmd, args, configFile)
		},
	}
	sortCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	sortCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	sortCmd.Flags().StringP("dest", "d", "", "Destination directory (default: same as source)")
	sortCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")
	sortCmd.Flags().String("link-mode", "none", "Link mode for copy operations: none, hard, soft")
	sortCmd.Flags().BoolP("nfo", "", true, "Generate NFO files")
	sortCmd.Flags().BoolP("download", "", true, "Download media (covers, screenshots, etc.)")
	sortCmd.Flags().Bool("extrafanart", false, "Download extrafanart (screenshots)")
	sortCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority (comma-separated, e.g., 'r18dev,dmm')")
	sortCmd.Flags().BoolP("force-update", "f", false, "Force update existing files")
	sortCmd.Flags().Bool("force-refresh", false, "Force refresh metadata from scrapers (clear cache)")
	return sortCmd
}

func Run(cmd *cobra.Command, args []string, configFile string) error {
	sourcePath := args[0]

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	recursive, _ := cmd.Flags().GetBool("recursive")
	destPath, _ := cmd.Flags().GetString("dest")
	moveFiles, _ := cmd.Flags().GetBool("move")
	linkModeRaw, _ := cmd.Flags().GetString("link-mode")
	generateNFO, _ := cmd.Flags().GetBool("nfo")
	downloadMedia, _ := cmd.Flags().GetBool("download")
	downloadExtrafanart, _ := cmd.Flags().GetBool("extrafanart")
	scraperPriority, _ := cmd.Flags().GetStringSlice("scrapers")
	forceUpdate, _ := cmd.Flags().GetBool("force-update")
	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")

	linkMode, err := organizer.ParseLinkMode(linkModeRaw)
	if err != nil {
		return err
	}
	if moveFiles && linkMode != organizer.LinkModeNone {
		return fmt.Errorf("--link-mode can only be used when --move is disabled")
	}

	// Load configuration
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

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
	fmt.Println("=== Javinizer Sort ===")
	fmt.Printf("Source: %s\n", sourcePath)
	fmt.Printf("Destination: %s\n", destPath)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[dryRun])
	operationLabel := "COPY"
	if moveFiles {
		operationLabel = "MOVE"
	} else if linkMode == organizer.LinkModeHard {
		operationLabel = "HARDLINK"
	} else if linkMode == organizer.LinkModeSoft {
		operationLabel = "SOFTLINK"
	}
	fmt.Printf("Operation: %s\n", operationLabel)
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

	// Step 4: Generate NFO files
	if generateNFO {
		_, err = commandutil.GenerateNFOs(movies, matches, nfoGenerator, fileOrganizer,
			deps.Config.Metadata.NFO.Enabled, deps.Config.Output.MoveToFolder,
			deps.Config.Metadata.NFO.PerFile, destPath, forceUpdate, dryRun)
		if err != nil {
			return err
		}
	}

	// Step 5: Download media
	if downloadMedia {
		_, err = commandutil.DownloadMediaFiles(movies, matches, mediaDownloader, fileOrganizer,
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
				if issues := fileOrganizer.ValidatePlan(plan); len(issues) > 0 {
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
				switch linkMode {
				case organizer.LinkModeHard:
					operation = "HARDLINK"
				case organizer.LinkModeSoft:
					operation = "SOFTLINK"
				}
				logging.Debugf("[%s] Executing %s operation", match.ID, operation)
				result, err = fileOrganizer.CopyWithLinkMode(plan, dryRun, linkMode)
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
