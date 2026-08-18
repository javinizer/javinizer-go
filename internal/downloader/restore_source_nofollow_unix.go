//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package downloader

import "syscall"

// restoreSourceNoFollow is passed through by afero.OsFs to os.OpenFile so a
// final symlink component cannot be opened on supported POSIX filesystems.
const restoreSourceNoFollow = syscall.O_NOFOLLOW
