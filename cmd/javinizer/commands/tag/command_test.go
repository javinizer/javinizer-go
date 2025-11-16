package tag_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
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
		io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	fn()

	wOut.Close()
	wErr.Close()

	return <-outC, <-errC
}

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
	db.Close()

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

	stdout, _ := captureOutput(t, func() {
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
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// List tags for the movie
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "list", "IPX-535"})
	stdout, _ := captureOutput(t, func() {
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
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "add", "ABC-123", "Collection"})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// List all mappings
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := tag.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"tag", "list"})
	stdout, _ := captureOutput(t, func() {
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
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Remove one tag
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "remove", "IPX-535", "Favorite"})
	stdout, _ := captureOutput(t, func() {
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
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "add", "ABC-123", "Favorite"})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// Search for tag
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := tag.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"tag", "search", "Favorite"})
	stdout, _ := captureOutput(t, func() {
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
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// List all tags
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := tag.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"tag", "tags"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// Verify output
	assert.Contains(t, stdout, "=== All Tags ===")
	assert.Contains(t, stdout, "Favorite")
	assert.Contains(t, stdout, "Collection")
	assert.Contains(t, stdout, "Total: 2 unique tags")
}
