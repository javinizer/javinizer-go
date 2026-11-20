package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestRetainedLoggerAdapterFallsBackAfterCloseAndFollowsReinitialization(t *testing.T) {
	// Like the other global logger tests, this test must not run in parallel.
	CloseLogger() // Finish prior output cleanup before installing owned files.
	dir := t.TempDir()
	standard := logrus.StandardLogger()
	originalOutput, originalLevel := standard.Out, standard.GetLevel()
	var fallback bytes.Buffer
	standard.SetOutput(&fallback)
	standard.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		CloseLogger()
		standard.SetOutput(originalOutput)
		standard.SetLevel(originalLevel)
	})

	firstPath, nextPath := filepath.Join(dir, "first.log"), filepath.Join(dir, "next.log")
	require.NoError(t, InitLogger(&Config{Level: "info", Format: "json", Output: firstPath}))
	retained := L()
	retained.Info("before-close-marker")
	Info("package-before-close-marker")
	CloseLogger()
	before, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	require.Contains(t, string(before), `"msg":"before-close-marker"`)
	require.Contains(t, string(before), `"msg":"package-before-close-marker"`)

	retained.Info("adapter-fallback-marker")
	Info("package-after-close-marker")
	require.Contains(t, fallback.String(), "adapter-fallback-marker", "a retained adapter must remain safe after public shutdown")
	require.Contains(t, fallback.String(), "package-after-close-marker", "package logging shares the post-shutdown fallback")
	after, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "shutdown must not leave the old file as the active destination")
	fallbackBeforeReload := fallback.String()

	require.NoError(t, InitLogger(&Config{Level: "info", Format: "json", Output: nextPath}))
	retained.Info("after-reinit-marker")
	Info("package-after-reinit-marker")
	CloseLogger()
	next, err := os.ReadFile(nextPath)
	require.NoError(t, err)
	require.Contains(t, string(next), `"msg":"after-reinit-marker"`, "the same adapter must follow the newly initialized logger")
	require.Contains(t, string(next), `"msg":"package-after-reinit-marker"`)
	require.NotContains(t, string(next), "after-close-marker")
	require.Equal(t, fallbackBeforeReload, fallback.String(), "reinitialization must stop routing to the fallback")
	after, err = os.ReadFile(firstPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "reinitialization must not reopen the old output")
}
