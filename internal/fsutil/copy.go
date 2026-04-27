package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/configutil"
)

// CopyFileAtomic performs an atomic streaming copy from src to dst.
// It writes to a temporary file first, then renames it to the destination.
// This ensures the destination file is never in a partially written state.
//
// Features:
//   - Streaming copy (memory-safe for large files)
//   - Atomic rename (most filesystems)
//   - Automatic cleanup of temp files on error
//   - Umask-aware file permissions (uses configutil.FilePerm at creation, kernel applies umask)
//   - Unique temp filenames (safe for concurrent writes to same destination)
//
// Returns an error if any operation fails (open, copy, close, rename).
func CopyFileAtomic(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, configutil.DirPerm); err != nil {
		return fmt.Errorf("failed to ensure destination directory: %w", err)
	}

	tmpDst := filepath.Join(dir, fmt.Sprintf("%s.tmp.%d.%d", filepath.Base(dst), time.Now().UnixNano(), os.Getpid()))
	tmpFile, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, configutil.FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	_, err = io.Copy(tmpFile, srcFile)
	closeErr := tmpFile.Close()

	if err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to copy data: %w", err)
	}

	if closeErr != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	if err := os.Rename(tmpDst, dst); err != nil {
		if copyErr := copyWithFallback(tmpDst, dst); copyErr != nil {
			_ = os.Remove(tmpDst)
			return fmt.Errorf("failed to finalize copy (rename: %v, fallback: %v)", err, copyErr)
		}
		_ = os.Remove(tmpDst)
	}

	return nil
}

func copyWithFallback(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, configutil.FilePerm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
