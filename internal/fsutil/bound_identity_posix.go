//go:build freebsd || linux || netbsd || solaris

package fsutil

import (
	"os"
	"syscall"
)

// boundObjectIdentity extracts the kernel identity (dev/inode) an afero.OsFs
// FileInfo exposes. In-memory afero files return Sys()==nil and deliberately
// report not-OK; their take-aside binding keeps the shape/metadata legs
// instead (same posture as the downloader/history restoreSourceIdentity
// helpers this mirrors).
func boundObjectIdentity(info os.FileInfo) (device, inode uint64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, false
	}
	return stat.Dev, stat.Ino, true
}
