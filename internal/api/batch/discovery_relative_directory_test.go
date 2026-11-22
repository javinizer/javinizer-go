package batch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSiblingPartsDoesNotAppendAbsolutePathsFromRelativeScan(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	absoluteDir := filepath.Join(root, "absolute")
	relativeDir := "relative"
	makeParts := func(dir string) (string, string) {
		t.Helper()
		require.NoError(t, os.Mkdir(dir, 0o755))
		first := filepath.Join(dir, "QZT-535-pt1.mp4")
		second := filepath.Join(dir, "QZT-535-pt2.mp4")
		require.NoError(t, os.WriteFile(first, []byte("first"), 0o644))
		require.NoError(t, os.WriteFile(second, []byte("second"), 0o644))
		return first, second
	}
	// The absolute submission seeds discovery for the same multipart ID.
	// The scanner emits absolute paths even for the separately allowed
	// relative directory; those entries must not be appended as siblings.
	absoluteFirst, absoluteSecond := makeParts(absoluteDir)
	relativeFirst, relativeSecond := makeParts(relativeDir)
	absRelativeFirst, err := filepath.Abs(relativeFirst)
	require.NoError(t, err)
	absRelativeSecond, err := filepath.Abs(relativeSecond)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	apiCfg := core.ConfigFromAppConfig(cfg)
	deps := createTestDeps(t, cfg, "")
	files, metadata := discoverSiblingPartsWithMetadata(t.Context(), []string{absoluteFirst, relativeFirst}, testkit.GetTestRuntime(deps).Snapshot(), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())

	require.Equal(t, []string{absoluteFirst, relativeFirst, absoluteSecond}, files)
	require.True(t, metadata[absoluteSecond].IsMultiPart)
	require.Equal(t, 2, metadata[absoluteSecond].PartNumber)
	require.NotContains(t, metadata, absRelativeFirst)
	require.NotContains(t, metadata, absRelativeSecond)
}
