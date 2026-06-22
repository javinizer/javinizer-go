package token_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/token"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureOutput(t *testing.T, fn func()) string {
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

func setupTokenTestDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "test.db")

	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = dbPath
	configPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: testCfg.Database.Type, DSN: testCfg.Database.DSN, LogLevel: testCfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	return configPath
}

func buildRootCmd(configPath string) *cobra.Command {
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(token.NewCommand())
	return rootCmd
}

func TestRunCreate_Success(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create"})

	stdout := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Token created successfully!")
	assert.Contains(t, stdout, "jv_")
	assert.Contains(t, stdout, "This token value will NOT be shown again")
}

func TestRunCreate_WithName(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "my-token"})

	stdout := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "my-token")
	assert.Contains(t, stdout, "Token created successfully!")
}

func TestRunCreate_JSONOutput(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "json-token", "--json"})

	stdout := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "json-token", result["name"])
	assert.Contains(t, result["token"], "jv_")
	assert.NotEmpty(t, result["id"])
	assert.NotEmpty(t, result["token_prefix"])
}

func TestRunCreate_Unnamed(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create"})

	stdout := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "(unnamed)")
}

func TestRunRevoke_Success(t *testing.T) {
	configPath := setupTokenTestDB(t)

	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "to-revoke"})
	_ = captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "list", "--json"})
	listOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(listOutput), &entries))
	require.NotEmpty(t, entries)
	tokenID := entries[0]["id"].(string)

	rootCmd3 := buildRootCmd(configPath)
	rootCmd3.SetArgs([]string{"token", "revoke", tokenID})
	revokeOutput := captureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, revokeOutput, "Token revoked successfully!")
}

func TestRunRevoke_ByPrefix(t *testing.T) {
	configPath := setupTokenTestDB(t)

	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "prefix-test", "--json"})
	createOutput := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	var createResult map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(createOutput), &createResult))
	prefix := createResult["token_prefix"].(string)

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "revoke", "jv_" + prefix})
	revokeOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, revokeOutput, "Token revoked successfully!")
}

func TestRunRevoke_PrefixTooShort(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "revoke", "jv_abc"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prefix too short")
}

func TestRunRevoke_NotFound(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "revoke", "nonexistent-id"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunRevoke_JSONOutput(t *testing.T) {
	configPath := setupTokenTestDB(t)

	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "revoke-json", "--json"})
	createOutput := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	var createResult map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(createOutput), &createResult))
	tokenID := createResult["id"].(string)

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "revoke", tokenID, "--json"})
	revokeOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	var revokeResult map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(revokeOutput), &revokeResult))
	assert.Equal(t, true, revokeResult["revoked"])
	assert.NotEmpty(t, revokeResult["id"])
}

func TestRunList_Success(t *testing.T) {
	configPath := setupTokenTestDB(t)

	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "list-test"})
	_ = captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "list"})
	listOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, listOutput, "ID")
	assert.Contains(t, listOutput, "PREFIX")
	assert.Contains(t, listOutput, "NAME")
	assert.Contains(t, listOutput, "list-test")
	assert.Contains(t, listOutput, "jv_")
}

func TestRunList_Empty(t *testing.T) {
	configPath := setupTokenTestDB(t)
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "list"})

	stdout := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No active tokens found.")
}

func TestRunList_JSONOutput(t *testing.T) {
	configPath := setupTokenTestDB(t)

	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", "json-list"})
	_ = captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "list", "--json"})
	listOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(listOutput), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "json-list", entries[0]["name"])
	assert.Contains(t, entries[0], "token_prefix")
	assert.Contains(t, entries[0], "created_at")
}

func TestRunList_LongNameTruncated(t *testing.T) {
	configPath := setupTokenTestDB(t)

	longName := "this-is-a-very-long-token-name-that-exceeds-thirty-characters"
	rootCmd := buildRootCmd(configPath)
	rootCmd.SetArgs([]string{"token", "create", "--name", longName})
	_ = captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := buildRootCmd(configPath)
	rootCmd2.SetArgs([]string{"token", "list"})
	listOutput := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, listOutput, "this-is-a-very-long-token-n...")
}
