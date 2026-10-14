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

// In-place copy planning renames the staging directory beside the source;
// its parent is part of the output layout, not just a temporary location.
func (o *applyOrchImpl) allocateArtifactStageForApply(ctx context.Context, finalRoot string, cmd ApplyCmd) (string, error) {
	if cmd.OperationMode == operationmode.OperationModeInPlace && !cmd.Organize.Skip && !cmd.Organize.MoveFiles {
		return allocateArtifactStageAt(o.fs, filepath.Dir(finalRoot), finalRoot)
	}
	root, err := allocateArtifactStage(o.fs, finalRoot)
	if err == nil || (!errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EROFS)) || cmd.Organize.Skip || (cmd.OperationMode != "" && cmd.OperationMode != operationmode.OperationModeOrganize) {
		return root, err
	}
	// A read-only library root can contain an existing writable movie folder.
	// Stage in that planned folder, never beside the root: even equal device
	// IDs do not prove rename compatibility across a bind mount.
	planner, ok := o.organizer.(artifactPlanExecutor)
	if !ok {
		return "", err
	}
	plan, planErr := planner.PlanOrganize(ctx, organizer.OrganizeCmd{Match: cmd.Match, Movie: cmd.Movie, DestDir: finalRoot, ForceUpdate: cmd.Organize.ForceUpdate, MoveFiles: cmd.Organize.MoveFiles, LinkMode: cmd.Organize.LinkMode, OperationMode: cmd.OperationMode, ForceRenameFile: cmd.Organize.ForceRenameFile})
	if planErr != nil {
		return "", errors.Join(err, fmt.Errorf("plan artifact staging destination: %w", planErr))
	}
	if plan == nil || plan.TargetDir == "" || filepath.Clean(plan.TargetDir) == filepath.Clean(finalRoot) {
		return "", err
	}
	rel, relErr := filepath.Rel(finalRoot, plan.TargetDir)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.Join(err, fmt.Errorf("artifact staging destination is outside library: %s", plan.TargetDir))
	}
	info, statErr := o.fs.Stat(plan.TargetDir)
	if statErr != nil {
		return "", errors.Join(err, fmt.Errorf("inspect artifact staging destination: %w", statErr))
	}
	if !info.IsDir() {
		return "", errors.Join(err, fmt.Errorf("artifact staging destination is not a directory: %s", plan.TargetDir))
	}
	root, fallbackErr := allocateArtifactStageAt(o.fs, plan.TargetDir, "")
	if fallbackErr != nil {
		return "", errors.Join(err, fallbackErr)
	}
	return root, nil
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
