package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
)

// In-place copy planning renames the staging directory beside the source;
// its parent is part of the output layout, not just a temporary location.
func allocateArtifactStageForApply(fs afero.Fs, finalRoot string, cmd ApplyCmd) (string, error) {
	if cmd.OperationMode == operationmode.OperationModeInPlace && !cmd.Organize.Skip && !cmd.Organize.MoveFiles {
		return allocateArtifactStageAt(fs, filepath.Dir(finalRoot), finalRoot)
	}
	return allocateArtifactStage(fs, finalRoot)
}

func allocateArtifactStage(fs afero.Fs, finalRoot string) (string, error) {
	parent := filepath.Dir(finalRoot)
	// Reap authenticated residue from runs before this destination existed.
	sweepArtifactStaging(fs, parent)
	fallback := finalRoot
	// An existing destination can be a mount point or a symlink into another
	// filesystem even when its parent is writable. Stage within it first:
	// equal device IDs alone do not rule out an EXDEV boundary at a bind mount.
	if info, err := fs.Stat(finalRoot); err == nil && info.IsDir() {
		fallback = ""
		if parentInfo, parentErr := fs.Stat(parent); parentErr == nil {
			destDevice, _, destKnown := fsutil.BoundObjectIdentity(fs, finalRoot, info)
			parentDevice, _, parentKnown := fsutil.BoundObjectIdentity(fs, parent, parentInfo)
			if destKnown && parentKnown && destDevice == parentDevice {
				// Preserve the legacy permission fallback for a read-only library
				// root with writable movie subdirectories, but never fall back
				// across a known filesystem boundary or an unproven device.
				fallback = parent
			}
		}
		parent = finalRoot
	}
	return allocateArtifactStageAt(fs, parent, fallback)
}

func allocateArtifactStageAt(fs afero.Fs, parent, fallback string) (string, error) {
	sweepArtifactStaging(fs, parent)
	var root string
	var err error
	if err = fs.MkdirAll(parent, 0o755); err != nil {
		err = fmt.Errorf("create artifact staging parent: %w", err)
	} else if root, err = afero.TempDir(fs, parent, artifactStageDirPrefix); err != nil {
		err = fmt.Errorf("create artifact staging area: %w", err)
	}
	if err == nil {
		return root, nil
	}
	if (!errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EROFS)) || fallback == "" || filepath.Clean(fallback) == filepath.Clean(parent) {
		return "", err
	}
	if mkdirErr := fs.MkdirAll(fallback, 0o755); mkdirErr != nil {
		return "", errors.Join(err, fmt.Errorf("create artifact staging fallback parent: %w", mkdirErr))
	}
	sweepArtifactStaging(fs, fallback)
	root, fallbackErr := afero.TempDir(fs, fallback, artifactStageDirPrefix)
	if fallbackErr != nil {
		return "", errors.Join(err, fmt.Errorf("create artifact staging area within fallback parent: %w", fallbackErr))
	}
	return root, nil
}
