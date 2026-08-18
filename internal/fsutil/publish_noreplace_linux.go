//go:build linux

package fsutil

import (
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameNoReplaceKernel is the syscall behind publishNoReplaceOSFS, exposed
// as a test seam (same discipline as probeRootStat / replacementReadProcFile):
// host kernels and CI filesystems cannot be coerced into producing ENOSYS /
// EINVAL / EOPNOTSUPP or an arbitrary renameat2 failure on demand, so tests
// replay those kernel responses here to cover the degrade and error legs.
var renameNoReplaceKernel = func(src, dst string) error {
	return unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
}

// publishNoReplaceOSFS is the Linux kernel primitive: renameat2 with
// RENAME_NOREPLACE fails EEXIST atomically when the destination is occupied,
// closing the classify→publish window inside the kernel. A kernel or
// filesystem that cannot express the flag (ENOSYS / EINVAL / EOPNOTSUPP)
// degrades to the hard-link publish.
func publishNoReplaceOSFS(src, dst string) error {
	err := renameNoReplaceKernel(src, dst)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EEXIST):
		return publishCollision(dst)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EOPNOTSUPP):
		return publishNoReplaceFallback(src, dst)
	default:
		return fmt.Errorf("no-replace renameat2 %s -> %s: %w", src, dst, err)
	}
}
