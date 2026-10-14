package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

type artifactInPlaceDirectoryMove struct {
	oldDir     string
	newDir     string
	currentDir string
	identity   artifactStageDirIdentity
}

func (s *artifactStage) relocateInPlaceDirectory(ctx context.Context, o *applyOrchImpl, opID OperationID, state *applyPipelineState) error {
	if !s.inPlace || !s.videoDeferred || !s.original.Organize.MoveFiles || s.original.OperationMode != operationmode.OperationModeInPlace {
		return nil
	}
	if err := s.revalidateAdmittedSource(s.sourcePath, s.sourceIdentity); err != nil {
		return err
	}
	executor, ok := o.organizer.(artifactPlanExecutor)
	if !ok {
		return fmt.Errorf("in-place directory publication requires planned execution")
	}
	plan, err := executor.PlanOrganize(ctx, organizer.OrganizeCmd{Match: s.original.Match, Movie: s.original.Movie, DestDir: s.finalRoot, ForceUpdate: s.original.Organize.ForceUpdate, MoveFiles: true, OperationMode: s.original.OperationMode, ForceRenameFile: s.original.Organize.ForceRenameFile})
	if err != nil {
		return fmt.Errorf("plan in-place directory publication: %w", err)
	}
	if err := s.setInPlaceFinalRoot(plan.TargetDir); err != nil {
		return err
	}
	oldDir := filepath.Dir(s.sourcePath)
	if !plan.RenameFolder || filepath.Clean(oldDir) == filepath.Clean(plan.TargetDir) {
		return nil
	}
	if artifactDirectoryPath(oldDir, ".", plan.TargetDir) != plan.TargetDir {
		return fmt.Errorf("cannot move in-place source directory into itself")
	}
	move := &artifactInPlaceDirectoryMove{oldDir: oldDir, newDir: plan.TargetDir, currentDir: oldDir, identity: s.sourceDirIdentity}
	s.directoryMove = move
	location, err := s.moveInPlaceDirectory(ctx, o, opID, oldDir, move.newDir, move.identity)
	move.currentDir = location
	if location != oldDir {
		s.rebaseInPlaceDirectory(oldDir, location, state)
	}
	if err != nil {
		return err
	}
	s.original.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
	return nil
}

func (s *artifactStage) moveInPlaceDirectory(ctx context.Context, o *applyOrchImpl, opID OperationID, source, target string, identity artifactStageDirIdentity) (string, error) {
	if err := ctx.Err(); err != nil {
		return source, err
	}
	if !artifactStageDirMatches(s.fs, source, identity) {
		return source, fmt.Errorf("in-place source directory changed since admission: %w", errArtifactSourceChanged)
	}
	if _, err := lstatArtifactStageDir(s.fs, target); !os.IsNotExist(err) {
		return source, fmt.Errorf("in-place directory destination is occupied or unavailable: %s", target)
	}
	if err := s.fs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return source, fmt.Errorf("create in-place directory parent: %w", err)
	}
	scratch := filepath.Join(filepath.Dir(source), ".javinizer-directory-"+rand.Text())
	reservation, err := s.fs.OpenFile(scratch, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return source, fmt.Errorf("reserve in-place directory relocation: %w", err)
	}
	claim, statErr := reservation.Stat()
	closeErr := reservation.Close()
	if claim != nil {
		defer func() { _ = fsutil.UnlinkVerified(s.fs, scratch, claim) }()
	}
	if err := errors.Join(statErr, closeErr); err != nil {
		return source, fmt.Errorf("pin in-place directory reservation: %w", err)
	}
	if err := s.checkpointInPlaceDirectory(ctx, o, opID, scratch, true); err != nil {
		return source, err
	}
	hold, err := fsutil.TakeAside(fsutil.TakeAsideSpec{FS: s.fs, Src: source, Scratch: scratch, Claim: claim, Prove: func(info os.FileInfo) error {
		if !identity.matches(s.fs, scratch, info) {
			return fmt.Errorf("in-place source directory changed during relocation: %w", errArtifactSourceChanged)
		}
		return nil
	}})
	if err != nil {
		if errors.Is(err, fsutil.ErrTakeAsideRestoreFailed) && artifactStageDirMatches(s.fs, scratch, identity) {
			return scratch, fmt.Errorf("take in-place source directory: %w", err)
		}
		return source, fmt.Errorf("take in-place source directory: %w", err)
	}
	if err := s.checkpointInPlaceDirectory(ctx, o, opID, target, false); err != nil {
		if restoreErr := hold.Restore(); restoreErr != nil {
			return scratch, errors.Join(err, restoreErr)
		}
		return source, err
	}
	if err := fsutil.PublishNoReplace(s.fs, scratch, target); err != nil {
		if restoreErr := hold.Restore(); restoreErr != nil {
			return scratch, errors.Join(err, restoreErr)
		}
		return source, fmt.Errorf("publish in-place directory: %w", err)
	}
	if !artifactStageDirMatches(s.fs, target, identity) {
		return target, fmt.Errorf("published in-place directory no longer names the admitted object")
	}
	if err := s.checkpointInPlaceDirectory(ctx, o, opID, target, true); err != nil {
		return target, err
	}
	return target, nil
}

func (s *artifactStage) checkpointInPlaceDirectory(ctx context.Context, o *applyOrchImpl, opID OperationID, dir string, persist bool) error {
	if o.revertLog == nil || opID == "" {
		return nil
	}
	original := s.directoryOriginalPath(s.sourcePath)
	current := filepath.Join(dir, filepath.Base(s.sourcePath))
	if err := o.revertLog.RecordMoveIntent(ctx, opID, original, current); err != nil {
		return fmt.Errorf("journal in-place directory relocation: %w", err)
	}
	if !persist {
		return nil
	}
	result := &organizer.OrganizeResult{OriginalPath: original, NewPath: current, FolderPath: dir, InPlaceRenamed: true, OldDirectoryPath: s.directoryMove.oldDir, NewDirectoryPath: dir, Moved: true}
	if err := o.revertLog.Complete(ctx, opID, &ApplyResult{OrganizeResult: result, Movie: s.original.Movie, OperationID: opID}); err != nil {
		return fmt.Errorf("persist in-place directory relocation: %w", err)
	}
	return nil
}

func (s *artifactStage) rollbackInPlaceDirectory(ctx context.Context, o *applyOrchImpl, state *applyPipelineState) error {
	move := s.directoryMove
	if move == nil || move.currentDir == move.oldDir {
		return nil
	}
	location, err := s.moveInPlaceDirectory(ctx, o, state.operationID, move.currentDir, move.oldDir, move.identity)
	if location != move.currentDir {
		s.rebaseInPlaceDirectory(move.currentDir, location, state)
		move.currentDir = location
	}
	if err != nil {
		return fmt.Errorf("rollback in-place directory relocation: %w", err)
	}
	s.original.OperationMode = operationmode.OperationModeInPlace
	s.directoryMove = nil
	return nil
}

func artifactDirectoryPath(oldDir, newDir, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(oldDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(newDir, rel)
}

func (s *artifactStage) rebaseInPlaceDirectory(oldDir, newDir string, state *applyPipelineState) {
	rebase := func(path string) string { return artifactDirectoryPath(oldDir, newDir, path) }
	s.sourcePath, s.root, s.finalRoot, s.stagedSource = rebase(s.sourcePath), rebase(s.root), rebase(s.finalRoot), rebase(s.stagedSource)
	s.original.Match.Path = rebase(s.original.Match.Path)
	s.original.DestPath = rebase(s.original.DestPath)
	for i := range s.siblings {
		s.siblings[i].sourcePath = rebase(s.siblings[i].sourcePath)
		s.siblings[i].stagedPath = rebase(s.siblings[i].stagedPath)
	}
	if state.organizeResult != nil {
		state.organizeResult.NewPath = rebase(state.organizeResult.NewPath)
		state.organizeResult.FolderPath = rebase(state.organizeResult.FolderPath)
	}
	state.finalDir, state.targetDir, state.nfoPath, state.foundNFOPath = rebase(state.finalDir), rebase(state.targetDir), rebase(state.nfoPath), rebase(state.foundNFOPath)
}

func (s *artifactStage) directoryOriginalPath(path string) string {
	if move := s.directoryMove; move != nil {
		return artifactDirectoryPath(move.currentDir, move.oldDir, path)
	}
	return path
}

func (s *artifactStage) annotateInPlaceDirectory(result *organizer.OrganizeResult) {
	if move := s.directoryMove; move != nil && result != nil {
		result.OriginalPath = s.directoryOriginalPath(result.OriginalPath)
		result.InPlaceRenamed = true
		result.OldDirectoryPath, result.NewDirectoryPath = move.oldDir, move.newDir
	}
}
