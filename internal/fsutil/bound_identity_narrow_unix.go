//go:build darwin || dragonfly || openbsd

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
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

// BoundObjectIdentity is the narrow-Dev twin of the POSIX variant (see
// bound_identity_posix.go / bound_identity_windows.go): the dev/inode pair
// travels with the lookup, so the filesystem and path legs are unused.
func BoundObjectIdentity(_ afero.Fs, _ string, info os.FileInfo) (device, inode uint64, ok bool) {
	return boundObjectIdentity(info)
}
