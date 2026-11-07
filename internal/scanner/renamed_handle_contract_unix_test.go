//go:build linux || darwin

package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestScanSingleFromHandleReportsMetadataLossAfterDirectoryRename(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original")
	moved := filepath.Join(root, "moved")
	require.NoError(t, os.Mkdir(original, 0700))
	contents := map[string]string{"FIRST-001.mp4": "first preserved video", "SECOND-002.mp4": "second preserved video"}
	for name, content := range contents {
		require.NoError(t, os.WriteFile(filepath.Join(original, name), []byte(content), 0600))
	}
	handle, err := os.Open(original)
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Close() })
	// A separate native entry detects filesystems that eagerly provide metadata.
	// The scanner's handle remains unread until after the rename.
	probe, err := os.Open(original)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Close() })
	entries, err := probe.ReadDir(-1)
	require.NoError(t, err)
	require.Len(t, entries, len(contents))
	require.NoError(t, probe.Close())
	require.NoError(t, os.Rename(original, moved))
	_, probeErr := entries[0].Info()
	if probeErr == nil {
		t.Skip("native directory entries eagerly cache metadata on this filesystem")
	}
	require.ErrorIs(t, probeErr, os.ErrNotExist)

	scanner := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mp4"}})
	result, err := scanner.ScanSingleFromHandle(handle, original)
	require.NoError(t, err, "directory enumeration succeeds through the retained handle")
	require.NotNil(t, result)
	require.Empty(t, result.Files, "unverified metadata must not become usable file matches")
	require.Empty(t, result.Skipped)
	require.Zero(t, result.SkippedCount, "metadata failures are errors, not configured exclusions")
	require.Len(t, result.Errors, len(contents))
	failedPaths := make([]string, 0, len(result.Errors))
	for _, entryErr := range result.Errors {
		require.ErrorIs(t, entryErr, os.ErrNotExist)
		var pathErr *os.PathError
		require.ErrorAs(t, entryErr, &pathErr)
		failedPaths = append(failedPaths, pathErr.Path)
	}
	require.ElementsMatch(t, []string{filepath.Join(original, "FIRST-001.mp4"), filepath.Join(original, "SECOND-002.mp4")}, failedPaths)
	// The caller retains ownership, including after per-entry errors.
	info, err := handle.Stat()
	require.NoError(t, err)
	movedInfo, err := os.Stat(moved)
	require.NoError(t, err)
	require.True(t, os.SameFile(info, movedInfo))
	require.NoError(t, handle.Close())
	require.NoDirExists(t, original)
	for name, content := range contents {
		actual, readErr := os.ReadFile(filepath.Join(moved, name))
		require.NoError(t, readErr)
		require.Equal(t, content, string(actual))
	}

	fresh, err := os.Open(moved)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })
	recovered, err := scanner.ScanSingleFromHandle(fresh, moved)
	require.NoError(t, err)
	require.Empty(t, recovered.Errors)
	require.Len(t, recovered.Files, len(contents))
	recoveredPaths := make([]string, 0, len(recovered.Files))
	for _, file := range recovered.Files {
		require.EqualValues(t, len(contents[file.Name]), file.Size)
		recoveredPaths = append(recoveredPaths, file.Path)
	}
	require.ElementsMatch(t, []string{filepath.Join(moved, "FIRST-001.mp4"), filepath.Join(moved, "SECOND-002.mp4")}, recoveredPaths)
}
