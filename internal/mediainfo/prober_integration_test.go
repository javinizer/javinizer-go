package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProberRegistry_ProbeWithFallback_UnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	unknownPath := filepath.Join(tmpDir, "unknown.bin")

	// Create a file with unknown format
	err := os.WriteFile(unknownPath, []byte("UNKNOWN_FORMAT_HEADER_DATA"), 0644)
	require.NoError(t, err)

	f, err := os.Open(unknownPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	cfg := DefaultMediaInfoConfig()
	registry := NewProberRegistry(cfg)

	_, err = registry.ProbeWithFallback(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported container format")
}

func TestProberRegistry_ProbeWithFallback_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.bin")

	// Create a file smaller than 16 bytes
	err := os.WriteFile(smallPath, []byte("TOO_SMALL"), 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	cfg := DefaultMediaInfoConfig()
	registry := NewProberRegistry(cfg)

	_, err = registry.ProbeWithFallback(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file header")
}

func TestAnalyzeWithConfig_InvalidFile(t *testing.T) {
	cfg := DefaultMediaInfoConfig()
	_, err := AnalyzeWithConfig("/nonexistent/file.mp4", cfg)
	assert.Error(t, err)
}

func TestAnalyzeWithConfig_TooSmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "tiny.mp4")

	err := os.WriteFile(smallPath, []byte("tiny"), 0644)
	require.NoError(t, err)

	cfg := DefaultMediaInfoConfig()
	_, err = AnalyzeWithConfig(smallPath, cfg)
	assert.Error(t, err)
}

func TestProberRegistry_NewProberRegistry_WithCLI(t *testing.T) {
	cfg := &MediaInfoConfig{
		CLIEnabled: true,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}

	registry := NewProberRegistry(cfg)
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.cliProber)
}

func TestProberRegistry_NewProberRegistry_WithoutCLI(t *testing.T) {
	cfg := &MediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}

	registry := NewProberRegistry(cfg)
	assert.NotNil(t, registry)
	assert.Nil(t, registry.cliProber)
}
