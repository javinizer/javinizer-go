//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package fsutil

import "os"

// The supported POSIX Stat_t identity is unavailable on this target (Windows
// included: the take-aside binding there degrades to the shape/metadata legs
// exactly like the downloader/history quarantine constructions). The Lstat
// no-follow binding and regularity checks remain in force.
func boundObjectIdentity(os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}
