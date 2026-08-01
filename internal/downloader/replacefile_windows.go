//go:build windows

package downloader

import (
	"errors"
	"fmt"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

var errReplaceUnsupported = errors.New("replace existing destination unsupported")

func replaceFile(fs afero.Fs, src, dst string) error {
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
	if err := fs.Rename(src, dst); err != nil {
		return fmt.Errorf("%w: %v", errReplaceUnsupported, err)
	}
	return nil
}
