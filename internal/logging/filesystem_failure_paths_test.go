package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestFailedWriterReloadPreservesActiveLogger(t *testing.T) {
	for _, kind := range []string{"directory creation", "plain open", "rotation stat", "rotation create"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			activePath := filepath.Join(root, "active.log")
			init := newLoggerInit()
			require.NoError(t, init.Init(&Config{Level: "info", Format: "text", Output: activePath}))
			active := init.getLogrus()
			activeFile, ok := active.Out.(*os.File)
			require.True(t, ok)
			t.Cleanup(func() { require.NoError(t, activeFile.Close()) })
			active.Info("before rejected reload")
			first := filepath.Join(root, "first.log")
			bad := filepath.Join(root, "bad")
			rotation := 0
			want := ""
			switch kind {
			case "directory creation":
				require.NoError(t, os.WriteFile(bad, []byte("not a directory"), 0600))
				bad = filepath.Join(bad, "output.log")
				want = "failed to create log directory"
			case "plain open":
				require.NoError(t, os.Mkdir(bad, 0700))
				want = "failed to open log file"
			case "rotation stat":
				rotation = 1
				if err := os.Symlink(bad, bad); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
				want = "failed to stat log file"
			case "rotation create":
				rotation = 1
				if err := os.Symlink(filepath.Join(root, "missing-parent", "target.log"), bad); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
				want = "failed to create log file"
			}
			err := init.Init(&Config{Level: "info", Format: "text", Output: first + "," + bad, MaxSizeMB: rotation})
			require.ErrorContains(t, err, want)
			require.Same(t, active, init.getLogrus(), "failed reload must leave the working logger installed")
			init.L().Info("after rejected reload")
			contents, err := os.ReadFile(activePath)
			require.NoError(t, err)
			require.Contains(t, string(contents), "before rejected reload")
			require.Contains(t, string(contents), "after rejected reload")
			firstContents, err := os.ReadFile(first)
			require.NoError(t, err)
			require.Empty(t, firstContents, "no records should be routed to partially prepared output")
		})
	}
}

func TestFreshInitializerLazilyInstallsDefaults(t *testing.T) {
	init := newLoggerInit()
	logger := init.getLogrus()
	require.Equal(t, logrus.InfoLevel, logger.Level)
	require.IsType(t, &logrus.TextFormatter{}, logger.Formatter)
	require.Same(t, os.Stdout, logger.Out)
	require.Same(t, logger, init.getLogrus(), "subsequent lookups must preserve the initialized logger")
}
