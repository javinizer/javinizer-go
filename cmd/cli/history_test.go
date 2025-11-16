package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function for history tests
func setupHistoryTestDB(t *testing.T) (configPath string, dbPath string) {
	t.Helper()
	return setupTagTestDB(t) // Reuse the same setup
}

// TestRunHistoryMovie_AllFields tests all optional fields in history records
func TestRunHistoryMovie_AllFields(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create history record using LogOrganize
		err = logger.LogOrganize("IPX-123", "/old/path/file.mp4", "/new/path/file.mp4", true, nil)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		// Test with all fields
		stdout, _ := captureOutput(t, func() {
			err = runHistoryMovie(cmd, []string{"IPX-123"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "=== History for IPX-123 ===")
		assert.Contains(t, stdout, "organize")
		assert.Contains(t, stdout, "From: /old/path/file.mp4")
		assert.Contains(t, stdout, "To:   /new/path/file.mp4")
		assert.Contains(t, stdout, "(Dry Run)")
	})
}

// TestRunHistoryMovie_FailedStatus tests failed operation display
func TestRunHistoryMovie_FailedStatus(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create failed record using LogOrganize with error
		testError := fmt.Errorf("Test error")
		err = logger.LogOrganize("IPX-123", "/old/path", "/new/path", false, testError)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryMovie(cmd, []string{"IPX-123"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "❌") // Failed icon
		assert.Contains(t, stdout, "Error: Test error")
	})
}

// TestRunHistoryMovie_RevertedStatus tests reverted operation display
func TestRunHistoryMovie_RevertedStatus(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create reverted record using LogRevert
		err = logger.LogRevert("IPX-123", "/old/path", "/reverted/from", nil)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryMovie(cmd, []string{"IPX-123"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "↩️") // Reverted icon
		assert.Contains(t, stdout, "reverted")
	})
}

// TestRunHistoryMovie_EmptyMetadata tests that empty metadata is not displayed
func TestRunHistoryMovie_EmptyMetadata(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create record using LogOrganize (no metadata field, so it should be empty)
		err = logger.LogOrganize("IPX-123", "/old", "/new", false, nil)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryMovie(cmd, []string{"IPX-123"}, deps)
			require.NoError(t, err)
		})

		// Empty metadata should not be displayed
		assert.NotContains(t, stdout, "Metadata:")
	})
}

// TestRunHistoryList_DatabaseError tests error handling
func TestRunHistoryList_DatabaseError(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		// Close database to cause error
		deps.DB.Close()

		cmd := &cobra.Command{}
		cmd.Flags().Int("limit", 10, "")

		err = runHistoryList(cmd, []string{}, deps)
		assert.Error(t, err)
	})
}

// TestRunHistoryStats_EmptyDatabase tests stats with no history
func TestRunHistoryStats_EmptyDatabase(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryStats(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Should show zero stats
		assert.Contains(t, stdout, "Total Operations: 0")
	})
}

// TestRunHistoryStats_MultipleOperationTypes tests stats with various operation types
func TestRunHistoryStats_MultipleOperationTypes(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create multiple operation types using specific Log methods
		err = logger.LogScrape("IPX-123", "http://test.com", nil, nil)
		require.NoError(t, err)

		err = logger.LogOrganize("IPX-123", "/old", "/new", false, nil)
		require.NoError(t, err)

		err = logger.LogDownload("IPX-123", "http://test.com/image.jpg", "/local/image.jpg", "cover", nil)
		require.NoError(t, err)

		err = logger.LogNFO("IPX-123", "/path/to/nfo.nfo", nil)
		require.NoError(t, err)

		// Add some failed operations
		testErr := fmt.Errorf("test error")

		err = logger.LogScrape("IPX-456", "http://test.com", nil, testErr)
		require.NoError(t, err)

		err = logger.LogOrganize("IPX-456", "/old", "/new", false, testErr)
		require.NoError(t, err)

		err = logger.LogDownload("IPX-456", "http://test.com/image.jpg", "/local/image.jpg", "cover", testErr)
		require.NoError(t, err)

		err = logger.LogNFO("IPX-456", "/path/to/nfo.nfo", testErr)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryStats(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Should show all operation types (with capital letters)
		assert.Contains(t, stdout, "Scrape")
		assert.Contains(t, stdout, "Organize")
		assert.Contains(t, stdout, "Download")
		assert.Contains(t, stdout, "NFO")
		assert.Contains(t, stdout, "Total Operations: 8")
	})
}

// TestRunHistoryStats_TimeRanges tests stats with different time ranges
func TestRunHistoryStats_TimeRanges(t *testing.T) {
	configPath, _ := setupHistoryTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		logger := history.NewLogger(deps.DB)

		// Create records from different time periods
		now := time.Now()

		// Old record (should be in older range)
		err = logger.LogScrape("OLD-123", "http://test.com", nil, nil)
		require.NoError(t, err)

		// Update timestamp to be old using direct SQL
		deps.DB.DB.Exec("UPDATE histories SET created_at = ? WHERE movie_id = ?", now.Add(-48*time.Hour), "OLD-123")

		// Recent record
		err = logger.LogOrganize("NEW-456", "/old", "/new", false, nil)
		require.NoError(t, err)

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runHistoryStats(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Should show time-based statistics
		assert.Contains(t, stdout, "Total Operations: 2")
	})
}
