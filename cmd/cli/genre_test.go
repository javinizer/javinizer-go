package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGenreTestDB creates a test database in a temp directory with proper directory structure
func setupGenreTestDB(t *testing.T) (configPath string, dbPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath = filepath.Join(tmpDir, "data", "test.db")

	// Ensure database directory exists
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	configPath, cfg := createTestConfig(t, WithDatabaseDSN(dbPath))

	// Initialize database with migrations to ensure it exists
	db, err := database.New(cfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)
	db.Close()

	return configPath, dbPath
}

// TestRunGenreAdd_Success verifies adding a genre replacement
func TestRunGenreAdd_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		// Load config
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})

		// Verify success message
		assert.Contains(t, stdout, "Genre replacement added")
		assert.Contains(t, stdout, "ドラマ")
		assert.Contains(t, stdout, "Drama")

		// Verify in database
		var replacement models.GenreReplacement
		err = deps.DB.DB.Where("original = ?", "ドラマ").First(&replacement).Error
		require.NoError(t, err)
		assert.Equal(t, "Drama", replacement.Replacement)
	})
}

// TestRunGenreAdd_MultipleReplacements verifies adding multiple genre replacements
func TestRunGenreAdd_MultipleReplacements(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add first replacement
		stdout1, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout1, "Genre replacement added")

		// Add second replacement
		stdout2, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"アクション", "Action"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout2, "Genre replacement added")

		// Add third replacement
		stdout3, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"コメディ", "Comedy"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout3, "Genre replacement added")

		// Verify all in database
		repo := database.NewGenreReplacementRepository(deps.DB)
		replacements, err := repo.List()
		require.NoError(t, err)
		assert.Equal(t, 3, len(replacements))

		// Verify specific entries
		originalToReplacement := make(map[string]string)
		for _, r := range replacements {
			originalToReplacement[r.Original] = r.Replacement
		}

		assert.Equal(t, "Drama", originalToReplacement["ドラマ"])
		assert.Equal(t, "Action", originalToReplacement["アクション"])
		assert.Equal(t, "Comedy", originalToReplacement["コメディ"])
	})
}

// TestRunGenreAdd_Duplicate verifies that duplicate entries are handled (upsert behavior)
func TestRunGenreAdd_Duplicate(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add first time
		stdout1, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout1, "Genre replacement added")

		// Add again with different replacement (should update)
		stdout2, _ := captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Story"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout2, "Genre replacement added")

		// Verify updated in database
		var replacement models.GenreReplacement
		err = deps.DB.DB.Where("original = ?", "ドラマ").First(&replacement).Error
		require.NoError(t, err)
		assert.Equal(t, "Story", replacement.Replacement, "replacement should be updated")

		// Verify only one entry exists (not duplicated)
		repo := database.NewGenreReplacementRepository(deps.DB)
		replacements, err := repo.List()
		require.NoError(t, err)
		assert.Equal(t, 1, len(replacements), "should have exactly one entry")
	})
}

// TestRunGenreList_Success verifies listing genre replacements
func TestRunGenreList_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add some test data
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"アクション", "Action"}, deps)
			require.NoError(t, err)
		})

		// List all replacements
		stdout, _ := captureOutput(t, func() {
			err = runGenreList(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Verify output format
		assert.Contains(t, stdout, "=== Genre Replacements ===")
		assert.Contains(t, stdout, "Original")
		assert.Contains(t, stdout, "Replacement")
		assert.Contains(t, stdout, "ドラマ")
		assert.Contains(t, stdout, "Drama")
		assert.Contains(t, stdout, "アクション")
		assert.Contains(t, stdout, "Action")
		assert.Contains(t, stdout, "Total: 2 replacements")
	})
}

// TestRunGenreList_Empty verifies listing with no genre replacements
func TestRunGenreList_Empty(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		stdout, _ := captureOutput(t, func() {
			err = runGenreList(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Verify empty message
		assert.Contains(t, stdout, "No genre replacements configured")
		assert.NotContains(t, stdout, "===")
		assert.NotContains(t, stdout, "Total:")
	})
}

// TestRunGenreList_OutputFormat verifies the table format of genre list
func TestRunGenreList_OutputFormat(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add test data with varying lengths
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"A", "Short"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"VeryLongOriginalGenreName", "LongReplacementGenreName"}, deps)
			require.NoError(t, err)
		})

		stdout, _ := captureOutput(t, func() {
			err = runGenreList(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		// Verify table structure
		assert.Contains(t, stdout, "=== Genre Replacements ===")
		assert.Contains(t, stdout, "→") // Arrow separator

		// Verify both entries are present
		assert.Contains(t, stdout, "A")
		assert.Contains(t, stdout, "Short")
		assert.Contains(t, stdout, "VeryLongOriginalGenreName")
		assert.Contains(t, stdout, "LongReplacementGenreName")

		// Verify total count
		assert.Contains(t, stdout, "Total: 2 replacements")
	})
}

// TestRunGenreRemove_Success verifies removing a genre replacement
func TestRunGenreRemove_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add a replacement
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})

		// Verify it exists
		var replacement models.GenreReplacement
		err = deps.DB.DB.Where("original = ?", "ドラマ").First(&replacement).Error
		require.NoError(t, err)

		// Remove it
		stdout, _ := captureOutput(t, func() {
			err = runGenreRemove(cmd, []string{"ドラマ"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "Genre replacement removed")
		assert.Contains(t, stdout, "ドラマ")

		// Verify it's gone from database
		err = deps.DB.DB.Where("original = ?", "ドラマ").First(&replacement).Error
		assert.Error(t, err, "should not find removed replacement")
	})
}

// TestRunGenreRemove_NotFound verifies removing non-existent genre replacement
func TestRunGenreRemove_NotFound(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Add some genre to ensure database is not empty
		cmd := &cobra.Command{}
		err = runGenreAdd(cmd, []string{"TestGenre", "Test"}, deps)
		require.NoError(t, err)

		// Get initial count
		var initialCount int64
		deps.DB.DB.Model(&models.GenreReplacement{}).Count(&initialCount)

		// Try to remove non-existent genre
		stdout, _ := captureOutput(t, func() {
			cmd := &cobra.Command{}
			err = runGenreRemove(cmd, []string{"NonExistentGenre"}, deps)
			require.NoError(t, err)
		})

		// Verify output mentions the removal attempt
		assert.Contains(t, stdout, "NonExistentGenre")

		// Verify database unchanged - no rows deleted
		var finalCount int64
		deps.DB.DB.Model(&models.GenreReplacement{}).Count(&finalCount)
		assert.Equal(t, initialCount, finalCount, "No rows should be deleted")
	})
}

// TestRunGenreRemove_MultipleRemoves verifies removing multiple genre replacements
func TestRunGenreRemove_MultipleRemoves(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		cmd := &cobra.Command{}

		// Add multiple replacements
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"ドラマ", "Drama"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"アクション", "Action"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runGenreAdd(cmd, []string{"コメディ", "Comedy"}, deps)
			require.NoError(t, err)
		})

		// Verify all exist
		repo := database.NewGenreReplacementRepository(deps.DB)
		replacements, err := repo.List()
		require.NoError(t, err)
		assert.Equal(t, 3, len(replacements))

		// Remove one
		stdout1, _ := captureOutput(t, func() {
			err = runGenreRemove(cmd, []string{"ドラマ"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout1, "Genre replacement removed")

		// Verify count decreased
		replacements, err = repo.List()
		require.NoError(t, err)
		assert.Equal(t, 2, len(replacements))

		// Remove another
		stdout2, _ := captureOutput(t, func() {
			err = runGenreRemove(cmd, []string{"アクション"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout2, "Genre replacement removed")

		// Verify count decreased again
		replacements, err = repo.List()
		require.NoError(t, err)
		assert.Equal(t, 1, len(replacements))

		// Verify only Comedy remains
		assert.Equal(t, "コメディ", replacements[0].Original)
		assert.Equal(t, "Comedy", replacements[0].Replacement)
	})
}

// TestRunGenreAdd_DatabaseError tests database error handling
func TestRunGenreAdd_DatabaseError(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		// Close database to cause error
		deps.DB.Close()

		cmd := &cobra.Command{}

		err = runGenreAdd(cmd, []string{"original", "replacement"}, deps)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to add genre replacement")
	})
}

// TestRunGenreList_DatabaseError tests error handling in runGenreList
func TestRunGenreList_DatabaseError(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		// Close database to trigger error
		deps.DB.Close()

		cmd := &cobra.Command{}
		err = runGenreList(cmd, []string{}, deps)

		// Should handle error gracefully
		assert.Error(t, err)
	})
}
