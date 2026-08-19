//go:build darwin || dragonfly || openbsd

package fsutil

import (
	"os"
	"syscall"
)

// These POSIX targets expose Dev as a narrower integer type. Keep the
// platform-required widening separate from the uint64 Stat_t targets so the
// common implementation does not carry an unnecessary conversion (mirrors
// the downloader/history identity helpers' split).
func boundObjectIdentity(info os.FileInfo) (device, inode uint64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, false
	}
	return uint64(stat.Dev), stat.Ino, true
}
