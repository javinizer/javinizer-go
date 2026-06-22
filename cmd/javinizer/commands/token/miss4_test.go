package token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMiss4DB creates a test config + migrated DB.
func setupMiss4DB(t *testing.T) string {
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

// --- RunCreate: Prepare config error (line 84-85) ---

func TestMiss4_RunCreate_PrepareError(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Create a config that will fail Prepare — empty scraper priority
	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	cfg.Scrapers.Priority = []string{} // This should cause Prepare to fail or succeed; we need invalid config
	// Actually, let's create a config with invalid DSN to trigger dependency init error
	cfg.Database.DSN = "" // Empty DSN
	require.NoError(t, config.Save(cfg, configPath))

	cmd := newCreateCommand()
	result, err := RunCreate(cmd, nil, configPath)
	// This should fail at some point in the pipeline
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- RunRevoke: Prepare config error (line 168-169) ---

func TestMiss4_RunRevoke_PrepareError(t *testing.T) {
	configPath := setupMiss4DB(t)

	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	cfg.Database.DSN = ""
	require.NoError(t, config.Save(cfg, configPath))

	result, err := RunRevoke(&cobra.Command{}, []string{"some-id"}, configPath)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- RunList: Prepare config error (line 254-255) ---

func TestMiss4_RunList_PrepareError(t *testing.T) {
	configPath := setupMiss4DB(t)

	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	cfg.Database.DSN = ""
	require.NoError(t, config.Save(cfg, configPath))

	entries, err := RunList(&cobra.Command{}, nil, configPath)
	assert.Error(t, err)
	assert.Nil(t, entries)
}

// --- runCreate: fmt.Fprintf error paths in text output (lines 131-154) ---
// These are the `if _, err := fmt.Fprintf(...)` error branches.
// Each one returns immediately, so we need a writer that succeeds N times then fails.

type miss4ErrorWriter struct{}

func (miss4ErrorWriter) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write error")
}

// failAfterNWriter succeeds for the first n writes then fails.
type failAfterNWriter struct {
	buf    bytes.Buffer
	remain int
}

func (w *failAfterNWriter) Write(p []byte) (n int, err error) {
	if w.remain <= 0 {
		return 0, fmt.Errorf("write error after %d writes", w.remain)
	}
	w.remain--
	return w.buf.Write(p)
}

func TestMiss4_RunCreate_TextOutputWriteError(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Build a cobra command that uses an error writer
	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-err-write", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(miss4ErrorWriter{})

	err := runCreate(cmd, configPath)
	// Should hit the first fmt.Fprintf error branch
	assert.Error(t, err)
}

// --- runCreate: fmt.Fprintf error on 2nd write (line 134) ---

func TestMiss4_RunCreate_TextOutputWriteError2(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write2", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 1})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runCreate: fmt.Fprintf error on 3rd write (line 137) ---

func TestMiss4_RunCreate_TextOutputWriteError3(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write3", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 2})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runCreate: fmt.Fprintf error on 4th write (line 140) ---

func TestMiss4_RunCreate_TextOutputWriteError4(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write4", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 3})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runCreate: fmt.Fprintf error on later writes ---

func TestMiss4_RunCreate_TextOutputWriteError5(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write5", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 4})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

func TestMiss4_RunCreate_TextOutputWriteError6(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write6", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 5})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

func TestMiss4_RunCreate_TextOutputWriteError7(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write7", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 6})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

func TestMiss4_RunCreate_TextOutputWriteError8(t *testing.T) {
	configPath := setupMiss4DB(t)

	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().String("config", configPath, "")
	cmd.Flags().String("name", "miss4-write8", "")
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 7})

	err := runCreate(cmd, configPath)
	assert.Error(t, err)
}

// --- runRevoke: fmt.Fprintf error paths in text output (lines 231-242) ---

func TestMiss4_RunRevoke_TextOutputWriteError(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Create a token first
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-revoke-err"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Revoke with error writer (fails immediately)
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(miss4ErrorWriter{})

	err = runRevoke(cmd, configPath, createResult.ID)
	assert.Error(t, err)
}

func TestMiss4_RunRevoke_TextOutputWriteError2(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-revoke-err2"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 1})

	err = runRevoke(cmd, configPath, createResult.ID)
	assert.Error(t, err)
}

func TestMiss4_RunRevoke_TextOutputWriteError3(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-revoke-err3"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 2})

	err = runRevoke(cmd, configPath, createResult.ID)
	assert.Error(t, err)
}

func TestMiss4_RunRevoke_TextOutputWriteError4(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-revoke-err4"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 3})

	err = runRevoke(cmd, configPath, createResult.ID)
	assert.Error(t, err)
}

// --- runList: tabwriter fmt.Fprintf error (line 327-328) ---

func TestMiss4_RunList_TabwriterWriteError(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Create a token so the list is not empty
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-list-err"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(miss4ErrorWriter{})

	err = runList(cmd, configPath)
	// Should hit the tabwriter Fprintf error
	assert.Error(t, err)
}

func TestMiss4_RunList_TabwriterWriteError2(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-list-err2"))
	_, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&failAfterNWriter{remain: 1})

	err = runList(cmd, configPath)
	assert.Error(t, err)
}

// --- runList: name truncation (> 30 chars, line 303) ---

func TestMiss4_RunList_LongNameTruncation(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Create a token with a very long name
	longName := strings.Repeat("a", 35)
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

	// The truncated name should appear as "aaa..." (27 chars + "...")
	output := buf.String()
	assert.Contains(t, output, "aaa...")
}

// --- runList: token with LastUsedAt set (line 309-311) ---

func TestMiss4_RunList_TokenWithLastUsedAt(t *testing.T) {
	configPath := setupMiss4DB(t)

	// Create token then use it (sets LastUsedAt)
	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-lastused"))
	result, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	// Load the DB and mark the token as used
	cfg, err := config.LoadOrCreate(configPath)
	require.NoError(t, err)
	config.ApplyEnvironmentOverrides(cfg)
	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewApiTokenRepository(db)
	require.NoError(t, repo.UpdateLastUsed(context.Background(), result.ID))

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "list"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	// Should NOT show "never" since LastUsedAt is set
	output := buf.String()
	assert.NotContains(t, output, "never")
	assert.Contains(t, output, "miss4-lastused")
}

// --- runRevoke: JSON output (line 225-226, printJSON for revoke) ---

func TestMiss4_RunRevoke_JSONOutput(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-revoke-json"))
	createResult, err := RunCreate(createCmd, nil, configPath)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"token", "revoke", createResult.ID, "--json"})

	err = rootCmd.Execute()
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result))
	assert.True(t, result["revoked"].(bool))
}

// --- runList: JSON output with tokens (line 297-300) ---

func TestMiss4_RunList_JSONOutputWithTokens(t *testing.T) {
	configPath := setupMiss4DB(t)

	createCmd := newCreateCommand()
	require.NoError(t, createCmd.Flags().Set("name", "miss4-list-json"))
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

	var result []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result))
	require.NotEmpty(t, result)
	assert.Equal(t, "miss4-list-json", result[0]["name"])
}

// Suppress unused imports
var _ = context.Background
var _ = os.ReadFile
