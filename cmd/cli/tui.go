package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/tui"
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

	return tuiCmd
}

func runTUI(cmd *cobra.Command, args []string) {
	// Get source path
	sourcePath := "."
	if len(args) > 0 {
		sourcePath = args[0]
	}

	recursive, _ := cmd.Flags().GetBool("recursive")

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

	// Set files in model
	model.SetFiles(fileItems)

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
