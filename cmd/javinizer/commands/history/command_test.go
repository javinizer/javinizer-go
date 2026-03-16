package history_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	historyinternal "github.com/javinizer/javinizer-go/internal/history"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

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

	// Ensure database directory exists
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	// Create test config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	configPath = filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Initialize database with migrations to ensure it exists
	db, err := database.New(testCfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)
	_ = db.Close()

	return configPath, dbPath
}

// Tests

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

	// Load config and create DB connection to add test data
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogOrganize("IPX-001", "/src/file.mp4", "/dest/file.mp4", false, nil))
	require.NoError(t, logger.LogDownload("IPX-001", "http://example.com/cover.jpg", "/dest/cover.jpg", "cover", nil))
	require.NoError(t, logger.LogNFO("IPX-001", "/dest/IPX-001.nfo", nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	for i := 1; i <= 5; i++ {
		movieID := fmt.Sprintf("IPX-%03d", i)
		require.NoError(t, logger.LogScrape(movieID, "http://example.com", nil, nil))
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogOrganize("IPX-002", "/src", "/dest", false, nil))
	require.NoError(t, logger.LogDownload("IPX-003", "http://example.com/cover.jpg", "/path", "cover", nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogScrape("IPX-002", "http://example.com", nil, fmt.Errorf("scrape failed")))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogOrganize("IPX-001", "/src", "/dest", true, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogScrape("IPX-002", "http://example.com", nil, nil))
	require.NoError(t, logger.LogScrape("IPX-003", "http://example.com", nil, fmt.Errorf("failed")))
	require.NoError(t, logger.LogOrganize("IPX-001", "/src", "/dest", false, nil))
	require.NoError(t, logger.LogDownload("IPX-001", "http://example.com/cover.jpg", "/path", "cover", nil))
	require.NoError(t, logger.LogNFO("IPX-001", "/path", nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	for i := 0; i < 8; i++ {
		_ = logger.LogScrape(fmt.Sprintf("IPX-%03d", i), "http://example.com", nil, nil)
	}
	for i := 0; i < 2; i++ {
		_ = logger.LogScrape(fmt.Sprintf("IPX-%03d", i+10), "http://example.com", nil, fmt.Errorf("failed"))
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogOrganize("IPX-001", "/src", "/dest", false, nil))
	require.NoError(t, logger.LogDownload("IPX-001", "http://example.com/cover.jpg", "/path", "cover", nil))
	require.NoError(t, logger.LogScrape("IPX-002", "http://example.com", nil, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogOrganize("IPX-001", "/source/file.mp4", "/destination/file.mp4", false, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	_ = logger.LogScrape("IPX-001", "http://example.com", nil, fmt.Errorf("network timeout"))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))
	require.NoError(t, logger.LogScrape("IPX-002", "http://example.com", nil, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	require.NoError(t, logger.LogScrape("IPX-001", "http://example.com", nil, nil))

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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	err = logger.LogOrganize("IPX-123", "/old/path/file.mp4", "/new/path/file.mp4", true, nil)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	testError := fmt.Errorf("Test error")
	err = logger.LogOrganize("IPX-123", "/old/path", "/new/path", false, testError)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	err = logger.LogRevert("IPX-123", "/old/path", "/reverted/from", nil)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)
	err = logger.LogOrganize("IPX-123", "/old", "/new", false, nil)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)

	err = logger.LogScrape("IPX-123", "http://test.com", nil, nil)
	require.NoError(t, err)

	err = logger.LogOrganize("IPX-123", "/old", "/new", false, nil)
	require.NoError(t, err)

	err = logger.LogDownload("IPX-123", "http://test.com/image.jpg", "/local/image.jpg", "cover", nil)
	require.NoError(t, err)

	err = logger.LogNFO("IPX-123", "/path/to/nfo.nfo", nil)
	require.NoError(t, err)

	testErr := fmt.Errorf("test error")

	err = logger.LogScrape("IPX-456", "http://test.com", nil, testErr)
	require.NoError(t, err)

	err = logger.LogOrganize("IPX-456", "/old", "/new", false, testErr)
	require.NoError(t, err)

	err = logger.LogDownload("IPX-456", "http://test.com/image.jpg", "/local/image.jpg", "cover", testErr)
	require.NoError(t, err)

	err = logger.LogNFO("IPX-456", "/path/to/nfo.nfo", testErr)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	logger := historyinternal.NewLogger(db)

	now := time.Now()

	err = logger.LogScrape("OLD-123", "http://test.com", nil, nil)
	require.NoError(t, err)

	db.Exec("UPDATE histories SET created_at = ? WHERE movie_id = ?", now.Add(-48*time.Hour), "OLD-123")

	err = logger.LogOrganize("NEW-456", "/old", "/new", false, nil)
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
