package r18devdump

import (
	"errors"
	"io/fs"
	"os"
)

// ReplaceFile swaps src onto dst. os.Rename cannot overwrite an existing
// destination on Windows ("The process cannot access the file..."), so this
// falls back to remove-then-rename. The fallback drops replace-atomicity, but
// this helper is only reached after the previous dump handle is closed and
// for user-confirmed dump replacements, so the brief window is acceptable.
func ReplaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if rmErr := os.Remove(dst); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return rmErr
	}
	return os.Rename(src, dst)
}
