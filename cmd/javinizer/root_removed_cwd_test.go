//go:build linux

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

// A shell can retain its working directory after another process removes it.
// Linux still resolves ../ paths through that directory's surviving parent,
// even though Getwd (and therefore filepath.Abs) can no longer name it.
func TestInitConfigLogsFromRemovedWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if upper == "LOG_LEVEL" || upper == "UMASK" || strings.HasPrefix(upper, "JAVINIZER_") || strings.HasPrefix(upper, "TRANSLATION_") || strings.HasPrefix(upper, "METADATA_TRANSLATION_") || strings.HasPrefix(upper, "OPENAI_") || strings.HasPrefix(upper, "DEEPL_") || strings.HasPrefix(upper, "GOOGLE_") || strings.HasPrefix(upper, "ANTHROPIC_") {
			t.Setenv(key, "")
		}
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.Umask = ""
	cfg.System.VersionCheckEnabled = false
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.Database.DSN = filepath.Join(dir, "must-not-open.db")
	cfg.Logging.Output = "../app.log"
	cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.MaxSizeMB = "info", "json", 0
	path := filepath.Join(dir, "config.yaml")
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	require.NoError(t, config.Save(cfg, path))
	loaded, err := config.LoadOrCreate(path)
	require.NoError(t, err)
	require.Empty(t, loaded.System.Umask)
	require.Equal(t, "../app.log", loaded.Logging.Output)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	oldFile, oldVerbose, oldOutput, oldCmd := cfgFile, verboseFlag, originalLogOutput, currentCmd
	t.Cleanup(func() {
		cfgFile, verboseFlag, originalLogOutput, currentCmd = oldFile, oldVerbose, oldOutput, oldCmd
		logging.CloseLogger()
		require.NoError(t, logging.InitLogger(nil))
	})
	working := filepath.Join(dir, "removed")
	require.NoError(t, os.Mkdir(working, 0700))
	t.Chdir(working)
	require.NoError(t, os.Remove(working))
	_, err = os.Getwd()
	require.ErrorIs(t, err, os.ErrNotExist)
	// Preflight the real logger boundary before calling initConfig, which exits
	// on initialization failure. No logger or filesystem implementation is replaced.
	probe, err := os.OpenFile("../app.log", os.O_CREATE|os.O_WRONLY, 0600)
	require.NoError(t, err)
	require.NoError(t, probe.Close())
	cfgFile, verboseFlag, currentCmd = path, false, nil
	umaskBefore := config.UmaskValue()
	initConfig()
	logging.CloseLogger()
	require.Equal(t, umaskBefore, config.UmaskValue())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(cfg.Database.DSN)
	require.ErrorIs(t, err, os.ErrNotExist)
	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	require.NoError(t, err)
	found := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if strings.HasPrefix(event.Message, "Log file:") {
			require.Equal(t, "info", event.Level)
			require.Equal(t, "Log file: ../app.log", event.Message)
			found++
		}
	}
	require.Equal(t, 1, found, "diagnostic must retain the usable relative log path when cwd has no absolute name")
}
