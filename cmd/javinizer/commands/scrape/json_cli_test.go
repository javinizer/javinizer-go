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

func TestRunScrapeJSON_ValidationNoScrapers(t *testing.T) {
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--output", "json"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	require.Error(t, err)
	output := buf.String()
	assert.Contains(t, output, "exactly one scraper")
}

func TestRunScrapeJSON_ValidationTwoScrapers(t *testing.T) {
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--scrapers", "r18dev,dmm"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	require.Error(t, err)
	output := buf.String()
	assert.Contains(t, output, "exactly one scraper")
}

func TestRunScrapeJSON_ValidationForce(t *testing.T) {
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--scrapers", "r18dev", "--force"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	require.Error(t, err)
	output := buf.String()
	assert.Contains(t, output, "--force")
}

func TestRunScrapeJSON_ValidationEmptyScraperName(t *testing.T) {
	// Cobra's StringSlice with empty string gives []string{} (empty slice),
	// which triggers the len != 1 check, not the empty-name check.
	// Test validateJSONMode directly for the empty-name case.
	err := validateJSONMode([]string{""}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty")
}

func TestRunScrapeJSON_ExplicitEmptyOutputRejected(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--output="})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output value")
}

func TestRunScrapeJSON_InvalidOutputValue(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--output", "xml"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output value")
}

func TestRunScrapeJSON_LoggerErrorEmitsJSON(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "config_version: 3\nlogging:\n  level: invalid\nscrapers:\n  priority:\n    - e2emock\n"
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
	assert.Contains(t, wrap.Error.Message, "failed to initialize logger")
}

func TestRunScrapeJSON_ConfigErrorEmitsJSON(t *testing.T) {
	// Use an invalid YAML config file to trigger a config load error on all platforms
	tmpDir := t.TempDir()
	cfgPath := tmpDir + "/bad.yaml"
	os.WriteFile(cfgPath, []byte(":invalid: yaml: ["), 0644)
	t.Setenv("JAVINIZER_CONFIG", cfgPath)
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--scrapers", "r18dev"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	output := strings.TrimSpace(buf.String())
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal([]byte(output), &wrap), "output was: %q", output)
	assert.Equal(t, "unknown", wrap.Error.Kind)
}

func TestRunScrapeJSON_UnknownFlagEmitsJSON(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--unknown"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "unknown flag")
}

func TestRunScrapeJSON_MissingFlagValueEmitsJSON(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--output", "json", "--scrapers"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "needs an argument")
}

func TestRunScrapeJSON_UnknownFlagBeforeOutputEmitsJSON(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"javinizer", "scrape", "TEST-001", "--unknown", "--output", "json"}
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--unknown", "--output", "json"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "unknown flag")
}

func TestRunScrapeJSON_MissingFlagValueBeforeOutputEmitsJSON(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"javinizer", "scrape", "TEST-001", "--scrapers", "--output", "json"}
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--scrapers", "--output", "json"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "needs an argument")
}

func TestRunScrapeJSON_UnknownFlagBeforeOutputEqualsEmitsJSON(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"javinizer", "scrape", "TEST-001", "--unknown", "--output=json"}
	cmd := NewCommand()
	cmd.SetArgs([]string{"TEST-001", "--unknown", "--output=json"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.Execute()
	assert.Equal(t, ErrJSONExit, err)
	var wrap jsonErrorWrapper
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &wrap))
	assert.Contains(t, wrap.Error.Message, "unknown flag")
}

func TestErrJSONExit_IsSentinel(t *testing.T) {
	assert.Equal(t, "json error already emitted", ErrJSONExit.Error())
}

func TestWriteJSONError(t *testing.T) {
	cmd := NewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	writeJSONError(cmd, unknownErrorEnvelope("test error"))
	output := buf.String()
	assert.Contains(t, output, "test error")
	assert.Contains(t, output, "unknown")
}

func TestRemoveStdoutFromLogOutput_AllPatterns(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"", ""},
		{"stdout", ""},
		{"stderr", "stderr"},
		{"stdout,stderr", "stderr"},
		{"stdout,file.log", "file.log"},
		{"stdout,file.log,stderr", "file.log,stderr"},
		{"file.log,stdout,stderr", "file.log,stderr"},
		{"file.log", "file.log"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expect, removeStdoutFromLogOutput(tt.input))
	}
}

func TestHasOutputToken_AllPatterns(t *testing.T) {
	assert.True(t, hasOutputToken("stderr", "stderr"))
	assert.True(t, hasOutputToken("stdout,stderr", "stderr"))
	assert.True(t, hasOutputToken("stderr,file.log", "stderr"))
	assert.False(t, hasOutputToken("/var/log/javinizer-stderr.log", "stderr"))
	assert.False(t, hasOutputToken("stdout,file.log", "stderr"))
	assert.False(t, hasOutputToken("", "stderr"))
}

func TestApplyUmask_NoPanic(t *testing.T) {
	assert.NotPanics(t, func() { applyUmask(0o077) })
}
