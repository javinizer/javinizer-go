package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

var (
	ErrMaxFilesExceeded = errors.New("maximum file limit exceeded")
	ErrScanTimeout      = errors.New("scan operation timed out")
)

const (
	// MaxSkippedFiles is the maximum number of skipped file paths to store
	// to prevent unbounded memory growth when scanning directories with millions of non-video files
	MaxSkippedFiles = 1000
)

// Scanner finds video files based on configuration
type Scanner struct {
	fs     afero.Fs
	config *config.MatchingConfig
}

// NewScanner creates a new file scanner
func NewScanner(fs afero.Fs, cfg *config.MatchingConfig) *Scanner {
	return &Scanner{
		fs:     fs,
		config: cfg,
	}
}

// lstatInfo returns file info without following symlinks when the filesystem supports it.
func (s *Scanner) lstatInfo(path string) (os.FileInfo, error) {
	if lstater, ok := s.fs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(path)
		return info, err
	}
	return s.fs.Stat(path)
}

// trackSkipped records a skipped path while capping stored paths to MaxSkippedFiles.
func trackSkipped(result *ScanResult, path string) {
	result.SkippedCount++
	if len(result.Skipped) < MaxSkippedFiles {
		result.Skipped = append(result.Skipped, path)
	}
}

// FileInfo represents a discovered video file
type FileInfo struct {
	Path      string    // Full absolute path
	Name      string    // Filename without path
	Extension string    // File extension (e.g., ".mp4")
	Size      int64     // File size in bytes
	ModTime   time.Time // Last modified time
	Dir       string    // Directory containing the file
}

// ScanResult contains the results of a directory scan
type ScanResult struct {
	Files        []FileInfo // Matched video files
	Skipped      []string   // Sample of skipped files (capped at MaxSkippedFiles)
	SkippedCount int        // Total count of skipped files
	Errors       []error    // Errors encountered during scan
	LimitReached bool       // Whether max file limit was reached
	TimedOut     bool       // Whether scan timed out
	TotalScanned int        // Total number of files scanned before limit/timeout
}

// Scan recursively scans a directory for video files (no limits)
func (s *Scanner) Scan(rootPath string) (*ScanResult, error) {
	// Call ScanWithLimits with no limits (context.Background(), maxFiles = 0)
	return s.ScanWithLimits(context.Background(), rootPath, 0)
}

// ScanWithLimits recursively scans a directory for video files with timeout and file count limits
// maxFiles = 0 means no limit
func (s *Scanner) ScanWithLimits(ctx context.Context, rootPath string, maxFiles int) (*ScanResult, error) {
	return s.ScanWithFilter(ctx, rootPath, maxFiles, "")
}

// ScanWithFilter recursively scans a directory for video files with timeout, file count limits, and optional name filter
// maxFiles = 0 means no limit
// filter = "" means no filter; otherwise, only directories/files containing the filter string (case-insensitive) are processed
func (s *Scanner) ScanWithFilter(ctx context.Context, rootPath string, maxFiles int, filter string) (*ScanResult, error) {
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
	if _, err := s.fs.Stat(absPath); err != nil {
		return nil, err
	}

	// Skip symlink roots entirely to avoid following links outside the scan boundary.
	rootInfo, err := s.lstatInfo(absPath)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		trackSkipped(result, absPath)
		return result, nil
	}

	// Normalize filter for case-insensitive matching
	filterLower := strings.ToLower(filter)

	// Walk the directory tree
	fileCount := 0
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		// Check context for timeout/cancellation (check every 100 files for performance)
		fileCount++
		if fileCount%100 == 0 {
			select {
			case <-ctx.Done():
				result.TimedOut = true
				return filepath.SkipAll // Stop walking
			default:
			}
		}

		if err != nil {
			result.Errors = append(result.Errors, err)
			return nil // Continue scanning
		}

		// Use lstat to detect symlinks without following them.
		info, statErr := s.lstatInfo(path)
		if statErr != nil {
			result.Errors = append(result.Errors, statErr)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			trackSkipped(result, path)
			return nil
		}

		// For directories: skip if filter is set and directory name doesn't match
		// Always process the root directory regardless of filter
		if d.IsDir() {
			if filterLower != "" && path != absPath {
				dirName := strings.ToLower(d.Name())
				if !strings.Contains(dirName, filterLower) {
					// Skip this directory entirely - don't recurse into it
					return filepath.SkipDir
				}
			}
			return nil
		}

		// For files: check if filter matches the file name
		if filterLower != "" {
			fileName := strings.ToLower(d.Name())
			if !strings.Contains(fileName, filterLower) {
				// File doesn't match filter, skip it
				result.SkippedCount++
				return nil
			}
		}

		result.TotalScanned++

		// Check if file matches criteria
		if s.shouldIncludeFile(path, d) {
			fileInfo := FileInfo{
				Path:      path,
				Name:      d.Name(),
				Extension: filepath.Ext(path),
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				Dir:       filepath.Dir(path),
			}

			result.Files = append(result.Files, fileInfo)

			// Check if we've reached the file limit
			if maxFiles > 0 && len(result.Files) >= maxFiles {
				result.LimitReached = true
				return filepath.SkipAll // Stop walking
			}
		} else {
			trackSkipped(result, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// ScanSingle scans a single file or directory (non-recursive)
//
// WARNING: This method has a TOCTOU vulnerability. Between path validation and
// the internal Stat/ReadDir calls, the path could be replaced with a symlink.
// For TOCTOU-safe scanning, use ScanSingleFromHandle with a pre-validated
// directory handle from ValidateAndOpenPath.
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

	// Detect and skip symlinks without following their targets.
	lstatInfo, err := s.lstatInfo(absPath)
	if err != nil {
		return nil, err
	}
	if lstatInfo.Mode()&os.ModeSymlink != 0 {
		trackSkipped(result, absPath)
		return result, nil
	}

	// Check if it's a file or directory
	info, err := s.fs.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		// Scan directory (non-recursive)
		entries, err := afero.ReadDir(s.fs, absPath)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			fullPath := filepath.Join(absPath, entry.Name())

			entryInfo, err := s.lstatInfo(fullPath)
			if err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}

			if entryInfo.IsDir() {
				continue
			}

			if entryInfo.Mode()&os.ModeSymlink != 0 {
				trackSkipped(result, fullPath)
				continue
			}

			if s.shouldIncludeFile(fullPath, nil) {
				fileInfo := FileInfo{
					Path:      fullPath,
					Name:      entryInfo.Name(),
					Extension: filepath.Ext(fullPath),
					Size:      entryInfo.Size(),
					ModTime:   entryInfo.ModTime(),
					Dir:       absPath,
				}

				result.Files = append(result.Files, fileInfo)
			} else {
				trackSkipped(result, fullPath)
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
				ModTime:   info.ModTime(),
				Dir:       filepath.Dir(absPath),
			}

			result.Files = append(result.Files, fileInfo)
		} else {
			trackSkipped(result, absPath)
		}
	}

	return result, nil
}

// ScanSingleFromHandle scans an open directory handle (non-recursive, TOCTOU-safe).
//
// This is the TOCTOU-safe version of ScanSingle. It reads directory entries
// directly from the provided file handle, preventing symlink swap attacks
// between validation and scanning.
//
// The canonicalPath is used for recording paths in the returned FileInfo
// structures. It should be the absolute, symlink-resolved path that was
// validated before opening the handle.
//
// The caller retains ownership of the file handle and must close it after
// this call returns.
func (s *Scanner) ScanSingleFromHandle(dir *os.File, canonicalPath string) (*ScanResult, error) {
	result := &ScanResult{
		Files:   make([]FileInfo, 0),
		Skipped: make([]string, 0),
		Errors:  make([]error, 0),
	}

	// Guard against nil directory handle
	if dir == nil {
		return nil, errors.New("ScanSingleFromHandle: nil directory handle")
	}

	// Read directory entries directly from the open handle (TOCTOU-safe)
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(canonicalPath, entry.Name())

		// Get file info without following symlinks
		info, err := entry.Info()
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}

		// Skip directories (non-recursive scan)
		if info.IsDir() {
			continue
		}

		// Skip symlinks (security policy)
		if info.Mode()&os.ModeSymlink != 0 {
			trackSkipped(result, fullPath)
			continue
		}

		if s.shouldIncludeFile(fullPath, entry) {
			fileInfo := FileInfo{
				Path:      fullPath,
				Name:      info.Name(),
				Extension: filepath.Ext(fullPath),
				Size:      info.Size(),
				ModTime:   info.ModTime(),
				Dir:       canonicalPath,
			}
			result.Files = append(result.Files, fileInfo)
		} else {
			trackSkipped(result, fullPath)
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
			info, err := s.fs.Stat(path)
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
		info, err := s.lstatInfo(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if !s.shouldIncludeFile(path, nil) {
			continue
		}

		fileInfo := FileInfo{
			Path:      path,
			Name:      info.Name(),
			Extension: filepath.Ext(path),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Dir:       filepath.Dir(path),
		}

		result = append(result, fileInfo)
	}

	return result
}
