//go:build !linux && !darwin && !windows

package fsutil

import (
	"fmt"
)

// vacateLinkObjectOsFs has no atomic OBJECT-LEVEL no-replace rename on this
// target: the platform neither offers renameat2(RENAME_NOREPLACE) /
// renameatx_np(RENAME_EXCL), and PublishNoReplace's POSIX fallback (link(2))
// may dereference a symlink SOURCE — POSIX leaves symlink-source link(2)
// behavior implementation-defined — so even borrowing that leg could alias
// the link's TARGET instead of moving the link object. The vacate refuses
// TYPED and the pinned delete intent retains (retention-first) until the
// platform grows a provable object-level primitive; nothing is ever
// pathname-removed on this doubt.
func vacateLinkObjectOsFs(name, _ string) error {
	return fmt.Errorf("%w: no atomic object-level no-replace rename for the link-object vacate %s on this platform", ErrPublishNoReplaceUnsupported, name)
}
