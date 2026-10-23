//go:build darwin

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var renameNoReplaceKernel = func(src, dst string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCL)
}

func publishNoReplaceOSFS(src, dst string) error {
	err := renameNoReplaceKernel(src, dst)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EEXIST):
		return publishCollision(dst)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENOTSUP):
		info, statErr := os.Lstat(src)
		if statErr != nil {
			return fmt.Errorf("no-replace renameatx_np fallback source %s: %w", src, statErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: renameatx_np(RENAME_EXCL) unavailable for nonregular source %s", ErrPublishNoReplaceUnsupported, src)
		}
		if fallbackErr := publishNoReplaceFallback(src, dst); fallbackErr != nil {
			return fmt.Errorf("renameatx_np(RENAME_EXCL): %v; %w", err, fallbackErr)
		}
		return nil
	default:
		return fmt.Errorf("no-replace renameatx_np %s -> %s: %w", src, dst, err)
	}
}
