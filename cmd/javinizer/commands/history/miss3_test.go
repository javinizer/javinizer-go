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

// captureMiss3Output captures stdout during fn execution.
func captureMiss3Output(t *testing.T, fn func()) string {
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

// setupMiss3TestDB creates a config + migrated DB for miss3-coverage tests.
func setupMiss3TestDB(t *testing.T) (configPath string, db *database.DB) {
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

// --- runHistoryList: filter by operation type ---

func TestRunHistoryList_Miss3_FilterByOperation(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
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

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "OP-001")
}

// --- runHistoryList: filter by status ---

func TestRunHistoryList_Miss3_FilterByStatus(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "ST-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusFailed,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "-s", "failed"})

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "ST-001")
}

// --- runHistoryList: empty result ---

func TestRunHistoryList_Miss3_EmptyResult(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list"})

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No history records found")
}

// --- runHistoryStats: with data ---

func TestRunHistoryStats_Miss3_WithData(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:   "STAT-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "stats"})

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Total Operations:")
}

// --- runHistoryMovie: movie not found ---

func TestRunHistoryMovie_Miss3_NotFound(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "NONEXISTENT-999"})

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No history found")
}

// --- runHistoryMovie: with dry run record ---

func TestRunHistoryMovie_Miss3_DryRunRecord(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.History{
		MovieID:      "DRY-002",
		Operation:    models.HistoryOpOrganize,
		Status:       models.HistoryStatusSuccess,
		DryRun:       true,
		OriginalPath: "/old/dry.mp4",
		NewPath:      "/new/dry.mp4",
		CreatedAt:    time.Now().UTC(),
	}))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "movie", "DRY-002"})

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Dry Run")
}

// --- runHistoryClean: no records older than threshold ---

func TestRunHistoryClean_Miss3_NoOldRecords(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
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

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No records older")
}

// --- runHistoryListBatch: batch not found ---

func TestRunHistoryListBatch_Miss3_NotFound(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
	defer db.Close()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "list", "--batch", "nonexistent-batch"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runHistoryListBatch: batch with no operations ---

func TestRunHistoryListBatch_Miss3_NoOperations(t *testing.T) {
	configPath, db := setupMiss3TestDB(t)
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

	output := captureMiss3Output(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No operations found")
}
