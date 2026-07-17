package scrape

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScrapeJSON_FullSuccessPath(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	dbBlocker := filepath.Join(tmpDir, "database-blocker")
	require.NoError(t, os.WriteFile(dbBlocker, []byte("not a directory"), 0o600))
	dbPath := filepath.Join(dbBlocker, "javinizer.db")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n"
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"GOOD-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.NoError(t, err, "output: %s", buf.String())
	output := strings.TrimSpace(buf.String())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result), "output: %q", output)
	assert.Equal(t, "e2emock", result["source"])
	_, statErr := os.Stat(dbPath)
	assert.Error(t, statErr)
}

func TestRunScrapeJSON_TimeoutError(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "javinizer.db")
	cfgContent := "config_version: 3\ndatabase:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - e2emock\n  request_timeout_seconds: 1\n"
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--scrapers", "e2emock"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	output := strings.TrimSpace(buf.String())
	require.NotEmpty(t, output)
	if err != nil {
		var wrap jsonErrorWrapper
		require.NoError(t, json.Unmarshal([]byte(output), &wrap))
	} else {
		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(output), &result))
	}
}
