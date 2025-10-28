package scanner

import (
	"os"
	"path/filepath"
	"testing"

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
			file.Close()
			t.Fatalf("Failed to set file size: %v", err)
		}
		file.Close()
	}

	t.Run("Scan with default config", func(t *testing.T) {
		cfg := &config.MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
		}
		scanner := NewScanner(cfg)

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
		scanner := NewScanner(cfg)

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
		scanner := NewScanner(cfg)

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
	scanner := NewScanner(cfg)

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
	scanner := NewScanner(cfg)

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
			scanner := NewScanner(cfg)

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
			os.Remove(path)
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
	scanner := NewScanner(cfg)

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
	scanner := NewScanner(cfg)

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
	scanner := NewScanner(cfg)

	_, err := scanner.Scan("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Expected error for non-existent path, got nil")
	}
}
