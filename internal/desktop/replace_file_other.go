//go:build !windows

package desktop

import "os"

// replaceFile swaps src onto dst atomically: on POSIX os.Rename replaces an
// existing destination, so the previous export stays intact until the new
// one is fully written and the rename is a single atomic step.
//
//nolint:unused // reached only via saveFileDiskFS, which is //go:build desktop
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
