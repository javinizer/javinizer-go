package testutil_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAferoBasicOperations demonstrates the basic usage of afero.NewMemMapFs()
// for in-memory filesystem testing. This approach provides:
//
// 1. Speed: 100-1000x faster than real filesystem I/O
// 2. Isolation: No cleanup needed, tests don't interfere with each other
// 3. Simplicity: No temp directory management or cleanup logic
// 4. Safety: No risk of accidentally modifying real files
//
// Use afero.NewMemMapFs() for all test code and afero.NewOsFs() for production code.
func TestAferoBasicOperations(t *testing.T) {
	// Create an in-memory filesystem
	// This is completely isolated from the real filesystem
	fs := afero.NewMemMapFs()

	// Create a directory structure
	err := fs.MkdirAll("/test/data/movies", 0755)
	require.NoError(t, err, "should create nested directories")

	// Write a file
	testContent := []byte("IPX-001 Test Movie")
	err = afero.WriteFile(fs, "/test/data/movies/IPX-001.mp4", testContent, 0644)
	require.NoError(t, err, "should write file to in-memory filesystem")

	// Read the file back
	readContent, err := afero.ReadFile(fs, "/test/data/movies/IPX-001.mp4")
	require.NoError(t, err, "should read file from in-memory filesystem")
	assert.Equal(t, testContent, readContent, "content should match")

	// Check if file exists
	exists, err := afero.Exists(fs, "/test/data/movies/IPX-001.mp4")
	require.NoError(t, err)
	assert.True(t, exists, "file should exist")

	// List directory contents
	entries, err := afero.ReadDir(fs, "/test/data/movies")
	require.NoError(t, err, "should read directory")
	require.Len(t, entries, 1, "should have one file")
	assert.Equal(t, "IPX-001.mp4", entries[0].Name())

	// Get file info
	fileInfo, err := fs.Stat("/test/data/movies/IPX-001.mp4")
	require.NoError(t, err, "should stat file")
	assert.Equal(t, int64(len(testContent)), fileInfo.Size())
	assert.False(t, fileInfo.IsDir())

	// Remove file
	err = fs.Remove("/test/data/movies/IPX-001.mp4")
	require.NoError(t, err, "should remove file")

	// Verify file is gone
	exists, err = afero.Exists(fs, "/test/data/movies/IPX-001.mp4")
	require.NoError(t, err)
	assert.False(t, exists, "file should not exist after removal")

	// Note: No cleanup needed! The in-memory filesystem is automatically
	// garbage collected when the test ends. This is much simpler than
	// managing temp directories with defer cleanup logic.
}

// TestAferoFileOperations demonstrates advanced file operations
// including opening files, copying, and moving.
func TestAferoFileOperations(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create source and destination directories
	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, fs.MkdirAll("/dest", 0755))

	// Create a source file using File interface
	srcPath := "/source/movie.mp4"
	srcFile, err := fs.Create(srcPath)
	require.NoError(t, err)

	// Write content
	content := []byte("Movie content here")
	_, err = srcFile.Write(content)
	require.NoError(t, err)
	require.NoError(t, srcFile.Close())

	// Open file for reading
	readFile, err := fs.Open(srcPath)
	require.NoError(t, err)
	defer func() { _ = readFile.Close() }()

	// Read content back
	readBuf := make([]byte, len(content))
	n, err := readFile.Read(readBuf)
	require.NoError(t, err)
	assert.Equal(t, len(content), n)
	assert.Equal(t, content, readBuf)

	// Copy file to destination (manual copy example)
	destPath := "/dest/movie.mp4"
	destFile, err := fs.Create(destPath)
	require.NoError(t, err)

	err = afero.WriteFile(fs, destPath, content, 0644)
	require.NoError(t, err)
	_ = destFile.Close()

	// Verify both files exist
	srcExists, _ := afero.Exists(fs, srcPath)
	destExists, _ := afero.Exists(fs, destPath)
	assert.True(t, srcExists, "source should exist")
	assert.True(t, destExists, "destination should exist")

	// Verify content matches
	destContent, err := afero.ReadFile(fs, destPath)
	require.NoError(t, err)
	assert.Equal(t, content, destContent)

	// Rename/move operation
	newPath := "/dest/renamed.mp4"
	err = fs.Rename(destPath, newPath)
	require.NoError(t, err)

	// Verify old path doesn't exist, new path does
	oldExists, _ := afero.Exists(fs, destPath)
	newExists, _ := afero.Exists(fs, newPath)
	assert.False(t, oldExists, "old path should not exist")
	assert.True(t, newExists, "new path should exist")
}

// TestAferoWithComponentIntegration demonstrates how to use afero
// with actual application components (like Scanner, Downloader, etc.)
func TestAferoWithComponentIntegration(t *testing.T) {
	// This demonstrates the pattern used throughout the codebase
	fs := afero.NewMemMapFs()

	// 1. Setup test directory structure
	require.NoError(t, fs.MkdirAll("/videos/jav", 0755))

	// 2. Create test video files
	testFiles := []string{
		"IPX-001.mp4",
		"IPX-002.mkv",
		"ABW-123.avi",
		"readme.txt", // Non-video file to test filtering
	}

	for _, filename := range testFiles {
		path := filepath.Join("/videos/jav", filename)
		err := afero.WriteFile(fs, path, []byte("test content"), 0644)
		require.NoError(t, err, "should create test file: %s", filename)
	}

	// 3. In real code, you'd pass this fs to your components like:
	//
	//    scanner := scanner.NewScanner(fs, &config.MatchingConfig{
	//        Extensions: []string{".mp4", ".mkv", ".avi"},
	//    })
	//    result, err := scanner.Scan("/videos/jav")
	//
	//    organizer := organizer.NewOrganizer(fs, &config.OutputConfig{})
	//    err := organizer.Organize(fileInfo, movie, "/output")
	//
	//    downloader := downloader.NewDownloader(fs, cfg, userAgent)
	//    err := downloader.DownloadCover(movie, "/output/covers")
	//
	//    nfoGen := nfo.NewGenerator(fs, nfoConfig)
	//    err := nfoGen.Generate(movie, "/output/movie.nfo", "", "")

	// 4. Verify files were created
	entries, err := afero.ReadDir(fs, "/videos/jav")
	require.NoError(t, err)
	assert.Len(t, entries, len(testFiles), "should have all test files")

	// 5. The key benefit: No cleanup needed!
	// Compare to traditional approach:
	//
	//    tmpDir := os.TempDir()
	//    defer os.RemoveAll(tmpDir)  // Must remember to clean up
	//    // Risk of leaving files behind if test panics
	//    // Risk of deleting wrong directory if path logic is buggy
	//
	// With afero.NewMemMapFs():
	//    - No temp directory needed
	//    - No cleanup code needed
	//    - No risk of filesystem pollution
	//    - Tests run 100-1000x faster
}

// TestAferoProductionVsTest demonstrates the difference between
// production and test filesystem initialization.
func TestAferoProductionVsTest(t *testing.T) {
	// In TESTS: Always use NewMemMapFs() for fast, isolated testing
	testFs := afero.NewMemMapFs()
	assert.NotNil(t, testFs)

	// In PRODUCTION: Always use NewOsFs() for real filesystem operations
	// Example from production code:
	//
	//    func main() {
	//        cfg := config.Load()
	//        fs := afero.NewOsFs()  // Real filesystem for production
	//        scanner := scanner.NewScanner(fs, &cfg.Matching)
	//        organizer := organizer.NewOrganizer(fs, &cfg.Output)
	//        // ... rest of application
	//    }

	// The beauty of the afero.Fs interface is that the same code works
	// for both production (afero.NewOsFs) and testing (afero.NewMemMapFs)
	// without any changes to the component logic.
}

// TestAferoErrorHandling demonstrates error handling patterns
func TestAferoErrorHandling(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Reading non-existent file returns error
	_, err := afero.ReadFile(fs, "/nonexistent.txt")
	assert.Error(t, err, "should error on missing file")

	// Stating non-existent file returns error
	_, err = fs.Stat("/nonexistent.txt")
	assert.Error(t, err, "should error when statting missing file")

	// Note: MemMapFs automatically creates parent directories when using Create()
	// This is different from OsFs behavior, but it's fine for testing purposes
	// as it makes tests simpler. For testing directory creation explicitly,
	// use MkdirAll and verify with Stat.

	// Creating directories explicitly
	require.NoError(t, fs.MkdirAll("/deep/nested", 0755))

	// Verify directory exists
	info, err := fs.Stat("/deep/nested")
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "should be a directory")

	// Create file in the directory
	f, err := fs.Create("/deep/nested/file.txt")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Verify file exists
	exists, err := afero.Exists(fs, "/deep/nested/file.txt")
	require.NoError(t, err)
	assert.True(t, exists, "file should exist")
}
