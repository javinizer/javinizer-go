package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/tui"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
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

	linkMode, err := workflow.ResolveLinkMode(linkModeRaw)
	if err != nil {
		return err
	}
	if moveFiles && linkModeRaw != "" && !strings.EqualFold(linkModeRaw, "none") {
		return fmt.Errorf("--link-mode can only be used when --move is disabled")
	}
	if preset != "" {
		var presetErr error
		scalarStrategy, arrayStrategy, presetErr = nfo.ApplyPreset(preset, scalarStrategy, arrayStrategy)
		if presetErr != nil {
			return presetErr
		}
	}

	// Default destination is same as source
	if destPath == "" {
		destPath = sourcePath
	}

	// Load config
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)

	// Override config with flag if extrafanart is explicitly enabled
	if downloadExtrafanart {
		cfg.Output.Download.DownloadExtrafanart = true
	}

	// Resolve effective move mode: an explicit --move flag overrides config.yaml (issue #36).
	// Without --move, the TUI loads move_files from config so the setting persists across restarts.
	effectiveMove := tui.ResolveMoveMode(cfg.Output.Operation.MoveFiles, cmd.Flags().Changed("move"), moveFiles)
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
		return fmt.Errorf("invalid configuration: %w", err)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logging.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Create TUI model with narrow config
	model := tui.New(tui.TUIModelConfig{
		DownloadExtrafanart: cfg.Output.Download.DownloadExtrafanart,
		MaxWorkers:          cfg.Performance.MaxWorkers,
	})
	model.SetConfigPath(configFile)

	bs, err := commandutil.Bootstrap(cfg)
	if err != nil {
		return fmt.Errorf("failed to bootstrap: %w", err)
	}
	defer func() { _ = bs.Close() }()

	// Scan for files before starting TUI — route through the seam
	// (ValidateMultipartInDirectory is called inside the seam)
	logging.Info("Scanning for video files...")

	scanResult, err := bs.Workflow.ScanAndMatch(ctx, workflow.ScanAndMatchCmd{
		Directory: sourcePath,
		Recursive: recursive,
	})

	if err != nil {
		logging.Errorf("Scan failed: %v", err)
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	logging.Infof("Found %d video files", len(scanResult.Files))

	// Build match map and file refs from seam result
	// Convert workflow.ScanAndMatchResult → tui.ScanResult for the TUI
	tuiScanResult := &tui.ScanResult{
		Files:   scanResult.Files,
		Skipped: scanResult.Skipped,
	}
	matchMap, fileRefs := tui.BuildMatchMapFromScanResult(tuiScanResult)

	// Build tree structure
	fileItems := tui.BuildFileTree(sourcePath, fileRefs, matchMap)

	// Set files, match results, source path in model
	model.SetFiles(fileItems)
	model.SetMatchResults(matchMap)
	model.SetSourcePath(sourcePath)

	// Set scan service (wraps workflow.WorkflowInterface) for rescan operations
	model.SetScanService(tui.NewWorkflowScanService(bs.Workflow, recursive), recursive)

	// Initialize repositories
	actressRepo := database.NewActressRepository(bs.DB)
	model.SetActressRepo(actressRepo)

	// --- Construct SortService (the TUI→worker seam) ---
	bgRunner := tui.NewSimpleRunner(ctx)

	processorCfg := tui.TUIProcessorConfig{
		BatchJobConfig:      commandutil.BatchJobConfigFromAppConfig(cfg),
		DownloadExtrafanart: cfg.Output.Download.DownloadExtrafanart,
	}

	sortSvc, sortEventCh, err := tui.NewSortService(
		bgRunner,
		worker.NewBatchJobFactory(
			nil, // no JobStore — TUI doesn't need persistence
			bs.Workflow,
			bs.Matcher,
			bs.PosterGen,
			worker.BatchJobConfig{
				MaxWorkers:      processorCfg.MaxWorkers,
				WorkerTimeout:   processorCfg.WorkerTimeout,
				ScraperPriority: processorCfg.ScraperPriority,
				NFOEnabled:      processorCfg.NFOEnabled,
			},
			nil, // no emitter for TUI
		),
		bs.ScraperRegistry,
		processorCfg,
		destPath,
		effectiveMove,
	)
	if err != nil {
		return fmt.Errorf("failed to create sort service: %w", err)
	}
	sortSvc.SetConfig(processorCfg)

	// Apply initial options from config + CLI flags
	opts := sortSvc.LoadOptions()
	opts.NFOEnabled = processorCfg.NFOEnabled
	opts.DownloadExtrafanartOverride = processorCfg.DownloadExtrafanart
	opts.LinkMode = linkModeRaw
	opts.UpdateMode = updateMode
	opts.ScalarStrategy = scalarStrategy
	opts.ArrayStrategy = arrayStrategy
	sortSvc.SetOptions(opts)

	// Set the resolved move mode on the model BEFORE the sort service so
	// settings propagate correctly (issue #36).
	model.SetMoveFiles(effectiveMove)
	// Record link mode so the runtime move-files toggle can guard against the
	// move+link combo rejected at startup (ValidateMoveLinkMode) (issue #36).
	model.SetLinkMode(linkMode)
	// Set processor in model (syncs moveFiles, dryRun, updateMode to the processor)
	// Set event subscriber on model — reads SortEvents from the sort service
	model.SetEventSubscriber(tui.NewChannelSortEventSubscriber(sortEventCh))

	// Set sort service in model
	model.SetSortService(sortSvc)

	// Set destination path AFTER sort service is set
	model.SetDestPath(destPath)

	// Set dry-run mode AFTER sort service is set so it propagates correctly
	model.SetDryRun(dryRun)
	model.SetUpdateMode(updateMode)

	// Log initial state
	matchedCount := 0
	for _, mr := range matchMap {
		if mr.MovieID != "" {
			matchedCount++
		}
	}
	model.AddLog("info", fmt.Sprintf("Scanned %d files", len(scanResult.Files)))
	model.AddLog("info", fmt.Sprintf("Matched %d JAV IDs", matchedCount))

	if scanResult.Skipped > 0 {
		model.AddLog("warn", fmt.Sprintf("Skipped %d files", scanResult.Skipped))
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
		return fmt.Errorf("TUI error: %w", err)
	}

	// Check for errors in final model
	if m, ok := finalModel.(*tui.Model); ok {
		if m.Error() != nil {
			logging.Errorf("TUI exited with error: %v", m.Error())
			return fmt.Errorf("TUI exited with error: %w", m.Error())
		}
	}

	logging.Info("TUI exited successfully")
	return nil
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
