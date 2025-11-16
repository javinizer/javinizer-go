package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTagTestDB creates a test database in a temp directory with proper directory structure
func setupTagTestDB(t *testing.T) (configPath string, dbPath string) {
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

// TestRunTagAdd_Success verifies adding tags to a movie
func TestRunTagAdd_Success(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		// Setup database with a test movie
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		// Insert test movie directly (tags don't require movie to exist in some implementations)
		// But it's better practice to have the movie exist
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add single tag
		stdout, _ := captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "Added tag")
		assert.Contains(t, stdout, "favorite")
		assert.Contains(t, stdout, "IPX-535")

		// Verify in database
		repo := database.NewMovieTagRepository(deps.DB)
		tags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, 1, len(tags))
		assert.Equal(t, "favorite", tags[0])
	})
}

// TestRunTagAdd_MultipleTags verifies adding multiple tags at once
func TestRunTagAdd_MultipleTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add multiple tags
		stdout, _ := captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite", "watched", "collection"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "Added")
		assert.Contains(t, stdout, "3 tags")
		assert.Contains(t, stdout, "IPX-535")

		// Verify all tags in database
		repo := database.NewMovieTagRepository(deps.DB)
		tags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, 3, len(tags))
		assert.Contains(t, tags, "favorite")
		assert.Contains(t, tags, "watched")
		assert.Contains(t, tags, "collection")
	})
}

// TestRunTagAdd_DuplicateTag verifies handling of duplicate tags
func TestRunTagAdd_DuplicateTag(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add tag first time
		stdout1, _ := captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})
		assert.Contains(t, stdout1, "Added tag")

		// Add same tag again
		stdout2, _ := captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})

		// Should show warning or skip message
		assert.Contains(t, stdout2, "No new tags added")

		// Verify only one tag in database
		repo := database.NewMovieTagRepository(deps.DB)
		tags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, 1, len(tags), "should not have duplicate tags")
	})
}

// TestRunTagAdd_MultipleMovies verifies adding same tag to multiple movies
func TestRunTagAdd_MultipleMovies(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		// Insert multiple movies
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Movie 1")
		insertTestMovie(t, deps.DB.DB, "ABC-123", "Movie 2")
		insertTestMovie(t, deps.DB.DB, "XYZ-789", "Movie 3")

		cmd := &cobra.Command{}

		// Add same tag to all movies
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"ABC-123", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"XYZ-789", "favorite"}, deps)
			require.NoError(t, err)
		})

		// Verify all have the tag
		repo := database.NewMovieTagRepository(deps.DB)

		tags1, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Contains(t, tags1, "favorite")

		tags2, err := repo.GetTagsForMovie("ABC-123")
		require.NoError(t, err)
		assert.Contains(t, tags2, "favorite")

		tags3, err := repo.GetTagsForMovie("XYZ-789")
		require.NoError(t, err)
		assert.Contains(t, tags3, "favorite")
	})
}

// TestRunTagList_MovieTags verifies listing tags for a specific movie
func TestRunTagList_MovieTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add tags
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite", "watched"}, deps)
			require.NoError(t, err)
		})

		// List tags for movie
		stdout, _ := captureOutput(t, func() {
			err = runTagList(cmd, []string{"IPX-535"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "=== Tags for IPX-535 ===")
		assert.Contains(t, stdout, "favorite")
		assert.Contains(t, stdout, "watched")
		assert.Contains(t, stdout, "Total: 2 tags")
	})
}

// TestRunTagList_NoTags verifies listing when movie has no tags
func TestRunTagList_NoTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// List tags for movie with no tags
		stdout, _ := captureOutput(t, func() {
			err = runTagList(cmd, []string{"IPX-535"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "No tags for IPX-535")
		assert.NotContains(t, stdout, "===")
	})
}

// TestRunTagList_AllTags verifies listing all tag mappings
func TestRunTagList_AllTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		// Insert multiple movies
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Movie 1")
		insertTestMovie(t, deps.DB.DB, "ABC-123", "Movie 2")

		cmd := &cobra.Command{}

		// Add tags to different movies
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"ABC-123", "watched"}, deps)
			require.NoError(t, err)
		})

		// List all tag mappings (no movie ID argument)
		stdout, _ := captureOutput(t, func() {
			err = runTagList(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "=== Movie Tag Mappings ===")
		assert.Contains(t, stdout, "IPX-535")
		assert.Contains(t, stdout, "favorite")
		assert.Contains(t, stdout, "ABC-123")
		assert.Contains(t, stdout, "watched")
		assert.Contains(t, stdout, "Total: 2 movies tagged")
	})
}

// TestRunTagList_EmptyDatabase verifies listing with no tags at all
func TestRunTagList_EmptyDatabase(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		cmd := &cobra.Command{}

		// List all tags with empty database
		stdout, _ := captureOutput(t, func() {
			err = runTagList(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "No tag mappings configured")
		assert.NotContains(t, stdout, "===")
	})
}

// TestRunTagRemove_SpecificTag verifies removing a specific tag from a movie
func TestRunTagRemove_SpecificTag(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add multiple tags
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite", "watched", "collection"}, deps)
			require.NoError(t, err)
		})

		// Remove one tag
		stdout, _ := captureOutput(t, func() {
			err = runTagRemove(cmd, []string{"IPX-535", "watched"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "Removed tag")
		assert.Contains(t, stdout, "watched")
		assert.Contains(t, stdout, "IPX-535")

		// Verify remaining tags
		repo := database.NewMovieTagRepository(deps.DB)
		tags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, 2, len(tags))
		assert.Contains(t, tags, "favorite")
		assert.Contains(t, tags, "collection")
		assert.NotContains(t, tags, "watched")
	})
}

// TestRunTagRemove_AllTags verifies removing all tags from a movie
func TestRunTagRemove_AllTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add tags
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite", "watched"}, deps)
			require.NoError(t, err)
		})

		// Remove all tags (no tag argument)
		stdout, _ := captureOutput(t, func() {
			err = runTagRemove(cmd, []string{"IPX-535"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "Removed all tags")
		assert.Contains(t, stdout, "IPX-535")

		// Verify no tags remain
		repo := database.NewMovieTagRepository(deps.DB)
		tags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, 0, len(tags))
	})
}

// TestRunTagRemove_TagNotFound verifies handling of removing non-existent tag
func TestRunTagRemove_TagNotFound(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add a tag first so database is not empty
		err = runTagAdd(cmd, []string{"IPX-535", "test-tag"}, deps)

		// Get initial tag count
		repo := database.NewMovieTagRepository(deps.DB)
		initialTags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		initialCount := len(initialTags)

		// Try to remove non-existent tag
		stdout, _ := captureOutput(t, func() {
			err = runTagRemove(cmd, []string{"IPX-535", "nonexistent"}, deps)
			require.NoError(t, err)
		})

		// Verify output indicates the operation
		assert.Contains(t, stdout, "IPX-535")

		// Verify database unchanged - existing tags still there
		finalTags, err := repo.GetTagsForMovie("IPX-535")
		require.NoError(t, err)
		assert.Equal(t, initialCount, len(finalTags), "Existing tags should not be affected")
		assert.Contains(t, finalTags, "test-tag", "Original tag should still exist")
	})
}

// TestRunTagSearch_SingleTag verifies searching for movies with a tag
func TestRunTagSearch_SingleTag(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		// Insert multiple movies
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Movie 1")
		insertTestMovie(t, deps.DB.DB, "ABC-123", "Movie 2")
		insertTestMovie(t, deps.DB.DB, "XYZ-789", "Movie 3")

		cmd := &cobra.Command{}

		// Add tags
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"ABC-123", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"XYZ-789", "watched"}, deps)
			require.NoError(t, err)
		})

		// Search for "favorite" tag
		stdout, _ := captureOutput(t, func() {
			err = runTagSearch(cmd, []string{"favorite"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "=== Movies with tag 'favorite' ===")
		assert.Contains(t, stdout, "IPX-535")
		assert.Contains(t, stdout, "ABC-123")
		assert.NotContains(t, stdout, "XYZ-789")
		assert.Contains(t, stdout, "Total: 2 movies")
	})
}

// TestRunTagSearch_NoResults verifies searching with no matching movies
func TestRunTagSearch_NoResults(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Test Movie")

		cmd := &cobra.Command{}

		// Add a tag
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite"}, deps)
			require.NoError(t, err)
		})

		// Search for different tag
		stdout, _ := captureOutput(t, func() {
			err = runTagSearch(cmd, []string{"nonexistent"}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "No movies found with tag 'nonexistent'")
		assert.NotContains(t, stdout, "===")
	})
}

// TestRunTagAllTags_Success verifies listing all unique tags
func TestRunTagAllTags_Success(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		// Insert multiple movies
		insertTestMovie(t, deps.DB.DB, "IPX-535", "Movie 1")
		insertTestMovie(t, deps.DB.DB, "ABC-123", "Movie 2")
		insertTestMovie(t, deps.DB.DB, "XYZ-789", "Movie 3")

		cmd := &cobra.Command{}

		// Add various tags
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"IPX-535", "favorite", "watched"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"ABC-123", "favorite"}, deps)
			require.NoError(t, err)
		})
		captureOutput(t, func() {
			err = runTagAdd(cmd, []string{"XYZ-789", "collection"}, deps)
			require.NoError(t, err)
		})

		// List all tags
		stdout, _ := captureOutput(t, func() {
			err = runTagAllTags(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "=== All Tags ===")
		assert.Contains(t, stdout, "favorite")
		assert.Contains(t, stdout, "watched")
		assert.Contains(t, stdout, "collection")

		// Each tag should show count
		assert.Contains(t, stdout, "2") // favorite appears in 2 movies
	})
}

// TestRunTagAllTags_Empty verifies listing with no tags in database
func TestRunTagAllTags_Empty(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		cmd := &cobra.Command{}

		// List all tags with empty database
		stdout, _ := captureOutput(t, func() {
			err = runTagAllTags(cmd, []string{}, deps)
			require.NoError(t, err)
		})

		assert.Contains(t, stdout, "No tags in database")
		assert.NotContains(t, stdout, "===")
	})
}

// TestRunTagAdd_DatabaseError tests error handling for non-UNIQUE database errors
func TestRunTagAdd_DatabaseError(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		defer deps.Close()

		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		// Close the database to cause an error
		deps.DB.Close()

		cmd := &cobra.Command{}

		// Try to add tag with closed database - should fail with non-UNIQUE error
		err = runTagAdd(cmd, []string{"IPX-535", "test-tag"}, deps)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to add tag")
	})
}

// TestRunTagList_DatabaseError tests error handling in runTagList
func TestRunTagList_DatabaseError(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	withTempConfigFile(t, configPath, func() {
		err := loadConfig()
		require.NoError(t, err)

		deps := createTestDependencies(t, cfg)
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)

		// Close database to trigger error
		deps.DB.Close()

		cmd := &cobra.Command{}

		// Test with movie ID argument
		err = runTagList(cmd, []string{"IPX-123"}, deps)
		assert.Error(t, err)

		// Re-create deps for second test
		deps = createTestDependencies(t, cfg)
		err = deps.DB.AutoMigrate()
		require.NoError(t, err)
		deps.DB.Close()

		// Test without arguments (all tags)
		err = runTagList(cmd, []string{}, deps)
		assert.Error(t, err)
	})
}
