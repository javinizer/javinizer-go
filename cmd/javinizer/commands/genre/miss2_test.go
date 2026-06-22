package genre_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"testing"

	genrecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMiss2TestDB creates a config + migrated DB for miss2-coverage tests.
func setupMiss2TestDB(t *testing.T) string {
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

// --- genre add via cobra ---

func TestRunAdd_Miss2_ViaCobra(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "add", "TestGenre", "ReplacedGenre"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// --- genre add with invalid config ---

func TestRunAdd_Miss2_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "add", "HD", "HighDef"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre list via cobra ---

func TestRunList_Miss2_ViaCobra(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// --- genre list with invalid config ---

func TestRunList_Miss2_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "list"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre remove via cobra ---

func TestRunRemove_Miss2_ViaCobra(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	// Add first, then remove
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "add", "RemoveTestGenre", "Replaced"})
	require.NoError(t, rootCmd.Execute())

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genrecmd.NewCommand()
	rootCmd2.AddCommand(cmd2)
	rootCmd2.SetArgs([]string{"genre", "remove", "RemoveTestGenre"})

	err := rootCmd2.Execute()
	require.NoError(t, err)
}

// --- genre remove with invalid config ---

func TestRunRemove_Miss2_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "remove", "HD"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre export to file ---

func TestRunGenreExport_Miss2_ToFile(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "genres.json")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "export", exportPath})

	err := rootCmd.Execute()
	require.NoError(t, err)

	fileData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.True(t, len(fileData) > 0)
}

// --- genre export to stdout ---

func TestRunGenreExport_Miss2_Stdout(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"genre", "export"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.True(t, buf.Len() > 0)
}

// --- genre export with invalid config ---

func TestRunGenreExport_Miss2_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "export"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre import via cobra ---

func TestRunGenreImport_Miss2_ViaCobra(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "genres.json")
	importData := []byte(`[{"original": "HD", "replacement": "High Definition"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// --- genre import with nonexistent file ---

func TestRunGenreImport_Miss2_FileReadError(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "import", "/nonexistent/file.json"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre import with invalid JSON ---

func TestRunGenreImport_Miss2_InvalidJSON(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`{bad}`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre import with empty array ---

func TestRunGenreImport_Miss2_EmptyArray(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`[]`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre export to write-protected path ---

func TestRunGenreExport_Miss2_WriteError(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "export", "/nonexistent/deep/dir/genres.json"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- genre import with skipped existing (same replacement) ---

func TestRunGenreImport_Miss2_SkippedExisting(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	// Add a genre replacement first
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := genrecmd.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"genre", "add", "SkipTest", "SkippedRepl"})
	require.NoError(t, rootCmd.Execute())

	// Import the same replacement — should be skipped
	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "genres.json")
	importData := []byte(`[{"original": "SkipTest", "replacement": "SkippedRepl"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := genrecmd.NewCommand()
	rootCmd2.AddCommand(cmd2)
	rootCmd2.SetArgs([]string{"genre", "import", importPath})

	err := rootCmd2.Execute()
	require.NoError(t, err)
}

// Suppress unused imports
var _ = json.Marshal
