package word

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMissTestDB creates a config + migrated DB for miss-coverage tests.
func setupMissTestDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "test.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	require.NoError(t, db.Close())
	return configPath
}

// --- runWordList branches (lines 82-110) ---

func TestRunWordList_InvalidConfig(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordList(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunWordList_EmptyAfterRemovingDefaults(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Remove all default word replacements to test the "No word replacements" path
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	for _, r := range replacements {
		require.NoError(t, repo.Delete(context.Background(), r.Original))
	}

	// Now list should show "No word replacements configured"
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	// Output goes to fmt.Println, not captured here but we verified no error
}

// --- runWordAdd error paths (lines 122-139) ---

func TestRunWordAdd_InvalidConfig(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordAdd(cmd, []string{"test", "replace"}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runWordRemove branches (lines 152-164) ---

func TestRunWordRemove_InvalidConfig(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordRemove(cmd, []string{"test"}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunWordRemove_Success(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word first
	addCmd := &cobra.Command{}
	err := runWordAdd(addCmd, []string{"RemoveTest", "Replaced"}, configPath)
	require.NoError(t, err)

	// Remove it via cobra command to capture output
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "remove", "RemoveTest"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	// Output via fmt.Printf
}

func TestRunWordRemove_Nonexistent(t *testing.T) {
	configPath := setupMissTestDB(t)

	cmd := &cobra.Command{}
	err := runWordRemove(cmd, []string{"NonexistentWord12345"}, configPath)
	// GORM Delete on nonexistent record is a no-op, not an error
	assert.NoError(t, err)
}

// --- runWordExport branches (lines 175-206) ---

func TestRunWordExport_InvalidConfig(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordExport(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunWordExport_ToFile(t *testing.T) {
	configPath := setupMissTestDB(t)

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "words.json")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "export", exportPath})

	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify file was created
	fileData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.True(t, len(fileData) > 0)
}

func TestRunWordExport_WriteFileError(t *testing.T) {
	configPath := setupMissTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "export", "/nonexistent/deep/dir/words.json"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write file")
}

func TestRunWordExport_StdoutOutput(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Export to stdout (no file arg)
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := runWordExport(cmd, nil, configPath)
	require.NoError(t, err)
	// Data should be written to cmd.OutOrStdout()
	assert.True(t, buf.Len() > 0)
}

// --- runWordImport branches (lines 219-270) ---

func TestRunWordImport_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	importData := []byte(`[{"original": "Test", "replacement": "Repl"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordImport(cmd, []string{importPath}, configPath)
	assert.Error(t, err)
}

func TestRunWordImport_FileReadError(t *testing.T) {
	configPath := setupMissTestDB(t)

	cmd := &cobra.Command{}
	err := runWordImport(cmd, []string{"/nonexistent/file.json"}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestRunWordImport_SkipsIdenticalExisting(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word
	addCmd := &cobra.Command{}
	err := runWordAdd(addCmd, []string{"SkipIdentical", "ReplacedVal"}, configPath)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	// Import same word — should be skipped
	importData := []byte(`[{"original": "SkipIdentical", "replacement": "ReplacedVal"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "import", importPath})

	err = rootCmd.Execute()
	require.NoError(t, err)
	// Output via fmt.Printf
}

func TestRunWordImport_UpsertsChangedReplacement(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word
	addCmd := &cobra.Command{}
	err := runWordAdd(addCmd, []string{"UpsertWord", "OldReplacement"}, configPath)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	// Import with different replacement — should upsert
	importData := []byte(`[{"original": "UpsertWord", "replacement": "NewReplacement"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "import", importPath})

	err = rootCmd.Execute()
	require.NoError(t, err)
}

func TestRunWordImport_EmptyArray(t *testing.T) {
	configPath := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`[]`), 0644))

	cmd := &cobra.Command{}
	err := runWordImport(cmd, []string{importPath}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no word replacements found")
}

func TestRunWordImport_InvalidJSON(t *testing.T) {
	configPath := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`{bad}`), 0644))

	cmd := &cobra.Command{}
	err := runWordImport(cmd, []string{importPath}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

func TestRunWordImport_WithIncludeDefaults(t *testing.T) {
	configPath := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "defaults.json")
	importData := []byte(`[
		{"original": "F***", "replacement": "CustomOverride"},
		{"original": "CustomWord", "replacement": "CustomReplacement"}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "import", importPath, "--include-defaults"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// --- Verify list with populated data (tabwriter path) ---

func TestRunWordList_WithData(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word first
	addCmd := &cobra.Command{}
	err := runWordAdd(addCmd, []string{"ListTestWord", "ListRepl"}, configPath)
	require.NoError(t, err)

	// List via cobra command
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	// Output via fmt.Println
}

// --- runWordExport stdout output (line 188) ---

func TestRunWordExport_StdoutWithWriter(t *testing.T) {
	configPath := setupMissTestDB(t)

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := runWordExport(cmd, nil, configPath)
	require.NoError(t, err)
	// Data should be written to cmd.OutOrStdout()
	assert.True(t, buf.Len() > 0)
}

// --- runWordList with empty list after removing defaults (full coverage) ---

func TestRunWordList_EmptyViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Remove all word replacements
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	for _, r := range replacements {
		require.NoError(t, repo.Delete(context.Background(), r.Original))
	}

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
}

// --- runWordAdd with upsert (duplicate original) ---

func TestRunWordAdd_UpsertDuplicate(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word
	cmd1 := &cobra.Command{}
	err := runWordAdd(cmd1, []string{"UpsertTest", "FirstValue"}, configPath)
	require.NoError(t, err)

	// Add same word with different replacement — should upsert
	cmd2 := &cobra.Command{}
	err = runWordAdd(cmd2, []string{"UpsertTest", "SecondValue"}, configPath)
	require.NoError(t, err)

	// Verify the replacement was updated
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	found, err := repo.FindByOriginal(context.Background(), "UpsertTest")
	require.NoError(t, err)
	assert.Equal(t, "SecondValue", found.Replacement)
}

// --- runWordRemove via cobra command ---

func TestRunWordRemove_ViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Add a word first
	addCmd := &cobra.Command{}
	err := runWordAdd(addCmd, []string{"CobraRemove", "Replaced"}, configPath)
	require.NoError(t, err)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "remove", "CobraRemove"})

	err = rootCmd.Execute()
	require.NoError(t, err)
}

// --- runWordImport with defaults skipped and include-defaults flag ---

func TestRunWordImport_SkipsDefaultsViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "defaults.json")
	importData := []byte(`[
		{"original": "F***", "replacement": "CustomOverride"},
		{"original": "UniqueWord", "replacement": "UniqueRepl"}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "import", importPath})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// Suppress json import
var _ = json.Marshal
