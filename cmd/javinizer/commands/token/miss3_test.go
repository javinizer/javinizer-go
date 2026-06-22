package token

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

// setupMiss3DB creates a test config + migrated DB for miss3-coverage tests.
func setupMiss3DB(t *testing.T) string {
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

// --- RunCreate: successful creation ---

func TestMiss3_RunCreate_Success(t *testing.T) {
	configPath := setupMiss3DB(t)

	cmd := newCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "miss3-test"))

	result, err := RunCreate(cmd, nil, configPath)
	require.NoError(t, err)
	assert.Equal(t, "miss3-test", result.Name)
	assert.NotEmpty(t, result.Token)
	assert.NotEmpty(t, result.ID)
}

// --- RunCreate: config error ---

func TestMiss3_RunCreate_ConfigError(t *testing.T) {
	cmd := newCreateCommand()
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunCreate(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- RunRevoke: by full ID ---

func TestMiss3_RunRevoke_ByID(t *testing.T) {
	configPath := setupMiss3DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-miss3"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	result, err := RunRevoke(&cobra.Command{}, []string{createResult.ID}, configPath)
	require.NoError(t, err)
	assert.True(t, result.Revoked)
}

// --- RunRevoke: by prefix ---

func TestMiss3_RunRevoke_ByPrefix(t *testing.T) {
	configPath := setupMiss3DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-prefix-miss3"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	result, err := RunRevoke(&cobra.Command{}, []string{"jv_" + createResult.TokenPrefix}, configPath)
	require.NoError(t, err)
	assert.True(t, result.Revoked)
}

// --- RunList: empty list ---

func TestMiss3_RunList_Empty(t *testing.T) {
	configPath := setupMiss3DB(t)

	entries, err := RunList(&cobra.Command{}, nil, configPath)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// --- RunList: with tokens ---

func TestMiss3_RunList_WithTokens(t *testing.T) {
	configPath := setupMiss3DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "list-miss3"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	entries, err := RunList(&cobra.Command{}, nil, configPath)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Equal(t, "list-miss3", entries[0].Name)
}

// --- runCreate: text output with named token ---

func TestMiss3_RunCreate_TextNamed(t *testing.T) {
	configPath := setupMiss3DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create", "--name", "miss3-named-text"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token created successfully!")
	assert.Contains(t, output, "miss3-named-text")
}

// --- runCreate: text output with unnamed token ---

func TestMiss3_RunCreate_TextUnnamed(t *testing.T) {
	configPath := setupMiss3DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "(unnamed)")
}

// --- runCreate: JSON output ---

func TestMiss3_RunCreate_JSON(t *testing.T) {
	configPath := setupMiss3DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create", "--name", "miss3-json", "--json"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
	assert.Equal(t, "miss3-json", result["name"])
}

// --- runRevoke: by ID text output ---

func TestMiss3_RunRevoke_TextByID(t *testing.T) {
	configPath := setupMiss3DB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-text-miss3"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "revoke", createResult.ID})

	err = rootCmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Token revoked successfully!")
}

// --- runList: empty list text output ---

func TestMiss3_RunList_EmptyText(t *testing.T) {
	configPath := setupMiss3DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "No active tokens found.")
}

// --- runList: with tokens text output (tabwriter) ---

func TestMiss3_RunList_WithTokensText(t *testing.T) {
	configPath := setupMiss3DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "list-text-miss3"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "list-text-miss3")
	assert.Contains(t, buf.String(), "never")
}

// --- RunRevoke: prefix too short ---

func TestMiss3_RunRevoke_PrefixTooShort(t *testing.T) {
	configPath := setupMiss3DB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_abc"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "prefix too short")
}

// --- RunRevoke: prefix not found ---

func TestMiss3_RunRevoke_PrefixNotFound(t *testing.T) {
	configPath := setupMiss3DB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_zzzzzzzz"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// Suppress unused imports
var _ = context.Background
var _ = os.ReadFile
