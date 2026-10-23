package config

import (
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/javinizer/javinizer-go/internal/config"
	_ "github.com/javinizer/javinizer-go/internal/config/migrations" // Match the root command migration registration.
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestLegacyMigrationConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, input, message string
		migrate              bool
	}{
		{"EOF", "", "Migration cancelled (invalid input).", false},
		{"declined", "no\n", "Migration cancelled.", false},
		{"confirmed", "Y\n", "Migration complete. Please review your settings.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			cfg := appconfig.DefaultConfig(nil, nil)
			cfg.ConfigVersion = 2
			require.NoError(t, appconfig.Save(cfg, path))
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			t.Setenv("JAVINIZER_CONFIG", path)
			oldContext := appconfig.GetMigrationContext()
			t.Cleanup(func() { appconfig.SetMigrationContext(oldContext) })
			inputPath := filepath.Join(dir, "answer")
			require.NoError(t, os.WriteFile(inputPath, []byte(tc.input), 0600))
			input, err := os.Open(inputPath)
			require.NoError(t, err)
			oldStdin := os.Stdin
			os.Stdin = input
			t.Cleanup(func() { os.Stdin = oldStdin; require.NoError(t, input.Close()) })
			var commandErr error
			output, _ := testutil.CaptureOutput(t, func() {
				cmd := newMigrateCommand()
				commandErr = cmd.Execute()
			})
			require.NoError(t, commandErr)
			require.Contains(t, output, tc.message)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			backups, err := filepath.Glob(path + ".bak-*")
			require.NoError(t, err)
			if !tc.migrate {
				require.Equal(t, before, after)
				require.Empty(t, backups)
				return
			}
			require.Len(t, backups, 1)
			backup, err := os.ReadFile(backups[0])
			require.NoError(t, err)
			require.Equal(t, before, backup)
			migrated, err := appconfig.Load(path)
			require.NoError(t, err)
			require.Equal(t, appconfig.CurrentConfigVersion, migrated.ConfigVersion)
			require.NotEqual(t, before, after)
			require.Contains(t, output, "Backup created: "+backups[0])
		})
	}
}

func TestMigrationRejectsUnsupportedLegacyVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := appconfig.DefaultConfig(nil, nil)
	cfg.ConfigVersion = -1
	require.NoError(t, appconfig.Save(cfg, path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_CONFIG", path)
	oldContext := appconfig.GetMigrationContext()
	t.Cleanup(func() { appconfig.SetMigrationContext(oldContext) })
	inputPath := filepath.Join(dir, "answer")
	require.NoError(t, os.WriteFile(inputPath, []byte("y\n"), 0600))
	input, err := os.Open(inputPath)
	require.NoError(t, err)
	oldStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = oldStdin; require.NoError(t, input.Close()) })
	cmd := newMigrateCommand()
	require.EqualError(t, cmd.Execute(), "migration failed: no migration from version -1. This version is not supported")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	backups, err := filepath.Glob(path + ".bak-*")
	require.NoError(t, err)
	require.Empty(t, backups)
}
