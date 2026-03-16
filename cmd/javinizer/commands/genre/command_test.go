package genre_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
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

func setupGenreTestDB(t *testing.T) (configPath string, dbPath string) {
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
	require.NoError(t, db.Close())

	return configPath, dbPath
}

// Tests

// TestRunGenreAdd_Success verifies adding a genre replacement
func TestRunGenreAdd_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the genre add subcommand
	rootCmd.SetArgs([]string{"genre", "add", "ドラマ", "Drama"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify success message
	assert.Contains(t, stdout, "Genre replacement added")
	assert.Contains(t, stdout, "ドラマ")
	assert.Contains(t, stdout, "Drama")

	// Verify in database
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	var replacement models.GenreReplacement
	err = db.DB.Where("original = ?", "ドラマ").First(&replacement).Error
	require.NoError(t, err)
	assert.Equal(t, "Drama", replacement.Replacement)
}

// TestRunGenreAdd_MultipleReplacements verifies adding multiple genre replacements
func TestRunGenreAdd_MultipleReplacements(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	// Add first replacement
	rootCmd.SetArgs([]string{"genre", "add", "ドラマ", "Drama"})
	stdout1, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout1, "Genre replacement added")

	// Reset command for second execution
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genre.NewCommand()
	rootCmd2.AddCommand(cmd2)

	// Add second replacement
	rootCmd2.SetArgs([]string{"genre", "add", "アクション", "Action"})
	stdout2, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout2, "Genre replacement added")

	// Reset command for third execution
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := genre.NewCommand()
	rootCmd3.AddCommand(cmd3)

	// Add third replacement
	rootCmd3.SetArgs([]string{"genre", "add", "コメディ", "Comedy"})
	stdout3, _ := captureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout3, "Genre replacement added")

	// Verify all in database
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	repo := database.NewGenreReplacementRepository(db)
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
}

// TestRunGenreAdd_Duplicate verifies that duplicate entries are handled (upsert behavior)
func TestRunGenreAdd_Duplicate(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	// Add first time
	rootCmd.SetArgs([]string{"genre", "add", "ドラマ", "Drama"})
	stdout1, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout1, "Genre replacement added")

	// Reset command for second execution
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genre.NewCommand()
	rootCmd2.AddCommand(cmd2)

	// Add again with different replacement (should update)
	rootCmd2.SetArgs([]string{"genre", "add", "ドラマ", "Story"})
	stdout2, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout2, "Genre replacement added")

	// Verify updated in database
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var replacement models.GenreReplacement
	err = db.DB.Where("original = ?", "ドラマ").First(&replacement).Error
	require.NoError(t, err)
	assert.Equal(t, "Story", replacement.Replacement, "replacement should be updated")

	// Verify only one entry exists (not duplicated)
	repo := database.NewGenreReplacementRepository(db)
	replacements, err := repo.List()
	require.NoError(t, err)
	assert.Equal(t, 1, len(replacements), "should have exactly one entry")
}

// TestRunGenreList_Success verifies listing genre replacements
func TestRunGenreList_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Add some test data first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"genre", "add", "ドラマ", "Drama"})
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genre.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"genre", "add", "アクション", "Action"})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	// List all replacements
	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := genre.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"genre", "list"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd3.Execute()
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
}

// TestRunGenreList_Empty verifies listing with no genre replacements
func TestRunGenreList_Empty(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the genre list subcommand
	rootCmd.SetArgs([]string{"genre", "list"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify empty message
	assert.Contains(t, stdout, "No genre replacements configured")
	assert.NotContains(t, stdout, "===")
	assert.NotContains(t, stdout, "Total:")
}

// TestRunGenreRemove_Success verifies removing a genre replacement
func TestRunGenreRemove_Success(t *testing.T) {
	configPath, _ := setupGenreTestDB(t)

	// Add a replacement first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genre.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"genre", "add", "ドラマ", "Drama"})
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify it exists
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var replacement models.GenreReplacement
	err = db.DB.Where("original = ?", "ドラマ").First(&replacement).Error
	require.NoError(t, err)

	// Remove it
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genre.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"genre", "remove", "ドラマ"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Genre replacement removed")
	assert.Contains(t, stdout, "ドラマ")

	// Verify it's gone from database
	err = db.DB.Where("original = ?", "ドラマ").First(&replacement).Error
	assert.Error(t, err, "should not find removed replacement")
}
