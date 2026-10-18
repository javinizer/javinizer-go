//go:build linux

package fsutil

import (
	"syscall"
	"testing"
)

// On Linux the take hop normally publishes through renameat2(RENAME_NOREPLACE),
// a kernel-atomic rename with no link-then-unlink residue construction — the
// wedged publishNoReplaceRemove seam never fires on it, and the strip scenario
// silently degrades into the happy path (CI run 36525447204, linux leg).
// Wedging the kernel seam into its documented ENOSYS degrade routes the take
// through the hard-link fallback darwin/BSD ride unconditionally.
func forceVerifiedTakeFallbackLeg(t *testing.T) {
	t.Helper()
	original := renameNoReplaceKernel
	renameNoReplaceKernel = func(_, _ string) error { return syscall.ENOSYS }
	t.Cleanup(func() { renameNoReplaceKernel = original })
}
