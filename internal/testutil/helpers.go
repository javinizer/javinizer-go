// Package testutil provides shared test utilities and helpers for javinizer-go tests.
package testutil

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// CaptureOutput captures stdout and stderr from a function execution.
// This is useful for testing CLI commands that write to console.
//
// Example:
//
//	stdout, stderr := testutil.CaptureOutput(t, func() {
//	    cmd.Execute()
//	})
func CaptureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	fn()

	_ = wOut.Close()
	_ = wErr.Close()

	return <-outC, <-errC
}

// CreateTestConfig creates a test configuration file with optional customizations.
// Returns the config path and the config object.
//
// Example:
//
//	configPath, cfg := testutil.CreateTestConfig(t, func(cfg *config.Config) {
//	    cfg.Scrapers.Priority = []string{"dmm", "r18dev"}
//	})
func CreateTestConfig(t *testing.T, customizeFn func(*config.Config)) (configPath string, cfg *config.Config) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "config.yaml")

	cfg = config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	if customizeFn != nil {
		customizeFn(cfg)
	}

	err := config.Save(cfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	return configPath, cfg
}

// CreateTestVideoFile creates a test video file with dummy content.
// Returns the full path to the created file.
//
// Example:
//
//	videoPath := testutil.CreateTestVideoFile(t, tmpDir, "IPX-535.mp4")
func CreateTestVideoFile(t *testing.T, dir string, filename string) string {
	t.Helper()

	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte("dummy video content"), 0644)
	require.NoError(t, err, "Failed to create test video file")

	return path
}

// InvalidConfigPath returns a path to a config file with invalid YAML content.
// The file exists (so LoadOrCreate calls Load instead of createFromEmbedded),
// but Load will fail with a parse error. This works reliably on all platforms,
// including Windows where directory-permission or blocker-file patterns
// do not prevent file creation.
func InvalidConfigPath(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: valid: yaml: [[["), 0644))
	return path
}

// UnreachableConfigPath is a deprecated alias for InvalidConfigPath.
// The name is misleading — the path is reachable (the file exists), but the
// YAML content is invalid. Use InvalidConfigPath in new code; existing callers
// are forwarded automatically.
func UnreachableConfigPath(t *testing.T) string {
	return InvalidConfigPath(t)
}
