package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/tui"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/cobra"
)

// createTUICommand creates the TUI command
func createTUICommand() *cobra.Command {
	tuiCmd := &cobra.Command{
		Use:   "tui [path]",
		Short: "Launch interactive TUI for file organization",
		Long:  `Launch an interactive Terminal User Interface for browsing, selecting, and organizing JAV files with real-time progress tracking`,
		Args:  cobra.MaximumNArgs(1),
		Run:   runTUI,
	}

	tuiCmd.Flags().BoolP("recursive", "r", true, "Scan directories recursively")
	tuiCmd.Flags().StringP("dest", "d", "", "Destination directory (default: same as source)")
	tuiCmd.Flags().BoolP("move", "m", false, "Move files instead of copying")

	return tuiCmd
}

func runTUI(cmd *cobra.Command, args []string) {
	// Get source path
	sourcePath := "."
	if len(args) > 0 {
		sourcePath = args[0]
	}

	recursive, _ := cmd.Flags().GetBool("recursive")
	destPath, _ := cmd.Flags().GetString("dest")
	moveFiles, _ := cmd.Flags().GetBool("move")

	// Default destination is same as source
	if destPath == "" {
		destPath = sourcePath
	}

	// Load config
	if err := loadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// For TUI mode, log to file only (not stdout)
	if cfg.Logging.Output == "stdout" {
		cfg.Logging.Output = "data/logs/javinizer-tui.log"
		// Reinitialize logger
		logCfg := &logging.Config{
			Level:  cfg.Logging.Level,
			Format: cfg.Logging.Format,
			Output: cfg.Logging.Output,
		}
		if verboseFlag {
			logCfg.Level = "debug"
		}
		if err := logging.InitLogger(logCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
			os.Exit(1)
		}
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

	// Scan for files before starting TUI
	logging.Info("Scanning for video files...")
	fileScanner := scanner.NewScanner(&cfg.Matching)

	var scanResult *scanner.ScanResult
	var err error

	if recursive {
		scanResult, err = fileScanner.Scan(sourcePath)
	} else {
		scanResult, err = fileScanner.ScanSingle(sourcePath)
	}

	if err != nil {
		logging.Errorf("Scan failed: %v", err)
		fmt.Fprintf(os.Stderr, "Failed to scan directory: %v\n", err)
		os.Exit(1)
	}

	logging.Infof("Found %d video files", len(scanResult.Files))

	// Match JAV IDs
	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		logging.Errorf("Failed to create matcher: %v", err)
		fmt.Fprintf(os.Stderr, "Failed to create matcher: %v\n", err)
		os.Exit(1)
	}

	matches := fileMatcher.Match(scanResult.Files)
	logging.Infof("Matched %d files", len(matches))

	// Convert to TUI file items
	fileItems := make([]tui.FileItem, 0, len(scanResult.Files))
	matchMap := make(map[string]matcher.MatchResult)

	for _, match := range matches {
		matchMap[match.File.Path] = match
	}

	// Add directories first
	if recursive {
		dirSet := make(map[string]bool)
		for _, file := range scanResult.Files {
			dir := filepath.Dir(file.Path)
			if dir != sourcePath && !dirSet[dir] {
				dirSet[dir] = true
				fileItems = append(fileItems, tui.FileItem{
					Path:     dir,
					Name:     filepath.Base(dir),
					Size:     0,
					IsDir:    true,
					Selected: false,
					Matched:  false,
				})
			}
		}
	}

	// Add files
	for _, file := range scanResult.Files {
		item := tui.FileItem{
			Path:     file.Path,
			Name:     file.Name,
			Size:     file.Size,
			IsDir:    false,
			Selected: false,
			Matched:  false,
		}

		if match, found := matchMap[file.Path]; found {
			item.Matched = true
			item.ID = match.ID
		}

		fileItems = append(fileItems, item)
	}

	// Set files and match results in model
	model.SetFiles(fileItems)
	model.SetMatchResults(matchMap)

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		logging.Errorf("Failed to connect to database: %v", err)
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.AutoMigrate(); err != nil {
		logging.Errorf("Failed to run migrations: %v", err)
	}

	movieRepo := database.NewMovieRepository(db)

	// Initialize scraper registry
	registry := models.NewScraperRegistry()
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg))

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize downloader
	dl := downloader.NewDownloader(&cfg.Output, cfg.Scrapers.UserAgent)

	// Initialize organizer
	org := organizer.NewOrganizer(&cfg.Output)

	// Initialize NFO generator
	nfoGen := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO))

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
		moveFiles,
	)

	// Set processor in model
	model.SetProcessor(processor)

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
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Check for errors in final model
	if m, ok := finalModel.(*tui.Model); ok {
		if m.Error() != nil {
			logging.Errorf("TUI exited with error: %v", m.Error())
			fmt.Fprintf(os.Stderr, "Error: %v\n", m.Error())
			os.Exit(1)
		}
	}

	logging.Info("TUI exited successfully")
}
