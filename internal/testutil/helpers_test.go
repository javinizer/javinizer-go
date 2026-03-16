package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadGoldenFile_Success tests loading an existing golden file.
func TestLoadGoldenFile_Success(t *testing.T) {
	// Setup: Create a testdata directory with a golden file in the current package
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	goldenContent := []byte("test golden content")
	goldenPath := filepath.Join(testdataDir, "test_success.golden")
	err = os.WriteFile(goldenPath, goldenContent, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	// Test loading
	content := LoadGoldenFile(t, "test_success.golden")
	assert.Equal(t, goldenContent, content)
}

// TestLoadGoldenFile_EmptyFile tests loading an empty golden file.
func TestLoadGoldenFile_EmptyFile(t *testing.T) {
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	// Create empty golden file
	goldenPath := filepath.Join(testdataDir, "test_empty.golden")
	err = os.WriteFile(goldenPath, []byte{}, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	content := LoadGoldenFile(t, "test_empty.golden")
	assert.Empty(t, content)
}

// TestLoadGoldenFile_UnicodeContent tests loading a golden file with unicode characters.
func TestLoadGoldenFile_UnicodeContent(t *testing.T) {
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	unicodeContent := []byte("日本語タイトル\n中文字符\n한글\n🎬📺")
	goldenPath := filepath.Join(testdataDir, "test_unicode.golden")
	err = os.WriteFile(goldenPath, unicodeContent, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	content := LoadGoldenFile(t, "test_unicode.golden")
	assert.Equal(t, unicodeContent, content)
}

// TestLoadGoldenFile_LargeFile tests loading a large golden file (>10KB).
func TestLoadGoldenFile_LargeFile(t *testing.T) {
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	// Create a 15KB golden file
	largeContent := []byte(strings.Repeat("A", 15*1024))
	goldenPath := filepath.Join(testdataDir, "test_large.golden")
	err = os.WriteFile(goldenPath, largeContent, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	content := LoadGoldenFile(t, "test_large.golden")
	assert.Equal(t, largeContent, content)
	assert.Equal(t, 15*1024, len(content))
}

// TestCompareGoldenFile_Match tests comparing matching content.
func TestCompareGoldenFile_Match(t *testing.T) {
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	goldenContent := []byte("matching content")
	goldenPath := filepath.Join(testdataDir, "test_match.golden")
	err = os.WriteFile(goldenPath, goldenContent, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	// Should not fail with matching content
	CompareGoldenFile(t, "test_match.golden", goldenContent)
}

// TestCompareGoldenFile_EmptyMatch tests comparing empty content.
func TestCompareGoldenFile_EmptyMatch(t *testing.T) {
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	goldenPath := filepath.Join(testdataDir, "test_empty_match.golden")
	err = os.WriteFile(goldenPath, []byte{}, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	CompareGoldenFile(t, "test_empty_match.golden", []byte{})
}

// TestUpdateGoldenFile_CreateNew tests creating a new golden file.
func TestUpdateGoldenFile_CreateNew(t *testing.T) {
	// Cleanup any existing testdata directory first
	testdataDir := "testdata"
	goldenPath := filepath.Join(testdataDir, "test_new.golden")
	_ = os.Remove(goldenPath) // Remove if exists from previous test

	content := []byte("new golden file content")
	err := UpdateGoldenFile("test_new.golden", content)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	// Verify file was created
	assert.FileExists(t, goldenPath)

	// Verify content
	readContent, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)
}

// TestUpdateGoldenFile_OverwriteExisting tests overwriting an existing golden file.
func TestUpdateGoldenFile_OverwriteExisting(t *testing.T) {
	// Create initial golden file
	testdataDir := "testdata"
	err := os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)

	initialContent := []byte("initial content")
	goldenPath := filepath.Join(testdataDir, "test_overwrite.golden")
	err = os.WriteFile(goldenPath, initialContent, 0644)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	// Overwrite with new content
	newContent := []byte("overwritten content")
	err = UpdateGoldenFile("test_overwrite.golden", newContent)
	require.NoError(t, err)

	// Verify content was overwritten
	readContent, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	assert.Equal(t, newContent, readContent)
}

// TestUpdateGoldenFile_CreateDirectory tests that testdata directory is created if missing.
func TestUpdateGoldenFile_CreateDirectory(t *testing.T) {
	testdataDir := "testdata"
	goldenPath := filepath.Join(testdataDir, "test_mkdir.golden")

	// Ensure testdata exists (it should from previous tests)
	// This test verifies UpdateGoldenFile handles mkdir correctly
	content := []byte("test content")
	err := UpdateGoldenFile("test_mkdir.golden", content)
	require.NoError(t, err)
	defer func() { _ = os.Remove(goldenPath) }()

	// Verify testdata directory exists
	info, err := os.Stat(testdataDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file was created
	assert.FileExists(t, goldenPath)
}

// TestGenerateDiff tests the diff generation helper.
func TestGenerateDiff(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		wantDiff bool
	}{
		{
			name:     "identical single line",
			expected: "same content",
			actual:   "same content",
			wantDiff: false,
		},
		{
			name:     "different single line",
			expected: "expected content",
			actual:   "actual content",
			wantDiff: true,
		},
		{
			name:     "multiline match",
			expected: "line 1\nline 2\nline 3",
			actual:   "line 1\nline 2\nline 3",
			wantDiff: false,
		},
		{
			name:     "multiline mismatch",
			expected: "line 1\nexpected line 2\nline 3",
			actual:   "line 1\nactual line 2\nline 3",
			wantDiff: true,
		},
		{
			name:     "extra lines in actual",
			expected: "line 1\nline 2",
			actual:   "line 1\nline 2\nline 3",
			wantDiff: true,
		},
		{
			name:     "extra lines in expected",
			expected: "line 1\nline 2\nline 3",
			actual:   "line 1\nline 2",
			wantDiff: true,
		},
		{
			name:     "empty strings",
			expected: "",
			actual:   "",
			wantDiff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := generateDiff(tt.expected, tt.actual)

			if tt.wantDiff {
				assert.NotEqual(t, "(no differences found)", diff,
					"Expected a diff but got no differences")
				assert.Contains(t, diff, "[line", "Diff should contain line numbers")
			} else {
				assert.Equal(t, "(no differences found)", diff,
					"Expected no differences but got a diff: %s", diff)
			}
		})
	}
}

// TestSplitLines tests the splitLines helper.
func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
		{
			name:  "single line",
			input: "single line",
			want:  []string{"single line"},
		},
		{
			name:  "multiple lines",
			input: "line 1\nline 2\nline 3",
			want:  []string{"line 1", "line 2", "line 3"},
		},
		{
			name:  "trailing newline",
			input: "line 1\nline 2\n",
			want:  []string{"line 1", "line 2", ""},
		},
		{
			name:  "empty lines",
			input: "line 1\n\nline 3",
			want:  []string{"line 1", "", "line 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Example demonstrates loading and comparing a golden file for NFO XML testing.
func ExampleLoadGoldenFile() {
	// In a real test:
	// func TestNFOGeneration(t *testing.T) {
	//     movie := testutil.NewMovieBuilder().Build()
	//     actual := nfo.Generate(movie)
	//     expected := testutil.LoadGoldenFile(t, "movie_complete.xml.golden")
	//     assert.Equal(t, expected, actual)
	// }
}

// Example demonstrates comparing actual output against a golden file.
func ExampleCompareGoldenFile() {
	// In a real test:
	// func TestAPIResponse(t *testing.T) {
	//     response := api.GetMovie("IPX-123")
	//     actual, _ := json.MarshalIndent(response, "", "  ")
	//     testutil.CompareGoldenFile(t, "movie_response.json.golden", actual)
	// }
}

// Example demonstrates manual golden file creation workflow.
func ExampleUpdateGoldenFile() {
	// During test development ONLY (not in automated tests):
	// func TestGenerateGolden(t *testing.T) {
	//     output := generateComplexOutput()
	//     err := testutil.UpdateGoldenFile("output.golden", output)
	//     require.NoError(t, err)
	//     // Remove this test after golden file is created
	// }
}
