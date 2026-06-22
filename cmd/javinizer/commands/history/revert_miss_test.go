package history

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRevertMissDB creates a test config + migrated DB for revert miss-coverage tests.
func setupRevertMissDB(t *testing.T) (configPath string, db *database.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "test.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	configPath = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return configPath, db
}

// createOrganizedJob creates a job with "organized" status in the database.
func createOrganizedJob(t *testing.T, db *database.DB, batchID string) {
	t.Helper()
	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	job := &models.Job{
		ID:         batchID,
		Status:     models.JobStatusOrganized,
		TotalFiles: 1,
		StartedAt:  now,
	}
	require.NoError(t, jobRepo.Create(ctx, job))
}

// --- runHistoryRevert: batch revert with already-reverted batch ---

func TestRunHistoryRevert_BatchAlreadyReverted(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	createOrganizedJob(t, db, "already-reverted-batch")

	// Create operations that are all already reverted
	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "already-reverted-batch",
		MovieID:       "REV-001",
		OriginalPath:  "/old/rev.mp4",
		NewPath:       "/new/rev.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", "already-reverted-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "already reverted")
}

// --- runHistoryRevert: batch revert with no operations found ---

func TestRunHistoryRevert_NoOpsForOrganizedJob(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	// Create a job with organized status but no operations
	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "no-ops-org",
		Status:     models.JobStatusOrganized,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", "no-ops-org"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No operations found")
}

// --- runHistoryRevert: batch revert success with all reverted (no failed/skipped) ---

func TestRunHistoryRevert_BatchRevertAllSuccess(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "full-success-batch"
	createOrganizedJob(t, db, batchID)

	// Create a temp file structure for the revert to work
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "new", "ABC-001")
	require.NoError(t, os.MkdirAll(newDir, 0755))
	newFile := filepath.Join(newDir, "ABC-001.mp4")
	require.NoError(t, os.WriteFile(newFile, []byte("test"), 0644))
	// Pre-create the old directory so os.Rename target dir exists on all platforms.
	// On Windows, the revert code's CanonicalizePath strips the drive letter,
	// which can cause MkdirAll to target the wrong path, so we ensure the
	// target directory exists before the Rename call.
	oldDir := filepath.Join(tmpDir, "old")
	require.NoError(t, os.MkdirAll(oldDir, 0755))

	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "ABC-001",
		OriginalPath:  filepath.Join(oldDir, "ABC-001.mp4"),
		NewPath:       newFile,
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Reverted batch")
	assert.Contains(t, output, "successfully")

	// Verify job status updated to reverted
	jobRepo := database.NewJobRepository(db)
	job, err := jobRepo.FindByID(ctx, batchID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusReverted, job.Status)
	assert.NotNil(t, job.RevertedAt)
}

// --- runHistoryRevert: scrape-ids with already-reverted movie ---

func TestRunHistoryRevert_ScrapeIDsAlreadyReverted(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-reverted-batch"
	createOrganizedJob(t, db, batchID)

	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "ALR-001",
		OriginalPath:  "/old/alr.mp4",
		NewPath:       "/new/alr.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", "ALR-001"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// When all operations for a movie are already reverted, RevertScrape returns
	// "no processable operations found" (not ErrBatchAlreadyReverted)
	assert.Contains(t, output, "Failed to revert movie ALR-001")
}

// --- runHistoryRevert: scrape-ids with non-existent movie ---

func TestRunHistoryRevert_ScrapeIDsNonExistent(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-nonexist-batch"
	createOrganizedJob(t, db, batchID)

	// Don't create any operations for the movie ID

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", "NOSUCH-999"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Should still succeed — errors are printed per-movie
	assert.Contains(t, output, "Failed to revert")
}

// --- runHistoryRevert: scrape-ids with multiple IDs ---

func TestRunHistoryRevert_ScrapeIDsMultiple(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-multi-batch"
	createOrganizedJob(t, db, batchID)

	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "MUL-001",
		OriginalPath:  "/old/mul1.mp4",
		NewPath:       "/new/mul1.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "MUL-002",
		OriginalPath:  "/old/mul2.mp4",
		NewPath:       "/new/mul2.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", "MUL-001,MUL-002"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Already reverted operations get "Failed to revert movie" from RevertScrape
	assert.Contains(t, output, "Failed to revert movie MUL-001")
	assert.Contains(t, output, "Failed to revert movie MUL-002")
}

// --- runHistoryRevert: scrape-ids where revert succeeds and job status updates ---

func TestRunHistoryRevert_ScrapeIDsSuccessAndJobStatusUpdate(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-success-batch"
	createOrganizedJob(t, db, batchID)

	// Create a temp file so revert actually works
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "new", "SUC-001")
	require.NoError(t, os.MkdirAll(newDir, 0755))
	newFile := filepath.Join(newDir, "SUC-001.mp4")
	require.NoError(t, os.WriteFile(newFile, []byte("test"), 0644))
	// Pre-create the old directory so os.Rename target dir exists on all platforms.
	// On Windows, the revert code's CanonicalizePath strips the drive letter,
	// which can cause MkdirAll to target the wrong path, so we ensure the
	// target directory exists before the Rename call.
	oldDir := filepath.Join(tmpDir, "old")
	require.NoError(t, os.MkdirAll(oldDir, 0755))

	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "SUC-001",
		OriginalPath:  filepath.Join(oldDir, "SUC-001.mp4"),
		NewPath:       newFile,
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", "SUC-001"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Reverting SUC-001")

	// Verify job status updated to reverted (since all ops are now reverted)
	jobRepo := database.NewJobRepository(db)
	job, err := jobRepo.FindByID(ctx, batchID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusReverted, job.Status)
}

// --- runHistoryRevert: scrape-ids with whitespace in IDs (trimmed) ---

func TestRunHistoryRevert_ScrapeIDsWhitespace(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-whitespace-batch"
	createOrganizedJob(t, db, batchID)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	// Extra spaces around IDs should be trimmed
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", " WSH-001 ,  "})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// WSH-001 doesn't exist, so it should print failure
	assert.Contains(t, output, "Failed to revert")
}

// --- runHistoryRevert: batch revert with failed/skipped outcomes ---

func TestRunHistoryRevert_BatchWithSkipped(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "batch-skipped-batch"
	createOrganizedJob(t, db, batchID)

	// Create an operation where the anchor file doesn't exist (will be skipped)
	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "SKP-001",
		OriginalPath:  "/nonexistent/old/skp.mp4",
		NewPath:       "/nonexistent/new/skp.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Reverted batch")
	assert.Contains(t, output, "skipped")
}

// --- runHistoryRevert: batch revert with both failed and skipped ---

func TestRunHistoryRevert_BatchWithFailedAndSkipped(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "batch-fail-skip"
	createOrganizedJob(t, db, batchID)

	batchRepo := database.NewBatchFileOperationRepository(db)
	ctx := context.Background()

	// Skipped: anchor file doesn't exist
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "FS-001",
		OriginalPath:  "/nonexistent/old/fs1.mp4",
		NewPath:       "/nonexistent/new/fs1.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	// Failed: copy operation type (can't revert)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    batchID,
		MovieID:       "FS-002",
		OriginalPath:  "/nonexistent/old/fs2.mp4",
		NewPath:       "/nonexistent/new/fs2.mp4",
		OperationType: models.OperationTypeCopy,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Reverted batch")
}

// --- NewRevertCommand: verify command structure ---

func TestNewRevertCommand_Use(t *testing.T) {
	cmd := NewRevertCommand()
	assert.Equal(t, "revert [batch-id]", cmd.Use)
	assert.NotNil(t, cmd.Args, "should have Args validator")
}

// --- runHistoryRevert: invalid config path ---

func TestRunHistoryRevert_InvalidConfigPath(t *testing.T) {
	cmd := NewRevertCommand()
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.InvalidConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"revert", "some-batch"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runHistoryRevert: scrape-ids empty string after trim (no IDs added) ---

func TestRunHistoryRevert_ScrapeIDsAllWhitespace(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "scrape-emptyids-batch"
	createOrganizedJob(t, db, batchID)

	// Don't create operations — empty batch triggers "No operations found"
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID, "--scrape-ids", " , , "})

	// With all-whitespace IDs, scrapeIDs will be empty, so it falls through to batch revert
	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No operations found")
}
