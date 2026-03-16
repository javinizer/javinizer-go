package commandutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanAndMatch_Success tests successful scanning and matching
func TestScanAndMatch_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test video file
	testFile := filepath.Join(tmpDir, "IPX-123.mp4")
	err := os.WriteFile(testFile, []byte("fake video"), 0644)
	require.NoError(t, err)

	// Create test config with matching patterns
	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	stdout, _ := captureOutput(t, func() {
		matches, scanResult, err := ScanAndMatch(tmpDir, true, fileScanner, fileMatcher)
		require.NoError(t, err)
		assert.Len(t, matches, 1)
		assert.Equal(t, "IPX-123", matches[0].ID)
		assert.NotNil(t, scanResult)
	})

	assert.Contains(t, stdout, "Scanning for video files")
	assert.Contains(t, stdout, "Found 1 video file(s)")
	assert.Contains(t, stdout, "Extracting JAV IDs")
	assert.Contains(t, stdout, "Matched 1 file(s)")
}

// TestScanAndMatch_NonRecursive tests non-recursive scanning
func TestScanAndMatch_NonRecursive(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test video file
	testFile := filepath.Join(tmpDir, "ABC-456.mp4")
	err := os.WriteFile(testFile, []byte("fake video"), 0644)
	require.NoError(t, err)

	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	// Test with recursive=false (should use ScanSingle)
	matches, scanResult, err := ScanAndMatch(testFile, false, fileScanner, fileMatcher)
	require.NoError(t, err)
	assert.Len(t, matches, 1)
	assert.Equal(t, "ABC-456", matches[0].ID)
	assert.NotNil(t, scanResult)
}

// TestScanAndMatch_ScanError tests error handling during scan
func TestScanAndMatch_ScanError(t *testing.T) {
	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	// Try to scan a non-existent directory
	matches, scanResult, err := ScanAndMatch("/nonexistent/path/that/does/not/exist", true, fileScanner, fileMatcher)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan failed")
	assert.Nil(t, matches)
	assert.Nil(t, scanResult)
}

// TestScanAndMatch_NoFilesFound tests when no video files are found
func TestScanAndMatch_NoFilesFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create non-video file
	testFile := filepath.Join(tmpDir, "readme.txt")
	err := os.WriteFile(testFile, []byte("not a video"), 0644)
	require.NoError(t, err)

	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4", ".mkv"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	stdout, _ := captureOutput(t, func() {
		matches, scanResult, err := ScanAndMatch(tmpDir, true, fileScanner, fileMatcher)
		require.NoError(t, err)
		assert.Nil(t, matches)
		assert.NotNil(t, scanResult)
		assert.Len(t, scanResult.Files, 0)
	})

	assert.Contains(t, stdout, "Found 0 video file(s)")
	assert.Contains(t, stdout, "No files to process")
}

// TestScanAndMatch_NoMatchesFound tests when files are found but no JAV IDs match
func TestScanAndMatch_NoMatchesFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create video file without JAV ID pattern
	testFile := filepath.Join(tmpDir, "random_movie.mp4")
	err := os.WriteFile(testFile, []byte("fake video"), 0644)
	require.NoError(t, err)

	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	stdout, _ := captureOutput(t, func() {
		matches, scanResult, err := ScanAndMatch(tmpDir, true, fileScanner, fileMatcher)
		require.NoError(t, err)
		assert.Nil(t, matches)
		assert.NotNil(t, scanResult)
		assert.Len(t, scanResult.Files, 1)
	})

	assert.Contains(t, stdout, "Found 1 video file(s)")
	assert.Contains(t, stdout, "Matched 0 file(s)")
	assert.Contains(t, stdout, "No JAV IDs found in filenames")
}

// TestScanAndMatch_WithSkippedFiles tests when some files are skipped
func TestScanAndMatch_WithSkippedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid video file
	validFile := filepath.Join(tmpDir, "IPX-789.mp4")
	err := os.WriteFile(validFile, []byte("fake video"), 0644)
	require.NoError(t, err)

	// Create a very small file that might be skipped
	smallFile := filepath.Join(tmpDir, "ABC-123.mp4")
	err = os.WriteFile(smallFile, []byte("x"), 0644)
	require.NoError(t, err)

	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
		WithMinFileSize(1), // 1 MB minimum (will skip small test files)
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	_, _ = captureOutput(t, func() {
		_, scanResult, err := ScanAndMatch(tmpDir, true, fileScanner, fileMatcher)
		require.NoError(t, err)
		assert.NotNil(t, scanResult)

		// Should have skipped the small file due to size filtering
		assert.GreaterOrEqual(t, len(scanResult.Skipped), 1)
	})
}

// TestScanAndMatch_MultipleFiles tests scanning multiple files with grouping
func TestScanAndMatch_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple files for same ID
	file1 := filepath.Join(tmpDir, "IPX-100-part1.mp4")
	file2 := filepath.Join(tmpDir, "IPX-100-part2.mp4")
	file3 := filepath.Join(tmpDir, "ABC-200.mp4")

	err := os.WriteFile(file1, []byte("fake video 1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("fake video 2"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file3, []byte("fake video 3"), 0644)
	require.NoError(t, err)

	configPath, testCfg := createTestConfig(t,
		WithVideoExtensions([]string{".mp4"}),
		WithMatchingPatterns([]string{`([A-Z]+-\d+)`}),
	)
	_ = configPath

	fileScanner := scanner.NewScanner(afero.NewOsFs(), &testCfg.Matching)
	fileMatcher, err := matcher.NewMatcher(&testCfg.Matching)
	require.NoError(t, err)

	stdout, _ := captureOutput(t, func() {
		matches, scanResult, err := ScanAndMatch(tmpDir, true, fileScanner, fileMatcher)
		require.NoError(t, err)
		assert.Len(t, matches, 3) // 3 files matched
		assert.NotNil(t, scanResult)
	})

	assert.Contains(t, stdout, "Found 3 video file(s)")
	assert.Contains(t, stdout, "Matched 3 file(s)")
	assert.Contains(t, stdout, "Found 2 unique ID(s)") // IPX-100 and ABC-200
}
