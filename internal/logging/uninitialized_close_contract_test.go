package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalCloseBeforeInitializationPreservesLaterLogging(t *testing.T) {
	// Isolate the singleton and compatibility alias just as other shutdown
	// tests do. No parallel tests may observe this temporary initializer.
	previous, previousCurrent := globalInit, current
	globalInit = newLoggerInit()
	current = &globalInit.current
	t.Cleanup(func() {
		closeLogger()
		globalInit, current = previous, previousCurrent
	})

	retained := L()
	for i := 0; i < 2; i++ {
		closeLogger()
		require.Nil(t, current.Load(), "early shutdown must not initialize an output")
	}

	path := filepath.Join(t.TempDir(), "after-early-shutdown.log")
	require.NoError(t, InitLogger(&Config{Level: "info", Format: "json", Output: path}))
	retained.Info("retained-adapter-after-early-shutdown")
	closeLogger()
	closeLogger()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"msg":"retained-adapter-after-early-shutdown"`)
	require.NoError(t, os.Remove(path), "shutdown must release the later output file")
}
