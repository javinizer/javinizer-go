package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/require"
)

// Exercise the real same-process initialization boundary, as the root pre-run
// does. This asserts diagnostics, not a downstream routing mutation: initConfig
// holds its loaded config locally. No Cobra command or callback is replaced.
func TestInitConfigDownloadProxyInheritanceDiagnostic(t *testing.T) {
	root, err := filepath.Abs(t.TempDir())
	require.NoError(t, err)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if upper == "LOG_LEVEL" || upper == "UMASK" || strings.HasPrefix(upper, "JAVINIZER_") || strings.HasPrefix(upper, "TRANSLATION_") || strings.HasPrefix(upper, "METADATA_TRANSLATION_") || strings.HasPrefix(upper, "OPENAI_") || strings.HasPrefix(upper, "DEEPL_") || strings.HasPrefix(upper, "GOOGLE_") || strings.HasPrefix(upper, "ANTHROPIC_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config-home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "userprofile"))
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	oldFile, oldVerbose, oldOutput, oldCmd := cfgFile, verboseFlag, originalLogOutput, currentCmd
	t.Cleanup(func() {
		cfgFile, verboseFlag, originalLogOutput, currentCmd = oldFile, oldVerbose, oldOutput, oldCmd
		logging.CloseLogger()
		// The logger API has no arbitrary-state snapshot. Restore the known default
		// lifecycle used by root tests, rather than claiming to restore prior files.
		require.NoError(t, logging.InitLogger(nil))
	})
	verboseFlag, currentCmd = false, nil
	for _, enabled := range []bool{true, false} {
		name := "inherit while global disabled"
		if !enabled {
			name = "download proxy explicitly disabled"
		}
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.Abs(t.TempDir())
			require.NoError(t, err)
			path, logPath := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "root.log")
			cfg := config.DefaultConfig(nil, nil)
			cfg.ConfigVersion = config.CurrentConfigVersion
			cfg.System.VersionCheckEnabled, cfg.System.Umask = false, ""
			cfg.Database.DSN = filepath.Join(dir, "must-not-open.db")
			cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.Output = "info", "json", logPath
			cfg.Scrapers.Proxy.Enabled = false
			cfg.Output.Download.DownloadProxy.Enabled = enabled
			cfg.Output.Download.DownloadProxy.Profile = ""
			_, err = config.Prepare(cfg)
			require.NoError(t, err)
			require.NoError(t, config.Save(cfg, path))
			// Preflight the same real load/validation under the isolated environment.
			// In particular, empty umask must survive load so initConfig cannot change
			// the process umask. No invalid configuration/os.Exit case is exercised.
			loaded, err := config.LoadOrCreate(path)
			require.NoError(t, err)
			_, err = config.Prepare(loaded)
			require.NoError(t, err)
			require.Empty(t, loaded.System.Umask)
			require.False(t, loaded.Scrapers.Proxy.Enabled)
			require.Equal(t, enabled, loaded.Output.Download.DownloadProxy.Enabled)
			require.Empty(t, loaded.Output.Download.DownloadProxy.Profile)
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			beforeUmask := config.UmaskValue()
			cfgFile = path
			initConfig()
			logging.CloseLogger()
			require.Equal(t, beforeUmask, config.UmaskValue())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			_, err = os.Stat(cfg.Database.DSN)
			require.ErrorIs(t, err, os.ErrNotExist)
			data, err := os.ReadFile(logPath)
			require.NoError(t, err)
			warningCount := 0
			const warning = "Download proxy is enabled but resolved profile URL is empty, disabling proxy"
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var event struct {
					Level   string `json:"level"`
					Message string `json:"msg"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &event))
				require.NotContains(t, event.Message, "Download proxy enabled:")
				if event.Message == warning {
					require.Equal(t, "warning", event.Level)
					warningCount++
				}
			}
			if enabled {
				require.Equal(t, 1, warningCount)
			} else {
				require.Zero(t, warningCount)
			}
		})
	}
}
