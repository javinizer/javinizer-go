package update

import (
	"context"
	"fmt"
	"io"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
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
	// --show-merge-stats is registered for backward compatibility with existing
	// scripts/aliases but is a no-op (merge stats are not collected per-file).
	// Mark it hidden so help does not advertise a flag that does nothing.
	updateCmd.Flags().Bool("show-merge-stats", false, "Display detailed merge statistics for each file")
	_ = updateCmd.Flags().MarkHidden("show-merge-stats")
	updateCmd.Flags().String("preset", "", "Merge strategy preset: conservative, gap-fill, or aggressive (overrides scalar/array strategies)")
	updateCmd.Flags().String("scalar-strategy", "prefer-nfo", "Scalar field merge strategy: prefer-nfo, prefer-scraper, preserve-existing, or fill-missing-only")
	updateCmd.Flags().String("array-strategy", "merge", "Array field merge strategy: merge or replace")
	return updateCmd
}

// Run executes the update command with the given arguments and config file.
// Exported for testing purposes (Epic 6 Story 6.3).
func Run(cmd *cobra.Command, args []string, configFile string) error {
	w := cmd.OutOrStdout()
	sourcePath := args[0]

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	downloadMedia, _ := cmd.Flags().GetBool("download")
	downloadExtrafanart, _ := cmd.Flags().GetBool("extrafanart")
	scraperPriority, _ := cmd.Flags().GetStringSlice("scrapers")
	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	preset, _ := cmd.Flags().GetString("preset")
	scalarStrategyStr, _ := cmd.Flags().GetString("scalar-strategy")
	arrayStrategyStr, _ := cmd.Flags().GetString("array-strategy")

	// Apply preset if specified (overrides individual strategy flags)
	if preset != "" {
		fmt.Fprintf(w, "Using preset: %s\n", preset)
	}

	resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		Preset:         preset,
		ScalarStrategy: scalarStrategyStr,
		ArrayStrategy:  arrayStrategyStr,
	})
	if err != nil {
		return err
	}

	// Report the EFFECTIVE strategies after preset expansion (not the raw flag
	// values), so the summary reflects what was actually applied. A preset
	// overrides the individual strategy flags, and printing the raw flags would
	// misrepresent the merge behavior the run used.
	resolvedScalarStr := string(resolved.ScalarStrategy)
	resolvedArrayStr := "replace"
	if resolved.ArrayStrategy {
		resolvedArrayStr = "merge"
	}

	// In update mode: always generate NFO, never move files
	// Context resolution
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	return commandutil.RunBatchCommand(ctx, w, commandutil.BatchCommandOptions{
		ConfigFile:          configFile,
		SourcePath:          sourcePath,
		Destination:         sourcePath, // Update mode: files stay in place
		Recursive:           true,       // Always scan recursively
		DryRun:              dryRun,
		DownloadMedia:       downloadMedia,
		DownloadExtrafanart: downloadExtrafanart,
		GenerateNFO:         true, // Update always generates NFO
		SkipOrganize:        true, // Never move files in update mode
		ScraperPriority:     scraperPriority,
		ForceRefresh:        forceRefresh,
		ForceOverwrite:      forceOverwrite,
		PreserveNFO:         preserveNFO,
		Resolved:            resolved,
		CommandLabel:        "Javinizer Update",
		ActionVerb:          "Updating metadata",
		CompletionMessage:   "Update complete!",
		ModeLine:            "Update (metadata & artwork, files remain in place)",
		EventHandler:        commandutil.UpdateEventHandler,
		SummaryPrinter:      updateSummaryPrinter(resolvedScalarStr, resolvedArrayStr),
	})
}

// updateSummaryPrinter returns a custom summary printer for the update command
// that includes merge-specific output (NFO merge summary, scalar/array strategy info).
func updateSummaryPrinter(scalarStrategy, arrayStrategy string) func(w io.Writer, opts commandutil.BatchCommandOptions, result commandutil.BatchCommandResult) {
	return func(w io.Writer, opts commandutil.BatchCommandOptions, result commandutil.BatchCommandResult) {
		mergedCount := result.SuccessCount

		fmt.Fprintf(w, "   Updated: %d, Failed: %d\n", len(result.Movies), result.FailedCount)

		if mergedCount > 0 {
			fmt.Fprintf(w, "\n=== NFO Merge Summary ===\n")
			fmt.Fprintf(w, "Movies merged with existing NFO: %d\n", mergedCount)
			fmt.Fprintf(w, "Scalar strategy: %s\n", scalarStrategy)
			fmt.Fprintf(w, "Array strategy: %s\n", arrayStrategy)
		}

		if len(result.Movies) == 0 {
			fmt.Fprintln(w, "\n⚠️  No metadata found")
			return
		}

		// NFO summary
		fmt.Fprintln(w)
		if opts.DryRun {
			fmt.Fprintf(w, "   Would generate %d NFO file(s)\n", len(result.Movies))
		} else {
			fmt.Fprintf(w, "   Generated %d NFO file(s)\n", len(result.Movies))
		}

		// Standard summary
		fmt.Fprintln(w, "\n=== Summary ===")
		fmt.Fprintf(w, "Files scanned: %d\n", len(result.ScanResult.Files))
		fmt.Fprintf(w, "IDs matched: %d\n", result.MatchedCount)
		fmt.Fprintf(w, "Metadata found: %d\n", len(result.Movies))
		if opts.DryRun {
			fmt.Fprintf(w, "NFOs generated: %d (dry-run)\n", len(result.Movies))
		} else {
			fmt.Fprintf(w, "NFOs generated: %d\n", len(result.Movies))
		}
		fmt.Fprintf(w, "Mode: %s\n", opts.ModeLine)

		if opts.DryRun {
			fmt.Fprintln(w, "\n💡 Run without --dry-run to apply changes")
		} else {
			fmt.Fprintf(w, "\n✅ %s\n", opts.CompletionMessage)
		}
	}
}
