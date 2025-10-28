package scanner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
)

// Scanner finds video files based on configuration
type Scanner struct {
	config *config.MatchingConfig
}

// NewScanner creates a new file scanner
func NewScanner(cfg *config.MatchingConfig) *Scanner {
	return &Scanner{
		config: cfg,
	}
}

// FileInfo represents a discovered video file
type FileInfo struct {
	Path      string // Full absolute path
	Name      string // Filename without path
	Extension string // File extension (e.g., ".mp4")
	Size      int64  // File size in bytes
	Dir       string // Directory containing the file
}

// ScanResult contains the results of a directory scan
type ScanResult struct {
	Files   []FileInfo // Matched video files
	Skipped []string   // Files that were skipped (reason in comment)
	Errors  []error    // Errors encountered during scan
}

// Scan recursively scans a directory for video files
func (s *Scanner) Scan(rootPath string) (*ScanResult, error) {
	result := &ScanResult{
		Files:   make([]FileInfo, 0),
		Skipped: make([]string, 0),
		Errors:  make([]error, 0),
	}

	// Ensure path is absolute
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}

	// Verify path exists
	if _, err := os.Stat(absPath); err != nil {
		return nil, err
	}

	// Walk the directory tree
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, err)
			return nil // Continue scanning
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Check if file matches criteria
		if s.shouldIncludeFile(path, d) {
			info, err := d.Info()
			if err != nil {
				result.Errors = append(result.Errors, err)
				return nil
			}

			fileInfo := FileInfo{
				Path:      path,
				Name:      d.Name(),
				Extension: filepath.Ext(path),
				Size:      info.Size(),
				Dir:       filepath.Dir(path),
			}

			result.Files = append(result.Files, fileInfo)
		} else {
			result.Skipped = append(result.Skipped, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// ScanSingle scans a single file or directory (non-recursive)
func (s *Scanner) ScanSingle(path string) (*ScanResult, error) {
	result := &ScanResult{
		Files:   make([]FileInfo, 0),
		Skipped: make([]string, 0),
		Errors:  make([]error, 0),
	}

	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Check if it's a file or directory
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		// Scan directory (non-recursive)
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			fullPath := filepath.Join(absPath, entry.Name())
			if s.shouldIncludeFile(fullPath, entry) {
				entryInfo, err := entry.Info()
				if err != nil {
					result.Errors = append(result.Errors, err)
					continue
				}

				fileInfo := FileInfo{
					Path:      fullPath,
					Name:      entry.Name(),
					Extension: filepath.Ext(fullPath),
					Size:      entryInfo.Size(),
					Dir:       absPath,
				}

				result.Files = append(result.Files, fileInfo)
			} else {
				result.Skipped = append(result.Skipped, fullPath)
			}
		}
	} else {
		// Single file
		if s.shouldIncludeFile(absPath, nil) {
			fileInfo := FileInfo{
				Path:      absPath,
				Name:      info.Name(),
				Extension: filepath.Ext(absPath),
				Size:      info.Size(),
				Dir:       filepath.Dir(absPath),
			}

			result.Files = append(result.Files, fileInfo)
		} else {
			result.Skipped = append(result.Skipped, absPath)
		}
	}

	return result, nil
}

// shouldIncludeFile checks if a file should be included based on configuration
func (s *Scanner) shouldIncludeFile(path string, entry os.DirEntry) bool {
	// Check extension
	ext := strings.ToLower(filepath.Ext(path))
	hasValidExt := false
	for _, validExt := range s.config.Extensions {
		if ext == strings.ToLower(validExt) {
			hasValidExt = true
			break
		}
	}
	if !hasValidExt {
		return false
	}

	// Check exclude patterns (glob patterns)
	basename := filepath.Base(path)
	for _, pattern := range s.config.ExcludePatterns {
		matched, err := filepath.Match(pattern, basename)
		if err == nil && matched {
			return false
		}
	}

	// Check minimum file size
	if s.config.MinSizeMB > 0 {
		var size int64
		if entry != nil {
			info, err := entry.Info()
			if err == nil {
				size = info.Size()
			}
		} else {
			info, err := os.Stat(path)
			if err == nil {
				size = info.Size()
			}
		}

		minBytes := int64(s.config.MinSizeMB) * 1024 * 1024
		if size < minBytes {
			return false
		}
	}

	return true
}

// Filter filters a list of files based on configuration
func (s *Scanner) Filter(files []string) []FileInfo {
	result := make([]FileInfo, 0)

	for _, path := range files {
		if !s.shouldIncludeFile(path, nil) {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if info.IsDir() {
			continue
		}

		fileInfo := FileInfo{
			Path:      path,
			Name:      info.Name(),
			Extension: filepath.Ext(path),
			Size:      info.Size(),
			Dir:       filepath.Dir(path),
		}

		result = append(result, fileInfo)
	}

	return result
}
