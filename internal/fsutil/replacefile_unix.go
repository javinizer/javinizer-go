//go:build !windows

package fsutil

import "github.com/spf13/afero"

// ReplaceFile atomically (same-filesystem) replaces dst with src via rename.
// POSIX rename replaces existing destinations in place.
func ReplaceFile(fs afero.Fs, src, dst string) error {
	return fs.Rename(src, dst)
}
