//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package workflow

import "os"

// The supported POSIX Stat_t identity is unavailable on this target (Windows
// included): the identity degrades to the size+modtime legs exactly like the
// downloader/history constructions.
func artifactSourceDevIno(os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}
