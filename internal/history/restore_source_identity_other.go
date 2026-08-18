//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package history

import "os"

// The supported POSIX Stat_t identity is unavailable on this target. The
// Lstat plus regularity check and any filesystem-native no-follow semantics
// remain in force.
func restoreSourceIdentity(os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}
