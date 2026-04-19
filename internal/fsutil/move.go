package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/afero"
)

func MoveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	if !isCrossDeviceError(err) {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return crossDeviceMove(src, dst)
}

func MoveFileFs(fs afero.Fs, src, dst string) error {
	if err := fs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	err := fs.Rename(src, dst)
	if err == nil {
		return nil
	}

	if !isCrossDeviceError(err) {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return crossDeviceMoveFs(fs, src, dst)
}

func crossDeviceMove(src, dst string) error {
	if err := copyFileData(src, dst); err != nil {
		return fmt.Errorf("failed to copy file across devices: %w", err)
	}

	if err := os.Remove(src); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("failed to remove source after cross-device copy: %w", err)
	}

	return nil
}

func crossDeviceMoveFs(fs afero.Fs, src, dst string) error {
	if err := copyFileDataFs(fs, src, dst); err != nil {
		return fmt.Errorf("failed to copy file across devices: %w", err)
	}

	if err := fs.Remove(src); err != nil {
		_ = fs.Remove(dst)
		return fmt.Errorf("failed to remove source after cross-device copy: %w", err)
	}

	return nil
}

func isCrossDeviceError(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EINVAL)
}

func copyFileData(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	return nil
}

func copyFileDataFs(fs afero.Fs, src, dst string) error {
	srcFile, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	dstFile, err := fs.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	return nil
}
