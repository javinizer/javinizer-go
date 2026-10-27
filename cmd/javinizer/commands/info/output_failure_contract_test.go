package info_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type updateLineFailWriter struct {
	bytes.Buffer
	prefix   string
	err      error
	failures int
}

func (w *updateLineFailWriter) Write(p []byte) (int, error) {
	if w.prefix != "" && strings.HasPrefix(string(p), w.prefix) {
		w.failures++
		return 0, w.err
	}
	return w.Buffer.Write(p)
}

func TestInfoDependencyDirectoryFailurePreservesCause(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "not-a-directory")
	require.NoError(t, os.WriteFile(parent, []byte("keep this file"), 0o600))
	t.Setenv("JAVINIZER_DB", "")
	configPath, _ := createTestConfig(t, WithDatabaseDSN(filepath.Join(parent, "movies.db")))
	var out bytes.Buffer
	cmd := info.NewCommand()
	cmd.Flags().String("config", configPath, "config file")
	cmd.SetOut(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(nil)

	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "failed to initialize dependencies: failed to create database directory:")
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, parent, pathErr.Path)
	assert.Contains(t, out.String(), "Scrapers:")
	assert.NotContains(t, out.String(), "Output:")
	assert.NotContains(t, out.String(), "Update:")
	contents, readErr := os.ReadFile(parent)
	require.NoError(t, readErr)
	assert.Equal(t, "keep this file", string(contents))
}

func TestInfoCachedUpdateOutputPropagatesWriterFailure(t *testing.T) {
	// Stable build metadata makes the prerelease-warning contract independent
	// of the version injected by the test runner or a release build.
	originalVersion := appversion.Version
	appversion.Version = "v1.0.0"
	t.Cleanup(func() { appversion.Version = originalVersion })
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	lines := []string{
		"  - Last checked: ",
		"  - Latest version: ",
		"  - Update available: ",
		"  - Warning: Latest version is a prerelease",
	}
	for _, failurePrefix := range append([]string{""}, lines...) {
		name := strings.TrimSpace(failurePrefix)
		if name == "" {
			name = "successful cached status"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("JAVINIZER_DATA_DIR", dir)
			configPath, cfg := createTestConfig(t, WithVersionCheckEnabled(true))
			checkedAt := time.Now().UTC().Format(time.RFC3339)
			cachePath := filepath.Join(dir, "update_cache.json")
			// A fresh cache avoids GetStatus spawning a background HTTP refresh.
			writeTestState(t, cachePath, "v2.0.0-rc1", checkedAt, true, true, update.UpdateSourceCached)
			before, err := os.ReadFile(cachePath)
			require.NoError(t, err)
			writeErr := errors.New("update status output unavailable")
			writer := &updateLineFailWriter{prefix: failurePrefix, err: writeErr}
			var stderr bytes.Buffer
			root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().String("config", configPath, "config file")
			root.SetOut(writer)
			root.SetErr(&stderr)
			root.AddCommand(info.NewCommand())
			root.SetArgs([]string{"info"})

			err = root.ExecuteContext(t.Context())
			if failurePrefix == "" {
				require.NoError(t, err)
				assert.Zero(t, writer.failures)
				assert.Contains(t, writer.String(), "Last checked: "+checkedAt)
				assert.Contains(t, writer.String(), "Latest version: v2.0.0-rc1")
				assert.Contains(t, writer.String(), "Update available: true")
				assert.Contains(t, writer.String(), lines[3])
			} else {
				require.ErrorIs(t, err, writeErr, "preserve the writer error through the public command")
				assert.Equal(t, 1, writer.failures)
				failed := false
				for _, line := range lines {
					failed = failed || line == failurePrefix
					if failed {
						assert.NotContains(t, writer.String(), line, "stop writing after the first failure")
					} else {
						assert.Contains(t, writer.String(), line)
					}
				}
			}
			assert.Empty(t, stderr.String())
			assert.Contains(t, writer.String(), "Output:")
			assert.Contains(t, writer.String(), "Current version: v1.0.0")
			after, err := os.ReadFile(cachePath)
			require.NoError(t, err)
			assert.Equal(t, before, after, "displaying status must not alter the fresh cache")
			_, err = os.Stat(cfg.Database.DSN + "-wal")
			assert.True(t, os.IsNotExist(err), "info must close its owned database even after output failure")
		})
	}
}
