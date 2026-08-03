//go:build windows

package desktop

import (
	"errors"
	"io/fs"
	"os"
)

// replaceFile swaps src onto dst. Windows os.Rename refuses to overwrite an
// existing destination, so fall back to remove-then-rename; that fallback is
// not atomic, but the window only opens for a user-confirmed overwrite.
//
//nolint:unused // reached only via saveFileDiskFS, which is //go:build desktop
func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if rmErr := os.Remove(dst); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return rmErr
	}
	return os.Rename(src, dst)
}
