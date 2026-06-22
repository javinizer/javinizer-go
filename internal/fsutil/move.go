package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

func CopyFileFs(fs afero.Fs, src, dst string) error {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}

	return copyFileDataFs(fs, src, dst)
}

func MoveFileFs(fs afero.Fs, src, dst string) error {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
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

func crossDeviceMoveFs(fs afero.Fs, src, dst string) error {
	if err := copyFileDataFs(fs, src, dst); err != nil {
		_ = fs.Remove(dst) // clean up partial destination file
		return fmt.Errorf("failed to copy file across devices: %w", err)
	}

	if err := fs.Remove(src); err != nil {
		_ = fs.Remove(dst)
		return fmt.Errorf("failed to remove source after cross-device copy: %w", err)
	}

	return nil
}

func copyFileDataFs(fs afero.Fs, src, dst string) error {
	srcFile, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := fs.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, config.FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	return nil
}
