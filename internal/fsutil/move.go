package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

// CopyFileFs copies a file within the afero filesystem, creating destination directories as needed.
func CopyFileFs(fs afero.Fs, src, dst string) error {
	_, err := CopyFileFsDestReplaced(fs, src, dst)
	return err
}

// CopyFileFsDestReplaced is CopyFileFs extended with the PUBLISH-BOUND
// replacement signal (PR #249 codex P2): destReplaced reports whether the
// staged replace-publish actually displaced an OCCUPIED destination whose
// object does not alias the source's own inode — an alias entry (hardlink
// twin of the source) names no foreign bytes, so displacing it is deliberately
// NOT a replacement. The evidence is captured at syscall adjacency with the
// publish rename ITSELF (the CopyFile publish identity machinery is the #234
// bound staged publish): the copy lane stages the full source stream between
// any caller classification and the publish, so classify-time occupancy
// answers could be raced stale by a foreign plant/vacate anywhere in that
// window (a false or suppressed force-overwrite audit crumb). A probe whose
// lookup fails answers "no replacement" — no confirmation, no claim.
func CopyFileFsDestReplaced(fs afero.Fs, src, dst string) (destReplaced bool, err error) {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return false, fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return false, nil
	}

	return copyFileDataFsDestReplaced(fs, src, dst)
}

// MoveFileFs moves a file within the afero filesystem, falling back to copy-and-remove across devices.
func MoveFileFs(fs afero.Fs, src, dst string) error {
	_, err := MoveFileFsDestReplaced(fs, src, dst)
	return err
}

// MoveFileFsDestReplaced is MoveFileFs extended with the same publish-bound
// replacement signal as CopyFileFsDestReplaced (PR #249 codex P2). The
// same-device leg probes one syscall ahead of the inline rename (a rename
// publishes THE object src names — including a symlink object itself — so the
// source identity rides the same no-follow probe, never a chased Stat); the
// cross-device fallback inherits the staged publish's bound signal instead.
func MoveFileFsDestReplaced(fs afero.Fs, src, dst string) (destReplaced bool, err error) {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return false, fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return false, nil
	}

	srcInfo := publishProbeIdentity(fs, src)
	occupied := publishDisplacesForeign(fs, dst, srcInfo)
	err = fs.Rename(src, dst)
	if err == nil {
		return occupied, nil
	}

	if !isCrossDeviceError(err) {
		return false, fmt.Errorf("failed to move file: %w", err)
	}

	return crossDeviceMoveFsDestReplaced(fs, src, dst)
}

// publishProbeIdentity records a path's OBJECT identity for a publish probe's
// alias exclusion, no-follow wherever the filesystem exposes the distinction
// (the same asideLstat discipline the bound take-aside flow uses). Any lookup
// failure — transient or absence — answers nil, and the exclusion is simply
// not applied (confirm-or-silent, never guess).
func publishProbeIdentity(fs afero.Fs, name string) os.FileInfo {
	info, _ := asideLstat(fs, name)
	return info
}

// publishDisplacesForeign reports — probed at syscall adjacency with the
// caller's replace-publish — whether dst names an object that publish would
// displace that is NOT an alias of the source's own object (publishDisplaces
// == PHYSICAL occupancy at the publish instant, minus same-inode aliases,
// whose removal destroys no foreign bytes). A failed/absent probe answers
// false: the crumb never fires on unconfirmed evidence.
func publishDisplacesForeign(fs afero.Fs, dst string, srcInfo os.FileInfo) bool {
	info := publishProbeIdentity(fs, dst)
	if info == nil {
		return false
	}
	if srcInfo != nil && os.SameFile(info, srcInfo) {
		return false
	}
	return true
}

// crossDeviceMoveFsDestReplaced is the cross-device fallback's publish-bound
// twin (the plain error-only surface is MoveFileFs's).
func crossDeviceMoveFsDestReplaced(fs afero.Fs, src, dst string) (bool, error) {
	replaced, err := copyFileDataFsDestReplaced(fs, src, dst)
	if err != nil {
		// The copy leg never writes to dst directly (staging only), so there is
		// NOTHING of ours at dst to "clean up": removing it could delete a
		// pre-existing foreign file (#224). Keep both, surface the failure.
		return false, fmt.Errorf("failed to copy file across devices: %w", err)
	}

	if err := fs.Remove(src); err != nil {
		// dst was fully published via the bound replace; the source remove is
		// the only failed step — keep BOTH objects rather than deleting the
		// published destination, and surface the ambiguity (#224). The typed
		// ErrPublishCompleted marker rides along (PR #241 codex P1): the
		// destination already carries THIS operation's bytes, so compensation
		// and duplicate-claim classifiers must never read this failure as a
		// pre-publish no-op — the same sentinel the no-replace lineage wraps
		// on its cleanup-refusal leg (move_noreplace.go).
		return false, fmt.Errorf("%w: failed to remove source after cross-device copy: %w", ErrPublishCompleted, err)
	}

	return replaced, nil
}

// copyFileDataFsDestReplaced is the staging core of CopyFileFsDestReplaced:
// the displacement evidence is captured by the publish closure ITSELF (bound
// to the bound staged publish), never by any earlier classification.
func copyFileDataFsDestReplaced(fs afero.Fs, src, dst string) (bool, error) {
	srcFile, err := fs.Open(src)
	if err != nil {
		return false, fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// The open handle's own stat is the source identity for the publish
	// probe's alias exclusion — bound to the very bytes being staged below,
	// even if a foreign writer swaps the src NAME mid-stream (the staged
	// content is whatever this descriptor reads).
	srcInfo, _ := srcFile.Stat()

	// Stage dest-adjacent with O_EXCL (a predictable O_TRUNC name could
	// truncate a racing peer's staged bytes), stream through the open handle,
	// and publish through the bound discipline — a substitute planted on the
	// staged name is never published nor unbound-removed (#224). Publish keeps
	// REPLACE semantics (authorized overwrite); no-clobber uses the separate
	// NoReplace composites.
	staged, handle, sErr := CreateExclusiveStagingFile(fs, dst, ".mvstg", noreplaceOrdinal.Add(1), stagingFileMode())
	if sErr != nil {
		return false, fmt.Errorf("failed to create destination: %w", sErr)
	}

	if _, err := io.Copy(handle, srcFile); err != nil {
		DiscardFailedExclusiveStaging(fs, staged, handle)
		return false, fmt.Errorf("failed to copy data: %w", err)
	}

	stagedIdentity := stagingIdentity(handle)

	// displacedOccupant is the publish-bound audit signal (PR #249 codex P2):
	// the occupancy probe runs ONE SYSCALL ahead of the replace-publish in the
	// closure below, alias-excluded against the staged bytes' own source inode
	// — a signal that stays TRUE to physical occupancy at the publish instant
	// no matter how long the staging stream above took.
	displacedOccupant := false
	p := StagedPublish{
		FS: fs,
		Publish: func(pfs afero.Fs, stagedName, dest string) error {
			occupied := publishDisplacesForeign(pfs, dest, srcInfo)
			pubErr := ReplaceFile(pfs, stagedName, dest)
			// Union over successful attempts: a proven-substitution retry may
			// legitimately republish into absence, and ANY successful publish
			// that displaced resident bytes keeps its disclosure.
			if pubErr == nil && occupied {
				displacedOccupant = true
			}
			return pubErr
		},
		Staged:      staged,
		Handle:      handle,
		Dest:        dst,
		Suffix:      ".mvstg",
		NextOrdinal: nextNoReplaceOrdinal,
	}
	if err := PublishStagedBound(p); err != nil {
		// Same discard discipline as the no-replace composites: the old
		// implementation removed its temp on failure; keep that cleanliness
		// without ever deleting a possibly-foreign staged name.
		discardStagedAfterFailedPublish(fs, staged, stagedIdentity, err)
		return false, fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return displacedOccupant, nil
}
