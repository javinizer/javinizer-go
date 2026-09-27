//go:build windows

package r18devdump

import (
	"os"

	"golang.org/x/sys/windows"
)

// ReplaceFile swaps src onto dst via MoveFileEx with MOVEFILE_REPLACE_EXISTING:
// os.Rename on Windows refuses to overwrite an existing destination, and the
// remove-then-rename fallback would destroy the installed dump when the
// replacement cannot proceed. MoveFileEx replaces the destination only when
// the move can complete.
func ReplaceFile(src, dst string) error {
	s, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	d, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(s, d, windows.MOVEFILE_REPLACE_EXISTING); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
