//go:build freebsd || linux || netbsd || solaris

package workflow

import (
	"os"
	"syscall"
)

// artifactSourceDevIno extracts the kernel identity an afero.OsFs FileInfo
// exposes. In-memory afero files return Sys()==nil and deliberately report
// not-OK; their identity keeps the size+modtime legs (same posture as the
// downloader/history restoreSourceIdentity helpers this mirrors).
func artifactSourceDevIno(info os.FileInfo) (device, inode uint64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, false
	}
	return stat.Dev, stat.Ino, true
}
