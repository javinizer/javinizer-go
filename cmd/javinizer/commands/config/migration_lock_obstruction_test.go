package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	configcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/config"
	appconfig "github.com/javinizer/javinizer-go/internal/config"
	_ "github.com/javinizer/javinizer-go/internal/config/migrations"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMigrationSaveRefusesOwnedLockObstruction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	cfg := appconfig.DefaultConfig(nil, nil)
	cfg.ConfigVersion = 2
	require.NoError(t, appconfig.Save(cfg, path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	loaded, err := appconfig.Load(path)
	require.NoError(t, err)
	require.Equal(t, 2, loaded.ConfigVersion)

	// This real, non-empty directory cannot be interpreted as a stale lock
	// token. No injected lock seam, permissions change or timing race is used.
	lockPath := path + ".lock"
	require.NoError(t, os.Mkdir(lockPath, 0700))
	markerPath := filepath.Join(lockPath, "owned-marker")
	require.NoError(t, os.WriteFile(markerPath, []byte("preserve me"), 0600))
	lockBefore, err := os.Stat(lockPath)
	require.NoError(t, err)

	t.Setenv("JAVINIZER_CONFIG", path)
	oldContext := appconfig.GetMigrationContext()
	t.Cleanup(func() { appconfig.SetMigrationContext(oldContext) })
	answerPath := filepath.Join(root, "answer")
	require.NoError(t, os.WriteFile(answerPath, []byte("y\n"), 0600))
	answer, err := os.Open(answerPath)
	require.NoError(t, err)
	oldStdin := os.Stdin
	os.Stdin = answer
	t.Cleanup(func() { os.Stdin = oldStdin; require.NoError(t, answer.Close()) })

	var commandErr error
	output, _ := testutil.CaptureOutput(t, func() {
		cmd := configcmd.NewCommand()
		cmd.SetArgs([]string{"migrate"})
		commandErr = cmd.Execute()
	})
	require.ErrorContains(t, commandErr, "failed to save migrated config:")
	require.ErrorContains(t, commandErr, "config lock")
	require.ErrorContains(t, commandErr, lockPath)
	if runtime.GOOS == "linux" {
		require.EqualError(t, commandErr, "failed to save migrated config: timed out waiting for config lock: "+lockPath)
	}
	require.NotContains(t, output, "Config migrated to version")
	require.NotContains(t, output, "Migration complete.")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed publication must preserve original config bytes")
	loaded, err = appconfig.Load(path)
	require.NoError(t, err)
	require.Equal(t, 2, loaded.ConfigVersion)
	backups, err := filepath.Glob(path + ".bak-*")
	require.NoError(t, err)
	require.Len(t, backups, 1)
	backup, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	require.Equal(t, before, backup)
	require.Contains(t, output, "Backup created: "+backups[0])
	lockAfter, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.True(t, lockAfter.IsDir())
	require.True(t, os.SameFile(lockBefore, lockAfter), "an obstructing directory must not be reaped/replaced")
	marker, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	require.Equal(t, "preserve me", string(marker))
	entries, err := os.ReadDir(lockPath)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	staged, err := filepath.Glob(filepath.Join(root, ".config.yaml.tmp-*"))
	require.NoError(t, err)
	require.Empty(t, staged, "lock refusal must happen before staging a replacement")
}
