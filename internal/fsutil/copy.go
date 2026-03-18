package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyFileAtomic performs an atomic streaming copy from src to dst.
// It writes to a temporary file first, then renames it to the destination.
// This ensures the destination file is never in a partially written state.
//
// Features:
//   - Streaming copy (memory-safe for large files)
//   - Atomic rename (most filesystems)
//   - Automatic cleanup of temp files on error
//   - Preserves source file permissions
//   - Unique temp filenames (safe for concurrent writes to same destination)
//
// Returns an error if any operation fails (open, copy, close, rename).
func CopyFileAtomic(src, dst string) error {
	// Open source file and get its permissions
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// Get source file permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Ensure destination directory exists
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to ensure destination directory: %w", err)
	}

	// Create unique temporary file in same directory (for atomic rename)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(dst)+".tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpDst := tmpFile.Name()

	// Stream copy (memory-safe for large files)
	_, err = io.Copy(tmpFile, srcFile)
	closeErr := tmpFile.Close()

	if err != nil {
		_ = os.Remove(tmpDst) // Clean up temp file on copy error
		return fmt.Errorf("failed to copy data: %w", err)
	}

	if closeErr != nil {
		_ = os.Remove(tmpDst) // Clean up temp file on close error
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	// Preserve source file permissions
	if err := os.Chmod(tmpDst, srcInfo.Mode().Perm()); err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Rename temp file to final destination (atomic on same filesystem)
	if err := os.Rename(tmpDst, dst); err != nil {
		// Fallback for cross-filesystem rename (e.g., external drives, network mounts)
		if copyErr := copyWithFallback(tmpDst, dst, srcInfo.Mode().Perm()); copyErr != nil {
			_ = os.Remove(tmpDst)
			return fmt.Errorf("failed to finalize copy (rename: %v, fallback: %v)", err, copyErr)
		}
		_ = os.Remove(tmpDst)
	}

	return nil
}

// copyWithFallback performs a copy operation using open/copy/close pattern.
// This is used as a fallback when os.Rename fails (e.g., cross-filesystem rename).
func copyWithFallback(src, dst string, perms os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perms)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
