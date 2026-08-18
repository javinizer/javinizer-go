package history

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// preRevertSweepTimeout bounds the CLI's pre-revert replacement sweep
// (codex P2 review): without a cap, PreRunE's all-roots sweep ran under
// context.Background(), so one hung network filesystem blocked the revert
// forever. Package-level (not const) so tests can shrink it.
var preRevertSweepTimeout = 30 * time.Second

// Pre-revert sweep seams: production wires the real history-package sweeps;
// tests substitute capture/blocking doubles to observe scoping and force the
// deadline/fallback legs deterministically.
var (
	preRevertScopedSweep = historypkg.SweepRootsOnStartupWithContext
	preRevertFullSweep   = historypkg.SweepOnStartupWithContext
	preRevertFindOps     = func(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, batchID string) ([]models.BatchFileOperation, error) {
		return repo.FindByBatchJobID(ctx, batchID)
	}
)

// runPreRevertReplacementSweep heals leftover crash-window replacement backups
// before the revert reads destination state. The target batch's operations
// are resolved FIRST and only their unique root set (media columns +
// generated-files ledger — historypkg.OperationSweepRoots) is swept; an
// op-resolution failure falls back to all journaled roots (the previous
// behavior). The sweep context carries preRevertSweepTimeout; a deadline is
// logged and swallowed — the revert proceeds either way.
func runPreRevertReplacementSweep(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, batchID string) {
	sweepCtx, cancel := context.WithTimeout(ctx, preRevertSweepTimeout)
	defer cancel()
	ops, err := preRevertFindOps(sweepCtx, repo, batchID)
	if err != nil {
		logging.Warnf("pre-revert sweep: root resolution for batch %s failed (%v) — sweeping all journaled roots", batchID, err)
		preRevertFullSweep(sweepCtx, afero.NewOsFs(), repo)
	} else {
		preRevertScopedSweep(sweepCtx, afero.NewOsFs(), repo, historypkg.OperationSweepRoots(ops))
	}
	if errors.Is(sweepCtx.Err(), context.DeadlineExceeded) {
		logging.Warnf("pre-revert sweep for batch %s exceeded %s — continuing with revert anyway", batchID, preRevertSweepTimeout)
	}
}

// NewRevertCommand creates the revert subcommand for history.
func NewRevertCommand() *cobra.Command {
	revertCmd := &cobra.Command{
		Use:   "revert [batch-id]",
		Short: "Revert an organize batch job",
		Long:  `Revert file organization operations for a batch job, moving files back to their original paths and deleting generated NFO/images.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("config")
			return runHistoryRevert(cmd, args, configFile)
		},
	}
	revertCmd.Flags().String("scrape-ids", "", "Comma-separated movie IDs to revert individually (e.g., ABC-123,DEF-456)")
	return revertCmd
}

func runHistoryRevert(cmd *cobra.Command, args []string, configFile string) error {
	batchID := args[0]

	// Parse --scrape-ids flag if present
	scrapeIDsStr, _ := cmd.Flags().GetString("scrape-ids")
	var scrapeIDs []string
	if scrapeIDsStr != "" {
		for _, id := range strings.Split(scrapeIDsStr, ",") {
			trimmed := strings.TrimSpace(id)
			if trimmed != "" {
				scrapeIDs = append(scrapeIDs, trimmed)
			}
		}
	}

	// Initialize dependencies
	cfg, err := config.LoadOrCreate(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	deps, err := commandutil.NewDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() { _ = deps.Close() }()

	// Create repositories
	jobRepo := database.NewJobRepository(deps.DB)
	batchFileOpRepo := database.NewBatchFileOperationRepository(deps.DB)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate job exists
	job, err := jobRepo.FindByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("batch job not found: %s", batchID)
	}

	// Validate job status is "organized" — T-04-05: prevents reverting non-organized jobs
	if job.Status != models.JobStatusOrganized {
		return fmt.Errorf("job is not in organized status (current: %s). Only organized jobs can be reverted", job.Status)
	}

	// codex P2 (R18 CLI): sweep for any leftover replacement backups from an
	// interrupted CLI apply before revert — but BOUNDED: scoped to the target
	// batch's roots and time-capped, so a hung network root can never block
	// the revert forever (a deadline logs and the revert proceeds).
	deps2 := deps.DB.Repositories()
	runPreRevertReplacementSweep(ctx, deps2.BatchFileOpRepo, batchID)

	// Create Reverter
	reverter := historypkg.NewReverter(afero.NewOsFs(), batchFileOpRepo)

	var totalSucceeded, totalFailed, totalSkipped int
	var allOutcomes []historypkg.RevertFileResult

	if len(scrapeIDs) > 0 {
		// Revert individual movies by scrape IDs (D-05, HIST-09)
		for _, movieID := range scrapeIDs {
			result, err := reverter.RevertScrape(ctx, batchID, movieID)
			if err != nil {
				if errors.Is(err, historypkg.ErrBatchAlreadyReverted) {
					fmt.Printf("⚠️  Movie %s in batch %s is already reverted\n", movieID, batchID)
					continue
				}
				fmt.Printf("❌ Failed to revert movie %s: %v\n", movieID, err)
				continue
			}

			fmt.Printf("Reverting %s: %d succeeded, %d failed\n", movieID, result.Succeeded, result.Failed)
			totalSucceeded += result.Succeeded
			totalSkipped += result.Skipped
			totalFailed += result.Failed
			allOutcomes = append(allOutcomes, result.Outcomes...)
		}

		// After all individual scrapes, check if ALL operations for the batch are now reverted
		appliedCount, err := batchFileOpRepo.CountByBatchJobIDAndRevertStatus(ctx, batchID, models.RevertStatusApplied)
		if err != nil {
			fmt.Printf("⚠️  Failed to verify revert completion: %v\n", err)
		} else {
			failedCount, err := batchFileOpRepo.CountByBatchJobIDAndRevertStatus(ctx, batchID, models.RevertStatusFailed)
			if err != nil {
				fmt.Printf("⚠️  Failed to verify revert completion: %v\n", err)
			} else if appliedCount == 0 && failedCount == 0 {
				// All operations reverted — update job status
				now := time.Now()
				job.Status = models.JobStatusReverted
				job.RevertedAt = &now
				if err := jobRepo.Update(ctx, job); err != nil {
					fmt.Printf("⚠️  Failed to update job status: %v\n", err)
				}
			}
		}
	} else {
		// Batch revert (D-04, HIST-08)
		result, err := reverter.RevertBatch(ctx, batchID)
		if err != nil {
			if errors.Is(err, historypkg.ErrBatchAlreadyReverted) {
				fmt.Printf("⚠️  Batch %s is already reverted\n", batchID)
				return nil
			}
			if errors.Is(err, historypkg.ErrNoOperationsFound) {
				fmt.Printf("❌ No operations found for batch %s\n", batchID)
				return nil
			}
			return fmt.Errorf("failed to revert batch: %w", err)
		}

		totalSucceeded = result.Succeeded
		totalSkipped = result.Skipped
		totalFailed = result.Failed
		allOutcomes = result.Outcomes

		// Only mark job as reverted when no operations are skipped or failed
		// (skipped ops remain "applied" and can be retried later)
		if totalFailed == 0 && totalSkipped == 0 {
			now := time.Now()
			job.Status = models.JobStatusReverted
			job.RevertedAt = &now
			if err := jobRepo.Update(ctx, job); err != nil {
				fmt.Printf("⚠️  Failed to update job status: %v\n", err)
			}
		}
	}

	// Print summary
	if totalFailed == 0 && totalSkipped == 0 {
		fmt.Printf("✅ Reverted batch %s: %d file(s) reverted successfully\n", batchID, totalSucceeded)
	} else if totalFailed == 0 {
		fmt.Printf("⚠️  Reverted batch %s: %d succeeded, %d skipped\n", batchID, totalSucceeded, totalSkipped)
	} else {
		fmt.Printf("⚠️  Reverted batch %s: %d succeeded, %d skipped, %d failed\n", batchID, totalSucceeded, totalSkipped, totalFailed)
		for _, o := range allOutcomes {
			if o.Outcome == models.RevertOutcomeFailed || o.Error != "" {
				fmt.Printf("  ❌ Movie %s: %s\n", o.MovieID, o.Error)
			} else if o.Outcome == models.RevertOutcomeSkipped {
				fmt.Printf("  ⏭️  Movie %s: skipped (%s)\n", o.MovieID, o.Reason)
			}
		}
	}

	return nil
}
