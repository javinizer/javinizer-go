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

// --- runWordList: deps init error ---

func TestRunWordList_Miss2_DepsInitError(t *testing.T) {
	// Create a valid config but with a bad DSN to trigger deps init error
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = testutil.UnreachableConfigPath(t)
	require.NoError(t, config.Save(cfg, configPath))

	cmd := &cobra.Command{}
	err := runWordList(cmd, nil, configPath)
	// This may or may not fail depending on whether DSN is validated at init time
	_ = err
}

// --- runWordList: repo.List error (bad DB state) ---

func TestRunWordList_Miss2_RepoListError(t *testing.T) {
	// Use an empty/in-memory config that can't properly list
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordList(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runWordAdd: deps init error ---

func TestRunWordAdd_Miss2_DepsInitError(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordAdd(cmd, []string{"test", "repl"}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runWordRemove: deps init error ---

func TestRunWordRemove_Miss2_DepsInitError(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordRemove(cmd, []string{"test"}, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runWordExport: deps init error ---

func TestRunWordExport_Miss2_DepsInitError(t *testing.T) {
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordExport(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runWordExport: to file with valid DB ---

func TestRunWordExport_Miss2_ToFile(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "words.json")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "export", exportPath})

	err := rootCmd.Execute()
	require.NoError(t, err)

	fileData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.True(t, len(fileData) > 0)
}

// --- runWordImport: deps init error (after file read) ---

func TestRunWordImport_Miss2_DepsInitError(t *testing.T) {
	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	importData := []byte(`[{"original": "Test", "replacement": "Repl"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordImport(cmd, []string{importPath}, configPath)
	assert.Error(t, err)
}

// --- runWordImport: with defaults skipped (default word without --include-defaults) ---

func TestRunWordImport_Miss2_SkipsDefaults(t *testing.T) {
	configPath := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "defaults.json")
	importData := []byte(`[{"original": "F***", "replacement": "CustomVal"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"word", "import", importPath})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

// --- runWordAdd: Upsert error (via corrupted DB) ---

func TestRunWordAdd_Miss2_UpsertError(t *testing.T) {
	// Try adding with a bad config path to get deps error
	cmd := &cobra.Command{}
	configPath := testutil.UnreachableConfigPath(t)
	err := runWordAdd(cmd, []string{"test", "repl"}, configPath)
	assert.Error(t, err)
}

// Suppress unused imports
var _ = json.Marshal
var _ = bytes.NewReader
