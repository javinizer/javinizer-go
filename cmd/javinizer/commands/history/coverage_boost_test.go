package history

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// setupBatchTestDB creates a test database with migrations for batch tests.
func setupBatchTestDB(t *testing.T) (configPath string, db *database.DB) {
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

func TestRunHistoryListBatch_BatchNotFound(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "--batch", "nonexistent-id"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch job not found")
}

func TestRunHistoryListBatch_WithJob(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	job := &models.Job{
		ID:         "test-batch-1",
		Status:     models.JobStatusOrganized,
		TotalFiles: 2,
		StartedAt:  now,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	// Update organized_at
	organized := now.Add(5 * time.Minute)
	job.OrganizedAt = &organized
	require.NoError(t, jobRepo.Update(ctx, job))

	// Create operations
	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "test-batch-1",
		MovieID:       "ABC-001",
		OriginalPath:  "/old/file1.mp4",
		NewPath:       "/new/file1.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "test-batch-1",
		MovieID:       "ABC-002",
		OriginalPath:  "/old/file2.mp4",
		NewPath:       "/new/file2.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "--batch", "test-batch-1"})
	err := rootCmd.Execute()
	assert.NoError(t, err)
}

func TestRunHistoryListBatch_NoOperations(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "empty-batch",
		Status:     models.JobStatusOrganized,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "--batch", "empty-batch"})
	err := rootCmd.Execute()
	assert.NoError(t, err)
}

func TestRunHistoryRevert_InvalidConfig(t *testing.T) {
	cmd := NewRevertCommand()
	cmd.SetArgs([]string{"some-batch-id"})
	// Use nonexistent config to trigger error
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"revert", "some-batch-id"})
	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunHistoryRevert_BatchNotFound(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "revert", "nonexistent-batch"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch job not found")
}

func TestRunHistoryRevert_JobNotOrganized(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "pending-batch",
		Status:     models.JobStatusPending,
		TotalFiles: 1,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "revert", "pending-batch"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in organized status")
}

func TestRunHistoryRevert_NoOperationsFound(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	job := &models.Job{
		ID:         "organized-empty",
		Status:     models.JobStatusOrganized,
		TotalFiles: 0,
		StartedAt:  now,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "revert", "organized-empty"})
	// Should handle "no operations found" gracefully
	err := rootCmd.Execute()
	assert.NoError(t, err)
}

func TestRunHistoryRevert_WithScrapeIDsFlag(t *testing.T) {
	configPath, db := setupBatchTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	job := &models.Job{
		ID:         "revert-scrape-batch",
		Status:     models.JobStatusOrganized,
		TotalFiles: 1,
		StartedAt:  now,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "revert-scrape-batch",
		MovieID:       "XYZ-001",
		OriginalPath:  "/old/xyz.mp4",
		NewPath:       "/new/xyz.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "revert", "revert-scrape-batch", "--scrape-ids", "XYZ-001"})
	err := rootCmd.Execute()
	assert.NoError(t, err)
}

func TestNewRevertCommand_HasScrapeIDsFlag(t *testing.T) {
	cmd := NewRevertCommand()
	assert.NotNil(t, cmd)
	flag := cmd.Flags().Lookup("scrape-ids")
	assert.NotNil(t, flag, "revert command should have --scrape-ids flag")
}
