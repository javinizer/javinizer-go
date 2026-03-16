package tag_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper

func setupTagTestDB(t *testing.T) (configPath string, dbPath string) {
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

// TestRunTagAdd_Success verifies adding a tag to a movie
func TestRunTagAdd_Success(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the tag add subcommand
	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite"})

	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify success message
	assert.Contains(t, stdout, "Added tag")
	assert.Contains(t, stdout, "Favorite")
	assert.Contains(t, stdout, "IPX-535")
}

// TestRunTagList_ForMovie verifies listing tags for a specific movie
func TestRunTagList_ForMovie(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add some tags first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// List tags for the movie
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "list", "IPX-535"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// Verify output
	assert.Contains(t, stdout, "=== Tags for IPX-535 ===")
	assert.Contains(t, stdout, "Favorite")
	assert.Contains(t, stdout, "Collection")
	assert.Contains(t, stdout, "Total: 2 tags")
}

// TestRunTagList_AllMappings verifies listing all tag mappings
func TestRunTagList_AllMappings(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add tags to multiple movies
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "add", "ABC-123", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// List all mappings
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := tag.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"tag", "list"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	// Verify output
	assert.Contains(t, stdout, "=== Movie Tag Mappings ===")
	assert.Contains(t, stdout, "IPX-535")
	assert.Contains(t, stdout, "ABC-123")
	assert.Contains(t, stdout, "Total: 2 movies tagged")
}

// TestRunTagRemove_SpecificTag verifies removing a specific tag from a movie
func TestRunTagRemove_SpecificTag(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add tags first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Remove one tag
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "remove", "IPX-535", "Favorite"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Removed tag")
	assert.Contains(t, stdout, "Favorite")
}

// TestRunTagSearch_Success verifies searching for movies by tag
func TestRunTagSearch_Success(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add tags to multiple movies
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "add", "ABC-123", "Favorite"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// Search for tag
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := tag.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"tag", "search", "Favorite"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	// Verify output
	assert.Contains(t, stdout, "=== Movies with tag 'Favorite' ===")
	assert.Contains(t, stdout, "IPX-535")
	assert.Contains(t, stdout, "ABC-123")
	assert.Contains(t, stdout, "Total: 2 movies")
}

// TestRunTagAllTags_Success verifies listing all unique tags
func TestRunTagAllTags_Success(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add different tags
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// List all tags
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "tags"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// Verify output
	assert.Contains(t, stdout, "=== All Tags ===")
	assert.Contains(t, stdout, "Favorite")
	assert.Contains(t, stdout, "Collection")
	assert.Contains(t, stdout, "Total: 2 unique tags")
}

// Error path tests

// TestRunTagAdd_InvalidConfig tests config loading error
func TestRunTagAdd_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", "/nonexistent/invalid/path/config.yaml", "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite"})
	err := rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunTagAdd_DependencyInitError tests dependency initialization error
func TestRunTagAdd_DependencyInitError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a config with invalid database path
	testCfg := config.DefaultConfig()
	// Use a file as if it were a directory - this will cause DB creation to fail
	dbFilePath := filepath.Join(tmpDir, "blockfile")
	// Create a regular file at this path
	err := os.WriteFile(dbFilePath, []byte("block"), 0444)
	require.NoError(t, err)

	// Try to use this file as a directory for the DB
	testCfg.Database.DSN = filepath.Join(dbFilePath, "test.db")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite"})
	err = rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
}

// TestRunTagAdd_DuplicateTags tests scenario where all tags already exist
func TestRunTagAdd_DuplicateTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add tags first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Try to add same tags again
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No new tags added")
	assert.Contains(t, stdout, "all already exist")
}

// TestRunTagList_InvalidConfig tests config loading error
func TestRunTagList_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", "/nonexistent/invalid/path/config.yaml", "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "list", "IPX-535"})
	err := rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunTagList_DependencyInitError tests dependency initialization error
func TestRunTagList_DependencyInitError(t *testing.T) {
	tmpDir := t.TempDir()

	testCfg := config.DefaultConfig()
	dbFilePath := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(dbFilePath, []byte("block"), 0444)
	require.NoError(t, err)

	testCfg.Database.DSN = filepath.Join(dbFilePath, "test.db")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "list", "IPX-535"})
	err = rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
}

// TestRunTagList_NoTagsForMovie tests movie with no tags
func TestRunTagList_NoTagsForMovie(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "list", "NONEXISTENT-999"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No tags for NONEXISTENT-999")
}

// TestRunTagList_EmptyDatabase tests listing all mappings when database is empty
func TestRunTagList_EmptyDatabase(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "list"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No tag mappings configured")
}

// TestRunTagRemove_InvalidConfig tests config loading error
func TestRunTagRemove_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", "/nonexistent/invalid/path/config.yaml", "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "remove", "IPX-535", "Favorite"})
	err := rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunTagRemove_DependencyInitError tests dependency initialization error
func TestRunTagRemove_DependencyInitError(t *testing.T) {
	tmpDir := t.TempDir()

	testCfg := config.DefaultConfig()
	dbFilePath := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(dbFilePath, []byte("block"), 0444)
	require.NoError(t, err)

	testCfg.Database.DSN = filepath.Join(dbFilePath, "test.db")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "remove", "IPX-535", "Favorite"})
	err = rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
}

// TestRunTagRemove_AllTags tests removing all tags from a movie
func TestRunTagRemove_AllTags(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	// Add tags first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "add", "IPX-535", "Favorite", "Collection"})
	testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Remove all tags (no tag specified)
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "remove", "IPX-535"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Removed all tags from IPX-535")

	// Verify tags were removed
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := tag.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"tag", "list", "IPX-535"})
	stdout2, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout2, "No tags for IPX-535")
}

// TestRunTagSearch_InvalidConfig tests config loading error
func TestRunTagSearch_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", "/nonexistent/invalid/path/config.yaml", "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "search", "Favorite"})
	err := rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunTagSearch_DependencyInitError tests dependency initialization error
func TestRunTagSearch_DependencyInitError(t *testing.T) {
	tmpDir := t.TempDir()

	testCfg := config.DefaultConfig()
	dbFilePath := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(dbFilePath, []byte("block"), 0444)
	require.NoError(t, err)

	testCfg.Database.DSN = filepath.Join(dbFilePath, "test.db")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "search", "Favorite"})
	err = rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
}

// TestRunTagSearch_NoResults tests searching for a tag with no movies
func TestRunTagSearch_NoResults(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "search", "NonexistentTag"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No movies found with tag 'NonexistentTag'")
}

// TestRunTagAllTags_InvalidConfig tests config loading error
func TestRunTagAllTags_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", "/nonexistent/invalid/path/config.yaml", "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "tags"})
	err := rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunTagAllTags_DependencyInitError tests dependency initialization error
func TestRunTagAllTags_DependencyInitError(t *testing.T) {
	tmpDir := t.TempDir()

	testCfg := config.DefaultConfig()
	dbFilePath := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(dbFilePath, []byte("block"), 0444)
	require.NoError(t, err)

	testCfg.Database.DSN = filepath.Join(dbFilePath, "test.db")

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "tags"})
	err = rootCmd.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
}

// TestRunTagAllTags_EmptyDatabase tests listing tags when database is empty
func TestRunTagAllTags_EmptyDatabase(t *testing.T) {
	configPath, _ := setupTagTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := tag.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"tag", "tags"})
	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No tags in database")
}
