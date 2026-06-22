package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureOutput(t *testing.T) {
	stdout, stderr := CaptureOutput(t, func() {
		os.Stdout.Write([]byte("hello stdout"))
		os.Stderr.Write([]byte("hello stderr"))
	})

	assert.Equal(t, "hello stdout", stdout)
	assert.Equal(t, "hello stderr", stderr)
}

func TestCaptureOutput_Empty(t *testing.T) {
	stdout, stderr := CaptureOutput(t, func() {})

	assert.Equal(t, "", stdout)
	assert.Equal(t, "", stderr)
}

func TestCreateTestConfig(t *testing.T) {
	configPath, cfg := CreateTestConfig(t, nil)

	assert.NotEmpty(t, configPath)
	assert.NotNil(t, cfg)
	assert.FileExists(t, configPath)
}

func TestCreateTestConfig_WithCustomization(t *testing.T) {
	configPath, cfg := CreateTestConfig(t, func(c *config.Config) {
		c.Scrapers.Priority = []string{"r18dev"}
	})

	assert.NotEmpty(t, configPath)
	assert.Equal(t, []string{"r18dev"}, cfg.Scrapers.Priority)
}

func TestCreateTestVideoFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := CreateTestVideoFile(t, tmpDir, "IPX-123.mp4")

	assert.Equal(t, filepath.Join(tmpDir, "IPX-123.mp4"), path)
	assert.FileExists(t, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("dummy video content"), content)
}
