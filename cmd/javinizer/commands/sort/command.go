package sort

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
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
	sortCmd.Flags().StringSliceP("scrapers", "p", nil, "Scraper priority override — comma-separated subset of enabled scrapers (e.g., 'r18dev,dmm'); scraper must be enabled in config.yaml")
	sortCmd.Flags().BoolP("force-update", "f", false, "Force update existing files")
	sortCmd.Flags().Bool("force-refresh", false, "Force refresh metadata from scrapers (clear cache)")
	return sortCmd
}

func Run(cmd *cobra.Command, args []string, configFile string) error {
	w := cmd.OutOrStdout()
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

	resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		LinkMode: linkModeRaw,
	})
	if err != nil {
		return err
	}
	if moveFiles && linkModeRaw != "" && !strings.EqualFold(linkModeRaw, "none") {
		return fmt.Errorf("--link-mode can only be used when --move is disabled")
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

	// Determine operation label
	operationLabel := "COPY"
	if moveFiles {
		operationLabel = "MOVE"
	} else if strings.EqualFold(linkModeRaw, "hard") {
		operationLabel = "HARDLINK"
	} else if strings.EqualFold(linkModeRaw, "soft") {
		operationLabel = "SOFTLINK"
	}

	// Resolve context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	return commandutil.RunBatchCommand(ctx, w, commandutil.BatchCommandOptions{
		ConfigFile:          configFile,
		SourcePath:          sourcePath,
		Destination:         destPath,
		Recursive:           recursive,
		DryRun:              dryRun,
		DownloadMedia:       downloadMedia,
		DownloadExtrafanart: downloadExtrafanart,
		MoveFiles:           moveFiles,
		GenerateNFO:         generateNFO,
		ForceUpdate:         forceUpdate,
		// Non-update sort must refresh metadata: overwrite any existing NFO with
		// the fresh scrape rather than merging and preferring the stale NFO.
		// Mirrors the TUI's ForceOverwrite: !opts.UpdateMode wiring. Update mode
		// (-f/--force-update) instead merges with the existing NFO.
		ForceOverwrite:    !forceUpdate,
		ScraperPriority:   scraperPriority,
		ForceRefresh:      forceRefresh,
		Resolved:          resolved,
		CommandLabel:      "Javinizer Sort",
		OperationLabel:    operationLabel,
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
	})
}
