//go:build windows

package artifactstore

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// snapshotRoot captures eager handle-based identity. Windows path Lstat metadata
// can defer file-ID lookup until SameFile, after the path has been substituted.
// Share-delete permits legitimate rename while snapshots remain comparable;
// the handle is closed before the caller opens its directory capability.
func snapshotRoot(path string) (os.FileInfo, error) {
	name, err := windows.UTF16PtrFromString(handleRootPath(path))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("invalid root identity handle")
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return nil, err
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("root must not be a reparse point")
	}
	return info, nil
}

// Open has already required an absolute clean path. Preserve Go's short-path
// semantics and device/extended namespaces, while avoiding the legacy MAX_PATH
// limit for ordinary long drive and UNC roots without changing system settings.
func handleRootPath(path string) string {
	if len(path) < 248 || strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\??\`) || strings.HasPrefix(path, `\\.\`) {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}
