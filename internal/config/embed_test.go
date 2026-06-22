package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedConfigNotEmpty verifies the embedded config is not empty
func TestEmbeddedConfigNotEmpty(t *testing.T) {
	content := string(embeddedConfigBytes())
	assert.NotEmpty(t, content, "embedded config should not be empty")
	assert.Greater(t, len(content), 1000, "embedded config should be substantial ( > 1000 chars)")
}

// TestEmbeddedConfigBytesNotEmpty verifies the embedded config bytes are not empty
func TestEmbeddedConfigBytesNotEmpty(t *testing.T) {
	bytes := embeddedConfigBytes()
	assert.NotEmpty(t, bytes, "embedded config bytes should not be empty")
	assert.Greater(t, len(bytes), 1000, "embedded config should be substantial ( > 1000 bytes)")
}

// TestEmbeddedConfigBytesReturnsCopy verifies that embeddedConfigBytes returns a copy
func TestEmbeddedConfigBytesReturnsCopy(t *testing.T) {
	bytes1 := embeddedConfigBytes()
	bytes2 := embeddedConfigBytes()

	// Modify bytes1
	if len(bytes1) > 0 {
		bytes1[0] = 'X'
	}

	// bytes2 should be unchanged
	assert.NotEqual(t, bytes1[0], bytes2[0], "embeddedConfigBytes should return independent copies")
}

// TestEmbeddedConfigMatchesSourceFile verifies embedded content matches the source file
func TestEmbeddedConfigMatchesSourceFile(t *testing.T) {
	// Find the source file
	_, testFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(testFile)
	repoRoot := filepath.Join(testDir, "..", "..")
	sourcePath := filepath.Join(repoRoot, "configs", "config.yaml.example")

	// Read the source file
	sourceBytes, err := os.ReadFile(sourcePath)
	require.NoError(t, err, "failed to read source config.yaml.example")

	// Get embedded content
	embedded := embeddedConfigBytes()

	// Compare (normalize line endings for Windows compatibility)
	sourceNormalized := strings.ReplaceAll(string(sourceBytes), "\r\n", "\n")
	embeddedNormalized := strings.ReplaceAll(string(embedded), "\r\n", "\n")

	assert.Equal(t, sourceNormalized, embeddedNormalized, "embedded config should match source file exactly")
}

// TestEmbeddedConfigContainsExpectedSections verifies key sections are present
func TestEmbeddedConfigContainsExpectedSections(t *testing.T) {
	content := string(embeddedConfigBytes())

	// Check for key sections
	assert.Contains(t, content, "config_version:", "should contain config_version")
	assert.Contains(t, content, "server:", "should contain server section")
	assert.Contains(t, content, "scrapers:", "should contain scrapers section")
	assert.Contains(t, content, "metadata:", "should contain metadata section")
	assert.Contains(t, content, "output:", "should contain output section")
	assert.Contains(t, content, "database:", "should contain database section")
}
