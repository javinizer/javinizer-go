package tag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// A regular file in the database directory path exercises a real bootstrap
// failure without permissions assumptions (the test also works as root).
func TestStorageBootstrapFailurePreservesFiles(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	original := []byte("preserve this file")
	require.NoError(t, os.WriteFile(blocker, original, 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(blocker, "database.sqlite")
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	cfg.Logging.Output = "stdout"
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	invalidPath := filepath.Join(dir, "invalid.yaml")
	require.NoError(t, os.WriteFile(invalidPath, []byte("[malformed"), 0600))
	// Normalize the config as the loader does before dependency creation.
	_, err := config.LoadOrCreate(path)
	require.NoError(t, err)
	configBefore, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string, string) error
		args []string
	}{
		{"add", runAdd, []string{"TEST-001", "kept"}},
		{"list", runList, nil},
		{"remove", runRemove, []string{"TEST-001"}},
		{"search", runSearch, []string{"kept"}},
		{"tags", runAllTags, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(&cobra.Command{}, tc.args, invalidPath)
			require.ErrorContains(t, err, "failed to load config:")
			invalid, readErr := os.ReadFile(invalidPath)
			require.NoError(t, readErr)
			require.Equal(t, "[malformed", string(invalid))
			err = tc.run(&cobra.Command{}, tc.args, path)
			require.ErrorContains(t, err, "failed to initialize dependencies: failed to create database directory:")
			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, blocker, pathErr.Path)
			require.Equal(t, "mkdir", pathErr.Op)
			got, err := os.ReadFile(blocker)
			require.NoError(t, err)
			require.Equal(t, original, got)
			got, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, configBefore, got)
		})
	}
}
