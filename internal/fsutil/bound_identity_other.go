//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package fsutil

import (
	"os"

	"github.com/spf13/afero"
)

// The supported POSIX Stat_t identity is unavailable on this target (Windows
// included: the take-aside binding there degrades to the shape/metadata legs
// exactly like the downloader/history quarantine constructions). The Lstat
// no-follow binding and regularity checks remain in force.
func boundObjectIdentity(os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}

// BoundObjectIdentity degrades to not-OK alongside the take-aside binding on
// this target (Windows excluded — bound_identity_windows.go carries its
// handle route): admission proofs keep the size+modtime legs exactly like the
// POSIX in-memory posture.
func BoundObjectIdentity(afero.Fs, string, os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}
