//go:build windows

package fsutil

import (
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// ReplaceFile atomically replaces dst with src: MoveFileEx +
// MOVEFILE_REPLACE_EXISTING for OsFs; virtual filesystems fall back to the
// shared rename leg (replacefile.go), which keeps the filesystem's own error
// unwrap-reachable next to ErrReplaceUnsupported.
func ReplaceFile(fs afero.Fs, src, dst string) error {
	if _, ok := fs.(*afero.OsFs); ok {
		srcPtr, err := windows.UTF16PtrFromString(src)
		if err != nil {
			return err
		}
		dstPtr, err := windows.UTF16PtrFromString(dst)
		if err != nil {
			return err
		}
		return windows.MoveFileEx(srcPtr, dstPtr, windows.MOVEFILE_REPLACE_EXISTING)
	}
	return replaceFileVirtualFallback(fs, src, dst)
}
