package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

type artifactStageDirectoryProof struct {
	Native          bool   `json:"native"`
	Device          uint64 `json:"device,omitempty"`
	Inode           uint64 `json:"inode,omitempty"`
	Size            int64  `json:"size,omitempty"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano,omitempty"`
}

func readArtifactStageAuthenticatedManifest(fs afero.Fs, root string) (artifactStageManifest, bool) {
	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	var manifest artifactStageManifest
	if err != nil || json.Unmarshal(body, &manifest) != nil {
		return manifest, false
	}
	valid := manifest.Version == artifactStageManifestVer && manifest.PID > 0 && manifest.Token == artifactStageManifestToken(root) && manifest.Directory != nil && artifactStageSealed(&manifest)
	return manifest, valid
}

func artifactStageDirectoryIdentity(fs afero.Fs, root string) artifactStageDirIdentity {
	info, err := lstatArtifactStageDir(fs, root)
	if err != nil || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return artifactStageDirIdentity{}
	}
	return captureArtifactStageDirIdentity(fs, root, info)
}

func artifactStageDirectoryProofFromIdentity(identity artifactStageDirIdentity) *artifactStageDirectoryProof {
	if !identity.known {
		return nil
	}
	proof := &artifactStageDirectoryProof{Native: identity.hasDevIno, Device: identity.dev, Inode: identity.ino}
	if !proof.Native {
		proof.Size = identity.size
		proof.ModTimeUnixNano = identity.modTime.UnixNano()
	}
	return proof
}

func (p *artifactStageDirectoryProof) matchesIdentity(identity artifactStageDirIdentity) bool {
	if p == nil || !identity.known || p.Native != identity.hasDevIno {
		return false
	}
	if p.Native {
		return p.Device == identity.dev && p.Inode == identity.ino
	}
	return p.Size == identity.size && p.ModTimeUnixNano == identity.modTime.UnixNano()
}

func (p *artifactStageDirectoryProof) matchesPath(fs afero.Fs, root string) bool {
	return p.matchesIdentity(artifactStageDirectoryIdentity(fs, root))
}

func (p *artifactStageDirectoryProof) identity() artifactStageDirIdentity {
	if p == nil {
		return artifactStageDirIdentity{}
	}
	return artifactStageDirIdentity{known: true, hasDevIno: p.Native, dev: p.Device, ino: p.Inode, size: p.Size, modTime: time.Unix(0, p.ModTimeUnixNano)}
}
