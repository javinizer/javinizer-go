//go:build linux

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// vacateLinkObjectOsFs is the Linux kernel leg of the symlink-shaped vacate:
// renameat2(RENAME_NOREPLACE) renames the link OBJECT itself (never its
// target) and fails EEXIST atomically on an occupied terminal — the same
// primitive publishNoReplaceOSFS uses, shared through its syscall seam so
// tests replaying kernel responses cover this leg too. Unlike
// PublishNoReplace there is NO link(2) degrade: the hard-link fallback can
// dereference a symlink source (POSIX leaves the behavior
// implementation-defined), which would alias the TARGET instead of moving
// the link — a kernel that cannot express the flag refuses TYPED, and the
// pinned entry retains (retention-first) rather than gambling the follow.
func vacateLinkObjectOsFs(name, terminal string) error {
	err := renameNoReplaceKernel(name, terminal)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("symlink vacate renameat2 %s -> %s: %w", name, terminal, err)
	case errors.Is(err, syscall.EEXIST):
		return publishCollision(terminal)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EOPNOTSUPP):
		return fmt.Errorf("%w: kernel/filesystem cannot express renameat2(RENAME_NOREPLACE) for the link-object vacate %s", ErrPublishNoReplaceUnsupported, name)
	default:
		return fmt.Errorf("symlink vacate renameat2 %s -> %s: %w", name, terminal, err)
	}
}
