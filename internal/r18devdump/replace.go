//go:build !windows

package r18devdump

import "os"

// ReplaceFile atomically swaps src onto dst. POSIX rename replaces an existing
// destination in place; the previous dump is preserved whenever the swap
// cannot complete.
func ReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
