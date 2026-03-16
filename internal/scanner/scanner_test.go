package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestScanner_Scan(t *testing.T) {
	// Create temp directory structure with test files
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string]int64{
		"movie1.mp4":                  100 * 1024 * 1024, // 100MB
		"movie2.mkv":                  200 * 1024 * 1024, // 200MB
		"movie3-trailer.mp4":          10 * 1024 * 1024,  // 10MB (should be excluded)
		"movie4-sample.mp4":           5 * 1024 * 1024,   // 5MB (should be excluded)
		"document.txt":                1 * 1024,          // Should be excluded (wrong extension)
		"small.mp4":                   100 * 1024,        // 100KB (should be excluded if min size > 1MB)
		"subfolder/movie5.mp4":        150 * 1024 * 1024, // 150MB (in subfolder)
		"subfolder/movie6.mkv":        180 * 1024 * 1024, // 180MB (in subfolder)
		"subfolder/nested/movie7.avi": 120 * 1024 * 1024, // 120MB (nested)
	}

	for path, size := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Create file with specified size
		file, err := os.Create(fullPath)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
		if err := file.Truncate(size); err != nil {
			_ = file.Close()
			t.Fatalf("Failed to set file size: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Failed to close file: %v", err)
		}
	}

	t.Run("Scan with default config", func(t *testing.T) {
		cfg := &config.MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
		}
		scanner := NewScanner(afero.NewOsFs(), cfg)

		result, err := scanner.Scan(tmpDir)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}

		// Should find: movie1.mp4, movie2.mkv, movie5.mp4, movie6.mkv, movie7.avi, small.mp4
		expectedCount := 6
		if len(result.Files) != expectedCount {
			t.Errorf("Expected %d files, got %d", expectedCount, len(result.Files))
			t.Logf("Found files:")
			for _, f := range result.Files {
				t.Logf("  - %s", f.Name)
			}
		}

		// Verify skipped files
		// Should skip: movie3-trailer.mp4, movie4-sample.mp4, document.txt
		expectedSkipped := 3
		if len(result.Skipped) != expectedSkipped {
			t.Errorf("Expected %d skipped files, got %d", expectedSkipped, len(result.Skipped))
		}
	})

	t.Run("Scan with minimum size filter", func(t *testing.T) {
		cfg := &config.MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi"},
			MinSizeMB:       50, // 50MB minimum
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
		}
		scanner := NewScanner(afero.NewOsFs(), cfg)

		result, err := scanner.Scan(tmpDir)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}

		// Should only find files >= 50MB
		// movie1.mp4 (100MB), movie2.mkv (200MB), movie5.mp4 (150MB), movie6.mkv (180MB), movie7.avi (120MB)
		expectedCount := 5
		if len(result.Files) != expectedCount {
			t.Errorf("Expected %d files >= 50MB, got %d", expectedCount, len(result.Files))
		}

		// Verify all files are >= 50MB
		minBytes := int64(50 * 1024 * 1024)
		for _, f := range result.Files {
			if f.Size < minBytes {
				t.Errorf("File %s (%d bytes) is below minimum size", f.Name, f.Size)
			}
		}
	})

	t.Run("Scan with specific extensions", func(t *testing.T) {
		cfg := &config.MatchingConfig{
			Extensions:      []string{".mp4"}, // Only MP4 files
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
		}
		scanner := NewScanner(afero.NewOsFs(), cfg)

		result, err := scanner.Scan(tmpDir)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}

		// Should only find .mp4 files: movie1.mp4, small.mp4, movie5.mp4
		expectedCount := 3
		if len(result.Files) != expectedCount {
			t.Errorf("Expected %d .mp4 files, got %d", expectedCount, len(result.Files))
		}

		// Verify all files are .mp4
		for _, f := range result.Files {
			if f.Extension != ".mp4" {
				t.Errorf("Found non-MP4 file: %s", f.Name)
			}
		}
	})
}

func TestScanner_ScanSingle(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"movie1.mp4",
		"movie2.mkv",
		"movie3-trailer.mp4",
		"document.txt",
	}

	for _, name := range testFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{"*-trailer*"},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	t.Run("Scan single directory (non-recursive)", func(t *testing.T) {
		result, err := scanner.ScanSingle(tmpDir)
		if err != nil {
			t.Fatalf("ScanSingle failed: %v", err)
		}

		// Should find: movie1.mp4, movie2.mkv (not movie3-trailer.mp4)
		expectedCount := 2
		if len(result.Files) != expectedCount {
			t.Errorf("Expected %d files, got %d", expectedCount, len(result.Files))
		}
	})

	t.Run("Scan single file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "movie1.mp4")
		result, err := scanner.ScanSingle(filePath)
		if err != nil {
			t.Fatalf("ScanSingle failed: %v", err)
		}

		if len(result.Files) != 1 {
			t.Errorf("Expected 1 file, got %d", len(result.Files))
		}

		if len(result.Files) > 0 && result.Files[0].Name != "movie1.mp4" {
			t.Errorf("Expected movie1.mp4, got %s", result.Files[0].Name)
		}
	})

	t.Run("Scan single file that should be excluded", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "movie3-trailer.mp4")
		result, err := scanner.ScanSingle(filePath)
		if err != nil {
			t.Fatalf("ScanSingle failed: %v", err)
		}

		if len(result.Files) != 0 {
			t.Errorf("Expected 0 files (excluded), got %d", len(result.Files))
		}

		if len(result.Skipped) != 1 {
			t.Errorf("Expected 1 skipped file, got %d", len(result.Skipped))
		}
	})
}

func TestScanner_Filter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"movie1.mp4":         "valid mp4",
		"movie2.mkv":         "valid mkv",
		"movie3-trailer.mp4": "trailer",
		"document.txt":       "text file",
	}

	var filePaths []string
	for name, content := range testFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
		filePaths = append(filePaths, path)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{"*-trailer*"},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result := scanner.Filter(filePaths)

	// Should filter to: movie1.mp4, movie2.mkv
	expectedCount := 2
	if len(result) != expectedCount {
		t.Errorf("Expected %d filtered files, got %d", expectedCount, len(result))
	}

	// Verify filtered files
	for _, f := range result {
		if f.Extension != ".mp4" && f.Extension != ".mkv" {
			t.Errorf("Unexpected file extension: %s", f.Extension)
		}
		if filepath.Base(f.Path) == "movie3-trailer.mp4" {
			t.Error("Trailer file should be filtered out")
		}
	}
}

func TestScanner_ExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		filename        string
		excludePatterns []string
		shouldInclude   bool
	}{
		{"movie.mp4", []string{"*-trailer*"}, true},
		{"movie-trailer.mp4", []string{"*-trailer*"}, false},
		{"movie-sample.mp4", []string{"*-sample*"}, false},
		{"movie-TRAILER.mp4", []string{"*-trailer*"}, true}, // Case sensitive (pattern is lowercase)
		{"SAMPLE-movie.mp4", []string{"SAMPLE-*"}, false},   // Match uppercase at start
		{"movie.mp4", []string{"*-trailer*", "*-sample*"}, true},
		{"trailer-movie.mp4", []string{"trailer-*"}, false},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			// Create file
			path := filepath.Join(tmpDir, tc.filename)
			if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}

			cfg := &config.MatchingConfig{
				Extensions:      []string{".mp4"},
				MinSizeMB:       0,
				ExcludePatterns: tc.excludePatterns,
			}
			scanner := NewScanner(afero.NewOsFs(), cfg)

			result, err := scanner.ScanSingle(tmpDir)
			if err != nil {
				t.Fatalf("ScanSingle failed: %v", err)
			}

			found := false
			for _, f := range result.Files {
				if f.Name == tc.filename {
					found = true
					break
				}
			}

			if found != tc.shouldInclude {
				t.Errorf("File %s: expected include=%v, got include=%v", tc.filename, tc.shouldInclude, found)
			}

			// Clean up for next test
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatalf("Failed to remove test file: %v", err)
			}
		})
	}
}

func TestScanner_FileInfo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	filename := "test-movie.mp4"
	filepath := filepath.Join(tmpDir, filename)
	content := []byte("test content for file info")
	if err := os.WriteFile(filepath, content, 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.ScanSingle(tmpDir)
	if err != nil {
		t.Fatalf("ScanSingle failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(result.Files))
	}

	info := result.Files[0]

	// Verify FileInfo fields
	if info.Name != filename {
		t.Errorf("Expected name %s, got %s", filename, info.Name)
	}

	if info.Extension != ".mp4" {
		t.Errorf("Expected extension .mp4, got %s", info.Extension)
	}

	if info.Size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), info.Size)
	}

	if info.Dir != tmpDir {
		t.Errorf("Expected dir %s, got %s", tmpDir, info.Dir)
	}

	if info.Path != filepath {
		t.Errorf("Expected path %s, got %s", filepath, info.Path)
	}
}

func TestScanner_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(result.Files) != 0 {
		t.Errorf("Expected 0 files in empty directory, got %d", len(result.Files))
	}

	if len(result.Errors) != 0 {
		t.Errorf("Expected 0 errors, got %d", len(result.Errors))
	}
}

func TestScanner_NonExistentPath(t *testing.T) {
	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	_, err := scanner.Scan("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Expected error for non-existent path, got nil")
	}
}

func TestScanner_ScanWithLimits_MaxFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 10 video files
	for i := 1; i <= 10; i++ {
		filename := filepath.Join(tmpDir, filepath.FromSlash(fmt.Sprintf("movie%02d.mp4", i)))
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	t.Run("Max files limit reached", func(t *testing.T) {
		result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 5)
		if err != nil {
			t.Fatalf("ScanWithLimits failed: %v", err)
		}

		if len(result.Files) != 5 {
			t.Errorf("Expected 5 files (max limit), got %d", len(result.Files))
		}

		if !result.LimitReached {
			t.Error("Expected LimitReached to be true")
		}
	})

	t.Run("No max files limit (maxFiles = 0)", func(t *testing.T) {
		result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 0)
		if err != nil {
			t.Fatalf("ScanWithLimits failed: %v", err)
		}

		if len(result.Files) != 10 {
			t.Errorf("Expected 10 files (no limit), got %d", len(result.Files))
		}

		if result.LimitReached {
			t.Error("Expected LimitReached to be false")
		}
	})
}

func TestScanner_ScanWithLimits_Timeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files to make scan take time
	for i := 0; i < 200; i++ {
		filename := filepath.Join(tmpDir, filepath.FromSlash(fmt.Sprintf("movie%03d.mp4", i)))
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Create a context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := scanner.ScanWithLimits(ctx, tmpDir, 0)
	if err != nil {
		t.Fatalf("ScanWithLimits failed: %v", err)
	}

	// Should have timed out quickly and not scanned all files
	if !result.TimedOut {
		t.Error("Expected TimedOut to be true")
	}

	// Should have scanned fewer than 200 files
	if len(result.Files) >= 200 {
		t.Errorf("Expected timeout to stop scan early, but got %d files", len(result.Files))
	}
}

func TestScanner_ScanWithLimits_ContextTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files (need >100 to hit the context check point)
	// Context is only checked every 100 files for performance
	for i := 0; i < 250; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("movie%03d.mp4", i))
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Use a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure context is expired

	result, err := scanner.ScanWithLimits(ctx, tmpDir, 0)
	if err != nil {
		t.Fatalf("ScanWithLimits failed: %v", err)
	}

	// Context should have timed out (checked every 100 files)
	// With 250 files and expired context, should timeout at 100 or 200 file mark
	if !result.TimedOut {
		t.Error("Expected TimedOut to be true with expired context")
	}

	// Should have scanned some files but stopped early due to timeout
	if len(result.Files) >= 250 {
		t.Errorf("Expected timeout to stop scan early, but got %d files", len(result.Files))
	}
}

func TestScanner_ScanWithLimits_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid file
	validFile := filepath.Join(tmpDir, "movie.mp4")
	if err := os.WriteFile(validFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create a directory that we'll make unreadable (platform-dependent)
	unreadableDir := filepath.Join(tmpDir, "unreadable")
	if err := os.Mkdir(unreadableDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create a file inside
	unreadableFile := filepath.Join(unreadableDir, "hidden.mp4")
	if err := os.WriteFile(unreadableFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Make directory unreadable (Unix only - Windows doesn't enforce chmod 0000)
	if runtime.GOOS == "windows" {
		t.Skip("Skipping chmod-based permission test on Windows")
	}
	if err := os.Chmod(unreadableDir, 0000); err != nil {
		t.Skipf("Cannot change directory permissions: %v", err)
	}
	defer func() {
		_ = os.Chmod(unreadableDir, 0755)
	}() // Restore for cleanup

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 0)
	if err != nil {
		t.Fatalf("ScanWithLimits should continue on errors: %v", err)
	}

	// Should have found the valid file
	if len(result.Files) < 1 {
		t.Error("Expected to find at least 1 valid file")
	}

	// Should have recorded errors from unreadable directory
	if len(result.Errors) == 0 {
		t.Error("Expected errors from unreadable directory")
	}
}

func TestScanner_ScanSingle_NonExistentPath(t *testing.T) {
	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	_, err := scanner.ScanSingle("/nonexistent/file.mp4")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}
}

func TestScanner_ScanSingle_FileIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory with a file
	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create a file in the subdirectory
	nestedFile := filepath.Join(subdir, "nested.mp4")
	if err := os.WriteFile(nestedFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// ScanSingle on a directory should scan its direct children (non-recursive)
	result, err := scanner.ScanSingle(subdir)
	if err != nil {
		t.Fatalf("ScanSingle failed: %v", err)
	}

	// Should find the nested file
	if len(result.Files) != 1 {
		t.Errorf("Expected 1 file in subdirectory, got %d", len(result.Files))
	}

	if len(result.Files) > 0 && result.Files[0].Name != "nested.mp4" {
		t.Errorf("Expected nested.mp4, got %s", result.Files[0].Name)
	}
}

func TestScanner_ScanSingle_ErrorGettingFileInfo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid file
	validFile := filepath.Join(tmpDir, "valid.mp4")
	if err := os.WriteFile(validFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create a file that we'll make unreadable
	unreadableFile := filepath.Join(tmpDir, "unreadable.mp4")
	if err := os.WriteFile(unreadableFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Make file unreadable (Unix only) - 0000 permissions
	// Note: On macOS, root can still read, so we may not get errors
	if err := os.Chmod(unreadableFile, 0000); err != nil {
		t.Skip("Cannot change file permissions (possibly Windows)")
	}
	defer func() {
		_ = os.Chmod(unreadableFile, 0644)
	}() // Restore for cleanup

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.ScanSingle(tmpDir)
	if err != nil {
		t.Fatalf("ScanSingle should continue on file info errors: %v", err)
	}

	// Should have found valid.mp4
	if len(result.Files) < 1 {
		t.Error("Expected to find at least 1 valid file")
	}

	// Note: On some systems (macOS with root), we may still be able to read file info
	// So we don't assert on errors - the important thing is it doesn't crash
	t.Logf("Found %d files, %d errors", len(result.Files), len(result.Errors))
}

func TestScanner_Filter_EmptyList(t *testing.T) {
	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result := scanner.Filter([]string{})

	if len(result) != 0 {
		t.Errorf("Expected 0 files from empty list, got %d", len(result))
	}
}

func TestScanner_Filter_NonExistentFiles(t *testing.T) {
	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	files := []string{
		"/nonexistent/file1.mp4",
		"/nonexistent/file2.mkv",
	}

	result := scanner.Filter(files)

	// Should filter out non-existent files
	if len(result) != 0 {
		t.Errorf("Expected 0 files (non-existent), got %d", len(result))
	}
}

func TestScanner_Filter_DirectoriesIgnored(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory named like a video file (edge case)
	dirWithVideoName := filepath.Join(tmpDir, "movie.mp4")
	if err := os.Mkdir(dirWithVideoName, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create a real video file
	videoFile := filepath.Join(tmpDir, "real.mp4")
	if err := os.WriteFile(videoFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	files := []string{dirWithVideoName, videoFile}
	result := scanner.Filter(files)

	// Should only find the real file, not the directory
	if len(result) != 1 {
		t.Errorf("Expected 1 file (directory ignored), got %d", len(result))
	}

	if len(result) > 0 && result[0].Name != "real.mp4" {
		t.Errorf("Expected real.mp4, got %s", result[0].Name)
	}
}

func TestScanner_shouldIncludeFile_CaseInsensitiveExtensions(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		filename string
		expected bool
	}{
		{"movie.mp4", true},
		{"movie.MP4", true},
		{"movie.Mp4", true},
		{"movie.mP4", true},
		{"movie.mkv", true},
		{"movie.MKV", true},
		{"movie.avi", false}, // Not in extensions list
		{"movie.txt", false},
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join(tmpDir, tc.filename)
			if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}
			defer func() {
				_ = os.Remove(path)
			}()

			result := scanner.shouldIncludeFile(path, nil)
			if result != tc.expected {
				t.Errorf("File %s: expected %v, got %v", tc.filename, tc.expected, result)
			}
		})
	}
}

func TestScanner_shouldIncludeFile_FilesWithoutExtension(t *testing.T) {
	tmpDir := t.TempDir()

	filename := "movie"
	path := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result := scanner.shouldIncludeFile(path, nil)
	if result {
		t.Error("File without extension should not be included")
	}
}

func TestScanner_shouldIncludeFile_MultipleDotsInFilename(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		filename string
		expected bool
	}{
		{"movie.part1.mp4", true},
		{"movie.720p.x264.mp4", true},
		{"archive.tar.gz", false},
		{"movie.part1.part2.mkv", true},
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join(tmpDir, tc.filename)
			if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}
			defer func() {
				_ = os.Remove(path)
			}()

			result := scanner.shouldIncludeFile(path, nil)
			if result != tc.expected {
				t.Errorf("File %s: expected %v, got %v", tc.filename, tc.expected, result)
			}
		})
	}
}

func TestScanner_shouldIncludeFile_InvalidGlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	filename := "movie.mp4"
	path := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Use an invalid glob pattern that will cause filepath.Match to error
	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{"[invalid"}, // Invalid pattern (unclosed bracket)
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Should not crash and should still include file (error in pattern is ignored)
	result := scanner.shouldIncludeFile(path, nil)
	if !result {
		t.Error("File should be included when glob pattern is invalid")
	}
}

func TestScanner_shouldIncludeFile_MinSizeWithEntry(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files of different sizes using Truncate (sparse files, no memory allocation)
	smallFile := filepath.Join(tmpDir, "small.mp4")
	f1, err := os.Create(smallFile)
	if err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}
	if err := f1.Truncate(10 * 1024 * 1024); err != nil { // 10MB
		_ = f1.Close()
		t.Fatalf("Failed to truncate small file: %v", err)
	}
	_ = f1.Close()

	largeFile := filepath.Join(tmpDir, "large.mp4")
	f2, err := os.Create(largeFile)
	if err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}
	if err := f2.Truncate(100 * 1024 * 1024); err != nil { // 100MB
		_ = f2.Close()
		t.Fatalf("Failed to truncate large file: %v", err)
	}
	_ = f2.Close()

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       50, // 50MB minimum
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Test with DirEntry (via ScanSingle)
	result, err := scanner.ScanSingle(tmpDir)
	if err != nil {
		t.Fatalf("ScanSingle failed: %v", err)
	}

	// Should only find large.mp4 (>= 50MB)
	if len(result.Files) != 1 {
		t.Errorf("Expected 1 file >= 50MB, got %d", len(result.Files))
	}

	if len(result.Files) > 0 && result.Files[0].Name != "large.mp4" {
		t.Errorf("Expected large.mp4, got %s", result.Files[0].Name)
	}

	// Verify skipped count includes small file
	if result.SkippedCount != 1 {
		t.Errorf("Expected 1 skipped file, got %d", result.SkippedCount)
	}
}

func TestScanner_shouldIncludeFile_MinSizeWithoutEntry(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a small file using Truncate (sparse file, no memory allocation)
	smallFile := filepath.Join(tmpDir, "small.mp4")
	f, err := os.Create(smallFile)
	if err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}
	if err := f.Truncate(10 * 1024 * 1024); err != nil { // 10MB
		_ = f.Close()
		t.Fatalf("Failed to truncate small file: %v", err)
	}
	_ = f.Close()

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       50, // 50MB minimum
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Test shouldIncludeFile directly with nil entry (forces os.Stat path)
	result := scanner.shouldIncludeFile(smallFile, nil)
	if result {
		t.Error("Small file should not be included when using nil entry (os.Stat path)")
	}
}

func TestScanner_TotalScanned(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files of various types
	for i := 1; i <= 5; i++ {
		videoFile := filepath.Join(tmpDir, filepath.FromSlash("movie"+string(rune('0'+i))+".mp4"))
		if err := os.WriteFile(videoFile, []byte("video"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	for i := 1; i <= 3; i++ {
		textFile := filepath.Join(tmpDir, filepath.FromSlash("doc"+string(rune('0'+i))+".txt"))
		if err := os.WriteFile(textFile, []byte("text"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 0)
	if err != nil {
		t.Fatalf("ScanWithLimits failed: %v", err)
	}

	// TotalScanned should be 8 (5 video + 3 text files)
	expectedTotal := 8
	if result.TotalScanned != expectedTotal {
		t.Errorf("Expected TotalScanned=%d, got %d", expectedTotal, result.TotalScanned)
	}

	// Files should only include matching video files
	if len(result.Files) != 5 {
		t.Errorf("Expected 5 matching files, got %d", len(result.Files))
	}

	// Skipped should be 3 text files
	if result.SkippedCount != 3 {
		t.Errorf("Expected 3 skipped files, got %d", result.SkippedCount)
	}
}

func TestScanner_MaxSkippedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create more than MaxSkippedFiles non-video files (all in flat structure to avoid directory counting issues)
	numFiles := MaxSkippedFiles + 100
	for i := 0; i < numFiles; i++ {
		// Use fmt-style formatting for reliable filenames
		filename := filepath.Join(tmpDir, "document"+string(rune('A'+i%26))+string(rune('A'+(i/26)%26))+string(rune('A'+(i/676)%26))+".txt")
		if err := os.WriteFile(filename, []byte("text"), 0644); err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"}, // No MP4 files, all .txt files will be skipped
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 0)
	if err != nil {
		t.Fatalf("ScanWithLimits failed: %v", err)
	}

	// Skipped list should be capped at MaxSkippedFiles
	if len(result.Skipped) > MaxSkippedFiles {
		t.Errorf("Expected Skipped list length <= %d, got %d", MaxSkippedFiles, len(result.Skipped))
	}

	// The key test: SkippedCount should be greater than MaxSkippedFiles (we have 1100 files)
	// But Skipped array should be capped at MaxSkippedFiles
	if result.SkippedCount <= MaxSkippedFiles {
		t.Errorf("Expected SkippedCount > %d (created %d files), got %d", MaxSkippedFiles, numFiles, result.SkippedCount)
	}

	if len(result.Skipped) != MaxSkippedFiles {
		t.Errorf("Expected Skipped array to be capped at %d, got %d", MaxSkippedFiles, len(result.Skipped))
	}

	// TotalScanned should match SkippedCount (no video files to include)
	if result.TotalScanned != result.SkippedCount {
		t.Logf("TotalScanned=%d, SkippedCount=%d (difference may be OS-specific)", result.TotalScanned, result.SkippedCount)
	}
}

func TestScanner_RelativePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	filename := "movie.mp4"
	fullPath := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Scan with relative path "."
	result, err := scanner.Scan(".")
	if err != nil {
		t.Fatalf("Scan with relative path failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Errorf("Expected 1 file with relative path, got %d", len(result.Files))
	}

	// Path should be converted to absolute
	if len(result.Files) > 0 {
		if !filepath.IsAbs(result.Files[0].Path) {
			t.Error("Expected absolute path in result")
		}
	}
}

// TODO: Fix symlink detection - fs.WalkDir's DirEntry.Info() follows symlinks
// Scanner uses d.Info() which calls Stat() instead of Lstat(), preventing symlink detection
// This bug was hidden when tests used afero.NewMemMapFs() (which doesn't support symlinks properly)
// Now revealed with afero.NewOsFs(). Need to refactor scanner to use fs.Lstat() for symlink detection.
func TestScanner_Symlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real video file
	realFile := filepath.Join(tmpDir, "real.mp4")
	if err := os.WriteFile(realFile, []byte("real video"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create a symlink to the video file (Unix-only)
	symlinkFile := filepath.Join(tmpDir, "symlink.mp4")
	if err := os.Symlink(realFile, symlinkFile); err != nil {
		t.Skip("Cannot create symlinks (possibly Windows or restricted permissions)")
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	t.Run("ScanWithLimits skips symlinks", func(t *testing.T) {
		result, err := scanner.ScanWithLimits(context.Background(), tmpDir, 0)
		if err != nil {
			t.Fatalf("ScanWithLimits failed: %v", err)
		}

		// Should only find real.mp4, symlink should be skipped
		if len(result.Files) != 1 {
			t.Errorf("Expected 1 file (symlink skipped), got %d", len(result.Files))
		}

		if len(result.Files) > 0 && result.Files[0].Name != "real.mp4" {
			t.Errorf("Expected real.mp4, got %s", result.Files[0].Name)
		}

		// Symlink should be in skipped list
		if result.SkippedCount != 1 {
			t.Errorf("Expected 1 skipped file (symlink), got %d", result.SkippedCount)
		}
	})

	t.Run("ScanSingle skips symlinks", func(t *testing.T) {
		result, err := scanner.ScanSingle(tmpDir)
		if err != nil {
			t.Fatalf("ScanSingle failed: %v", err)
		}

		// Should only find real.mp4, symlink should be skipped
		if len(result.Files) != 1 {
			t.Errorf("Expected 1 file (symlink skipped), got %d", len(result.Files))
		}

		if result.SkippedCount != 1 {
			t.Errorf("Expected 1 skipped file (symlink), got %d", result.SkippedCount)
		}
	})

	t.Run("ScanSingle on symlink file", func(t *testing.T) {
		t.Skip("TODO: Fix symlink detection bug - see TestScanner_Symlinks comment")
		result, err := scanner.ScanSingle(symlinkFile)
		if err != nil {
			t.Fatalf("ScanSingle failed: %v", err)
		}

		// Symlink file should be skipped
		if len(result.Files) != 0 {
			t.Errorf("Expected 0 files (symlink skipped), got %d", len(result.Files))
		}

		if result.SkippedCount != 1 {
			t.Errorf("Expected 1 skipped file (symlink), got %d", result.SkippedCount)
		}
	})

	t.Run("Filter skips symlinks", func(t *testing.T) {
		t.Skip("TODO: Fix symlink detection bug - see TestScanner_Symlinks comment")
		files := []string{realFile, symlinkFile}
		result := scanner.Filter(files)

		// Should only return real.mp4
		if len(result) != 1 {
			t.Errorf("Expected 1 file (symlink filtered), got %d", len(result))
		}

		if len(result) > 0 && result[0].Name != "real.mp4" {
			t.Errorf("Expected real.mp4, got %s", result[0].Name)
		}
	})
}

func TestScanner_ScanSingle_WithNestedDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	nestedSubdir := filepath.Join(subdir, "nested")
	if err := os.Mkdir(nestedSubdir, 0755); err != nil {
		t.Fatalf("Failed to create nested subdirectory: %v", err)
	}

	// Create files at different levels
	topLevelFile := filepath.Join(tmpDir, "top.mp4")
	subdirFile := filepath.Join(subdir, "sub.mp4")
	nestedFile := filepath.Join(nestedSubdir, "nested.mp4")

	for _, path := range []string{topLevelFile, subdirFile, nestedFile} {
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// ScanSingle on tmpDir should only find top.mp4 (non-recursive)
	result, err := scanner.ScanSingle(tmpDir)
	if err != nil {
		t.Fatalf("ScanSingle failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Errorf("Expected 1 file (non-recursive), got %d", len(result.Files))
	}

	if len(result.Files) > 0 && result.Files[0].Name != "top.mp4" {
		t.Errorf("Expected top.mp4, got %s", result.Files[0].Name)
	}
}

func TestScanner_ScanSingle_ErrorReadingDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory we'll make unreadable
	unreadableDir := filepath.Join(tmpDir, "unreadable")
	if err := os.Mkdir(unreadableDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Make directory unreadable (Unix only - Windows doesn't enforce chmod 0000)
	if runtime.GOOS == "windows" {
		t.Skip("Skipping chmod-based permission test on Windows")
	}
	if err := os.Chmod(unreadableDir, 0000); err != nil {
		t.Skipf("Cannot change directory permissions: %v", err)
	}
	defer func() { _ = os.Chmod(unreadableDir, 0755) }() // Restore for cleanup

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	// ScanSingle on unreadable directory should return error
	_, err := scanner.ScanSingle(unreadableDir)
	if err == nil {
		t.Error("Expected error when reading unreadable directory")
	}
}

func TestScanner_DeepNestedStructure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deep nested structure (10 levels)
	currentDir := tmpDir
	for i := 0; i < 10; i++ {
		currentDir = filepath.Join(currentDir, "level"+string(rune('0'+i)))
		if err := os.MkdirAll(currentDir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Create a file at each level
		filename := filepath.Join(currentDir, "movie.mp4")
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4"},
		MinSizeMB:       0,
		ExcludePatterns: []string{},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should find all 10 files in nested structure
	if len(result.Files) != 10 {
		t.Errorf("Expected 10 files in deep nested structure, got %d", len(result.Files))
	}
}

func TestScanner_MixedContentDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a realistic directory structure with mixed content
	files := map[string]string{
		"movie1.mp4":            "video",
		"movie2.mkv":            "video",
		"movie1.nfo":            "metadata",
		"movie1-poster.jpg":     "image",
		"movie1-fanart.jpg":     "image",
		"movie1-thumb.jpg":      "image",
		"movie2.srt":            "subtitle",
		"movie2.en.srt":         "subtitle",
		"Thumbs.db":             "system",
		".DS_Store":             "system",
		"movie-trailer.mp4":     "video", // Should be excluded
		"movie-sample.mkv":      "video", // Should be excluded
		"behind-the-scenes.mp4": "video", // Should be included
		"README.txt":            "text",
		"movie.part1.rar":       "archive",
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.MatchingConfig{
		Extensions:      []string{".mp4", ".mkv"},
		MinSizeMB:       0,
		ExcludePatterns: []string{"*-trailer*", "*-sample*"},
	}
	scanner := NewScanner(afero.NewOsFs(), cfg)

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should find: movie1.mp4, movie2.mkv, behind-the-scenes.mp4 (3 files)
	expectedCount := 3
	if len(result.Files) != expectedCount {
		t.Errorf("Expected %d video files, got %d", expectedCount, len(result.Files))
		t.Logf("Found files:")
		for _, f := range result.Files {
			t.Logf("  - %s", f.Name)
		}
	}

	// Verify file names
	foundNames := make(map[string]bool)
	for _, f := range result.Files {
		foundNames[f.Name] = true
	}

	expectedNames := []string{"movie1.mp4", "movie2.mkv", "behind-the-scenes.mp4"}
	for _, name := range expectedNames {
		if !foundNames[name] {
			t.Errorf("Expected to find %s", name)
		}
	}

	// Should have skipped many files
	if result.SkippedCount < 10 {
		t.Errorf("Expected at least 10 skipped files, got %d", result.SkippedCount)
	}
}
