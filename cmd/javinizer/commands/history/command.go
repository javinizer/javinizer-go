package history

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "View operation history",
		Long:  `View and manage the history of scrape, organize, download, and NFO operations`,
	}

	historyListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent operations",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runHistoryList(cmd, args, configFile)
		},
	}
	historyListCmd.Flags().IntP("limit", "n", 20, "Number of records to show")
	historyListCmd.Flags().StringP("operation", "o", "", "Filter by operation type (scrape, organize, download, nfo)")
	historyListCmd.Flags().StringP("status", "s", "", "Filter by status (success, failed, reverted)")
	historyListCmd.Flags().StringP("batch", "b", "", "Show batch job details and operations for a specific batch ID")

	historyStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show operation statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runHistoryStats(cmd, args, configFile)
		},
	}

	historyMovieCmd := &cobra.Command{
		Use:   "movie <id>",
		Short: "Show history for a specific movie",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runHistoryMovie(cmd, args, configFile)
		},
	}

	historyCleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean up old history records",
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runHistoryClean(cmd, args, configFile)
		},
	}
	historyCleanCmd.Flags().IntP("days", "d", 30, "Delete records older than this many days")

	revertCmd := NewRevertCommand()

	historyCmd.AddCommand(historyListCmd, historyStatsCmd, historyMovieCmd, historyCleanCmd, revertCmd)
	return historyCmd
}

func runHistoryList(cmd *cobra.Command, args []string, configFile string) error {
	// Check for --batch flag (batch-centric view)
	batchID, _ := cmd.Flags().GetString("batch")
	if batchID != "" {
		return runHistoryListBatch(cmd, batchID, configFile)
	}

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	logger := history.NewLogger(deps.DB)

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
		return fmt.Errorf("failed to retrieve history: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("No history records found")
		return nil
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
		switch record.Status {
		case "failed":
			statusIcon = "❌"
		case "reverted":
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

	return nil
}

func runHistoryStats(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	logger := history.NewLogger(deps.DB)

	stats, err := logger.GetStats()
	if err != nil {
		return fmt.Errorf("failed to retrieve stats: %w", err)
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

	return nil
}

func runHistoryMovie(cmd *cobra.Command, args []string, configFile string) error {
	movieID := args[0]

	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	logger := history.NewLogger(deps.DB)

	records, err := logger.GetByMovieID(movieID)
	if err != nil {
		return fmt.Errorf("failed to retrieve history: %w", err)
	}

	if len(records) == 0 {
		fmt.Printf("No history found for movie: %s\n", movieID)
		return nil
	}

	fmt.Printf("=== History for %s ===\n\n", movieID)

	for _, record := range records {
		statusIcon := "✅"
		switch record.Status {
		case "failed":
			statusIcon = "❌"
		case "reverted":
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

	return nil
}

func runHistoryClean(cmd *cobra.Command, args []string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	logger := history.NewLogger(deps.DB)

	days, _ := cmd.Flags().GetInt("days")

	// Get count before deletion
	totalBefore, err := logger.GetRecent(0) // Get all
	if err != nil {
		return fmt.Errorf("failed to count records: %w", err)
	}

	// Perform cleanup
	if err := logger.CleanupOldRecords(time.Duration(days) * 24 * time.Hour); err != nil {
		return fmt.Errorf("failed to clean up history: %w", err)
	}

	// Get count after deletion
	totalAfter, err := logger.GetRecent(0)
	if err != nil {
		return fmt.Errorf("failed to count records: %w", err)
	}

	deleted := len(totalBefore) - len(totalAfter)

	if deleted == 0 {
		fmt.Printf("No records older than %d days found\n", days)
	} else {
		fmt.Printf("✅ Cleaned up %d record(s) older than %d days\n", deleted, days)
		fmt.Printf("Remaining: %d record(s)\n", len(totalAfter))
	}

	return nil
}

// runHistoryListBatch shows batch-centric view when --batch flag is provided (D-06, HIST-01, HIST-02).
func runHistoryListBatch(cmd *cobra.Command, batchID string, configFile string) error {
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	jobRepo := database.NewJobRepository(deps.DB)
	batchFileOpRepo := database.NewBatchFileOperationRepository(deps.DB)

	// Load job
	job, err := jobRepo.FindByID(batchID)
	if err != nil {
		return fmt.Errorf("batch job not found: %s", batchID)
	}

	// Load operations
	ops, err := batchFileOpRepo.FindByBatchJobID(batchID)
	if err != nil {
		return fmt.Errorf("failed to load operations: %w", err)
	}

	// Compute counts
	opCount := int64(len(ops))
	revertedCount, _ := batchFileOpRepo.CountByBatchJobIDAndRevertStatus(batchID, models.RevertStatusReverted)
	pendingCount, _ := batchFileOpRepo.CountByBatchJobIDAndRevertStatus(batchID, models.RevertStatusApplied)

	// Print batch header
	fmt.Printf("=== Batch Job: %s ===\n", batchID)

	statusIcon := "✅"
	switch job.Status {
	case "reverted":
		statusIcon = "↩️"
	case "failed":
		statusIcon = "❌"
	case "pending", "running":
		statusIcon = "⏳"
	}
	fmt.Printf("Status:      %s %s\n", statusIcon, job.Status)
	fmt.Printf("Total Files: %d\n", job.TotalFiles)
	fmt.Printf("Started:     %s\n", job.StartedAt.Format("2006-01-02 15:04:05"))
	if job.OrganizedAt != nil {
		fmt.Printf("Organized:   %s\n", job.OrganizedAt.Format("2006-01-02 15:04:05"))
	}
	if job.RevertedAt != nil {
		fmt.Printf("Reverted:    %s\n", job.RevertedAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("Operations:  %d total, %d reverted, %d pending\n", opCount, revertedCount, pendingCount)

	if len(ops) == 0 {
		fmt.Println("\nNo operations found for this batch")
		return nil
	}

	// Print operations table
	fmt.Println()
	fmt.Printf("%-5s %-10s %-6s %-14s %-50s %-50s\n",
		"ID", "Movie ID", "Type", "Revert Status", "Original Path", "New Path")
	fmt.Println(strings.Repeat("-", 135))

	for _, op := range ops {
		origPath := truncatePath(op.OriginalPath, 47)
		newPath := truncatePath(op.NewPath, 47)

		fmt.Printf("%-5d %-10s %-6s %-14s %-50s %-50s\n",
			op.ID,
			op.MovieID,
			op.OperationType,
			op.RevertStatus,
			origPath,
			newPath,
		)
	}

	fmt.Printf("\nShowing %d operation(s) for batch %s\n", len(ops), batchID)

	return nil
}

// truncatePath truncates a path for readability, prefixing with "..." if too long.
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-(maxLen-3):]
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
