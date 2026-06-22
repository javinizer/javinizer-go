package token

import (
	"bytes"
	"context"
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

// setupTokenBoostDB creates a test config + migrated DB for coverage tests.
func setupTokenBoostDB(t *testing.T) string {
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

// TestRunCreate_NamedToken tests creating a token with an explicit name via RunCreate.
func TestRunCreate_NamedToken(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	cmd := newCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "boost-token"))

	result, err := RunCreate(cmd, nil, configPath)
	require.NoError(t, err)
	assert.Equal(t, "boost-token", result.Name)
	assert.NotEmpty(t, result.Token)
	assert.True(t, len(result.ID) >= 8)
}

// TestRunCreate_UnnamedToken tests creating a token without a name.
func TestRunCreate_UnnamedToken(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	cmd := newCreateCommand()

	result, err := RunCreate(cmd, nil, configPath)
	require.NoError(t, err)
	assert.Empty(t, result.Name)
	assert.NotEmpty(t, result.Token)
}

// TestRunCreate_InvalidConfig tests RunCreate with invalid config path.
func TestRunCreate_InvalidConfig(t *testing.T) {
	cmd := newCreateCommand()
	configPath := testutil.UnreachableConfigPath(t)
	result, err := RunCreate(cmd, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestRunCreate_TextOutput tests the text output path of runCreate (non-JSON).
func TestRunCreate_TextOutput(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	cmd := newCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "text-output"))

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"create", "--name", "text-output"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Token created successfully!")
	assert.Contains(t, buf.String(), "text-output")
	assert.Contains(t, buf.String(), "jv_")
}

// TestRunRevoke_ByIDDirect tests RunRevoke with a full UUID.
func TestRunRevoke_ByIDDirect(t *testing.T) {
	configPath := setupTokenBoostDB(t)

	// Create token first
	createCmd := newCreateCommand()
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke by full ID
	result, err := RunRevoke(&cobra.Command{}, []string{createResult.ID}, configPath)
	require.NoError(t, err)
	assert.True(t, result.Revoked)
	assert.Equal(t, createResult.ID, result.ID)
}

// TestRunRevoke_PrefixTooShort tests error when prefix is too short.
func TestRunRevoke_PrefixTooShort(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_abc"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "prefix too short")
}

// TestRunRevoke_PrefixNotFound tests error when prefix doesn't match.
func TestRunRevoke_PrefixNotFound(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	result, err := RunRevoke(&cobra.Command{}, []string{"jv_abcdef12"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no active token found")
}

// TestRunList_AfterCreate tests that list returns tokens after creation.
func TestRunList_AfterCreate(t *testing.T) {
	configPath := setupTokenBoostDB(t)

	// Create a token
	createCmd := newCreateCommand()
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// List should return 1
	result, err := RunList(&cobra.Command{}, nil, configPath)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

// TestRunList_Empty tests that list returns empty when no tokens exist.
func TestRunList_Empty(t *testing.T) {
	configPath := setupTokenBoostDB(t)
	result, err := RunList(&cobra.Command{}, nil, configPath)
	require.NoError(t, err)
	assert.Empty(t, result)
}
