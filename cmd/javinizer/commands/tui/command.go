package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/tui"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// NewCommand creates the TUI command
func NewCommand() *cobra.Command {
	tuiCmd := &cobra.Command{
		Use:   "tui [path]",
		Short: "Launch interactive TUI for file organization",
		Long:  `Launch an interactive Terminal User Interface for browsing, selecting, and organizing JAV files with real-time progress tracking`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  run,
	}

	tuiCmd.Flags().StringP("source", "s", "", "Source directory to scan (can also use positional argument)")
	tuiCmd.Flags().StringP("dest", "d", "", "Destination directory (default: same as source)")
	tuiCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	tuiCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")
	tuiCmd.Flags().BoolP("dry-run", "n", false, "Preview operations without making changes")
	tuiCmd.Flags().String("link-mode", "none", "Link mode for copy operations: none, hard, soft")
	tuiCmd.Flags().Bool("extrafanart", false, "Download extrafanart (screenshots)")
	tuiCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority (comma-separated, e.g., 'r18dev,dmm')")
	tuiCmd.Flags().Bool("update-mode", false, "Update mode: merge metadata with existing NFO and skip file organization")
	tuiCmd.Flags().String("preset", "", "Merge strategy preset: conservative, gap-fill, or aggressive (used in update mode)")
	tuiCmd.Flags().String("scalar-strategy", "prefer-nfo", "Scalar field merge strategy for update mode")
	tuiCmd.Flags().String("array-strategy", "merge", "Array field merge strategy for update mode")

	return tuiCmd
}

func run(cmd *cobra.Command, args []string) error {
	// Get config file from persistent flag
	configFile, _ := cmd.Flags().GetString("config")
	configFile = resolveConfigPath(configFile)

	// Get source path - prioritize flag over positional argument
	sourcePath := "."
	sourceFlag, _ := cmd.Flags().GetString("source")

	if sourceFlag != "" {
		sourcePath = sourceFlag
	} else if len(args) > 0 {
		sourcePath = args[0]
	}

	recursive, _ := cmd.Flags().GetBool("recursive")
	destPath, _ := cmd.Flags().GetString("dest")
	moveFiles, _ := cmd.Flags().GetBool("move")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	linkModeRaw, _ := cmd.Flags().GetString("link-mode")
	downloadExtrafanart, _ := cmd.Flags().GetBool("extrafanart")
	scraperPriority, _ := cmd.Flags().GetStringSlice("scrapers")
	updateMode, _ := cmd.Flags().GetBool("update-mode")
	preset, _ := cmd.Flags().GetString("preset")
	scalarStrategy, _ := cmd.Flags().GetString("scalar-strategy")
	arrayStrategy, _ := cmd.Flags().GetString("array-strategy")
	verboseFlag, _ := cmd.Flags().GetBool("verbose")

	linkMode, err := organizer.ParseLinkMode(linkModeRaw)
	if err != nil {
		return err
	}
	if preset != "" {
		scalarStrategy, arrayStrategy, err = nfo.ApplyPreset(preset, scalarStrategy, arrayStrategy)
		if err != nil {
			return err
		}
	}

	// Default destination is same as source
	if destPath == "" {
		destPath = sourcePath
	}

	// Load config
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	config.ApplyEnvironmentOverrides(cfg)

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		cfg.Output.DownloadExtrafanart = true
	}

	// Resolve effective move mode: an explicit --move flag overrides config.yaml (issue #36).
	// Without --move, the TUI loads move_files from config so the setting persists across restarts.
	effectiveMove := tui.ResolveMoveMode(cfg.Output.MoveFiles, cmd.Flags().Changed("move"), moveFiles)
	// --link-mode is incompatible with move mode, whether move mode came from the
	// --move flag or from move_files in config (issue #36).
	if err := tui.ValidateMoveLinkMode(effectiveMove, linkMode); err != nil {
		return err
	}

	// Override config with flag if scraper priority is provided
	if len(scraperPriority) > 0 {
		cfg.Scrapers.Priority = scraperPriority
	}

	if _, err := config.Prepare(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// For TUI mode, log to file only — strip stdout/stderr targets so logs don't
	// leak into the terminal and corrupt the TUI display. The default config uses
	// "stdout,data/logs/javinizer.log" (dual output); the previous check only
	// handled the pure "stdout" case, so dual-output leaked into the TUI.
	logCfg := configureTUILogging(cfg, verboseFlag)
	if err := logging.InitLogger(logCfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logging.Infof("Starting TUI mode for path: %s", sourcePath)

	// Create context with cancellation
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logging.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Create TUI model
	model := tui.New(cfg)
	model.SetConfigPath(configFile)

	// Scan for files before starting TUI
	logging.Info("Scanning for video files...")
	fileScanner := scanner.NewScanner(afero.NewOsFs(), &cfg.Matching)

	var scanResult *scanner.ScanResult

	if recursive {
		scanResult, err = fileScanner.Scan(sourcePath)
	} else {
		scanResult, err = fileScanner.ScanSingle(sourcePath)
	}

	if err != nil {
		logging.Errorf("Scan failed: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "Failed to scan directory: %v\n", err)
		os.Exit(1)
	}

	logging.Infof("Found %d video files", len(scanResult.Files))

	// Match JAV IDs
	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		logging.Errorf("Failed to create matcher: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "Failed to create matcher: %v\n", err)
		os.Exit(1)
	}

	matches := fileMatcher.Match(scanResult.Files)
	logging.Infof("Matched %d files", len(matches))

	// Convert to TUI file items with tree structure
	matchMap := make(map[string]matcher.MatchResult)
	for _, match := range matches {
		matchMap[match.File.Path] = match
	}

	// Build tree structure
	fileItems := BuildFileTree(sourcePath, scanResult.Files, matchMap)

	// Set files, match results, source path, and destination in model
	model.SetFiles(fileItems)
	model.SetMatchResults(matchMap)
	model.SetSourcePath(sourcePath)
	model.SetScanner(fileScanner, fileMatcher, recursive)
	// Note: destPath will be set after processor is initialized

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		logging.Errorf("Failed to connect to database: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		logging.Errorf("Failed to run migrations: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "Failed to run migrations: %v\n", err)
		os.Exit(1)
	}

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)
	model.SetActressRepo(actressRepo)

	// Initialize scraper registry using centralized function
	registry, err := scraper.NewDefaultScraperRegistry(cfg, db)
	if err != nil {
		return fmt.Errorf("failed to initialize scraper registry: %w", err)
	}

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize HTTP client for downloader
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// Initialize downloader
	sharedEngine := template.NewEngine()
	dl := downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &cfg.Output, cfg.Scrapers.UserAgent, cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder, sharedEngine)

	// Initialize organizer
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output, sharedEngine)
	org.SetMatcher(fileMatcher)

	// Initialize NFO generator
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))

	// Create progress tracker and worker pool
	progressChan := make(chan worker.ProgressUpdate, cfg.Performance.BufferSize)
	progressTracker := worker.NewProgressTracker(progressChan)
	workerPool := worker.NewPool(
		cfg.Performance.MaxWorkers,
		time.Duration(cfg.Performance.WorkerTimeout)*time.Second,
		progressTracker,
	)

	// Create processing coordinator
	processor := tui.NewProcessingCoordinator(
		workerPool,
		progressTracker,
		movieRepo,
		registry,
		agg,
		dl,
		org,
		nfoGen,
		destPath,
		effectiveMove,
	)
	processor.SetConfig(cfg)
	processor.SetOptionsFromConfig(cfg)
	processor.SetLinkMode(linkMode)
	processor.SetTemplateEngine(sharedEngine)
	processor.SetUpdateMode(updateMode)
	processor.SetMergeStrategies(scalarStrategy, arrayStrategy)

	// Set worker pool and progress channel in model
	model.SetWorkerPool(workerPool, progressChan)

	// Set the resolved move mode on the model BEFORE the processor so SetProcessor's
	// defensive sync propagates the correct value (no transient mismatch when --move
	// overrides config) (issue #36).
	model.SetMoveFiles(effectiveMove)
	// Record link mode so the runtime move-files toggle can guard against the
	// move+link combo rejected at startup (ValidateMoveLinkMode) (issue #36).
	model.SetLinkMode(linkMode)
	// Set processor in model (syncs moveFiles, dryRun, updateMode to the processor)
	model.SetProcessor(processor)

	// Set destination path AFTER processor is set
	model.SetDestPath(destPath)

	// Set dry-run mode AFTER processor is set so it propagates correctly
	model.SetDryRun(dryRun)
	model.SetUpdateMode(updateMode)

	// Log initial state
	model.AddLog("info", fmt.Sprintf("Scanned %d files", len(scanResult.Files)))
	model.AddLog("info", fmt.Sprintf("Matched %d JAV IDs", len(matches)))

	if len(scanResult.Skipped) > 0 {
		model.AddLog("warn", fmt.Sprintf("Skipped %d files", len(scanResult.Skipped)))
	}

	if len(scanResult.Errors) > 0 {
		model.AddLog("error", fmt.Sprintf("%d errors during scan", len(scanResult.Errors)))
	}

	// Start TUI
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Run TUI
	finalModel, err := p.Run()
	if err != nil {
		logging.Errorf("TUI error: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Check for errors in final model
	if m, ok := finalModel.(*tui.Model); ok {
		if m.Error() != nil {
			logging.Errorf("TUI exited with error: %v", m.Error())
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", m.Error())
			os.Exit(1)
		}
	}

	logging.Info("TUI exited successfully")
	return nil
}

// BuildFileTree constructs a tree structure of files and directories
func BuildFileTree(basePath string, files []scanner.FileInfo, matchMap map[string]matcher.MatchResult) []tui.FileItem {
	absBasePath, err := filepath.Abs(filepath.FromSlash(basePath))
	if err != nil {
		absBasePath = filepath.FromSlash(basePath)
	}

	dirFiles := make(map[string][]scanner.FileInfo)
	allDirs := make(map[string]bool)

	for _, file := range files {
		normalizedPath := filepath.FromSlash(file.Path)
		absPath, err := filepath.Abs(normalizedPath)
		if err != nil {
			absPath = normalizedPath
		}
		dir := filepath.Dir(absPath)
		dirFiles[dir] = append(dirFiles[dir], file)

		current := dir
		for current != absBasePath && current != "." && current != string(os.PathSeparator) && strings.HasPrefix(current, absBasePath) {
			allDirs[current] = true
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	// Build sorted list of directories
	dirs := make([]string, 0, len(allDirs))
	for dir := range allDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	// Calculate relative depth
	getDepth := func(path string) int {
		rel, err := filepath.Rel(absBasePath, path)
		if err != nil || rel == "." {
			return 0
		}
		return strings.Count(rel, string(filepath.Separator)) + 1
	}

	result := []tui.FileItem{}

	// Process each directory
	for _, dir := range dirs {
		depth := getDepth(dir) - 1
		if depth < 0 {
			depth = 0
		}

		// Add directory item
		result = append(result, tui.FileItem{
			Path:     dir,
			Name:     filepath.Base(dir),
			Size:     0,
			IsDir:    true,
			Selected: false,
			Matched:  false,
			Depth:    depth,
			Parent:   filepath.Dir(dir),
		})

		// Add files in this directory
		if fileList, ok := dirFiles[dir]; ok {
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].Name < fileList[j].Name
			})

			for _, file := range fileList {
				item := tui.FileItem{
					Path:     file.Path,
					Name:     file.Name,
					Size:     file.Size,
					IsDir:    false,
					Selected: false,
					Matched:  false,
					Depth:    depth + 1,
					Parent:   dir,
				}

				lookupPath := file.Path
				normalizedLookup := filepath.FromSlash(file.Path)
				if match, found := matchMap[lookupPath]; found {
					item.Matched = true
					item.ID = match.ID
				} else if match, found := matchMap[normalizedLookup]; found {
					item.Matched = true
					item.ID = match.ID
				}

				result = append(result, item)
			}
		}
	}

	// Add files in the base directory itself
	if baseFiles, ok := dirFiles[absBasePath]; ok {
		sort.Slice(baseFiles, func(i, j int) bool {
			return baseFiles[i].Name < baseFiles[j].Name
		})

		for _, file := range baseFiles {
			item := tui.FileItem{
				Path:     file.Path,
				Name:     file.Name,
				Size:     file.Size,
				IsDir:    false,
				Selected: false,
				Matched:  false,
				Depth:    0,
				Parent:   absBasePath,
			}

			lookupPath := file.Path
			normalizedLookup := filepath.FromSlash(file.Path)
			if match, found := matchMap[lookupPath]; found {
				item.Matched = true
				item.ID = match.ID
			} else if match, found := matchMap[normalizedLookup]; found {
				item.Matched = true
				item.ID = match.ID
			}

			result = append(result, item)
		}
	}

	return result
}

// resolveConfigPath returns the config file path to load and persist, honoring
// the JAVINIZER_CONFIG env override so the TUI uses the same path the rest of the
// app (root.go initConfig) does. Without this, LoadOrCreate would load from the
// env path while SetConfigPath persisted to the --config flag path.
func resolveConfigPath(flagValue string) string {
	if envConfig := os.Getenv("JAVINIZER_CONFIG"); envConfig != "" {
		return envConfig
	}
	return flagValue
}

// configureTUILogging builds a file-only logging config for TUI mode, stripping
// stdout/stderr targets so logs don't leak into the terminal and corrupt the
// TUI display. Rotation settings (max_size_mb, max_backups, max_age_days,
// compress) are preserved, and the verbose flag overrides the level to debug.
// The returned config is intended for logging.InitLogger.
func configureTUILogging(cfg *config.Config, verbose bool) *logging.Config {
	output := logging.FileOnlyOutput(cfg.Logging.Output, "data/logs/javinizer-tui.log")
	logCfg := &logging.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     output,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}
	if verbose {
		logCfg.Level = "debug"
	}
	return logCfg
}
