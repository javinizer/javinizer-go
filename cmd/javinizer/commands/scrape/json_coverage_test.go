package scrape

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScrapeJSON_BootstrapError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "nonexistent", "deep", "db.sqlite")
	cfgContent := "database:\n  dsn: " + dbPath + "\n  type: sqlite\nscrapers:\n  priority:\n    - r18dev\n"
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)
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

func TestRunScrapeJSON_SuccessSerialization(t *testing.T) {
	result := &models.ScraperResult{Source: "test", ID: "TEST-001", Title: "Test Movie"}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Test Movie")
	assert.Contains(t, string(data), "TEST-001")
}

func TestRunScrapeJSON_UnknownScraperError(t *testing.T) {
	// The unknown-scraper path is tested in internal/scrape via TestQueryRaw_UnknownScraper.
	// Here we test the error envelope mapping.
	envelope := scraperErrorToEnvelope(&models.ScraperError{
		Scraper: "nonexistent",
		Kind:    models.ScraperErrorKindUnknown,
		Message: "scraper \"nonexistent\" is not registered or enabled",
	})
	assert.Equal(t, "unknown", envelope.Kind)
	assert.Contains(t, envelope.Message, "not registered")
}
