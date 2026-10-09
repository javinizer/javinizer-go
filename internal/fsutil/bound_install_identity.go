package fsutil

import (
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// BoundInstallIdentity carries the published FileInfo plus the strong kernel
// key captured while the installed object was still pinned (codex P1,
// PRRT_kwDORn9KaM6qJY2k).
type BoundInstallIdentity struct {
	info       os.FileInfo
	device     uint64
	inode      uint64
	ok         bool
	linkTarget string
}

func newBoundInstallIdentity(info os.FileInfo, device, inode uint64, ok bool) *BoundInstallIdentity {
	if info == nil {
		return nil
	}
	return &BoundInstallIdentity{info: info, device: device, inode: inode, ok: ok}
}

// NewWeakBoundInstallIdentity preserves legacy FileInfo-only install records.
func NewWeakBoundInstallIdentity(info os.FileInfo) *BoundInstallIdentity {
	return newBoundInstallIdentity(info, 0, 0, false)
}

// NewBoundInstallLinkIdentity is the observed identity of a soft-link install
// (codex P1, PRRT_kwDORn9KaM6qKDM5): the link object carries no strong kernel
// key a FileInfo can serve, so the recorded readlink payload IS the entire
// ownership certificate, matching the durable delete-intent pin's rule
// (models.DeleteEntry LinkTarget).
func NewBoundInstallLinkIdentity(info os.FileInfo, linkTarget string) *BoundInstallIdentity {
	id := newBoundInstallIdentity(info, 0, 0, false)
	if target := linkTarget; id != nil && target != "" {
		id.linkTarget = target
	}
	return id
}

// LinkTarget answers the readlink payload recorded for a soft-link install,
// or "" for a regular-file install. A non-empty answer routes rollback to the
// link-object unlink, never the regular-file one (which has no link model).
func (i *BoundInstallIdentity) LinkTarget() string {
	if i == nil {
		return ""
	}
	return i.linkTarget
}

// FileInfo returns the plain os.FileInfo for callers that need os.SameFile.
func (i *BoundInstallIdentity) FileInfo() os.FileInfo {
	if i == nil {
		return nil
	}
	return i.info
}

func (i *BoundInstallIdentity) strongIdentity() (device, inode uint64, ok bool) {
	if i == nil || !i.ok {
		return 0, 0, false
	}
	return i.device, i.inode, true
}

// VerifiedSourceProofFromBoundInstallIdentity turns a publish-time install
// identity into a verified-move source proof (codex P1,
// PRRT_kwDORn9KaM6qLkJ9). Strong keys are re-proven strong-or-refuse; only a
// genuinely weak install record falls back to the FileInfo size+modtime legs.
func VerifiedSourceProofFromBoundInstallIdentity(fs afero.Fs, installed *BoundInstallIdentity) VerifiedSourceProof {
	return func(path string, info os.FileInfo) error {
		if installed == nil || installed.FileInfo() == nil {
			return fmt.Errorf("bound install proof of %s requires the identity its publish produced", path)
		}
		if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("bound install proof of %s found no regular-file object", path)
		}
		same, err := sameBoundInstallObject(fs, path, info, installed)
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("%s no longer names the object this operation installed", path)
		}
		return nil
	}
}
