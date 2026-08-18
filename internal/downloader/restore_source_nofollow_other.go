//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package downloader

// The regularity Lstat gate remains mandatory on filesystems without a
// portable O_NOFOLLOW flag. MemMapFs also has no symlink model, so its
// LstatIfPossible fallback is the intended test-time protection.
const restoreSourceNoFollow = 0
