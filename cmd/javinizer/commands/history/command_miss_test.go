package history

import (
	"bytes"
	"context"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"io"
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

// captureMissOutput captures stdout and stderr during fn execution.
func captureMissOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	fn()
	require.NoError(t, wOut.Close())

	return <-outC
}

// setupMissTestDB creates a config + migrated DB for miss-coverage tests.
func setupMissTestDB(t *testing.T) (configPath string, db *database.DB) {
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

// --- runHistoryClean branches (lines 274-312) ---

func TestRunHistoryClean_DeletesOldRecords(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	// Create a record and manually set its created_at to be old
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "OLD-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	// Set created_at to 60 days ago (table name is "history" per History.TableName())
	db.DB.Exec("UPDATE history SET created_at = ? WHERE movie_id = ?", time.Now().UTC().Add(-60*24*time.Hour), "OLD-001")

	// Also create a recent record that should NOT be deleted
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "NEW-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "clean", "-d", "30"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Cleaned up 1 record(s)")
	assert.Contains(t, output, "Remaining: 1")
}

func TestRunHistoryClean_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "clean"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runHistoryListBatch additional branches (lines 320-370) ---

func TestRunHistoryListBatch_RevertedJob(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	reverted := now.Add(10 * time.Minute)
	job := &models.Job{
		ID:         "reverted-batch",
		Status:     models.JobStatusReverted,
		TotalFiles: 1,
		StartedAt:  now,
		RevertedAt: &reverted,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "reverted-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "↩️")
	assert.Contains(t, output, "reverted")
}

func TestRunHistoryListBatch_FailedJob(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "failed-batch",
		Status:     models.JobStatusFailed,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "failed-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "❌")
}

func TestRunHistoryListBatch_PendingJob(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "pending-batch",
		Status:     models.JobStatusPending,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "pending-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "⏳")
}

func TestRunHistoryListBatch_RunningJob(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "running-batch",
		Status:     models.JobStatusRunning,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "running-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "⏳")
}

func TestRunHistoryListBatch_OrganizedJobWithRevertedAt(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	organized := now.Add(5 * time.Minute)
	reverted := now.Add(10 * time.Minute)
	job := &models.Job{
		ID:          "org-rev-batch",
		Status:      models.JobStatusReverted,
		TotalFiles:  1,
		StartedAt:   now,
		OrganizedAt: &organized,
		RevertedAt:  &reverted,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "org-rev-batch",
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
	rootCmd.SetArgs([]string{"history", "list", "--batch", "org-rev-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Organized:")
	assert.Contains(t, output, "Reverted:")
	assert.Contains(t, output, "1 reverted")
}

func TestRunHistoryListBatch_LongPaths(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "longpath-batch",
		Status:     models.JobStatusOrganized,
		TotalFiles: 1,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	longPath := "/very/long/path/that/exceeds/the/maximum/display/length/allowed/for/readability/in/terminal/output/more/text/here"
	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "longpath-batch",
		MovieID:       "LONG-001",
		OriginalPath:  longPath,
		NewPath:       longPath + "/new",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "longpath-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "...")
}

func TestRunHistoryListBatch_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "some-id"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runHistoryList error paths (lines 80-110) ---

func TestRunHistoryList_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunHistoryList_WithErrorMessage(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "ERR-001",
		Operation:    models.HistoryOpScrape,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: "network timeout",
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Error: network timeout")
}

func TestRunHistoryList_LongPathTruncation(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	longPath := "/very/long/path/that/exceeds/the/maximum/display/length/allowed/in/terminal/output/with/more/text"
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "LONG-001",
		Operation: models.HistoryOpOrganize,
		NewPath:   longPath,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "...")
}

func TestRunHistoryList_UsesOriginalPathWhenNewPathEmpty(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "ORIG-001",
		Operation:    models.HistoryOpScrape,
		OriginalPath: "/original/path/file.mp4",
		NewPath:      "",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "/original/path/file.mp4")
}

// --- runHistoryStats error paths (lines 171-178) ---

func TestRunHistoryStats_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "stats"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runHistoryMovie error paths (lines 209-222) ---

func TestRunHistoryMovie_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "IPX-001"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunHistoryMovie_WithMetadata(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "META-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusSuccess,
		Metadata:  `{"key": "value"}`,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "META-001"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Metadata:")
}

// --- runHistoryClean error paths ---

// --- runHistoryClean with zero days (deletes all records) ---

func TestRunHistoryClean_ZeroDays(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "DEL-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "clean", "-d", "0"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Cleaned up")
}

// --- runHistoryList with reverted status icon ---

func TestRunHistoryList_RevertedStatusIcon(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "REV-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusReverted,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "↩️")
}

// --- runHistoryList with dry-run record ---

func TestRunHistoryList_DryRunRecord(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "DRY-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusSuccess,
		DryRun:    true,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "✓")
}

// --- runHistoryListBatch with organized job (completed status icon) ---

func TestRunHistoryListBatch_OrganizedStatusIcon(t *testing.T) {
	configPath, db := setupMissTestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "organized-icon-batch",
		Status:     models.JobStatusOrganized,
		TotalFiles: 1,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:    "organized-icon-batch",
		MovieID:       "ORG-001",
		OriginalPath:  "/old/org.mp4",
		NewPath:       "/new/org.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "organized-icon-batch"})

	output := captureMissOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "organized")
}
