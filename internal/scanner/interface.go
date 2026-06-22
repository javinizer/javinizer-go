package scanner

import (
	"context"
	"os"
)

// ScannerInterface defines the contract for filesystem video file discovery.
type ScannerInterface interface {
	// Scan recursively scans a directory for video files with no limits.
	Scan(rootPath string) (*ScanResult, error)

	// ScanWithFilter recursively scans a directory for video files with timeout, file count limits, and optional name filter.
	ScanWithFilter(ctx context.Context, rootPath string, maxFiles int, filter string) (*ScanResult, error)

	// ScanSingle scans a single file or directory without recursion.
	ScanSingle(path string) (*ScanResult, error)

	// ScanSingleFromHandle scans an open directory handle (non-recursive, TOCTOU-safe).
	ScanSingleFromHandle(dir *os.File, canonicalPath string) (*ScanResult, error)
}

var _ ScannerInterface = (*Scanner)(nil)
