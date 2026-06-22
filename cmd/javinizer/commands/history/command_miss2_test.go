package history

import (
	"bytes"
	"context"
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

// captureMiss2Output captures stdout during fn execution.
func captureMiss2Output(t *testing.T, fn func()) string {
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

// setupMiss2TestDB creates a config + migrated DB for miss2-coverage tests.
func setupMiss2TestDB(t *testing.T) (configPath string, db *database.DB) {
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

// --- runHistoryList with operation filter (line 93) ---

func TestRunHistoryList_FilterByOperation_Miss2(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	// Create records with different operations
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "OP-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "OP-002",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "-o", "scrape"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "OP-001")
}

// --- runHistoryList with status filter (line 95) ---

func TestRunHistoryList_FilterByStatus_Miss2(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "ST-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "ST-002",
		Operation:    models.HistoryOpScrape,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: "something went wrong",
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "-s", "failed"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "ST-002")
}

// --- runHistoryList with no records (line 99) ---

func TestRunHistoryList_NoRecords(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No history records found")
}

// --- runHistoryList with dry-run=false record (line 139 dry-run display) ---

func TestRunHistoryList_NonDryRunRecord(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "NDRY-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		DryRun:    false,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "NDRY-001")
}

// --- runHistoryStats with actual data (lines 171-200) ---

func TestRunHistoryStats_WithData(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "STAT-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "STAT-002",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusFailed,
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "STAT-003",
		Operation: models.HistoryOpDownload,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "STAT-004",
		Operation: models.HistoryOpNFO,
		Status:    models.HistoryStatusReverted,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "stats"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "=== History Statistics ===")
	assert.Contains(t, output, "Total Operations:")
	assert.Contains(t, output, "By Status:")
	assert.Contains(t, output, "Success:")
	assert.Contains(t, output, "Failed:")
	assert.Contains(t, output, "Reverted:")
	assert.Contains(t, output, "By Operation:")
	assert.Contains(t, output, "Scrape:")
	assert.Contains(t, output, "Organize:")
	assert.Contains(t, output, "Download:")
	assert.Contains(t, output, "NFO:")
}

// --- runHistoryMovie with various record types (lines 209-248) ---

func TestRunHistoryMovie_WithOriginalAndNewPath(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "PATH-001",
		Operation:    models.HistoryOpOrganize,
		Status:       models.HistoryStatusSuccess,
		OriginalPath: "/original/path.mp4",
		NewPath:      "/new/path.mp4",
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "PATH-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "From: /original/path.mp4")
	assert.Contains(t, output, "To:   /new/path.mp4")
}

func TestRunHistoryMovie_WithDryRunRecord(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "DRYMOV-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusSuccess,
		DryRun:    true,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "DRYMOV-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Dry Run")
}

func TestRunHistoryMovie_WithErrorMessage(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "ERRMOV-001",
		Operation:    models.HistoryOpScrape,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: "connection timed out",
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "ERRMOV-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Error: connection timed out")
}

func TestRunHistoryMovie_RevertedRecord(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "REVMOV-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusReverted,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "REVMOV-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "↩️")
}

func TestRunHistoryMovie_NoHistoryFound(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "NONEXISTENT-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No history found for movie: NONEXISTENT-001")
}

func TestRunHistoryMovie_WithMetadataNonEmpty(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "META2-001",
		Operation: models.HistoryOpOrganize,
		Status:    models.HistoryStatusSuccess,
		Metadata:  `{"scraper": "r18dev"}`,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "META2-001"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Metadata:")
}

// --- runHistoryListBatch with no operations (line 370) ---

func TestRunHistoryListBatch_NoOperations_Miss2(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	job := &models.Job{
		ID:         "empty-ops-batch",
		Status:     models.JobStatusCompleted,
		TotalFiles: 0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "empty-ops-batch"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No operations found for this batch")
}

// --- runHistoryListBatch with completed job (status icon) ---

func TestRunHistoryListBatch_CompletedJob(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	jobRepo := database.NewJobRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	completed := now.Add(5 * time.Minute)
	job := &models.Job{
		ID:          "completed-batch",
		Status:      models.JobStatusCompleted,
		TotalFiles:  1,
		StartedAt:   now,
		CompletedAt: &completed,
	}
	require.NoError(t, jobRepo.Create(ctx, job))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "completed-batch"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "✅")
}

// --- runHistoryListBatch not found (line 330) ---

func TestRunHistoryListBatch_BatchNotFound_Miss2(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "nonexistent-batch"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch job not found")
}

// --- percentage function (line 393) ---

func TestPercentage_ZeroTotal(t *testing.T) {
	result := percentage(5, 0)
	assert.Equal(t, 0.0, result)
}

func TestPercentage_NonZeroTotal(t *testing.T) {
	result := percentage(25, 100)
	assert.InDelta(t, 25.0, result, 0.01)
}

// --- truncatePath function (line 388) ---

func TestTruncatePath_ShortPath(t *testing.T) {
	result := truncatePath("/short/path.mp4", 47)
	assert.Equal(t, "/short/path.mp4", result)
}

func TestTruncatePath_LongPath(t *testing.T) {
	longPath := "/very/long/path/that/exceeds/the/maximum/display/length/for/testing"
	result := truncatePath(longPath, 20)
	assert.True(t, len(result) <= 20)
	assert.Contains(t, result, "...")
}

// --- runHistoryClean with no records to delete ---

func TestRunHistoryClean_NoOldRecords(t *testing.T) {
	configPath, db := setupMiss2TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	// Create a very recent record
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "RECENT-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "clean", "-d", "30"})

	output := captureMiss2Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No records older than 30 days found")
}
