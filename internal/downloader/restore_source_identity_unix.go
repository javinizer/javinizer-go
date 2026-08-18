//go:build freebsd || linux || netbsd || solaris

package downloader

import (
	"os"
	"syscall"
)

// restoreSourceIdentity uses the identity exposed by afero.OsFs' os.FileInfo.
// In-memory afero files return Sys()==nil and deliberately skip this deeper
// check; their pre-open regularity gate still runs.
func restoreSourceIdentity(info os.FileInfo) (device, inode uint64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, false
	}
	return stat.Dev, stat.Ino, true
}
