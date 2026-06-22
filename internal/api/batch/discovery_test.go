package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSiblingParts_AdditionalScenarios(t *testing.T) {
	t.Run("empty input returns empty slice", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		apiCfg := core.ConfigFromAppConfig(cfg)
		deps := createTestDeps(t, cfg, "")
		files, metadata := discoverSiblingPartsWithMetadata(context.Background(), nil, testkit.GetTestRuntime(deps), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())
		assert.Empty(t, files)
		assert.Nil(t, metadata)
	})

	t.Run("non multipart input stays unchanged", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		dir := t.TempDir()
		cfg.API.Security.AllowedDirectories = []string{dir}

		filePath := filepath.Join(dir, "IPX-123.mp4")
		require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

		apiCfg := core.ConfigFromAppConfig(cfg)
		deps := createTestDeps(t, cfg, "")
		got, metadata := discoverSiblingPartsWithMetadata(context.Background(), []string{filePath}, testkit.GetTestRuntime(deps), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())
		assert.Equal(t, []string{filePath}, got)
		assert.NotNil(t, metadata)
		// Non-multipart files should still have metadata
		assert.Contains(t, metadata, filePath)
		assert.False(t, metadata[filePath].IsMultiPart)
	})

	t.Run("discovers missing multipart siblings from allowed directory", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		dir := t.TempDir()
		cfg.API.Security.AllowedDirectories = []string{dir}
		cfg.API.Security.MaxFilesPerScan = 100

		partA := filepath.Join(dir, "IPX-535-pt1.mp4")
		partB := filepath.Join(dir, "IPX-535-pt2.mp4")
		other := filepath.Join(dir, "IPX-999.mp4")
		require.NoError(t, os.WriteFile(partA, []byte("a"), 0o644))
		require.NoError(t, os.WriteFile(partB, []byte("b"), 0o644))
		require.NoError(t, os.WriteFile(other, []byte("c"), 0o644))

		apiCfg := core.ConfigFromAppConfig(cfg)
		deps := createTestDeps(t, cfg, "")
		got, metadata := discoverSiblingPartsWithMetadata(context.Background(), []string{partA}, testkit.GetTestRuntime(deps), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())
		assert.ElementsMatch(t, []string{partA, partB}, got)
		// Both parts should have multipart metadata
		assert.True(t, metadata[partA].IsMultiPart)
		assert.Equal(t, 1, metadata[partA].PartNumber)
		assert.True(t, metadata[partB].IsMultiPart)
		assert.Equal(t, 2, metadata[partB].PartNumber)
	})

	t.Run("disallowed directory skips sibling discovery", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		dir := t.TempDir()
		cfg.API.Security.AllowedDirectories = []string{filepath.Join(dir, "elsewhere")}

		partA := filepath.Join(dir, "IPX-535-pt1.mp4")
		partB := filepath.Join(dir, "IPX-535-pt2.mp4")
		require.NoError(t, os.WriteFile(partA, []byte("a"), 0o644))
		require.NoError(t, os.WriteFile(partB, []byte("b"), 0o644))

		apiCfg := core.ConfigFromAppConfig(cfg)
		deps := createTestDeps(t, cfg, "")
		got, metadata := discoverSiblingPartsWithMetadata(context.Background(), []string{partA}, testkit.GetTestRuntime(deps), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())
		assert.Equal(t, []string{partA}, got)
		// Since directory is disallowed, the seam cannot scan it, so metadata
		// may be empty for files in disallowed directories. This is correct:
		// the API layer respects directory security settings.
		// Metadata absence means the file will be treated as single-part (safe default).
		_ = metadata
	})
}
