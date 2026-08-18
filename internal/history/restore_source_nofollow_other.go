//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package history

// The regularity Lstat gate remains mandatory on filesystems without a
// portable O_NOFOLLOW flag. MemMapFs also has no symlink model, so its
// LstatIfPossible fallback is the intended test-time protection. Windows is
// excluded here: it gets the reparse-point handle open in
// restore_source_nofollow_windows.go.
const restoreSourceNoFollow = 0
