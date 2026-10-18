//go:build darwin

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameExclLinkKernel is the syscall behind vacateLinkObjectOsFs on Darwin,
// exposed as a test seam (same discipline as renameNoReplaceKernel): host
// filesystems cannot be coerced into ENOSYS/ENOTSUP on demand, so tests
// replay those kernel responses here to cover the refusal legs.
var renameExclLinkKernel = func(src, dst string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCL)
}

// vacateLinkObjectOsFs is the Darwin kernel leg of the symlink-shaped
// vacate: renameatx_np(RENAME_EXCL) renames the link OBJECT itself and fails
// EEXIST atomically on an occupied terminal. PublishNoReplace cannot serve
// this shape off-Linux: its fallback is link(2), which POSIX leaves free to
// dereference a symlink SOURCE — the vacate would alias the target inode
// instead of moving the link. A filesystem that cannot express RENAME_EXCL
// refuses TYPED (the pinned entry retains; no follow gamble).
func vacateLinkObjectOsFs(name, terminal string) error {
	err := renameExclLinkKernel(name, terminal)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("symlink vacate renameatx_np %s -> %s: %w", name, terminal, err)
	case errors.Is(err, syscall.EEXIST):
		return publishCollision(terminal)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		return fmt.Errorf("%w: filesystem cannot express renameatx_np(RENAME_EXCL) for the link-object vacate %s", ErrPublishNoReplaceUnsupported, name)
	default:
		return fmt.Errorf("symlink vacate renameatx_np %s -> %s: %w", name, terminal, err)
	}
}
