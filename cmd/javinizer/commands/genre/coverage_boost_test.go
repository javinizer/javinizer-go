package genre

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBoostDB creates a test config + migrated DB for genre coverage boost tests.
func setupBoostDB(t *testing.T) string {
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

// buildGenreRoot creates a cobra root command wired with the genre subcommand.
func buildGenreRoot(configPath string) *cobra.Command {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	return rootCmd
}

// captureBoostOutput captures stdout during fn execution.
func captureBoostOutput(t *testing.T, fn func()) string {
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

// --- runAdd: success path ---

func TestBoost_RunAdd_Success(t *testing.T) {
	configPath := setupBoostDB(t)

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Action", "ActionDrama"})

	output := captureBoostOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Genre replacement added")
	assert.Contains(t, output, "Action")
	assert.Contains(t, output, "ActionDrama")
}

// --- runAdd: upsert (duplicate with different replacement) ---

func TestBoost_RunAdd_Upsert(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add first time
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Drama", "Dramatic"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Add again with different replacement
	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "add", "Drama", "Story"})
	err = rootCmd2.Execute()
	require.NoError(t, err)

	// Verify only one entry exists with updated value
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewGenreReplacementRepository(db)
	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, replacements, 1)
	assert.Equal(t, "Story", replacements[0].Replacement)
}

// --- runList: empty list ---

func TestBoost_RunList_Empty(t *testing.T) {
	configPath := setupBoostDB(t)

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "list"})

	output := captureBoostOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No genre replacements configured")
}

// --- runList: with data ---

func TestBoost_RunList_WithData(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add some data first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Comedy", "Funny"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "list"})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "=== Genre Replacements ===")
	assert.Contains(t, output, "Comedy")
	assert.Contains(t, output, "Funny")
	assert.Contains(t, output, "Total: 1 replacements")
}

// --- runRemove: success ---

func TestBoost_RunRemove_Success(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add a replacement first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Horror", "Scary"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Remove it
	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "remove", "Horror"})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Genre replacement removed")
	assert.Contains(t, output, "Horror")
}

// --- runExport: to stdout ---

func TestBoost_RunExport_Stdout(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add data first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "SciFi", "Science Fiction"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Export to stdout
	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "export"})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "SciFi")
	assert.Contains(t, output, "Science Fiction")
	assert.Contains(t, output, "Exported 1 genre replacement(s) to stdout")
}

// --- runExport: to file ---

func TestBoost_RunExport_ToFile(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add data first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Thriller", "Suspense"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Export to file
	exportPath := filepath.Join(t.TempDir(), "export.json")
	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "export", exportPath})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Exported 1 genre replacement(s) to")

	data, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Thriller")
	assert.Contains(t, string(data), "Suspense")
}

// --- runExport: empty ---

func TestBoost_RunExport_Empty(t *testing.T) {
	configPath := setupBoostDB(t)

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "export"})

	output := captureBoostOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "[]")
	assert.Contains(t, output, "Exported 0 genre replacement(s)")
}

// --- runGenreImport: valid import ---

func TestBoost_RunImport_Valid(t *testing.T) {
	configPath := setupBoostDB(t)

	// Create import file
	importData := []byte(`[
		{"original": "Mystery", "replacement": "Whodunit"},
		{"original": "Romance", "replacement": "Love Story"}
	]`)
	importPath := filepath.Join(t.TempDir(), "import.json")
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	output := captureBoostOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Imported: 2")
	assert.Contains(t, output, "Skipped: 0")
	assert.Contains(t, output, "Errors: 0")
}

// --- runGenreImport: empty array ---

func TestBoost_RunImport_EmptyArray(t *testing.T) {
	configPath := setupBoostDB(t)

	importPath := filepath.Join(t.TempDir(), "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`[]`), 0644))

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no genre replacements found")
}

// --- runGenreImport: invalid JSON ---

func TestBoost_RunImport_InvalidJSON(t *testing.T) {
	configPath := setupBoostDB(t)

	importPath := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`{bad}`), 0644))

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

// --- runGenreImport: nonexistent file ---

func TestBoost_RunImport_NonexistentFile(t *testing.T) {
	configPath := setupBoostDB(t)

	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "import", "/nonexistent/file.json"})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

// --- runGenreImport: skips identical existing entries ---

func TestBoost_RunImport_SkipsIdentical(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add a replacement first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Existing", "ExistingValue"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Import same entry
	importData := []byte(`[{"original": "Existing", "replacement": "ExistingValue"}]`)
	importPath := filepath.Join(t.TempDir(), "same.json")
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "import", importPath})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Skipped: 1")
	assert.Contains(t, output, "Imported: 0")
}

// --- runGenreImport: upserts different replacement ---

func TestBoost_RunImport_UpsertsDifferent(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add a replacement first
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Existing", "OldValue"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Import with different replacement
	importData := []byte(`[{"original": "Existing", "replacement": "NewValue"}]`)
	importPath := filepath.Join(t.TempDir(), "update.json")
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "import", importPath})

	output := captureBoostOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Imported: 1")

	// Verify replacement was updated
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewGenreReplacementRepository(db)
	entry, err := repo.FindByOriginal(context.Background(), "Existing")
	require.NoError(t, err)
	assert.Equal(t, "NewValue", entry.Replacement)
}

// --- error paths: invalid config ---

func TestBoost_RunAdd_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"genre", "add", "A", "B"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestBoost_RunList_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"genre", "list"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestBoost_RunRemove_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"genre", "remove", "A"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestBoost_RunExport_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"genre", "export"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- runExport: sorted output verification ---

func TestBoost_RunExport_SortedOutput(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add in reverse alphabetical order
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Zebra", "Z"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "add", "Apple", "A"})
	err = rootCmd2.Execute()
	require.NoError(t, err)

	// Export should be sorted by original
	rootCmd3 := buildGenreRoot(configPath)
	rootCmd3.SetArgs([]string{"genre", "export"})

	output := captureBoostOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	appleIdx := bytes.Index([]byte(output), []byte("Apple"))
	zebraIdx := bytes.Index([]byte(output), []byte("Zebra"))
	assert.Less(t, appleIdx, zebraIdx, "Apple should appear before Zebra in sorted export")
}

// --- JSON round-trip verification for export/import ---

func TestBoost_ExportImportRoundtrip(t *testing.T) {
	configPath := setupBoostDB(t)

	// Add some data
	rootCmd := buildGenreRoot(configPath)
	rootCmd.SetArgs([]string{"genre", "add", "Alpha", "AlphaR"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	rootCmd2 := buildGenreRoot(configPath)
	rootCmd2.SetArgs([]string{"genre", "add", "Beta", "BetaR"})
	err = rootCmd2.Execute()
	require.NoError(t, err)

	// Export to file
	exportPath := filepath.Join(t.TempDir(), "rt.json")
	rootCmd3 := buildGenreRoot(configPath)
	rootCmd3.SetArgs([]string{"genre", "export", exportPath})

	captureBoostOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	// Verify exported JSON is valid
	data, err := os.ReadFile(exportPath)
	require.NoError(t, err)

	var replacements []models.GenreReplacement
	require.NoError(t, json.Unmarshal(data, &replacements))
	assert.Len(t, replacements, 2)
}
