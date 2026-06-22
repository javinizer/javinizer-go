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

// setupMiss2DB creates a test config + migrated DB for miss2-coverage tests.
func setupMiss2DB(t *testing.T) string {
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

// --- NewCommand structure and subcommands ---

func TestNewCommand_HasSubcommands(t *testing.T) {
	cmd := NewCommand()
	assert.Equal(t, "token", cmd.Use)
	assert.NotNil(t, cmd.PersistentFlags().Lookup("json"))

	subcmds := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subcmds {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["create"])
	assert.True(t, subNames["revoke"])
	assert.True(t, subNames["list"])
}

// --- runCreate with named token via cobra command (text output path) ---

func TestMiss2_RunCreate_NamedTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create", "--name", "miss2-named"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token created successfully!")
	assert.Contains(t, output, "miss2-named")
	assert.Contains(t, output, "Token:")
	assert.Contains(t, output, "This token value will NOT be shown again")
}

// --- runCreate unnamed token text output ---

func TestMiss2_RunCreate_UnnamedTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "(unnamed)")
	assert.Contains(t, output, "Token:")
}

// --- runCreate JSON output ---

func TestMiss2_RunCreate_JSONOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create", "--name", "json-test2", "--json"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
	assert.Equal(t, "json-test2", result["name"])
	assert.NotEmpty(t, result["token"])
	assert.NotEmpty(t, result["id"])
}

// --- runRevoke by full ID text output ---

func TestMiss2_RunRevoke_ByIDTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-text-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke via cobra command with text output
	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "revoke", createResult.ID})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token revoked successfully!")
	assert.Contains(t, output, "jv_")
}

// --- runRevoke by prefix text output ---

func TestMiss2_RunRevoke_ByPrefixTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-prefix-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke via prefix
	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "revoke", "jv_" + createResult.TokenPrefix})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token revoked successfully!")
}

// --- runRevoke JSON output ---

func TestMiss2_RunRevoke_JSONOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-json-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke with JSON output
	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "revoke", createResult.ID, "--json"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
	assert.Equal(t, true, result["revoked"])
}

// --- runList empty list text output ---

func TestMiss2_RunList_EmptyTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

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

// --- runList with tokens text output (tabwriter path) ---

func TestMiss2_RunList_WithTokensTextOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "list-text-test"))
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

	output := buf.String()
	assert.Contains(t, output, "ID")
	assert.Contains(t, output, "PREFIX")
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "CREATED")
	assert.Contains(t, output, "LAST USED")
	assert.Contains(t, output, "list-text-test")
	assert.Contains(t, output, "never")
}

// --- runList JSON output ---

func TestMiss2_RunList_JSONOutput(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "list-json-test2"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "list", "--json"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &entries))
	require.NotEmpty(t, entries)
	assert.Equal(t, "list-json-test2", entries[0]["name"])
}

// --- RunCreate with config error ---

func TestMiss2_RunCreate_ConfigError(t *testing.T) {
	cmd := newCreateCommand()
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunCreate(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- RunRevoke with prefix too short ---

func TestMiss2_RunRevoke_PrefixTooShort(t *testing.T) {
	configPath := setupMiss2DB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_abc"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "prefix too short")
}

// --- RunRevoke with prefix not found ---

func TestMiss2_RunRevoke_PrefixNotFound(t *testing.T) {
	configPath := setupMiss2DB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_zzzzzzzz"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- RunRevoke with nonexistent UUID ---

func TestMiss2_RunRevoke_NonexistentUUID(t *testing.T) {
	configPath := setupMiss2DB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"nonexistent-uuid-99999"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- shortID function edge cases ---

func TestMiss2_ShortID_ShortString(t *testing.T) {
	assert.Equal(t, "abc", shortID("abc"))
	assert.Equal(t, "12345678", shortID("12345678"))
	assert.Equal(t, "12345678", shortID("1234567890"))
}

// --- printJSON success ---

func TestMiss2_PrintJSON_Success(t *testing.T) {
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := printJSON(cmd, map[string]string{"key": "value"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"key"`)
	assert.Contains(t, buf.String(), `"value"`)
}

// --- runCreate with invalid config ---

func TestMiss2_RunCreate_InvalidConfig(t *testing.T) {
	cmd := newCreateCommand()
	configPath := testutil.UnreachableConfigPath(t)
	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runRevoke with invalid config ---

func TestMiss2_RunRevoke_InvalidConfig(t *testing.T) {
	revokeCmd := newRevokeCommand()
	configPath := testutil.UnreachableConfigPath(t)
	err := runRevoke(revokeCmd, configPath, "some-id")
	assert.Error(t, err)
}

// --- runList with invalid config ---

func TestMiss2_RunList_InvalidConfig(t *testing.T) {
	listCmd := newListCommand()
	configPath := testutil.UnreachableConfigPath(t)
	err := runList(listCmd, configPath)
	assert.Error(t, err)
}

// --- RunCreate with name flag ---

func TestMiss2_RunCreate_WithNameFlag(t *testing.T) {
	configPath := setupMiss2DB(t)
	cmd := newCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "flag-name-test"))

	result, err := RunCreate(cmd, nil, configPath)
	require.NoError(t, err)
	assert.Equal(t, "flag-name-test", result.Name)
}

// --- RunList with token that has last_used_at set ---

func TestMiss2_RunList_WithLastUsedAt(t *testing.T) {
	configPath := setupMiss2DB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "lastused-miss2"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Set last_used_at directly in DB
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewApiTokenRepository(db)
	tokens, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	require.NoError(t, db.DB.Exec("UPDATE api_tokens SET last_used_at = datetime('now') WHERE id = ?", tokens[0].ID).Error)

	// List text output should show a date, not "never"
	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "lastused-miss2")
	assert.NotContains(t, output, "never")
}

// --- RunList with long name (truncation) ---

func TestMiss2_RunList_LongNameTruncation(t *testing.T) {
	configPath := setupMiss2DB(t)

	longName := "this-is-an-extremely-long-token-name-that-exceeds-thirty-characters-by-a-lot"
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", longName))
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

	assert.Contains(t, buf.String(), "...")
}
