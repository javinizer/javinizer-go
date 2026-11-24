package token

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTokenBootstrapRejectsInvalidRuntimeConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "saved.db")
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	require.NoError(t, config.Save(cfg, path))
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	runtimeDB := filepath.Join(dir, "runtime.db")
	t.Setenv("JAVINIZER_DB", runtimeDB)
	t.Setenv("METADATA_TRANSLATION_PROVIDER", "unsupported-fixture-provider")

	for _, args := range [][]string{{"create"}, {"revoke", "fixture-id"}, {"list"}} {
		t.Run(args[0], func(t *testing.T) {
			cmd := NewCommand()
			cmd.PersistentFlags().String("config", path, "configuration file")
			cmd.SetArgs(args)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			var output bytes.Buffer
			cmd.SetOut(&output)
			err := cmd.Execute()
			require.ErrorContains(t, err, "failed to load config:")
			require.ErrorContains(t, err, "metadata.translation.provider")
			require.Zero(t, output.Len())
			require.NoFileExists(t, cfg.Database.DSN)
			require.NoFileExists(t, runtimeDB)
		})
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestTokenBootstrapUsesRuntimeDatabaseWithoutChangingSavedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "saved.db")
	require.NoError(t, config.Save(cfg, path))
	runtimeDB := filepath.Join(dir, "runtime.db")
	t.Setenv("JAVINIZER_DB", runtimeDB)
	t.Setenv("JAVINIZER_TEMP_DIR", filepath.Join(dir, "temp"))

	create := newCreateCommand()
	require.NoError(t, create.Flags().Set("name", "local token bootstrap fixture"))
	created, err := RunCreate(create, nil, path)
	require.NoError(t, err)
	entries, err := RunList(&cobra.Command{}, nil, path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, created.ID, entries[0].ID)
	revoked, err := RunRevoke(newRevokeCommand(), []string{created.ID}, path)
	require.NoError(t, err)
	require.True(t, revoked.Revoked)
	entries, err = RunList(&cobra.Command{}, nil, path)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.FileExists(t, runtimeDB)
	require.NoFileExists(t, cfg.Database.DSN)
	saved, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, cfg.Database.DSN, saved.Database.DSN)
}
