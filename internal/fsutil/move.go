package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

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

	// Write to a temp file in the same directory as dst so the final rename
	// is same-filesystem and atomic. On any failure the temp file is removed
	// and dst is never left in a partial or truncated state.
	tmp := filepath.Join(filepath.Dir(dst),
		fmt.Sprintf(".%s.tmp-%d-%d", filepath.Base(dst), time.Now().UnixNano(), os.Getpid()))

	dstFile, err := fs.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, config.FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		_ = fs.Remove(tmp)
		return fmt.Errorf("failed to copy data: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("failed to close destination: %w", err)
	}

	if err := fs.Rename(tmp, dst); err != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return nil
}
