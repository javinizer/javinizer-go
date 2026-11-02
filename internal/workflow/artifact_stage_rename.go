package workflow

import (
	"github.com/spf13/afero"
)

// artifactStageRestoreOriginalAfterIdentityDrift renames a quarantined staging
// root back to its original name after the quarantine rename itself changed
// the directory identity.
//
// A filesystem may derive a directory's inode number from its path. mergerfs
// does that with inodecalc=hybrid-hash, its default, whenever the tree is
// reached through a container bind mount. There the rename mints a new inode,
// every identity gate refuses by design, and the tree is retained with the
// proof already unlinked, so no later sweep can ever reclaim it: one orphan
// per apply.
//
// The restore is only offered to a tree that still carries the sealed manifest
// this process wrote, and only when the restored name
// reproduces exactly the directory identity that manifest recorded. A tree
// planted at the quarantine name has no such manifest, and a different tree
// cannot reproduce a sealed identity, so the anti-swap refusal is unchanged.
func artifactStageRestoreOriginalAfterIdentityDrift(fs afero.Fs, quarantined, original string) (artifactStageDirIdentity, bool) {
	if fs == nil || quarantined == "" || original == "" {
		return artifactStageDirIdentity{}, false
	}
	manifest, authenticated := readArtifactStageAuthenticatedManifest(fs, quarantined)
	if !authenticated || manifest.Directory == nil {
		return artifactStageDirIdentity{}, false
	}
	if artifactStageDirStillNamed(fs, original) {
		// Something already occupies the original name: a replant, never a
		// rename-induced identity drift. Restoring over it would move the
		// quarantined tree onto a name another writer now owns.
		return artifactStageDirIdentity{}, false
	}
	if err := fs.Rename(quarantined, original); err != nil {
		return artifactStageDirIdentity{}, false
	}
	if artifactStageDirStillNamed(fs, quarantined) {
		return artifactStageDirIdentity{}, false
	}
	identity := artifactStageDirectoryIdentity(fs, original)
	if identity.known && manifest.Directory.matchesIdentity(identity) {
		return identity, true
	}
	// Not the tree the seal described: put it back under the quarantine name so
	// the caller's retention path keeps refusing to touch foreign bytes.
	_ = fs.Rename(original, quarantined)
	return artifactStageDirIdentity{}, false
}
