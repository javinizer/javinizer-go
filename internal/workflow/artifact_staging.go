package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

type artifactPlanExecutor interface {
	PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error)
	PlanSourceExists(*organizer.OrganizePlan) bool
	ExecuteOrganizePlan(*organizer.OrganizePlan, bool, organizer.LinkMode) (*organizer.OrganizeResult, error)
}

type artifactSibling struct {
	sourcePath string
	stagedPath string
}

type artifactStage struct {
	fs                 afero.Fs
	fencer             database.ApplyArtifactPublicationFencer
	root               string
	finalRoot          string
	sourcePath         string
	stagedSource       string
	siblings           []artifactSibling
	inPlace            bool
	original           ApplyCmd
	rejected           bool
	duplicatePlan      *organizer.OrganizePlan
	publishBatch       *downloader.ReplacementBatch
	publishCtx         context.Context
	completedBatch     *downloader.ReplacementBatch
	unresolvedBatch    *downloader.ReplacementBatch
	sourceCleanupArmed bool
	sharedClaims       []SharedArtifactClaim
	sharedConsumers    []SharedArtifactClaim
	sharedPublishBegan bool
	sharedPoisoned     bool
}

var errArtifactDirtyAdmission = errors.New("artifact publication preparation failed")

func artifactFencer(cmd ApplyCmd) database.ApplyArtifactPublicationFencer {
	fencer, _ := cmd.PublicationFence.(database.ApplyArtifactPublicationFencer)
	return fencer
}

func markArtifactDirty(cmd ApplyCmd) {
	fencer := artifactFencer(cmd)
	if fencer == nil || cmd.Movie == nil || strings.TrimSpace(cmd.Movie.ContentID) == "" {
		return
	}
	_ = fencer.WithApplyArtifactPublicationFence(context.Background(), cmd.Movie.ContentID, cmd.Movie.RenderGeneration, func(*models.Movie) error {
		return errArtifactDirtyAdmission
	})
}

func (o *applyOrchImpl) prepareArtifact(ctx context.Context, cmd ApplyCmd) (*artifactStage, ApplyCmd, error) {
	if cmd.DryRun {
		return nil, cmd, nil
	}
	needsArtifacts := !cmd.Organize.Skip || cmd.Download || cmd.GenerateNFO
	if needsArtifacts && cmd.PublicationFence != nil && artifactFencer(cmd) == nil {
		return nil, cmd, fmt.Errorf("artifact staging blocked: publication fence lacks artifact capability")
	}
	if artifactFencer(cmd) == nil {
		return nil, cmd, nil
	}
	if cmd.Movie == nil || strings.TrimSpace(cmd.Movie.ContentID) == "" {
		return nil, cmd, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, cmd, err
	}
	mode := cmd.OperationMode
	if mode == "" {
		mode = operationmode.OperationModeOrganize
	}
	inPlace := mode == operationmode.OperationModeInPlace || mode == operationmode.OperationModeInPlaceNoRenameFolder
	if cmd.Organize.Skip && !cmd.Download && !cmd.GenerateNFO {
		return nil, cmd, nil
	}
	sourcePath := strings.TrimSpace(cmd.Match.Path)
	finalRoot := strings.TrimSpace(cmd.DestPath)
	if finalRoot == "" {
		if !inPlace && mode != operationmode.OperationModeMetadataArtwork {
			return nil, cmd, fmt.Errorf("artifact staging requires a destination path")
		}
		if sourcePath == "" {
			return nil, cmd, fmt.Errorf("artifact staging requires a source path")
		}
		finalRoot = filepath.Dir(sourcePath)
	} else {
		finalRoot = filepath.Clean(finalRoot)
	}
	var duplicatePlan *organizer.OrganizePlan
	if tracker := cmd.Organize.DuplicateTracker; tracker != nil && !cmd.Organize.Skip {
		executor, ok := o.organizer.(artifactPlanExecutor)
		if !ok {
			return nil, cmd, fmt.Errorf("artifact staging blocked: organizer has no planned execution seam")
		}
		plan, planErr := executor.PlanOrganize(ctx, organizer.OrganizeCmd{Match: cmd.Match, Movie: cmd.Movie, DestDir: finalRoot, ForceUpdate: cmd.Organize.ForceUpdate, MoveFiles: cmd.Organize.MoveFiles, LinkMode: cmd.Organize.LinkMode, OperationMode: cmd.OperationMode, ForceRenameFile: cmd.Organize.ForceRenameFile})
		if planErr != nil {
			return nil, cmd, fmt.Errorf("plan artifact duplicate claim: %w", planErr)
		}
		_, duplicate := tracker.ObserveClaim(ctx, plan.SourcePath, plan.TargetPath, plan.WillMove)
		if duplicate {
			if !cmd.Organize.ForceUpdate {
				return nil, cmd, fmt.Errorf("conflicts detected: %s", plan.TargetPath)
			}
			return nil, cmd, nil
		}
		duplicatePlan = plan
	}
	if err := o.fs.MkdirAll(filepath.Dir(finalRoot), 0o755); err != nil {
		return nil, cmd, fmt.Errorf("create artifact staging parent: %w", err)
	}
	root, err := afero.TempDir(o.fs, filepath.Dir(finalRoot), ".javinizer-apply-")
	if err != nil {
		return nil, cmd, fmt.Errorf("create artifact staging area: %w", err)
	}
	stage := &artifactStage{fs: o.fs, fencer: artifactFencer(cmd), root: root, finalRoot: finalRoot, sourcePath: cmd.Match.Path, inPlace: inPlace, original: cmd, duplicatePlan: duplicatePlan}
	stagedCmd := cmd
	if duplicatePlan != nil {
		stagedCmd.Organize.DuplicateTracker = nil
	}
	if !cmd.Organize.Skip {
		if sourcePath == "" {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging requires a source path")
		}
		sourceInfo, statErr := o.fs.Stat(sourcePath)
		if statErr != nil {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging source: %w", statErr)
		}
		if !sourceInfo.Mode().IsRegular() {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging blocked for non-regular source %s", cmd.Match.Path)
		}
		base := filepath.Base(sourcePath)
		stagedDir := filepath.Join(root, ".source")
		if inPlace {
			stagedDir = root
		}
		stage.stagedSource = filepath.Join(stagedDir, base)
		if err := copyArtifactFile(o.fs, sourcePath, stage.stagedSource, sourceInfo.Mode().Perm()); err != nil {
			stage.cleanup()
			return nil, cmd, err
		}
		entries, readErr := afero.ReadDir(o.fs, filepath.Dir(sourcePath))
		if readErr != nil {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging source directory: %w", readErr)
		}
		for _, entry := range entries {
			if entry.Name() == base || entry.IsDir() || !isStagedArtifactSibling(base, entry.Name()) {
				continue
			}
			sibling := filepath.Join(filepath.Dir(sourcePath), entry.Name())
			siblingInfo, siblingErr := o.fs.Stat(sibling)
			if siblingErr != nil {
				stage.cleanup()
				return nil, cmd, fmt.Errorf("artifact staging sibling %s: %w", sibling, siblingErr)
			}
			if !siblingInfo.Mode().IsRegular() {
				continue
			}
			stagedSibling := filepath.Join(stagedDir, entry.Name())
			if err := copyArtifactFile(o.fs, sibling, stagedSibling, siblingInfo.Mode().Perm()); err != nil {
				stage.cleanup()
				return nil, cmd, err
			}
			stage.siblings = append(stage.siblings, artifactSibling{sourcePath: sibling, stagedPath: stagedSibling})
		}
		stagedCmd.Match.Path = stage.stagedSource
		stagedCmd.Match.Name = base
	}
	stagedCmd.DestPath = root
	return stage, stagedCmd, nil
}

func isStagedArtifactSibling(sourceName, siblingName string) bool {
	sourceStem := strings.TrimSuffix(strings.ToLower(sourceName), strings.ToLower(filepath.Ext(sourceName)))
	siblingExt := strings.ToLower(filepath.Ext(siblingName))
	siblingStem := strings.TrimSuffix(strings.ToLower(siblingName), siblingExt)
	if isStagedArtifactSubtitleExtension(siblingExt) {
		return siblingStem == sourceStem || isStagedArtifactPrefixedStem(sourceStem, siblingStem)
	}
	if !isStagedArtifactVideoExtension(siblingExt) {
		return false
	}
	if stagedArtifactMultipartRoot(sourceStem) != "" {
		return false
	}
	sourcePart := stagedArtifactMultipartRoot(sourceStem)
	siblingPart := stagedArtifactMultipartRoot(siblingStem)
	return siblingPart == sourceStem || sourcePart == siblingStem || (sourcePart != "" && sourcePart == siblingPart)
}

func isStagedArtifactPrefixedStem(sourceStem, siblingStem string) bool {
	if !strings.HasPrefix(siblingStem, sourceStem) || len(siblingStem) == len(sourceStem) {
		return false
	}
	separator := siblingStem[len(sourceStem)]
	return separator == '.' || separator == '-' || separator == '_'
}

func isStagedArtifactSubtitleExtension(ext string) bool {
	switch ext {
	case ".srt", ".ass", ".ssa", ".sub", ".idx", ".sup", ".vtt", ".smi", ".sami":
		return true
	default:
		return false
	}
}

func isStagedArtifactVideoExtension(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".webm", ".mpg", ".mpeg", ".m2ts", ".ts":
		return true
	default:
		return false
	}
}

func stagedArtifactMultipartRoot(stem string) string {
	lower := strings.ToLower(stem)
	markers := []string{"-cd", ".cd", "_cd", "-disc", ".disc", "_disc", "-part", ".part", "_part", "-pt", ".pt", "_pt"}
	for _, marker := range markers {
		index := strings.LastIndex(lower, marker)
		if index <= 0 {
			continue
		}
		suffix := lower[index+len(marker):]
		if suffix == "" {
			continue
		}
		digits := true
		for _, r := range suffix {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if digits {
			return lower[:index]
		}
	}
	return ""
}
func copyArtifactFile(fs afero.Fs, source, target string, mode os.FileMode) error {
	if err := fs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create staged source directory: %w", err)
	}
	in, err := fs.Open(source)
	if err != nil {
		return fmt.Errorf("open artifact source: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := fs.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create staged source: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = fs.Remove(target)
		return fmt.Errorf("stage artifact source: %w", copyErr)
	}
	if closeErr != nil {
		_ = fs.Remove(target)
		return fmt.Errorf("close staged source: %w", closeErr)
	}
	return nil
}

func (s *artifactStage) cleanup() {
	if s == nil || s.fs == nil || s.root == "" {
		return
	}
	_ = s.fs.RemoveAll(s.root)
}

func (s *artifactStage) finalPath(path string) (string, error) {
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(s.root, cleanPath)
	if err != nil {
		return "", fmt.Errorf("staged path escapes staging area: %s", path)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if s.inPlace {
			parentRel, parentErr := filepath.Rel(filepath.Dir(s.finalRoot), cleanPath)
			if parentErr == nil && parentRel != ".." && !strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
				return cleanPath, nil
			}
		}
		return "", fmt.Errorf("staged path escapes staging area: %s", path)
	}
	if rel == "." {
		return s.finalRoot, nil
	}
	return filepath.Join(s.finalRoot, rel), nil
}

func (s *artifactStage) mergeMatch(state *applyPipelineState, match models.FileMatchInfo) (models.FileMatchInfo, error) {
	path := s.finalRoot
	if state.organizeResult != nil && state.organizeResult.NewPath != "" {
		mapped, err := s.finalPath(state.organizeResult.NewPath)
		if err != nil {
			return match, err
		}
		path = mapped
	} else if match.Name != "" {
		path = filepath.Join(path, match.Name)
	}
	match.Path = path
	return match, nil
}

func (s *artifactStage) finishDuplicateClaim(err error) {
	plan := s.duplicatePlan
	if plan == nil {
		return
	}
	if tracker := s.original.Organize.DuplicateTracker; tracker != nil {
		if err == nil || fsutil.PublishCompleted(err) {
			tracker.SettleClaim(plan.SourcePath, plan.TargetPath)
		} else {
			tracker.ReleaseClaim(plan.SourcePath, plan.TargetPath)
		}
	}
	s.duplicatePlan = nil
}

func (s *artifactStage) publish(ctx context.Context, o *applyOrchImpl, state *applyPipelineState, steps *stepCompletion) (returnErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.finishSharedArtifactClaims(s.sharedCompletionDisposition(true, errors.New("artifact publication panicked")))
			panic(recovered)
		}
		s.finishSharedArtifactClaims(s.sharedCompletionDisposition(false, returnErr))
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.original.Movie == nil || strings.TrimSpace(s.original.Movie.ContentID) == "" {
		return fmt.Errorf("artifact publication requires movie content id")
	}
	returnErr = s.fencer.WithApplyArtifactPublicationFence(ctx, s.original.Movie.ContentID, s.original.Movie.RenderGeneration, func(authoritative *models.Movie) error {
		if authoritative == nil || authoritative.RenderGeneration != s.original.Movie.RenderGeneration {
			return fmt.Errorf("artifact publication authoritative movie changed")
		}
		return s.publishUnderFence(ctx, o, state.operationID, state, steps)
	})
	// Only a never-persisted movie may use the legacy publication path.
	if errors.Is(returnErr, database.ErrNotFound) && !s.original.PersistedMovie {
		returnErr = s.publishUnderFence(ctx, o, state.operationID, state, steps)
	}
	// Finalization rechecks generation and collision state after filesystem
	// publication. If that short transaction rejects the result, compensate the
	// already-journaled filesystem transaction.
	if returnErr != nil && s.completedBatch != nil {
		if rollbackErr := s.completedBatch.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil {
			s.sharedPoisoned = true
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback staged publication after fence failure: %w", rollbackErr))
		}
	}
	s.completedBatch, s.unresolvedBatch = nil, nil
	s.finishDuplicateClaim(returnErr)
	if errors.Is(returnErr, database.ErrApplyPublicationStale) {
		s.rejected = true
		s.reject(state)
		return returnErr
	}
	return returnErr
}

func (s *artifactStage) reject(state *applyPipelineState) {
	state.organizeResult = nil
	state.downloadPaths = nil
	state.nfoPath = ""
	state.finalDir = s.finalRoot
	state.targetDir = s.finalRoot
}

func (s *artifactStage) publishUnderFence(ctx context.Context, o *applyOrchImpl, opID OperationID, state *applyPipelineState, steps *stepCompletion) (returnErr error) {
	batch, err := downloader.NewReplacementBatch(s.fs, opID, replacementRecorder(o.revertLog))
	if err != nil {
		return err
	}
	s.publishBatch, s.publishCtx = batch, ctx
	committed := false
	createdFinalParent := ""
	defer func() {
		s.publishBatch, s.publishCtx = nil, nil
		if !committed {
			if rollbackErr := batch.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil {
				s.unresolvedBatch = batch
				s.sharedPoisoned = true
				returnErr = errors.Join(returnErr, fmt.Errorf("rollback staged publication: %w", rollbackErr))
			}
			if createdFinalParent != "" {
				_ = s.fs.Remove(createdFinalParent)
			}
		} else if returnErr == nil {
			s.completedBatch = batch
		}
	}()
	var finalResult *organizer.OrganizeResult
	finalReplaced := false
	stagedVideo := ""
	videoInstalledByTree := false
	if !s.original.Organize.Skip {
		if state.organizeResult == nil {
			return fmt.Errorf("artifact publication has no organize result")
		}
		if state.organizeResult.DuplicateSkipped {
			return nil
		}
		stagedVideo = state.organizeResult.NewPath
		if stagedVideo == "" {
			stagedVideo = s.stagedSource
		}
		executor, ok := o.organizer.(artifactPlanExecutor)
		if !ok {
			return fmt.Errorf("artifact publication blocked: organizer has no planned execution seam")
		}
		match := s.original.Match
		match.Path = stagedVideo
		if !s.original.Organize.MoveFiles && s.original.Organize.LinkMode != organizer.LinkModeNone {
			match.Path = s.sourcePath
		}
		match.Name = filepath.Base(match.Path)
		organizeCmd := organizer.OrganizeCmd{Match: match, Movie: s.original.Movie, DestDir: s.finalRoot, ForceUpdate: s.original.Organize.ForceUpdate, MoveFiles: s.original.Organize.MoveFiles, LinkMode: s.original.Organize.LinkMode, OperationMode: s.original.OperationMode, ForceRenameFile: s.original.Organize.ForceRenameFile}
		plan, err := executor.PlanOrganize(ctx, organizeCmd)
		if err != nil {
			return fmt.Errorf("replan artifact publication: %w", err)
		}
		if !executor.PlanSourceExists(plan) {
			return fmt.Errorf("artifact publication staged source disappeared: %s", stagedVideo)
		}
		artifactDestinations, preflightErr := s.treeDestinations(stagedVideo, filepath.Dir(s.stagedSource), filepath.Dir(stagedVideo), plan.TargetDir)
		if s.inPlace {
			videoInstalledByTree = filepath.Clean(plan.SourcePath) == filepath.Clean(plan.TargetPath)
			skipVideo := stagedVideo
			if videoInstalledByTree {
				skipVideo = ""
			}
			artifactDestinations, preflightErr = s.treeDestinations(skipVideo, "", "", "")
		}
		if preflightErr != nil {
			return preflightErr
		}
		if err := batch.Preflight(append([]string{plan.TargetPath}, artifactDestinations...)); err != nil {
			return err
		}
		if filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) {
			if !s.inPlace {
				parent := filepath.Dir(plan.TargetPath)
				if _, err := s.fs.Stat(parent); os.IsNotExist(err) {
					createdFinalParent = parent
				}
				if err := s.fs.MkdirAll(parent, 0o755); err != nil {
					return fmt.Errorf("create final video destination: %w", err)
				}
			}
			var armErr error
			finalReplaced, armErr = batch.BeforePublish(ctx, plan.TargetPath, s.original.Organize.ForceUpdate)
			if armErr != nil {
				return armErr
			}
			// The organizer acquires the same non-reentrant destination lock, so
			// batch must hand it off. Replan without overwrite authority after
			// vacating the journaled occupant: a late claimant then hits the
			// organizer's no-replace guard instead of being destroyed.
			organizeCmd.ForceUpdate = false
			guardedPlan, planErr := executor.PlanOrganize(ctx, organizeCmd)
			if planErr != nil {
				return fmt.Errorf("guard staged video publication: %w", planErr)
			}
			if filepath.Clean(guardedPlan.SourcePath) != filepath.Clean(plan.SourcePath) || filepath.Clean(guardedPlan.TargetPath) != filepath.Clean(plan.TargetPath) {
				return fmt.Errorf("guard staged video publication changed planned paths")
			}
			plan = guardedPlan
			batch.YieldToLockedPublisher(plan.TargetPath)
		}
		finalResult, err = executor.ExecuteOrganizePlan(plan, s.original.Organize.MoveFiles || s.original.Organize.LinkMode == organizer.LinkModeNone, s.original.Organize.LinkMode)
		if filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) && (err == nil || fsutil.PublishCompleted(err)) {
			batch.ObservePublishResult(plan.TargetPath)
		}
		if err != nil {
			return fmt.Errorf("publish organized video: %w", err)
		}
		if finalResult == nil {
			return fmt.Errorf("publish organized video returned no result")
		}
		if filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) {
			if err := batch.ConfirmPublish(ctx, plan.TargetPath); err != nil {
				return err
			}
		}
		if finalReplaced {
			finalResult.Warnings = append(finalResult.Warnings, organizer.AuthorizedOverwriteWarning(plan.TargetPath))
		}
		finalResult.OriginalPath = s.sourcePath
	}
	if err := s.rehomeRemainingSiblings(stagedVideo); err != nil {
		return err
	}
	artifactSkipDir := filepath.Dir(s.stagedSource)
	if s.inPlace {
		artifactSkipDir = ""
	}
	stagedArtifactDir := ""
	finalArtifactDir := ""
	if finalResult != nil && !s.inPlace {
		stagedArtifactDir = filepath.Dir(stagedVideo)
		finalArtifactDir = finalResult.FolderPath
		if finalArtifactDir == "" {
			finalArtifactDir = filepath.Dir(finalResult.NewPath)
		}
	}
	installSkipVideo := stagedVideo
	if videoInstalledByTree {
		installSkipVideo = ""
	}
	preservedMedia, err := s.installTree(installSkipVideo, artifactSkipDir, state.downloadPaths, stagedArtifactDir, finalArtifactDir)
	if err != nil {
		return err
	}
	// In-place organizer execution occurs inside the owned staging tree. Map
	// its result before source cleanup and inverse persistence so both use the
	// actual published paths rather than ephemeral staging names.
	if finalResult != nil && s.inPlace {
		if finalResult.NewPath != "" {
			finalResult.NewPath, err = s.finalPath(finalResult.NewPath)
			if err != nil {
				return err
			}
		}
		if finalResult.FolderPath != "" {
			finalResult.FolderPath, err = s.finalPath(finalResult.FolderPath)
			if err != nil {
				return err
			}
		}
	}
	if finalResult != nil {
		state.organizeResult = finalResult
		state.finalDir, state.targetDir = finalResult.FolderPath, finalResult.FolderPath
	} else {
		state.finalDir, state.targetDir = s.finalRoot, s.finalRoot
	}
	if state.nfoPath != "" {
		state.nfoPath, err = s.publicationPath(state.nfoPath, stagedArtifactDir, finalArtifactDir)
		if err != nil {
			return err
		}
		if s.isSharedConsumer(state.nfoPath) {
			state.nfoPath = ""
		}
	}
	mappedDownloads := make([]string, 0, len(state.downloadPaths))
	for _, path := range state.downloadPaths {
		mapped, mapErr := s.publicationPath(path, stagedArtifactDir, finalArtifactDir)
		if mapErr != nil {
			return mapErr
		}
		if !batch.IsReplacement(mapped) && !s.isSharedConsumer(mapped) {
			mappedDownloads = append(mappedDownloads, mapped)
		}
	}
	state.downloadPaths = mappedDownloads
	s.sharedConsumers = nil
	if preservedMedia && steps != nil {
		steps.PosterVerified = false
	}
	if !s.original.Organize.Skip && s.original.Organize.MoveFiles && s.sourcePath != "" && filepath.Clean(s.sourcePath) != filepath.Clean(finalResult.NewPath) {
		// Planned execution publishes the video, but may leave copied siblings
		// under .source. Publish them before removing any original sidecar.
		for _, sibling := range s.siblings {
			target := filepath.Join(filepath.Dir(finalResult.NewPath), stagedArtifactSiblingName(filepath.Base(s.sourcePath), filepath.Base(finalResult.NewPath), filepath.Base(sibling.sourcePath)))
			if _, statErr := s.fs.Stat(target); os.IsNotExist(statErr) {
				info, sourceErr := s.fs.Stat(sibling.stagedPath)
				if sourceErr != nil {
					return fmt.Errorf("inspect staged publication sidecar: %w", sourceErr)
				}
				if _, armErr := batch.BeforePublish(ctx, target, false); armErr != nil {
					return armErr
				}
				if copyErr := copyArtifactFile(s.fs, sibling.stagedPath, target, info.Mode().Perm()); copyErr != nil {
					return fmt.Errorf("publish sidecar before source cleanup: %w", copyErr)
				}
				if confirmErr := batch.ConfirmPublish(ctx, target); confirmErr != nil {
					return confirmErr
				}
				state.downloadPaths = append(state.downloadPaths, target)
			} else if statErr != nil {
				return fmt.Errorf("inspect publication sidecar: %w", statErr)
			}
		}
		// Persist the final-path inverse before deleting any source. A crash
		// after this point leaves history with every published path.
		if o.revertLog != nil && opID != "" {
			partial := &ApplyResult{OrganizeResult: finalResult, Movie: state.movie, DownloadPaths: state.downloadPaths, NFOPath: state.nfoPath, FoundNFOPath: state.foundNFOPath, Merged: state.merged, OperationID: opID}
			if err := o.revertLog.Complete(ctx, opID, partial); err != nil {
				return fmt.Errorf("persist inverse before source cleanup: %w", err)
			}
		}
		s.sourceCleanupArmed = true
		if err := batch.SetRollbackOrigin(finalResult.NewPath, s.sourcePath); err != nil {
			return err
		}
		if err := s.fs.Remove(s.sourcePath); err != nil {
			_ = batch.SetRollbackOrigin(finalResult.NewPath, "")
			return fmt.Errorf("remove original after artifact publication: %w", err)
		}
		for _, sibling := range s.siblings {
			target := filepath.Join(filepath.Dir(finalResult.NewPath), stagedArtifactSiblingName(filepath.Base(s.sourcePath), filepath.Base(finalResult.NewPath), filepath.Base(sibling.sourcePath)))
			if err := batch.SetRollbackOrigin(target, sibling.sourcePath); err != nil {
				return err
			}
			if err := s.fs.Remove(sibling.sourcePath); err != nil {
				_ = batch.SetRollbackOrigin(target, "")
				if !os.IsNotExist(err) {
					return fmt.Errorf("remove original sidecar after artifact publication: %w", err)
				}
			}
		}
		if s.inPlace && finalResult.FolderPath != "" {
			oldDir := filepath.Dir(s.sourcePath)
			if filepath.Clean(oldDir) != filepath.Clean(finalResult.FolderPath) {
				if entries, readErr := afero.ReadDir(s.fs, oldDir); readErr == nil && len(entries) == 0 {
					_ = s.fs.Remove(oldDir)
				}
			}
		}
	}
	committed = true
	return nil
}

func (s *artifactStage) rehomeRemainingSiblings(stagedVideo string) error {
	if stagedVideo == "" || s.original.Organize.LinkMode != organizer.LinkModeNone || s.stagedSource == "" {
		return nil
	}
	targetDir := filepath.Dir(stagedVideo)
	for _, sibling := range s.siblings {
		if _, err := s.fs.Stat(sibling.stagedPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect staged sidecar %s: %w", sibling.stagedPath, err)
		}
		target := filepath.Join(targetDir, stagedArtifactSiblingName(filepath.Base(s.stagedSource), filepath.Base(stagedVideo), filepath.Base(sibling.stagedPath)))
		if filepath.Clean(target) == filepath.Clean(sibling.stagedPath) {
			continue
		}
		if err := s.fs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create staged sidecar directory: %w", err)
		}
		if _, err := s.fs.Stat(target); err == nil {
			if err := s.fs.Remove(target); err != nil {
				return fmt.Errorf("replace staged sidecar %s: %w", target, err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect staged sidecar target %s: %w", target, err)
		}
		if err := s.fs.Rename(sibling.stagedPath, target); err != nil {
			return fmt.Errorf("stage sidecar %s: %w", target, err)
		}
	}
	return nil
}

func stagedArtifactSiblingName(sourceName, targetName, siblingName string) string {
	sourceExt := filepath.Ext(sourceName)
	sourceStem := strings.TrimSuffix(sourceName, sourceExt)
	siblingExt := filepath.Ext(siblingName)
	siblingStem := strings.TrimSuffix(siblingName, siblingExt)
	targetStem := strings.TrimSuffix(targetName, filepath.Ext(targetName))
	if strings.EqualFold(siblingStem, sourceStem) {
		return targetStem + siblingExt
	}
	if len(siblingStem) > len(sourceStem) && strings.EqualFold(siblingStem[:len(sourceStem)], sourceStem) {
		separator := siblingStem[len(sourceStem)]
		if separator == '.' || separator == '-' || separator == '_' {
			return targetStem + siblingStem[len(sourceStem):] + siblingExt
		}
	}
	return siblingName
}

func (s *artifactStage) treeDestinations(skipFile, skipDir, stagedArtifactDir, finalArtifactDir string) ([]string, error) {
	paths := make([]string, 0)
	if _, err := s.fs.Stat(s.root); os.IsNotExist(err) {
		return paths, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect staged artifact root: %w", err)
	}
	if err := afero.Walk(s.fs, s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		clean := filepath.Clean(path)
		if skipFile != "" && clean == filepath.Clean(skipFile) {
			return nil
		}
		if skipDir != "" && strings.HasPrefix(clean, filepath.Clean(skipDir)+string(filepath.Separator)) {
			return nil
		}
		target, mapErr := s.publicationPath(path, stagedArtifactDir, finalArtifactDir)
		if mapErr != nil {
			return mapErr
		}
		if filepath.Clean(path) != filepath.Clean(target) {
			paths = append(paths, target)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("preflight staged artifacts: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *artifactStage) installTree(skipFile, skipDir string, preserve []string, stagedArtifactDir, finalArtifactDir string) (bool, error) {
	if s.inPlace {
		if _, err := s.fs.Stat(s.root); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("inspect staged artifact root: %w", err)
		}
	}
	paths := make([]string, 0)
	if err := afero.Walk(s.fs, s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		clean := filepath.Clean(path)
		if skipFile != "" && clean == filepath.Clean(skipFile) {
			return nil
		}
		if skipDir != "" {
			prefix := filepath.Clean(skipDir) + string(filepath.Separator)
			if strings.HasPrefix(clean, prefix) {
				return nil
			}
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return false, fmt.Errorf("walk staged artifacts: %w", err)
	}
	sort.Strings(paths)
	return s.installPaths(paths, preserve, stagedArtifactDir, finalArtifactDir)
}

func (s *artifactStage) installPaths(paths, preserve []string, stagedArtifactDir, finalArtifactDir string) (bool, error) {
	type installPath struct {
		source, target string
		skip           bool
		replace        bool
		sharedOwner    bool
	}
	plans := make([]installPath, 0, len(paths))
	preserved := false

	// Preflight every deterministic path and type check before publication. The
	// later filesystem operations can still fail because of I/O errors or races.
	for _, source := range paths {
		target, err := s.publicationPath(source, stagedArtifactDir, finalArtifactDir)
		if err != nil {
			return false, err
		}
		sourceInfo, statErr := s.fs.Stat(source)
		if statErr != nil {
			return false, fmt.Errorf("inspect staged artifact %s: %w", source, statErr)
		}
		if !sourceInfo.Mode().IsRegular() {
			return false, fmt.Errorf("staged artifact is not a regular file: %s", source)
		}

		plan := installPath{source: source, target: target}
		if coordinator := s.original.ArtifactCoordinator; coordinator != nil {
			digest, digestErr := artifactDigest(s.fs, source)
			if digestErr != nil {
				return false, digestErr
			}
			claim, claimErr := coordinator.Claim(s.publishCtx, target, digest, s.original.ArtifactOwnerKey)
			if claimErr != nil {
				return false, claimErr
			}
			if claim.OwnsPublication() {
				s.sharedClaims = append(s.sharedClaims, claim)
				plan.sharedOwner = true
			} else {
				plan.skip = true
				s.sharedConsumers = append(s.sharedConsumers, claim)
			}
		}
		if filepath.Clean(source) == filepath.Clean(target) {
			plan.skip = true
			if !s.original.OverwriteExistingMedia && containsPath(preserve, source) {
				preserved = true
			}
			plans = append(plans, plan)
			continue
		}
		if plan.skip {
			plans = append(plans, plan)
			continue
		}
		if info, targetErr := s.fs.Stat(target); targetErr == nil {
			if info.IsDir() {
				return false, fmt.Errorf("artifact destination is a directory: %s", target)
			}
			if !s.original.OverwriteExistingMedia && containsPath(preserve, source) {
				plan.skip = true
				preserved = true
			} else {
				plan.replace = true
			}
		} else if !os.IsNotExist(targetErr) {
			return false, fmt.Errorf("inspect artifact destination %s: %w", target, targetErr)
		}
		for parent := filepath.Dir(target); parent != "."; parent = filepath.Dir(parent) {
			info, parentErr := s.fs.Stat(parent)
			if parentErr == nil {
				if !info.IsDir() {
					return false, fmt.Errorf("artifact destination parent is not a directory: %s", parent)
				}
				break
			}
			if !os.IsNotExist(parentErr) {
				return false, fmt.Errorf("inspect artifact destination parent %s: %w", parent, parentErr)
			}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
		}
		plans = append(plans, plan)
	}

	for _, plan := range plans {
		if plan.skip {
			continue
		}
		if err := s.fs.MkdirAll(filepath.Dir(plan.target), 0o755); err != nil {
			return false, fmt.Errorf("create artifact destination: %w", err)
		}
		if plan.sharedOwner {
			s.sharedPublishBegan = true
		}
		if s.publishBatch != nil {
			if _, err := s.publishBatch.BeforePublish(s.publishCtx, plan.target, plan.replace); err != nil {
				return false, err
			}
		} else if plan.replace {
			if removeErr := s.fs.Remove(plan.target); removeErr != nil {
				return false, fmt.Errorf("replace artifact destination %s: %w", plan.target, removeErr)
			}
		}
		if err := s.fs.Rename(plan.source, plan.target); err != nil {
			return false, fmt.Errorf("publish staged artifact %s: %w", plan.target, err)
		}
		if s.publishBatch != nil {
			if err := s.publishBatch.ConfirmPublish(s.publishCtx, plan.target); err != nil {
				return false, err
			}
		}
	}
	return preserved, nil
}

func (s *artifactStage) publicationPath(path, stagedArtifactDir, finalArtifactDir string) (string, error) {
	target, err := s.finalPath(path)
	if err != nil {
		return "", err
	}
	if stagedArtifactDir == "" || finalArtifactDir == "" {
		return target, nil
	}
	rel, _ := filepath.Rel(stagedArtifactDir, path)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return target, nil
	}
	return filepath.Join(finalArtifactDir, rel), nil
}

func artifactDigest(fs afero.Fs, path string) (string, error) {
	file, err := fs.Open(path)
	if err != nil {
		return "", fmt.Errorf("open staged artifact for digest %s: %w", path, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("digest staged artifact %s: %w", path, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close staged artifact digest %s: %w", path, closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *artifactStage) sharedCompletionDisposition(panicked bool, err error) SharedArtifactCompletionDisposition {
	if !panicked && err == nil {
		return SharedArtifactPublished
	}
	if s.sharedPoisoned || (panicked && s.sharedPublishBegan) {
		return SharedArtifactPoisoned
	}
	return SharedArtifactSafeToPromote
}

func (s *artifactStage) finishSharedArtifactClaims(disposition SharedArtifactCompletionDisposition) {
	coordinator := s.original.ArtifactCoordinator
	for _, claim := range s.sharedClaims {
		coordinator.Complete(claim, disposition)
	}
	s.sharedClaims = nil
}

func (s *artifactStage) isSharedConsumer(path string) bool {
	requested := filepath.Clean(path)
	for _, claim := range s.sharedConsumers {
		if claim.requested == requested {
			return true
		}
	}
	return false
}

func containsPath(paths []string, path string) bool {
	clean := filepath.Clean(path)
	for _, candidate := range paths {
		if filepath.Clean(candidate) == clean {
			return true
		}
	}
	return false
}
