package scrape

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScrape_FlagErrorNonJSON(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"javinizer", "scrape", "TEST-001", "--bogus-flag"}
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--bogus-flag"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	require.Error(t, err)
	assert.NotEqual(t, ErrJSONExit, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

func TestRunScrapeJSON_StderrLoggerFallback(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := initJSONStderrLogger
	t.Cleanup(func() { initJSONStderrLogger = orig })
	initJSONStderrLogger = func(*logging.Config) error { return errors.New("logger boom") }
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
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

func TestRunScrapeJSON_PrepareFailure(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n  browser:\n    enabled: true\n    timeout: 30\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock", "--browser-timeout", "99999"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "invalid configuration")
}

func TestRunScrapeJSON_LogOutputStdoutFallback(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	t.Cleanup(func() { logging.CloseLogger() })
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.ToSlash(filepath.Join(tmpDir, "javinizer.db"))
	cfgContent := "config_version: 3\nlogging:\n  output: stdout\ndatabase:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
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

func TestRunScrapeJSON_BootstrapFailure(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := bootstrapQueryOnlyDeps
	t.Cleanup(func() { bootstrapQueryOnlyDeps = orig })
	bootstrapQueryOnlyDeps = func(cfg *config.Config) (*commandutil.CoreDeps, error) {
		return nil, errors.New("bootstrap boom")
	}
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
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
	assert.Contains(t, wrap.Error.Message, "failed to bootstrap")
}

func TestRunScrapeJSON_ContextErrorAfterQuery(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n  request_timeout_seconds: 60\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cmd.ExecuteContext(ctx)
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Equal(t, "unavailable", wrap.Error.Kind)
}

func TestRunScrapeJSON_NilResult(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"NIL-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "scraper returned no result")
}

func TestRunScrapeJSON_MarshalFailure(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	orig := marshalScraperResult
	t.Cleanup(func() { marshalScraperResult = orig })
	marshalScraperResult = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + filepath.Join(tmpDir, "javinizer.db") + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
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
	assert.Contains(t, wrap.Error.Message, "failed to marshal result")
}
