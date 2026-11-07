package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloseBeforeFirstSuccessfulInitializationAllowsRetry(t *testing.T) {
	// Exercise the public singleton API from its real initial state, isolated
	// from prior tests. Keep the compatibility alias in sync and restore both.
	// This test and the other global logger tests must not run in parallel.
	dir := t.TempDir()
	previous, previousCurrent := globalInit, current
	globalInit = newLoggerInit()
	current = &globalInit.current
	t.Cleanup(func() {
		CloseLogger()
		globalInit, current = previous, previousCurrent
	})
	failedPath := filepath.Join(dir, "failed.log")
	retained := L() // Taking an adapter does not initialize logging.
	err := InitLogger(&Config{Level: "invalid-level", Format: "json", Output: failedPath})
	require.ErrorContains(t, err, "invalid log level")
	require.Nil(t, globalInit.current.Load(), "failed first initialization must not publish a logger")

	CloseLogger()
	CloseLogger() // Cleanup is idempotent even without a successful initialization.
	// Renaming and removing the failed output also checks that initialization
	// released its file handle on platforms such as Windows.
	archived := filepath.Join(dir, "failed-closed.log")
	require.NoError(t, os.Rename(failedPath, archived))
	failedBytes, err := os.ReadFile(archived)
	require.NoError(t, err)
	require.Empty(t, failedBytes)
	require.NoError(t, os.Remove(archived))

	goodPath := filepath.Join(dir, "ready.log")
	require.NoError(t, InitLogger(&Config{Level: "info", Format: "json", Output: goodPath}))
	retained.Info("initialized-after-early-cleanup")
	CloseLogger()
	data, err := os.ReadFile(goodPath)
	require.NoError(t, err)
	require.Contains(t, string(data), `"msg":"initialized-after-early-cleanup"`)
	require.NoFileExists(t, failedPath, "retry must not reopen the failed output")
	require.NoFileExists(t, archived)
	require.NoError(t, os.Remove(goodPath), "successful logger cleanup must release its output")
}
