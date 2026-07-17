package scrape

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScrapeJSON_Verbose(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.PersistentFlags().BoolP("verbose", "v", false, "")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock", "-v"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.NoError(t, err, "output: %s", buf.String())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &result))
	assert.Equal(t, "e2emock", result["source"])
}

func TestRunScrapeJSON_PrepareError(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority: []\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
}

func TestRunScrapeJSON_Umask(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\nsystem:\n  umask: \"022\"\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.NoError(t, err, "output: %s", buf.String())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &result))
	assert.Equal(t, "e2emock", result["source"])
}

func TestRunScrapeJSON_ConfiguredLogOutput(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	logFile := filepath.ToSlash(filepath.Join(tmpDir, "javinizer.log"))
	dbPath := filepath.ToSlash(filepath.Join(tmpDir, "javinizer.db"))
	cfgContent := "config_version: 3\nlogging:\n  output: " + logFile + "\ndatabase:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.NoError(t, err, "output: %s", buf.String())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &result))
	assert.Equal(t, "e2emock", result["source"])
}

func TestRunScrapeJSON_ConfiguredLogOutputWithStdout(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	logFile := filepath.ToSlash(filepath.Join(tmpDir, "javinizer.log"))
	dbPath := filepath.ToSlash(filepath.Join(tmpDir, "javinizer.db"))
	cfgContent := "config_version: 3\nlogging:\n  output: stdout," + logFile + "\ndatabase:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.NoError(t, err, "output: %s", buf.String())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &result))
	assert.Equal(t, "e2emock", result["source"])
}

func TestRunScrapeJSON_ContextTimeout(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n  request_timeout_seconds: 1\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	output := strings.TrimSpace(buf.String())
	require.NotEmpty(t, output)
	if err != nil {
		var wrap jsonErrorWrapper
		require.NoError(t, json.Unmarshal([]byte(output), &wrap))
		assert.Contains(t, []string{"unavailable", "unknown"}, wrap.Error.Kind)
	} else {
		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "e2emock", result["source"])
	}
}

func TestMalformedValueFlag_AllBranches(t *testing.T) {
	assert.Equal(t, "--scrapers", malformedValueFlag([]string{"--scrapers"}))
	assert.Equal(t, "--scrapers", malformedValueFlag([]string{"--scrapers", "--output"}))
	assert.Equal(t, "", malformedValueFlag([]string{"--scrapers", "r18dev"}))
	assert.Equal(t, "-s", malformedValueFlag([]string{"-s"}))
	assert.Equal(t, "--browser-timeout", malformedValueFlag([]string{"--browser-timeout", "--output"}))
	assert.Equal(t, "--config", malformedValueFlag([]string{"--config"}))
}

func TestRequestedJSONOutput_NotJSON(t *testing.T) {
	cmd := NewCommand()
	assert.False(t, requestedJSONOutput(cmd))
}

func TestContextErrorForJSON_Canceled(t *testing.T) {
	err := contextErrorForJSON(context.Canceled)
	assert.Equal(t, models.ScraperErrorKindUnavailable, err.Kind)
	assert.Equal(t, context.Canceled.Error(), err.Message)
	assert.True(t, err.Retryable)
	assert.True(t, err.Temporary)
}

func TestRemoveStdoutFromLogOutput_EdgeCases(t *testing.T) {
	assert.Equal(t, "", removeStdoutFromLogOutput("stdout,stdout"))
	assert.Equal(t, "file.log,stderr", removeStdoutFromLogOutput(" stdout , file.log , stderr "))
}

func TestRunScrapeJSON_ScraperError(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"UNKNOWN-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.NotEmpty(t, wrap.Error.Message)
}
