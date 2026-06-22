package token

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// --- RunCreate error paths (lines 84-100) ---

func TestRunCreate_InvalidConfigPath(t *testing.T) {
	cmd := newCreateCommand()
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunCreate(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- runCreate text output branches (lines 113, 128-154) ---

func TestRunCreate_TextOutputUnnamed(t *testing.T) {
	configPath := setupMissTestDB(t)

	cmd := newCreateCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"create"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "(unnamed)")
	assert.Contains(t, output, "Token created successfully!")
	assert.Contains(t, output, "Token:")
	assert.Contains(t, output, "This token value will NOT be shown again")
}

// --- RunRevoke error paths (lines 163-175) ---

func TestRunRevoke_InvalidConfigPath(t *testing.T) {
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"some-id"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- runRevoke text output (lines 228-242) ---

func TestRunRevoke_TextOutputByID(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token first
	createCmd := newCreateCommand()
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke by full ID with text output
	revokeCmd := newRevokeCommand()
	buf := new(bytes.Buffer)
	revokeCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(revokeCmd)
	rootCmd.SetArgs([]string{"revoke", createResult.ID})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token revoked successfully!")
	assert.Contains(t, output, "jv_")
}

func TestRunRevoke_TextOutputByPrefix(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "prefix-revoke-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke by prefix with text output
	revokeCmd := newRevokeCommand()
	buf := new(bytes.Buffer)
	revokeCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(revokeCmd)
	rootCmd.SetArgs([]string{"revoke", "jv_" + createResult.TokenPrefix})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Token revoked successfully!")
}

// --- RunList error paths (lines 249-270) ---

func TestRunList_InvalidConfigPath(t *testing.T) {
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunList(&cobra.Command{}, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- runList empty list + tabwriter path (lines 288-329) ---

func TestRunList_EmptyTextOutput(t *testing.T) {
	configPath := setupMissTestDB(t)

	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No active tokens found.")
}

func TestRunList_WithTokenTabwriterOutput(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a named token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "tab-test"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List with text output (tabwriter path)
	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ID")
	assert.Contains(t, output, "PREFIX")
	assert.Contains(t, output, "tab-test")
	assert.Contains(t, output, "jv_")
	assert.Contains(t, output, "CREATED")
	assert.Contains(t, output, "LAST USED")
}

func TestRunList_WithTokenLastUsedAt(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "lastused-test"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Update the token's last_used_at to test the "never" vs time branch
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewApiTokenRepository(db)
	tokens, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	// Update last_used_at via direct DB call
	require.NoError(t, db.DB.Exec("UPDATE api_tokens SET last_used_at = datetime('now') WHERE id = ?", tokens[0].ID).Error)

	// List with text output — should show a date instead of "never"
	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "lastused-test")
	// Should have a formatted date, not "never"
	assert.NotContains(t, output, "never")
}

func TestRunList_UnnamedTokenInTabwriter(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token without a name
	createCmd := newCreateCommand()
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List should show "(unnamed)"
	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "(unnamed)")
}

func TestRunList_LongNameTruncation(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token with a very long name
	createCmd := newCreateCommand()
	longName := "this-is-an-extremely-long-token-name-that-exceeds-thirty-characters"
	require.NoError(t, createCmd.Flags().Set("name", longName))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List should truncate the name
	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "...")
}

// --- printJSON error path (line 337) ---

func TestPrintJSON_MarshalError(t *testing.T) {
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	// Create a value that cannot be marshaled to JSON
	err := printJSON(cmd, unmarshallable{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal JSON")
}

// unmarshallable is a type that cannot be marshaled to JSON
type unmarshallable struct {
	Ch chan int
}

// --- RunRevoke by ID with FindByID fallback (line 237-242) ---

func TestRunRevoke_ByIDWithPrefixFallback(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "fallback-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke by full UUID (not jv_ prefix) — this hits the FindByID fallback path
	result, err := RunRevoke(&cobra.Command{}, []string{createResult.ID}, configPath)
	require.NoError(t, err)
	assert.True(t, result.Revoked)
	assert.NotEmpty(t, result.Prefix)
}

// --- runCreate error return path (line 113) ---

func TestRunCreate_RunCreateErrorPropagated(t *testing.T) {
	cmd := newCreateCommand()
	// Invalid config will cause RunCreate to return error
	configPath := testutil.UnreachableConfigPath(t)
	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runRevoke error return path (line 228) ---

func TestRunRevoke_RunRevokeErrorPropagated(t *testing.T) {
	revokeCmd := newRevokeCommand()
	// Invalid config will cause RunRevoke to return error
	configPath := testutil.UnreachableConfigPath(t)
	err := runRevoke(revokeCmd, configPath, "some-id")
	assert.Error(t, err)
}

// --- runList error return path (line 288) ---

func TestRunList_RunListErrorPropagated(t *testing.T) {
	listCmd := newListCommand()
	configPath := testutil.UnreachableConfigPath(t)
	err := runList(listCmd, configPath)
	assert.Error(t, err)
}

// --- RunRevoke with prefix resolution (line 168-175) ---

func TestRunRevoke_ByPrefixResolveAndRevoke(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "resolve-test"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke by prefix — should resolve ID and revoke
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_" + createResult.TokenPrefix}, configPath)
	require.NoError(t, err)
	assert.True(t, result.Revoked)
	assert.Equal(t, createResult.ID, result.ID)
}

// --- JSON output via cobra command execution (lines 128-130 runCreate JSON) ---

func TestRunCreate_JSONOutputViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "create", "--name", "json-miss-test", "--json"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
	assert.Equal(t, "json-miss-test", result["name"])
}

// --- RunRevoke JSON output via cobra (lines 231-236 runRevoke JSON) ---

func TestRunRevoke_JSONOutputViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "revoke-json-miss"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke with --json flag via full token command tree
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

// --- RunList JSON output via cobra (lines 268-270) ---

func TestRunList_JSONOutputViaCobra(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "list-json-miss"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List with --json flag via full token command tree
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
	assert.Equal(t, "list-json-miss", entries[0]["name"])
}

// --- RunRevoke with nonexistent UUID (line 173-175) ---

func TestRunRevoke_RevokNotFoundByUUID(t *testing.T) {
	configPath := setupMissTestDB(t)

	result, err := RunRevoke(&cobra.Command{}, []string{"nonexistent-uuid-12345"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- Verify RunRevoke with prefix but <8 chars after jv_ ---

func TestRunRevoke_PrefixExactly7CharsAfterJV(t *testing.T) {
	configPath := setupMissTestDB(t)

	result, err := RunRevoke(&cobra.Command{}, []string{"jv_abcdefg"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "prefix too short")
}

// --- Verify RunList with entries having LastUsedAt nil (shows "never") ---

func TestRunList_NeverUsedOutput(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token (won't have last_used_at set)
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "never-used"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List with text output
	listCmd := newListCommand()
	buf := new(bytes.Buffer)
	listCmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(listCmd)
	rootCmd.SetArgs([]string{"list"})

	err = rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "never")
}

// --- errorWriter is a writer that always returns an error ---

type errorWriter struct{}

func (errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

// --- runCreate with error writer (lines 128-154) ---

func TestRunCreate_TextOutputWriteError(t *testing.T) {
	configPath := setupMissTestDB(t)

	cmd := newCreateCommand()
	cmd.SetOut(errorWriter{})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runRevoke with error writer (lines 228-242) ---

func TestRunRevoke_TextOutputWriteError(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token first
	createCmd := newCreateCommand()
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	revokeCmd := newRevokeCommand()
	revokeCmd.SetOut(errorWriter{})

	err = runRevoke(revokeCmd, configPath, createResult.ID)
	assert.Error(t, err)
}

// --- runList with error writer (lines 303, 327) ---

func TestRunList_TabwriterWriteError(t *testing.T) {
	configPath := setupMissTestDB(t)

	// Create a token
	createCmd := newCreateCommand()
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	listCmd := newListCommand()
	listCmd.SetOut(errorWriter{})

	err = runList(listCmd, configPath)
	assert.Error(t, err)
}

// --- Compile-time check that errors package is used ---

var _ = errors.New("placeholder")
