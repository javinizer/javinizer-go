package history_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	fn()

	require.NoError(t, wOut.Close())
	require.NoError(t, wErr.Close())

	return <-outC, <-errC
}

func setupHistoryTestDB(t *testing.T) (configPath string, dbPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath = filepath.Join(tmpDir, "data", "test.db")

	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = dbPath
	configPath = filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: testCfg.Database.Type, DSN: testCfg.Database.DSN, LogLevel: testCfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	_ = db.Close()

	return configPath, dbPath
}

func createHistory(movieID string, op models.HistoryOperation, origPath, newPath string, status models.HistoryStatus, errMsg string, dryRun bool) *models.History {
	return &models.History{
		MovieID:      movieID,
		Operation:    op,
		OriginalPath: origPath,
		NewPath:      newPath,
		Status:       status,
		ErrorMessage: errMsg,
		DryRun:       dryRun,
		CreatedAt:    time.Now().UTC(),
	}
}

func TestRunHistoryList_Empty(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No history records found")
}

func TestRunHistoryList_MultipleOperations(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpOrganize, "/src/file.mp4", "/dest/file.mp4", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpDownload, "http://example.com/cover.jpg", "/dest/cover.jpg", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpNFO, "", "/dest/IPX-001.nfo", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "IPX-001")
	assert.Contains(t, stdout, "scrape")
	assert.Contains(t, stdout, "organize")
	assert.Contains(t, stdout, "download")
	assert.Contains(t, stdout, "nfo")
	assert.Contains(t, stdout, "=== Operation History ===")
}

func TestRunHistoryList_WithLimit(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		movieID := fmt.Sprintf("IPX-%03d", i)
		require.NoError(t, repo.Create(ctx, createHistory(movieID, models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	}

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "-n", "3"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Showing 3 record(s)")
}

func TestRunHistoryList_FilterByOperation(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-002", models.HistoryOpOrganize, "/src", "/dest", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-003", models.HistoryOpDownload, "http://example.com/cover.jpg", "/path", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "-o", "scrape"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "IPX-001")
	assert.Contains(t, stdout, "scrape")
	assert.NotContains(t, stdout, "organize")
	assert.NotContains(t, stdout, "download")
}

func TestRunHistoryList_FilterByStatus(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-002", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusFailed, "scrape failed", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list", "-s", "success"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "IPX-001")
	assert.NotContains(t, stdout, "IPX-002")
}

func TestRunHistoryList_DryRunFlag(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpOrganize, "/src", "/dest", models.HistoryStatusSuccess, "", true)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "list"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "IPX-001")
	assert.Contains(t, stdout, "✓")
}

func TestRunHistoryStats_Empty(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "=== History Statistics ===")
	assert.Contains(t, stdout, "Total Operations: 0")
	assert.Contains(t, stdout, "By Status:")
	assert.Contains(t, stdout, "By Operation:")
}

func TestRunHistoryStats_MultipleOperations(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-002", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-003", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusFailed, "failed", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpOrganize, "/src", "/dest", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpDownload, "http://example.com/cover.jpg", "/path", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpNFO, "", "/path", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Total Operations: 6")
	assert.Contains(t, stdout, "Success:  5")
	assert.Contains(t, stdout, "Failed:   1")
	assert.Contains(t, stdout, "Scrape:   3")
	assert.Contains(t, stdout, "Organize: 1")
	assert.Contains(t, stdout, "Download: 1")
	assert.Contains(t, stdout, "NFO:      1")
}

func TestRunHistoryStats_Percentages(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		require.NoError(t, repo.Create(ctx, createHistory(fmt.Sprintf("IPX-%03d", i), models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.Create(ctx, createHistory(fmt.Sprintf("IPX-%03d", i+10), models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusFailed, "failed", false)))
	}

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "80.0%")
	assert.Contains(t, stdout, "20.0%")
}

func TestRunHistoryMovie_Success(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpOrganize, "/src", "/dest", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpDownload, "http://example.com/cover.jpg", "/path", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-002", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-001"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "=== History for IPX-001 ===")
	assert.Contains(t, stdout, "scrape")
	assert.Contains(t, stdout, "organize")
	assert.Contains(t, stdout, "download")
	assert.Contains(t, stdout, "Total: 3 operation(s)")
	assert.NotContains(t, stdout, "IPX-002")
}

func TestRunHistoryMovie_NotFound(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-999"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No history found for movie: IPX-999")
}

func TestRunHistoryMovie_WithPaths(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpOrganize, "/source/file.mp4", "/destination/file.mp4", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-001"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "From: /source/file.mp4")
	assert.Contains(t, stdout, "To:   /destination/file.mp4")
}

func TestRunHistoryMovie_WithError(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusFailed, "network timeout", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-001"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Error: network timeout")
	assert.Contains(t, stdout, "❌")
}

func TestRunHistoryClean_NoRecords(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "clean"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No records older than 30 days found")
}

func TestRunHistoryClean_WithRecords(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))
	require.NoError(t, repo.Create(ctx, createHistory("IPX-002", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "clean"})

	captureOutput(t, func() {
		err = rootCmd.Execute()
		require.NoError(t, err)
	})
}

func TestRunHistoryClean_CustomDays(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, createHistory("IPX-001", models.HistoryOpScrape, "http://example.com", "", models.HistoryStatusSuccess, "", false)))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "clean", "-d", "7"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "7 days")
}

func TestRunHistoryMovie_AllFields(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpOrganize, "/old/path/file.mp4", "/new/path/file.mp4", models.HistoryStatusSuccess, "", true))
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-123"})

	stdout, _ := captureOutput(t, func() {
		err = rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "=== History for IPX-123 ===")
	assert.Contains(t, stdout, "organize")
	assert.Contains(t, stdout, "From: /old/path/file.mp4")
	assert.Contains(t, stdout, "To:   /new/path/file.mp4")
	assert.Contains(t, stdout, "(Dry Run)")
}

func TestRunHistoryMovie_FailedStatus(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpOrganize, "/old/path", "/new/path", models.HistoryStatusFailed, "Test error", false))
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-123"})

	stdout, _ := captureOutput(t, func() {
		err = rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "❌")
	assert.Contains(t, stdout, "Error: Test error")
}

func TestRunHistoryMovie_RevertedStatus(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	err = repo.Create(ctx, &models.History{
		MovieID:      "IPX-123",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/reverted/from",
		NewPath:      "/old/path",
		Status:       models.HistoryStatusReverted,
		DryRun:       false,
		CreatedAt:    time.Now().UTC(),
	})
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-123"})

	stdout, _ := captureOutput(t, func() {
		err = rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "↩️")
	assert.Contains(t, stdout, "reverted")
}

func TestRunHistoryMovie_EmptyMetadata(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()
	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpOrganize, "/old", "/new", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "movie", "IPX-123"})

	stdout, _ := captureOutput(t, func() {
		err = rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.NotContains(t, stdout, "Metadata:")
}

func TestRunHistoryStats_EmptyDatabase(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Total Operations: 0")
}

func TestRunHistoryStats_MultipleOperationTypes(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpScrape, "http://test.com", "", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpOrganize, "/old", "/new", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpDownload, "http://test.com/image.jpg", "/local/image.jpg", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-123", models.HistoryOpNFO, "", "/path/to/nfo.nfo", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-456", models.HistoryOpScrape, "http://test.com", "", models.HistoryStatusFailed, "test error", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-456", models.HistoryOpOrganize, "/old", "/new", models.HistoryStatusFailed, "test error", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-456", models.HistoryOpDownload, "http://test.com/image.jpg", "/local/image.jpg", models.HistoryStatusFailed, "test error", false))
	require.NoError(t, err)

	err = repo.Create(ctx, createHistory("IPX-456", models.HistoryOpNFO, "", "/path/to/nfo.nfo", models.HistoryStatusFailed, "test error", false))
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Scrape")
	assert.Contains(t, stdout, "Organize")
	assert.Contains(t, stdout, "Download")
	assert.Contains(t, stdout, "NFO")
	assert.Contains(t, stdout, "Total Operations: 8")
}

func TestRunHistoryStats_TimeRanges(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewHistoryRepository(db)
	ctx := context.Background()

	now := time.Now()

	err = repo.Create(ctx, createHistory("OLD-123", models.HistoryOpScrape, "http://test.com", "", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	db.Exec("UPDATE histories SET created_at = ? WHERE movie_id = ?", now.Add(-48*time.Hour), "OLD-123")

	err = repo.Create(ctx, createHistory("NEW-456", models.HistoryOpOrganize, "/old", "/new", models.HistoryStatusSuccess, "", false))
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := history.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"history", "stats"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Total Operations: 2")
}
