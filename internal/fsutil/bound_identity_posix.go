//go:build freebsd || linux || netbsd || solaris

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
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

// BoundObjectIdentity captures the kernel identity of the object info
// describes, for caller-side admission proofs (the verified composites'
// re-proofs, the workflow's deferred-source binding — see
// bound_identity_windows.go for the contract and the Windows handle route).
// On POSIX Stat_t targets the FileInfo already carries the dev/inode pair
// EAGERLY from the lookup, so the filesystem and path legs are unused.
func BoundObjectIdentity(_ afero.Fs, _ string, info os.FileInfo) (device, inode uint64, ok bool) {
	return boundObjectIdentity(info)
}
