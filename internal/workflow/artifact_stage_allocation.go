package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

// Stage on the filesystem selected by the real destination plan. Private
// in-place execution never renames the staging directory into the library.
func (o *applyOrchImpl) allocateArtifactStageForApply(ctx context.Context, finalRoot string, cmd ApplyCmd, plan *organizer.OrganizePlan) (string, error) {
	inPlace := cmd.OperationMode == operationmode.OperationModeInPlace || cmd.OperationMode == operationmode.OperationModeInPlaceNoRenameFolder
	if cmd.Organize.Skip || (cmd.OperationMode != "" && cmd.OperationMode != operationmode.OperationModeOrganize && !inPlace) {
		return allocateArtifactStage(o.fs, finalRoot)
	}
	if plan == nil {
		planner, ok := o.organizer.(artifactPlanExecutor)
		if !ok {
			return allocateArtifactStage(o.fs, finalRoot)
		}
		var err error
		plan, err = planner.PlanOrganize(ctx, organizer.OrganizeCmd{Match: cmd.Match, Movie: cmd.Movie, DestDir: finalRoot, ForceUpdate: cmd.Organize.ForceUpdate, MoveFiles: cmd.Organize.MoveFiles, LinkMode: cmd.Organize.LinkMode, OperationMode: cmd.OperationMode, ForceRenameFile: cmd.Organize.ForceRenameFile})
		if err != nil {
			return "", fmt.Errorf("plan artifact staging destination: %w", err)
		}
	}
	if plan == nil || plan.TargetDir == "" {
		return "", fmt.Errorf("artifact staging destination is empty")
	}
	rel, relErr := filepath.Rel(finalRoot, plan.TargetDir)
	if !inPlace && (relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return "", fmt.Errorf("artifact staging destination is outside library: %s", plan.TargetDir)
	}
	// An already-organized, artifact-free operation need not write its movie
	// folder. Preserve no-op applies to read-only folders without allowing
	// this placement for any primary, subtitle, multipart, or metadata output.
	if !cmd.Download && !cmd.GenerateNFO && plan.SourcePath != "" && filepath.Clean(plan.SourcePath) == filepath.Clean(plan.TargetPath) {
		entries, err := afero.ReadDir(o.fs, filepath.Dir(plan.SourcePath))
		if err != nil {
			return "", fmt.Errorf("inspect stationary artifact source: %w", err)
		}
		hasSiblings := len(o.organizer.(artifactPlanExecutor).PlanSubtitleMoves(plan)) != 0
		for _, entry := range entries {
			if !entry.IsDir() && isStagedArtifactVideoSibling(filepath.Base(plan.SourcePath), entry.Name()) {
				hasSiblings = true
			}
		}
		if !hasSiblings {
			if inPlace {
				return allocateArtifactStage(o.fs, filepath.Dir(plan.TargetDir))
			}
			return allocateArtifactStage(o.fs, finalRoot)
		}
	}
	if inPlace {
		// The in-place strategy selects the existing source directory or a
		// sibling beneath its existing parent. A retained source directory may
		// itself be mounted, so its parent cannot determine rename compatibility.
		return allocateArtifactStage(o.fs, plan.TargetDir)
	}
	// The movie folder or any existing ancestor can be a separate mount,
	// even when the library root is writable and device IDs are equal.
	// Stage in the deepest existing destination directory, without creating
	// final folders before publication or falling back outside that mount.
	for parent := filepath.Join(finalRoot, rel); parent != filepath.Clean(finalRoot); parent = filepath.Dir(parent) {
		info, statErr := o.fs.Stat(parent)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect artifact staging destination: %w", statErr)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("artifact staging destination is not a directory: %s", parent)
		}
		// Also reap authenticated residue from earlier root-based allocation.
		sweepArtifactStaging(o.fs, filepath.Dir(finalRoot))
		sweepArtifactStaging(o.fs, finalRoot)
		return allocateArtifactStageAt(o.fs, parent, "")
	}
	return allocateArtifactStage(o.fs, finalRoot)
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
