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
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
)

// videoStagingDeferred marks organize-mode stages whose source video was
// deliberately NOT copied into the staging tree: the fenced publication
// moves (or copies) it directly from its real path instead, so organizing a
// multi-GB file never duplicates the payload into a hidden sibling folder.
var observeBoundSidecarPublish = (*downloader.ReplacementBatch).ObservePublishResultBound

// observeBoundPrimaryPublish is the primary lane's bound observation (codex
// P1, PRRT_kwDORn9KaM6p3Dq1) — a package-level seam so the non-successor
// error leg stays testable, exactly like observeBoundSidecarPublish.
var observeBoundPrimaryPublish = (*downloader.ReplacementBatch).ObservePublishResultBound

func (s *artifactStage) videoStagingDeferred() bool { return s != nil && s.videoDeferred }

type artifactPlanExecutor interface {
	PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error)
	PlanSourceExists(*organizer.OrganizePlan) bool
	ExecuteOrganizePlan(*organizer.OrganizePlan, bool, organizer.LinkMode) (*organizer.OrganizeResult, error)
	PlanSubtitleMoves(*organizer.OrganizePlan) []models.SubtitleMove
}

// artifactFilenamePolicy exposes the effective per-command filename policy.
type artifactFilenamePolicy interface {
	PreservesOriginalFilenames(forceRename bool) bool
}

// artifactSubtitleSelector exposes the organizer's configured source policy
// independently of destination planning and its probe/admission freeze.
// Organizers without this seam do not authorize subtitle staging.
type artifactSubtitleSelector interface {
	SubtitleSourcePaths(models.FileMatchInfo) []string
}

type artifactSibling struct {
	sourcePath string
	stagedPath string
	identity   artifactSourceIdentity
	// subtitle records configured admission independently of execution results.
	subtitle bool
	// deferred marks a multipart video sibling admitted without staged bytes:
	// it installs by the verified move lane straight off its real source at
	// publication, like the deferred primary (codex P2, PRRT_kwDORn9KaM6rLI28).
	deferred bool
}

type artifactStage struct {
	preserveSiblingNames bool

	fs                 afero.Fs
	fencer             database.ApplyArtifactPublicationFencer
	root               string
	finalRoot          string
	sourcePath         string
	sourceIdentity     artifactSourceIdentity
	sourceDirIdentity  artifactStageDirIdentity
	directoryMove      *artifactInPlaceDirectoryMove
	stagedSource       string
	siblings           []artifactSibling
	inPlace            bool
	inPlaceResident    bool
	videoDeferred      bool
	original           ApplyCmd
	rejected           bool
	duplicatePlan      *organizer.OrganizePlan
	publishBatch       *downloader.ReplacementBatch
	publishCtx         context.Context
	completedBatch     *downloader.ReplacementBatch
	unresolvedBatch    *downloader.ReplacementBatch
	sourceCleanupArmed bool
	directOriginArmed  bool
	sharedClaims       []SharedArtifactClaim
	sharedConsumers    []SharedArtifactClaim
	sharedPublishBegan bool
	sharedPoisoned     bool
	// unownedSkips tracks destinations this stage did not publish, including
	// preserved existing media and identical artifacts. Skipping publication
	// never grants ownership of the occupant for the revert delete ledger.
	unownedSkips map[string]bool
	// Staged subtitle inputs already handled by the initial organizer must
	// not become independent outputs during the later tree publication.
	excludedStagedSubtitleInputs map[string]bool
	stagedSubtitleOutputs        map[string]int
	stagedSubtitleResults        []organizer.SubtitleResult
	// Only admitted source videos use the no-clobber tree lane. The value
	// distinguishes the requested primary from automatically included parts.
	stagedVideoOutputs map[string]bool
	stagedVideoSources map[string]artifactSibling
	publishVideoLog    RevertLog
	publishVideoOpID   OperationID

	// Resident primary ownership stays separate from unresolved sidecar moves.
	residentSidecarMutation bool
	residentPrimary         bool
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
	cmd = normalizeInPlaceLinkMode(cmd)
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
	root, err := o.allocateArtifactStageForApply(ctx, finalRoot, cmd, duplicatePlan)
	if err != nil {
		if duplicatePlan != nil {
			cmd.Organize.DuplicateTracker.ReleaseClaim(duplicatePlan.SourcePath, duplicatePlan.TargetPath)
		}
		return nil, cmd, err
	}
	writeArtifactStageManifest(o.fs, root)
	stage := &artifactStage{fs: o.fs, fencer: artifactFencer(cmd), root: root, finalRoot: finalRoot, sourcePath: cmd.Match.Path, inPlace: inPlace, original: cmd, duplicatePlan: duplicatePlan}
	if policy, ok := o.organizer.(artifactFilenamePolicy); ok {
		stage.preserveSiblingNames = policy.PreservesOriginalFilenames(cmd.Organize.ForceRenameFile)
	}
	stagedCmd := cmd
	if duplicatePlan != nil {
		stagedCmd.Organize.DuplicateTracker = nil
	}
	if !cmd.Organize.Skip {
		if sourcePath == "" {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging requires a source path")
		}
		// Admission is no-follow: the pin must name the directory entry the
		// deferred publication later moves — a symlink source would publish the
		// link object (not its bytes), so it fails the regularity gate like any
		// other non-regular entry instead of being admitted.
		sourceInfo, statErr := lstatArtifactSource(o.fs, sourcePath)
		if statErr != nil {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging source: %w", statErr)
		}
		if !sourceInfo.Mode().IsRegular() {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging blocked for non-regular source %s", cmd.Match.Path)
		}
		// Pin the admitted source identity: the deferred publication consumes
		// sourcePath directly after the merge/download/NFO interval and must
		// re-prove it still names THIS file before any byte moves or copies.
		stage.sourceIdentity = captureArtifactSourceIdentity(o.fs, sourcePath, sourceInfo)
		if inPlace && cmd.Organize.MoveFiles {
			dir := filepath.Dir(sourcePath)
			dirInfo, dirErr := lstatArtifactStageDir(o.fs, dir)
			if dirErr != nil {
				stage.cleanup()
				return nil, cmd, fmt.Errorf("admit in-place source directory: %w", dirErr)
			}
			stage.sourceDirIdentity = captureArtifactStageDirIdentity(o.fs, dir, dirInfo)
		}
		base := filepath.Base(sourcePath)
		stagedDir := filepath.Join(root, ".source")
		if inPlace && !cmd.Organize.MoveFiles {
			stagedDir = root
		}
		stage.stagedSource = filepath.Join(stagedDir, base)

		entries, readErr := afero.ReadDir(o.fs, filepath.Dir(sourcePath))
		if readErr != nil {
			stage.cleanup()
			return nil, cmd, fmt.Errorf("artifact staging source directory: %w", readErr)
		}
		if err := validateArtifactVideoSiblingNames(base, entries, stage.preserveSiblingNames); err != nil {
			stage.cleanup()
			return nil, cmd, err
		}
		if inPlace && !cmd.Organize.MoveFiles {
			if err := copyArtifactFile(o.fs, sourcePath, stage.stagedSource, sourceInfo.Mode().Perm()); err != nil {
				stage.cleanup()
				return nil, cmd, err
			}
		} else {
			// Organize mode defers the video: the fenced publication moves (or
			// copies) it directly from its real path, so a same-volume "move" is
			// a rename again instead of a full copy through .javinizer-apply-*.
			stage.videoDeferred = true
		}
		selectedSubtitles := map[string]bool{}
		if selector, ok := o.organizer.(artifactSubtitleSelector); ok {
			for _, path := range selector.SubtitleSourcePaths(cmd.Match) {
				selectedSubtitles[filepath.Clean(path)] = true
			}
		}
		for _, entry := range entries {
			sibling := filepath.Join(filepath.Dir(sourcePath), entry.Name())
			if entry.Name() == base || !entry.Mode().IsRegular() ||
				(!selectedSubtitles[filepath.Clean(sibling)] && !isStagedArtifactVideoSibling(base, entry.Name())) {
				continue
			}
			// No-follow like the video admission: a symlinked sibling is skipped,
			// never pinned as a source the publication may consume.
			siblingInfo, siblingErr := lstatArtifactSource(o.fs, sibling)
			if siblingErr != nil {
				stage.cleanup()
				return nil, cmd, fmt.Errorf("artifact staging sibling %s: %w", sibling, siblingErr)
			}
			if !siblingInfo.Mode().IsRegular() {
				continue
			}
			admitted := artifactSibling{sourcePath: sibling, identity: captureArtifactSourceIdentity(o.fs, sibling, siblingInfo), subtitle: selectedSubtitles[filepath.Clean(sibling)]}
			_, siblingExtension := artifactMatchName(entry.Name())
			if cmd.Organize.MoveFiles && isStagedArtifactVideoExtension(strings.ToLower(siblingExtension)) {
				// Defer multipart video siblings with the primary in MOVE flows
				// (codex P2, PRRT_kwDORn9KaM6rLI28): the publication installs
				// them straight off the real source by the verified move lane,
				// so staging their bytes would restore exactly the
				// multi-gigabyte copy this mode exists to eliminate. Copy/link
				// flows keep the staged copy — their source must be retained.
				admitted.deferred = true
				stage.siblings = append(stage.siblings, admitted)
				continue
			}
			stagedSibling := filepath.Join(stagedDir, entry.Name())
			if err := copyArtifactFile(o.fs, sibling, stagedSibling, siblingInfo.Mode().Perm()); err != nil {
				stage.cleanup()
				return nil, cmd, err
			}
			admitted.stagedPath = stagedSibling
			stage.siblings = append(stage.siblings, admitted)
		}
		stagedCmd.Match.Path = stage.stagedSource
		stagedCmd.Match.Name, stagedCmd.Match.Extension = artifactMatchName(base)
	}
	stagedCmd.DestPath = root
	return stage, stagedCmd, nil
}

// artifactMatchName reconstructs the scanner's FileMatchInfo spelling contract
// after a path changes: keep the current raw stem, fold only the extension.
// Never copy the old Name: organizing within the stage may have renamed it.
// This mirrors scanner.foldNameExtension and fileExtension; Path stays raw.
func artifactMatchName(base string) (name, extension string) {
	folded := foldArtifactASCII(base)
	extension = filepath.Ext(folded)
	if extension == "" || folded == base {
		return base, extension
	}
	// Folding maps rune-to-rune, so the raw extension occupies the same
	// number of runes even when its fullwidth spelling occupies more bytes.
	runes := []rune(base)
	return string(runes[:len(runes)-len([]rune(extension))]) + extension, extension
}

// foldArtifactASCII is for matching only; physical source paths stay raw.
func foldArtifactASCII(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		default:
			return r
		}
	}, name)
}

// Reject ambiguous multipart destinations before staging any source bytes.
// Width/case normalization is many-to-one, so distinct regular directory
// entries must not compete for one target. The staging loop skips the same
// nonregular snapshot entries and re-proves regular sources without following links.
func validateArtifactVideoSiblingNames(sourceName string, entries []os.FileInfo, preserveNames bool) error {
	targets := make(map[string]string)
	for _, entry := range entries {
		if !entry.Mode().IsRegular() || !isStagedArtifactVideoSibling(sourceName, entry.Name()) {
			continue
		}
		targetName := entry.Name()
		if !preserveNames {
			targetName = stagedArtifactSiblingName(sourceName, sourceName, targetName)
		}
		target := strings.ToLower(targetName)
		if previous, exists := targets[target]; exists {
			return fmt.Errorf("ambiguous multipart destination: %s and %s", previous, entry.Name())
		}
		targets[target] = entry.Name()
	}
	return nil
}

func isStagedArtifactVideoSibling(sourceName, siblingName string) bool {
	sourceName, sourceExt := artifactMatchName(sourceName)
	siblingName, siblingExt := artifactMatchName(siblingName)
	sourceStem := strings.ToLower(foldArtifactASCII(strings.TrimSuffix(sourceName, sourceExt)))
	siblingStem := strings.ToLower(foldArtifactASCII(strings.TrimSuffix(siblingName, siblingExt)))
	siblingExt = strings.ToLower(siblingExt)
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
	stagedInfo, stagedInfoErr := lstatArtifactStageDir(s.fs, s.root)
	if os.IsNotExist(stagedInfoErr) {
		return
	}
	if stagedInfoErr != nil || stagedInfo == nil || stagedInfo.Mode()&os.ModeSymlink != 0 || !stagedInfo.IsDir() {
		logging.Warnf("artifact staging cleanup retained %s: directory identity unavailable: %v", s.root, stagedInfoErr)
		return
	}
	stagedIdentity := captureArtifactStageDirIdentity(s.fs, s.root, stagedInfo)
	if !stagedIdentity.known {
		logging.Warnf("artifact staging cleanup retained %s: strong directory identity unavailable", s.root)
		return
	}
	manifest, authenticated := readArtifactStageAuthenticatedManifest(s.fs, s.root)
	if !authenticated || !manifest.Directory.matchesIdentity(stagedIdentity) {
		logging.Warnf("artifact staging cleanup retained %s: authenticated directory ownership unavailable", s.root)
		return
	}
	markArtifactStageCompleted(s.fs, s.root, manifest.Directory.identity())
	writeArtifactStageProof(s.fs, s.root)
	proofSrc := artifactStageProofPath(s.root)
	if !artifactStageDirMatches(s.fs, s.root, stagedIdentity) {
		_ = s.fs.Remove(proofSrc)
		logging.Warnf("artifact staging cleanup retained %s: directory identity changed while writing ownership proof", s.root)
		return
	}
	originalRoot := s.root
	quarantine := artifactStageQuarantineName(s.root)
	proofDst := artifactStageProofPath(quarantine)
	if err := s.fs.Rename(s.root, quarantine); err != nil {
		logging.Warnf("artifact staging cleanup retention %s: quarantine rename failed, retained for the next organize sweep: %v", s.root, err)
		return
	}
	s.root = quarantine
	if !artifactStageDirMatches(s.fs, s.root, stagedIdentity) || artifactStageDirStillNamed(s.fs, originalRoot) {
		_ = s.fs.Remove(proofSrc)
		logging.Warnf("artifact staging cleanup retained %s: quarantined root identity changed during the rename", s.root)
		return
	}
	if _, statErr := s.fs.Stat(proofSrc); statErr == nil {
		if err := s.fs.Rename(proofSrc, proofDst); err != nil {
			logging.Warnf("artifact staging cleanup proof carry %s denied; restating beside the quarantined root: %v", proofSrc, err)
			writeArtifactStageProof(s.fs, s.root)
		}
	} else {
		writeArtifactStageProof(s.fs, s.root)
	}
	if !readArtifactStageProof(s.fs, s.root) {
		logging.Warnf("artifact staging cleanup retention %s: ownership proof unavailable beside the quarantined root, retained for the next organize sweep", s.root)
		return
	}
	if err := removeArtifactTreeWithRetry(s.fs, s.root, stagedIdentity); err != nil {
		logging.Warnf("artifact staging cleanup retained %s: %v", s.root, err)
		return
	}
	_ = s.fs.Remove(proofSrc)
	_ = s.fs.Remove(proofDst)
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
		rollbackErr := s.completedBatch.Rollback(context.WithoutCancel(ctx))
		if s.residentPrimary {
			state.publicationRecovery = s.completedBatch.RecoverySnapshot()
		}
		if rollbackErr != nil {
			s.sharedPoisoned = true
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback staged publication after fence failure: %w", rollbackErr))
		} else if directoryErr := s.rollbackInPlaceDirectory(context.WithoutCancel(ctx), o, state); directoryErr != nil {
			s.sharedPoisoned = true
			returnErr = errors.Join(returnErr, directoryErr)
		} else {
			s.clearRestoredResidentSidecars()
			if s.directOriginArmed {
				s.sourceCleanupArmed = false
				s.directOriginArmed = false
			}
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
	s.publishVideoLog, s.publishVideoOpID = o.revertLog, opID
	committed := false
	createdFinalParent := ""
	defer func() {
		s.publishBatch, s.publishCtx = nil, nil
		s.publishVideoLog, s.publishVideoOpID = nil, ""
		if !committed {
			rollbackErr := batch.Rollback(context.WithoutCancel(ctx))
			if s.residentPrimary {
				state.publicationRecovery = batch.RecoverySnapshot()
			}
			if rollbackErr != nil {
				s.unresolvedBatch = batch
				s.sharedPoisoned = true
				returnErr = errors.Join(returnErr, fmt.Errorf("rollback staged publication: %w", rollbackErr))
			} else if directoryErr := s.rollbackInPlaceDirectory(context.WithoutCancel(ctx), o, state); directoryErr != nil {
				s.sharedPoisoned = true
				returnErr = errors.Join(returnErr, directoryErr)
			} else {
				s.clearRestoredResidentSidecars()
				if s.directOriginArmed {
					// The rollback moved the video back onto the source path: the
					// apply terminates pre-publication despite the armed marker.
					s.sourceCleanupArmed = false
					s.directOriginArmed = false
				}
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
	sidecarIntentTargets := []string{}
	copiedSidecarTargets := map[string]bool{}
	// copiedSidecarIdentities carries each copy-installed sidecar's
	// publish-time destination identity (the verified copy's proven output,
	// finding ntCe6): the observation loop below binds THOSE identities
	// instead of re-resolving destination names the organizer already
	// unlocked.
	copiedSidecarIdentities := map[string]*fsutil.BoundInstallIdentity{}
	// A resident alias is user-owned: neither replacement nor inverse/delete
	// intents may claim it. Its frozen execution proof forbids a late install.
	primaryResident := false
	// primaryPinRecorded tracks ANY pre-execute durable pin on the deferred
	// primary target (its reconcile must run even with zero sidecars);
	// primaryPinCopyPartial narrows it to the copy lane's interim partial
	// pin, the only shape the publish's stream-teed digest later seals.
	primaryPinRecorded := false
	primaryPinCopyPartial := false
	// primarySuccessorRefused records that the bound observation proved another
	// writer replaced the primary install (codex P1, PRRT_kwDORn9KaM6p6mdQ): no
	// later leg may re-adopt the occupant by name, so the unconditional primary
	// confirmation is skipped and the move-lane inverse is not armed against a
	// destination this apply no longer owns.
	primarySuccessorRefused := false
	// The video leg moves the real source whenever the publish call carries
	// move semantics: explicit MoveFiles, or any flow where ExecuteOrganizePlan
	// would still rename (an irrelevant link_mode must not disarm intents).
	publishMove := s.original.Organize.MoveFiles || s.original.Organize.LinkMode == organizer.LinkModeNone
	if s.videoDeferred {
		publishMove = s.original.Organize.MoveFiles
	}
	if !s.original.Organize.Skip {
		if state.organizeResult == nil {
			return fmt.Errorf("artifact publication has no organize result")
		}
		if state.organizeResult.DuplicateSkipped {
			return nil
		}
		if err := s.relocateInPlaceDirectory(ctx, o, opID, state); err != nil {
			return err
		}
		if err := s.collectStagedSubtitleOutputs(state.organizeResult); err != nil {
			return err
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
		if s.videoDeferred || (!s.original.Organize.MoveFiles && s.original.Organize.LinkMode != organizer.LinkModeNone) {
			match.Path = s.sourcePath
		}
		match.Name, match.Extension = artifactMatchName(filepath.Base(match.Path))
		organizeCmd := organizer.OrganizeCmd{Match: match, Movie: s.original.Movie, DestDir: s.finalRoot, ForceUpdate: s.original.Organize.ForceUpdate, MoveFiles: s.original.Organize.MoveFiles, LinkMode: s.original.Organize.LinkMode, OperationMode: s.original.OperationMode, ForceRenameFile: s.original.Organize.ForceRenameFile}
		if s.inPlace && !s.videoDeferred {
			// The real final directory was resolved before staging. Keep this
			// execute within the stage; fenced tree publication applies the move.
			organizeCmd.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
			organizeCmd.StagingFinalDir = s.finalRoot
		}
		plan, err := executor.PlanOrganize(ctx, organizeCmd)
		if err != nil {
			return fmt.Errorf("replan artifact publication: %w", err)
		}
		if !executor.PlanSourceExists(plan) {
			return fmt.Errorf("artifact publication staged source disappeared: %s", stagedVideo)
		}
		artifactDestinations, preflightErr := s.treeDestinations(stagedVideo, filepath.Dir(s.stagedSource), filepath.Dir(stagedVideo), plan.TargetDir)
		if s.inPlace {
			videoInstalledByTree = !s.videoDeferred && filepath.Clean(plan.SourcePath) == filepath.Clean(plan.TargetPath)
			skipVideo, skipSource := stagedVideo, ""
			if s.videoDeferred {
				if err := s.setInPlaceFinalRoot(plan.TargetDir); err != nil {
					return err
				}
				skipSource = filepath.Dir(s.stagedSource)
			}
			if videoInstalledByTree {
				skipVideo = ""
			}
			artifactDestinations, preflightErr = s.treeDestinations(skipVideo, skipSource, "", "")
		}
		if preflightErr != nil {
			return preflightErr
		}
		if err := batch.Preflight(append([]string{plan.TargetPath}, artifactDestinations...)); err != nil {
			return err
		}
		// A stationary exact-path move owns no primary. Directory relocation
		// remains owned work and must retain its existing inverse.
		stationaryPrimary := s.original.Organize.MoveFiles && s.directoryMove == nil && filepath.Clean(plan.SourcePath) == filepath.Clean(plan.TargetPath)
		if s.videoDeferred && filepath.Clean(plan.SourcePath) == filepath.Clean(s.sourcePath) && (stationaryPrimary || ((!s.inPlace || s.original.Organize.MoveFiles) && filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath))) {
			primaryResident, err = s.stagedVideoResident("", plan.TargetPath, true)
			if err != nil {
				return err
			}
			// The directory checkpoint froze whether its inverse owns the primary.
			// Neither transition may replace that durable ownership contract.
			if s.directoryMove != nil && s.inPlaceResident != primaryResident {
				if !s.inPlaceResident {
					// Failure must retain the relocated source anchor, rather than
					// the earlier staging result, if directory rollback is blocked.
					state.organizeResult = &organizer.OrganizeResult{OriginalPath: s.sourcePath, NewPath: s.sourcePath, FolderPath: plan.TargetDir, Moved: true}
					s.annotateInPlaceDirectory(state.organizeResult)
				}
				return fmt.Errorf("resident primary changed after directory relocation")
			}
			if primaryResident {
				s.residentPrimary = true
				plan.BindResidentSource(s.deferredSourceProof())
				// A crash after a sidecar install must already know that this
				// move row has no primary anchor. Checkpoint before intents
				// can be consumed; normal completion preserves this marker.
				if o.revertLog != nil && opID != "" {
					resident := &organizer.OrganizeResult{PrimaryResident: true, OriginalPath: s.sourcePath, FolderPath: plan.TargetDir}
					if err := o.revertLog.Complete(ctx, opID, &ApplyResult{OrganizeResult: resident, Movie: state.movie, OperationID: opID, PublicationPending: true}); err != nil {
						return fmt.Errorf("persist resident primary before sidecar publication: %w", err)
					}
				}
			}
		}
		if !primaryResident && filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) {
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
		if s.inPlace && s.videoDeferred {
			plan.InPlace, plan.RenameFolder = false, false
		}
		// Bind the prepare-time admission BEFORE the first subtitle probe
		// (codex P2, PRRT_kwDORn9KaM6nnjvh): the probes below rescan the
		// source directory by name, so a regular subtitle materializing after
		// prepareArtifact's sibling scan would otherwise be frozen into the
		// plan's probe admission and journaled without an admitted identity or
		// a verified-source proof — a rename-swap before handleSubtitles would
		// then make move mode consume (or copy mode publish) a different file
		// than admission ever saw. The exclusion leg is deliberate: binding
		// identity for a file the admission gate never took would require a
		// fresh pre-execution proof, opening new attack surface. Only the
		// deferred real-source plan is bound, mirroring the proof bindings
		// below; staged/in-place plans enumerate staging-owned sources and
		// keep the rescan enumeration.
		if s.videoDeferred && filepath.Clean(plan.SourcePath) == filepath.Clean(s.sourcePath) {
			plan.BindSubtitleAdmissionSet(s.admittedSubtitleSources())
		}
		// Persist the intended inverse BEFORE the move consumes the real
		// source: a crash in the rename→record window must still leave a durable
		// source→destination trail (Begin cannot name it — the plan is only
		// final after the conflict guards). This is a pending intent — recovery
		// reads it via the MoveBack channel and can tell it apart from an
		// executed move (no completion columns are touched).
		if s.videoDeferred && publishMove && filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) && o.revertLog != nil && opID != "" {
			if !primaryResident {
				if err := o.revertLog.RecordMoveIntent(ctx, opID, s.directoryOriginalPath(plan.SourcePath), plan.TargetPath); err != nil {
					return fmt.Errorf("persist inverse before direct video publication: %w", err)
				}
			}
			// Subtitles move inside the same execution: their endpoints are known
			// from the plan, so the pending intent lands BEFORE the consume too.
			// Two sources can normalize onto ONE endpoint (e.g. .en.srt and
			// .eng.srt both become .eng.srt): the organizer's sequential lane
			// moves the FIRST planned source and skips the rest, so only the
			// first-planned entry per endpoint journals — the same first-wins
			// dedupe the copy lane's arming applies (codex P2,
			// PRRT_kwDORn9KaM6m7CBR). The lane also ENFORCES the first-wins
			// claim: once the first source attempts the endpoint, later
			// duplicates are refused even when that attempt failed before
			// publishing, so the single journaled source is the only one that
			// can ever install into it (codex P2, PRRT_kwDORn9KaM6nmSaI). A
			// second pending intent would survive
			// execution as a never-consumed row whose source is still present,
			// and its rename-suppression keys on the SHARED destination: in the
			// execute→reconcile crash window the winner's move-back would stay
			// suppressed and its bytes stranded at the destination (codex P2,
			// PRRT_kwDORn9KaM6m_lgt). The skipped source keeps no row at all —
			// execution leaves it in place, and the outcome reconcile never has
			// it to retract.
			journaledSubtitleEndpoints := map[string]bool{}
			for _, mv := range executor.PlanSubtitleMoves(plan) {
				if journaledSubtitleEndpoints[filepath.Clean(mv.NewPath)] {
					continue
				}
				journaledSubtitleEndpoints[filepath.Clean(mv.NewPath)] = true
				if err := o.revertLog.RecordMoveIntent(ctx, opID, s.directoryOriginalPath(mv.OriginalPath), mv.NewPath); err != nil {
					return fmt.Errorf("persist inverse before subtitle publication: %w", err)
				}
			}
		}
		// ExecuteOrganizePlan reports subtitles without installing them when the
		// primary plan is a no-op. Do not claim or pin those planned endpoints:
		// they remain unowned, and the publish reconciliation has no install.
		if s.videoDeferred && !publishMove && plan.WillMove {
			// Copy/link executions install sidecars directly into the destination
			// during execute. Arm each as a rollback-tracked created output now, or
			// a later failed leg would strand an untracked copy at the destination.
			// Two source subtitles can normalize onto ONE endpoint (e.g. .en.srt
			// and .eng.srt both become .eng.srt): the organizer's sequential lane
			// installs the FIRST planned source and skips the rest, so only the
			// first-planned entry per endpoint arms — a second BeforePublish would
			// collide with the first's own live busy claim and fail the whole
			// apply (codex P2, PRRT_kwDORn9KaM6m7CBR). The execute lane likewise
			// refuses later duplicates even after a failed first attempt (codex
			// P2, PRRT_kwDORn9KaM6nmSaI), so the pinned source stays the only
			// possible installer of the armed endpoint. The single durable pin
			// then carries the first source's digest, matching the bytes execute
			// lands and the reconciler's one-pin-per-endpoint keep-set.
			armedSidecarEndpoints := map[string]bool{}
			for _, mv := range executor.PlanSubtitleMoves(plan) {
				if armedSidecarEndpoints[filepath.Clean(mv.NewPath)] {
					continue
				}
				armedSidecarEndpoints[filepath.Clean(mv.NewPath)] = true
				if _, err := batch.BeforePublish(ctx, mv.NewPath, false); err != nil {
					return fmt.Errorf("arm copy-installed sidecar %s: %w", mv.NewPath, err)
				}
				sidecarIntentTargets = append(sidecarIntentTargets, mv.NewPath)
				// The copy install writes the source's bytes verbatim: pin the
				// delete intent before execute can place them so a crash between
				// execute and completion still finds a hash-proofed entry.
				if o.revertLog != nil && opID != "" {
					digest, digestErr := artifactDigest(s.fs, mv.OriginalPath)
					if digestErr != nil {
						return fmt.Errorf("arm copy-installed sidecar digest %s: %w", mv.NewPath, digestErr)
					}
					if err := o.revertLog.RecordDeleteIntent(ctx, opID, []models.DeleteEntry{{Path: mv.NewPath, SHA256: digest}}); err != nil {
						return fmt.Errorf("record copy-installed sidecar intent %s: %w", mv.NewPath, err)
					}
				}
				// The organizer's subtitle lane locks the same destination key
				// during execute; hand the in-process hold over exactly like the
				// video lane does for the video plan target.
				batch.YieldToLockedPublisher(mv.NewPath)
			}
			// The PRIMARY leg analogue of the sidecar pins: execute can publish the
			// video itself, yet an absent destination has no other durable trail
			// (an occupied one graduated through BeforePublish's replacement
			// journal). Pin the install against the target BEFORE execute so an
			// interrupted row's revert can still attribute exactly what this apply
			// landed, WITHOUT the content-hash pin's second
			// streaming read of the whole payload (codex P2,
			// PRRT_kwDORn9KaM6nBUrF): the proof shape keys on the publication's
			// link mode — a hard link IS the admitted source's object (its
			// identity tuple is the ownership certificate), a soft link's entire
			// payload is its target string (readlink authenticates it — a hash
			// pin could never fire on the non-regular entry at all, codex P2
			// PRRT_kwDORn9KaM6nBUq8), and only a byte-streaming copy carries a
			// content proof — the bounded head+tail interim digest, sealed to
			// the full sha256 the publish stream tees once the install lands. The
			// unsealed interim is intent only: recovery retains it rather than
			// unlinking on the bounded proof (codex P1, PRRT_kwDORn9KaM6novbT).
			// The confirmed publish below consumes this pin: a copy/link primary
			// is user-owned once installed and this row's revert retains it.
			if !primaryResident && filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) && !finalReplaced && o.revertLog != nil && opID != "" {
				entry, pinErr := s.deferredPrimaryDeleteEntry(plan)
				if pinErr != nil {
					return pinErr
				}
				if err := o.revertLog.RecordDeleteIntent(ctx, opID, []models.DeleteEntry{entry}); err != nil {
					return fmt.Errorf("record deferred primary copy intent %s: %w", plan.TargetPath, err)
				}
				primaryPinRecorded = true
				primaryPinCopyPartial = entry.CopyPartialSHA256 != ""
			}
		}
		// Fail closed one Stat before the execution consumes the real source
		// paths: a source replaced or rewritten during the merge/download/NFO
		// interval must abort here — publishing it would land bytes the
		// metadata/NFO was never generated for (or destroy a foreign original).
		if err := s.revalidateDirectSources(executor, plan); err != nil {
			return err
		}
		// …and bind the validation to the publication act itself (codex P1,
		// PRRT_kwDORn9KaM6m9ae4): the gate above and the organizer's consume
		// (rename or open) remain separate filesystem operations, so the
		// executed plan carries the admitted identity into the no-replace
		// legs — the move take-aside re-proves the claimed object before
		// publishing, the copy leg proves the very handle it streams, and the
		// hard-link leg re-proves the installed entry against the same
		// admission identity (link(2) resolves its source by name, so the
		// post-link alias proof is what refuses a swap that won the
		// validation→link window — codex P1, PRRT_kwDORn9KaM6nEnUw). Only the
		// deferred real-source plan is bound; staged/in-place plans publish
		// staging-owned copies the staging sweep already owns.
		if s.videoDeferred && filepath.Clean(plan.SourcePath) == filepath.Clean(s.sourcePath) {
			if proof := s.deferredSourceProof(); proof != nil {
				plan.BindVerifiedSource(proof)
				// The admitted source's permission bits ride the same bind (codex P2,
				// PRRT_kwDORn9KaM6pw_EY): before the video was deferred this flow staged a
				// copy AT the admitted mode and the publication renamed it into place, so
				// the direct consume must publish those bits rather than the
				// umask-masked staging default — a 0640 or read-only source stays private.
				plan.BindCopySourcePerm(s.sourceIdentity.perm)
				// Serve the copy lane's interim partial pin: the verified
				// copy tees the payload's sha256 off its single publish
				// stream, and the post-execute seal upgrades the pin with it
				// (never a second read). Link lanes ignore the flag — their
				// pins carry no content digest by construction.
				plan.BindCopyDigestCapture()
			}
			// The subtitle lane binds per endpoint the same way (codex P1,
			// PRRT_kwDORn9KaM6m_lgp): the probe freeze already decided WHICH
			// sources may execute; these proofs bind WHICH OBJECT each executed
			// byte stream carries, closing the admitted→consumed window the
			// gate above cannot (its check and the move/copy remain separate
			// filesystem acts for sidecars exactly as for the video). Keys are
			// cleaned REAL source paths, so only lanes consuming real sources
			// (this deferred plan) consult them — staged-tree installs
			// enumerate staging-owned paths and never match.
			plan.BindVerifiedSubtitleSources(s.siblingSourceProofs())
		}
		finalResult, err = executor.ExecuteOrganizePlan(plan, publishMove, s.original.Organize.LinkMode)
		if primaryResident && finalResult != nil {
			// A later failure must report final resident/sidecar outcomes, not
			// the temporary staging result still held by the pipeline.
			state.organizeResult = finalResult
			for _, sr := range finalResult.Subtitles {
				if publishMove && (sr.Moved || fsutil.PublishCompleted(sr.Error)) {
					s.residentSidecarMutation = true
				}
			}
		}
		s.annotateInPlaceDirectory(finalResult)
		if filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) && (err == nil || fsutil.PublishCompleted(err)) {
			// An explicitly-unproven successor is NOT this batch's installed
			// output (codex P1, PRRT_kwDORn9KaM6nsX9a): the verified hard-link
			// leg found the destination's current occupant DIVERGING from the
			// identity its own link installed — another writer replanted the
			// name inside the link→lstat window — and already retained that
			// occupant byte-intact, classifying the doubt with
			// ErrPublishCompleted joined to ErrPublishSuccessorUnproven.
			// Adopting it here would record the successor's own identity as
			// this batch's installed output, arming the failed apply's rollback
			// to UnlinkVerified the successor into deletion before restoring
			// any displaced backup. The sealed retain contract (round 46/47
			// lineage) applies to the batch's record-reflection exactly as it
			// did to the composite's cleanup: skip the observation, leave the
			// leg uninstalled, and let rollback retain the successor. The
			// proven publish-completed classification (round 42,
			// PRRT_kwDORn9KaM6nfbS3 — the pending-kind and subtitle data flow)
			// is unchanged: only this explicit-successor class drops out of
			// the observe.
			if !primaryResident && !fsutil.PublishSuccessorUnproven(err) {
				// Bind the primary observation to the object the publish PROVED it
				// installed (codex P1, PRRT_kwDORn9KaM6p3Dq1): execute released its
				// destination lock, so a foreign writer can replant the name before
				// this observation runs. A name-derived record would adopt that
				// successor as this batch's install, and a later failed leg's
				// rollback would then UnlinkVerified (or move back over the source)
				// bytes this apply never wrote — the same discipline the copied
				// sidecars already apply. An affirmative divergence retains the
				// occupant byte-intact and leaves the leg UNINSTALLED: the apply
				// commits and rollback can never be armed against the successor.
				// Lanes whose publish offers no identity (by-name copies, soft
				// links, authorized replace legs) keep the legacy observation.
				if finalResult != nil && finalResult.InstalledIdentity != nil {
					if oerr := observeBoundPrimaryPublish(batch, plan.TargetPath, finalResult.InstalledIdentity); oerr != nil {
						if fsutil.PublishSuccessorUnproven(oerr) {
							logging.Warnf("deferred primary %s holds an explicitly unproven successor — retained byte-intact and never armed for rollback deletion: %v", plan.TargetPath, oerr)
							// Release the leg UNINSTALLED and remember the refusal: the
							// unconditional confirmation below stats the pathname, and adopting
							// it would arm rollback's verified unlink against the very occupant
							// this observer just rejected (codex P1, PRRT_kwDORn9KaM6p6mdQ).
							_ = batch.ReleaseUninstalled(plan.TargetPath)
							primarySuccessorRefused = true
						} else {
							return fmt.Errorf("bind deferred primary install %s to its published identity: %w", plan.TargetPath, oerr)
						}
					}
				} else {
					batch.ObservePublishResult(plan.TargetPath)
				}
			}
			// Confirm only what execute proves installed: a would-be target
			// that turned out occupied/armed-skip mid-run must not be registered
			// as ours, or rollback would UnlinkVerified a foreign file. A
			// publish-completed subtitle error IS an install — the post-publish
			// leg failed after the bytes landed (the primary leg's
			// PublishCompleted check above applies the same classification), so
			// the seat stays in the confirmed-copy set: its batch leg confirms
			// and its durable pin survives reconciliation, letting a later
			// revert clean the installed sidecar instead of stranding it
			// untracked (codex P2, PRRT_kwDORn9KaM6m5-ms).
			if finalResult != nil {
				for _, sr := range finalResult.Subtitles {
					if sr.NewPath != "" && (sr.Copied || fsutil.PublishCompleted(sr.Error)) {
						copiedSidecarTargets[filepath.Clean(sr.NewPath)] = true
						if sr.Error == nil && sr.InstalledIdentity != nil {
							copiedSidecarIdentities[filepath.Clean(sr.NewPath)] = sr.InstalledIdentity
						}
					}
				}
			}
			for _, target := range sidecarIntentTargets {
				if !copiedSidecarTargets[filepath.Clean(target)] {
					// An armed-but-uncopied target pinned a .dlbusy claim that only
					// ConfirmPublish or rollback would release: free it now or the
					// destination reports ErrReplacementBusy for the server's lifetime.
					// A refusal (leg already proves an install) must never delete
					// those bytes just to free the marker.
					_ = batch.ReleaseUninstalled(target)
					continue
				}
				if identity := copiedSidecarIdentities[filepath.Clean(target)]; identity != nil {
					// Bind the observation to the identity the verified copy
					// PRODUCED, never to whatever the destination name resolves
					// to now (codex P1, PR #276, finding ntCe6 — the copy lane's
					// twin of the hard-link successor binding,
					// PRRT_kwDORn9KaM6nsX9a): handleSubtitles released its
					// destination lock after the copy, so an external writer can
					// have replaced the subtitle before this loop runs. A
					// name-derived observation would adopt that successor's
					// identity as this batch's install, and a later failed leg's
					// rollback would UnlinkVerified those foreign bytes before
					// restoring any displaced backup. An affirmative divergence
					// (ErrPublishSuccessorUnproven, joined with the round-42
					// ErrPublishCompleted doubt class) releases the armed leg
					// UNINSTALLED instead: the occupant is retained byte-intact
					// (the durable hash pin never matches the successor's bytes, so
					// a later revert retains it too — the PRRT_kwDORn9KaM6m7CBi
					// foreign-swap-survives contract), the apply commits, and
					// rollback can never be armed against the successor. Every
					// other observe failure keeps its legacy shape.
					if oerr := observeBoundSidecarPublish(batch, target, identity); oerr != nil {
						if fsutil.PublishSuccessorUnproven(oerr) {
							logging.Warnf("copy-installed sidecar %s holds an explicitly unproven successor — retained byte-intact and never armed for rollback deletion: %v", target, oerr)
							_ = batch.ReleaseUninstalled(target)
							// The durable ownership claim is retracted with the in-process
							// leg (codex P1, PRRT_kwDORn9KaM6p3Dq8): a surviving claim keeps
							// the target in the completion reconcile's keep-set and lets the
							// seat graduate into the ledger's unconditional Delete list, so
							// reverting this otherwise-successful apply would remove the
							// explicitly unproven successor.
							retractCopiedSidecarOwnership(finalResult, copiedSidecarTargets, copiedSidecarIdentities, target)
							continue
						}
						return fmt.Errorf("bind copy-installed sidecar %s to its installed identity: %w", target, oerr)
					}
				} else {
					batch.ObservePublishResult(target)
				}
				if cerr := batch.ConfirmPublish(ctx, target); cerr != nil {
					return cerr
				}
			}
			if !primaryResident && s.videoDeferred && publishMove && !primarySuccessorRefused && finalResult != nil {
				// The rename already consumed the real source: arm rollback before
				// any fallible leg (ConfirmPublish, later installs) can observe an
				// installed destination worth deleting with no armed inverse. The arm
				// binds to the move lane's carried identity (codex P1,
				// PRRT_kwDORn9KaM6qLkJ4); a destination re-stat may only confirm that
				// exact object, never adopt a name-only successor.
				s.sourceCleanupArmed = true
				s.directOriginArmed = true
				if finalResult.InstalledIdentity == nil {
					return fmt.Errorf("arm deferred primary rollback %s: missing bound installed identity", plan.TargetPath)
				}
				target := s.sourcePath
				if finalResult.NewPath != "" {
					target = finalResult.NewPath
				}
				if armErr := batch.SetRollbackOriginBound(target, s.sourcePath, finalResult.InstalledIdentity); armErr != nil {
					if altErr := batch.SetRollbackOriginBound(plan.TargetPath, s.sourcePath, finalResult.InstalledIdentity); altErr != nil {
						return errors.Join(armErr, altErr)
					}
					return armErr
				}
			}
		}
		if err != nil {
			return fmt.Errorf("publish organized video: %w", err)
		}
		if finalResult == nil {
			return fmt.Errorf("publish organized video returned no result")
		}
		if filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) {
			// A refused primary is never confirmed (codex P1,
			// PRRT_kwDORn9KaM6p6mdQ): ConfirmPublish stats the pathname and would
			// re-adopt the successor the bound observer rejected, leaving a later
			// seal/reconcile/generation failure to rollback-delete foreign bytes.
			if primarySuccessorRefused {
				logging.Warnf("deferred primary %s left unconfirmed — its occupant is not this apply's install", plan.TargetPath)
			} else if !primaryResident {
				if err := batch.ConfirmPublish(ctx, plan.TargetPath); err != nil {
					return err
				}
			}
			// Seal the copy lane's interim partial pin with the digest the
			// verified copy teed off its single publish stream: a crash after
			// the publish but before graduation then recovers against the full
			// hash of the very bytes that landed. Placement is deliberate —
			// AFTER every batch leg observed its install, so a seal refusal
			// rolls back with the same confirmed-install discipline the
			// reconcile refusal exercises; the interim entry stays the crash
			// evidence for any earlier exit — intent without removal power:
			// recovery retains it until this seal lands (codex P1,
			// PRRT_kwDORn9KaM6novbT).
			if primaryPinCopyPartial && finalResult != nil && finalResult.PrimaryCopySHA256 != "" && o.revertLog != nil && opID != "" {
				if sealErr := o.revertLog.FinalizeDeleteIntentCopyDigest(ctx, opID, plan.TargetPath, finalResult.PrimaryCopySHA256); sealErr != nil {
					return fmt.Errorf("seal deferred primary copy pin %s: %w", plan.TargetPath, sealErr)
				}
			}
			// The confirmed primary graduated to a user-owned install: settle the
			// durable pins in one journal transaction. The primary's pin is
			// retracted (this row's revert retains installed copy/link primaries —
			// and must never delete them), and every sidecar pin whose install the
			// organizer did NOT confirm is retracted: a surviving pin could
			// hash-match a same-content foreign occupant and let a later revert
			// delete bytes this apply never landed.
			if s.videoDeferred && !publishMove && o.revertLog != nil && opID != "" && (primaryPinRecorded || len(sidecarIntentTargets) > 0) {
				keep := make([]string, 0, len(sidecarIntentTargets))
				for _, target := range sidecarIntentTargets {
					if copiedSidecarTargets[filepath.Clean(target)] {
						keep = append(keep, target)
					}
				}
				if err := o.revertLog.ReconcileDeleteIntents(ctx, opID, keep); err != nil {
					return fmt.Errorf("reconcile copy-installed delete intents: %w", err)
				}
			}
		}
		if finalReplaced {
			finalResult.Warnings = append(finalResult.Warnings, organizer.AuthorizedOverwriteWarning(plan.TargetPath))
		}
		finalResult.OriginalPath = s.directoryOriginalPath(s.sourcePath)
	}
	// A deferred move may consume the primary or just its subtitles when the
	// primary is resident. Settle their actual inverses before a later leg fails.
	directSourceConsumed := s.videoDeferred && !s.original.Organize.Skip && publishMove && s.sourcePath != "" && finalResult != nil && filepath.Clean(s.sourcePath) != filepath.Clean(finalResult.NewPath)
	if directSourceConsumed {
		// The early arm (immediately after ObservePublishResult) owns the
		// inverse for the consumed source; every path reaching this block armed
		// it, so there is no arm work left here.
		// The same execution moved source sidecars along with the video:
		// register each with the batch (in-process rollback) and the journal
		// (crash recovery) before any later leg can fail, or cleanup would
		// strand installed copies beside a failed destination.
		for _, sr := range finalResult.Subtitles {
			// A publish-completed subtitle move error IS an install wearing the
			// sr.Moved=false ambiguity slot (codex P1, PRRT_kwDORn9KaM6nfbS3): the
			// post-publish cleanup leg refused AFTER the bytes landed at the
			// destination (the organizer withholds Moved so nothing re-aims a
			// surviving source by name). Skipping the arm here would strand an
			// untracked sidecar at the destination — the original left
			// recoverable only under the mover's hidden claim name. Arm it
			// exactly like a clean move: the batch move-back is no-replace and
			// the durable revert rename-back is source-vacancy-gated, so a
			// retained or reappeared source suppresses the compensation and
			// keeps both copies instead of clobbering or dropping one.
			installed := sr.Moved || fsutil.PublishCompleted(sr.Error)
			if !installed || sr.OriginalPath == "" || sr.NewPath == "" {
				continue
			}
			// The pending intent for this move was journaled pre-execution; only
			// the in-process rollback arm belongs here. The arm binds to the
			// subtitle move lane's carried identity (codex P1,
			// PRRT_kwDORn9KaM6qLkJ4); a destination re-stat may only confirm that
			// exact object, never adopt a name-only successor.
			if sr.InstalledIdentity == nil {
				_ = batch.SetRollbackOriginBound(sr.NewPath, sr.OriginalPath, nil)
				return fmt.Errorf("arm subtitle rollback %s: missing bound installed identity", sr.NewPath)
			}
			if err := batch.SetRollbackOriginBound(sr.NewPath, sr.OriginalPath, sr.InstalledIdentity); err != nil {
				// The organizer already moved this sidecar off its source: a failed
				// arm must not leave it stranded outside the batch. Reverse only the
				// carried identity — never clobbering anything that reappeared at the
				// source and never relocating a successor that replaced the sidecar.
				if _, statErr := s.fs.Stat(sr.OriginalPath); os.IsNotExist(statErr) {
					proof := fsutil.VerifiedSourceProofFromBoundInstallIdentity(s.fs, sr.InstalledIdentity)
					mode := fsutil.StagingFileMode()
					if info := sr.InstalledIdentity.FileInfo(); info != nil {
						mode = info.Mode().Perm()
					}
					if _, rerr := fsutil.MoveFileNoReplaceVerifiedMode(s.fs, sr.NewPath, sr.OriginalPath, proof, mode); rerr != nil {
						logging.Warnf("subtitle direct rollback failed for %s: %v (arm error: %v)", sr.NewPath, rerr, err)
					}
				}
				return fmt.Errorf("arm subtitle rollback %s: %w", sr.NewPath, err)
			}
		}
		if o.revertLog != nil && opID != "" {
			// Reconcile pending intents with the executed outcome: skipped subtitle
			// moves are retracted so a later revert never renames a retained
			// destination over its source.
			keep := make([]models.FileMove, 0, len(finalResult.Subtitles)+1)
			if !primaryResident && !primarySuccessorRefused {
				keep = append(keep, models.FileMove{OriginalPath: s.directoryOriginalPath(s.sourcePath), NewPath: finalResult.NewPath})
			}
			for _, sr := range finalResult.Subtitles {
				// Publish-completed moves keep their intent (codex P1,
				// PRRT_kwDORn9KaM6nfbS3): retracting it would commit the apply
				// with an untracked destination sidecar, while the kept intent
				// is exactly the vacancy-gated compensation the reverter runs
				// for an install whose claimed-source cleanup refused.
				if (sr.Moved || fsutil.PublishCompleted(sr.Error)) && sr.OriginalPath != "" && sr.NewPath != "" {
					keep = append(keep, models.FileMove{OriginalPath: s.directoryOriginalPath(sr.OriginalPath), NewPath: sr.NewPath})
				}
			}
			if err := o.revertLog.ReconcileMoveIntents(ctx, opID, keep); err != nil {
				return fmt.Errorf("reconcile move intents: %w", err)
			}
		}
		if o.revertLog != nil && opID != "" {
			partial := &ApplyResult{OrganizeResult: finalResult, Movie: state.movie, OperationID: opID}
			if primarySuccessorRefused {
				// Settle the row WITHOUT the move (codex P1, PRRT_kwDORn9KaM6p_JGh):
				// the ledger's NewPath would otherwise name the foreign successor as
				// this row's moved primary, and a later revert would relocate those
				// foreign bytes onto the now-vacant source path. The pending move
				// intent was already retracted above, so nothing in the ledger names
				// a destination this apply no longer owns.
				settled := *finalResult
				settled.Moved = false
				settled.NewPath = ""
				partial.OrganizeResult = &settled
			}
			if err := o.revertLog.Complete(ctx, opID, partial); err != nil {
				return fmt.Errorf("persist inverse after direct video publication: %w", err)
			}
		}
	}
	// Admitted subtitles publish only through their dedicated planned lanes.
	// Rehoming is for other staged artifacts, never an implicit subtitle retry
	// after a skipped, failed, or omitted result. Preserve result-based exclusions
	// for older/untyped stage records too, including installed outputs whose
	// destination may since have been claimed by a different writer.
	if !s.videoDeferred || !s.original.Organize.MoveFiles {
		excluded := s.refusedSidecarExcludedSiblings(finalResult)
		for staged := range s.installedSidecarExcludedSiblings(finalResult) {
			if excluded == nil {
				excluded = map[string]bool{}
			}
			excluded[staged] = true
		}
		if err := s.rehomeRemainingSiblings(stagedVideo, excluded); err != nil {
			return err
		}
	}
	artifactSkipDir := filepath.Dir(s.stagedSource)
	if s.inPlace && !s.videoDeferred {
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
	// Deletion intent lands after artifact claims settle (consumer, skipped,
	// and replacement destinations are never journaled for removal) and before
	// any install touches the final tree.
	journalDeleteIntent := func(entries []models.DeleteEntry) error {
		if len(entries) == 0 || o.revertLog == nil || opID == "" {
			return nil
		}
		if err := o.revertLog.RecordDeleteIntent(ctx, opID, entries); err != nil {
			return fmt.Errorf("journal artifact destination intent: %w", err)
		}
		return nil
	}
	if err := s.publishStagedSubtitles(ctx, o, opID, finalResult); err != nil {
		return err
	}
	stagedPrimary := ""
	if s.inPlace && !s.videoDeferred && finalResult != nil {
		stagedPrimary = finalResult.NewPath
	}
	s.registerStagedVideoOutputs(stagedVideo, stagedPrimary)
	preservedMedia, err := s.installTree(installSkipVideo, artifactSkipDir, state.downloadPaths, stagedArtifactDir, finalArtifactDir, journalDeleteIntent)
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
		if s.isSharedConsumer(state.nfoPath) || s.skippedUnowned(state.nfoPath) {
			state.nfoPath = ""
		}
	}
	mappedDownloads := make([]string, 0, len(state.downloadPaths))
	for _, path := range state.downloadPaths {
		mapped, mapErr := s.publicationPath(path, stagedArtifactDir, finalArtifactDir)
		if mapErr != nil {
			return mapErr
		}
		if !batch.IsReplacement(mapped) && !s.isSharedConsumer(mapped) && !s.skippedUnowned(mapped) {
			mappedDownloads = append(mappedDownloads, mapped)
		}
	}
	state.downloadPaths = mappedDownloads
	s.sharedConsumers = nil
	if preservedMedia && steps != nil {
		steps.PosterVerified = false
	}
	if !s.original.Organize.Skip && s.original.Organize.MoveFiles && s.sourcePath != "" && filepath.Clean(s.sourcePath) != filepath.Clean(finalResult.NewPath) {
		// Only admitted deferred multipart videos publish here. Subtitle sources
		// belong exclusively to the organizer's verified canonical lane; there
		// is no generic subtitle copy or cleanup fallback.
		failed := map[string]bool{}
		if s.videoDeferred && finalResult != nil {
			for _, sr := range finalResult.Subtitles {
				if sr.Error != nil && !fsutil.PublishCompleted(sr.Error) && sr.OriginalPath != "" {
					failed[filepath.Clean(sr.OriginalPath)] = true
				}
			}
		}
		deferredSiblingProofs := s.siblingSourceProofs()
		for _, sibling := range s.siblings {
			// Subtitle sources never publish under an unnormalized fallback name;
			// absent or failed result seats do not grant replacement authority.
			if failed[filepath.Clean(sibling.sourcePath)] {
				// Preserve the move lane's admission failure, without retrying a
				// failed subtitle under a different generic fallback name.
				if err := s.revalidateAdmittedSource(sibling.sourcePath, sibling.identity); err != nil {
					return err
				}
			}
			if !sibling.deferred || sibling.subtitle {
				continue
			}
			target := filepath.Join(filepath.Dir(finalResult.NewPath), s.siblingTargetName(filepath.Base(s.sourcePath), filepath.Base(finalResult.NewPath), filepath.Base(sibling.sourcePath)))
			// A deferred video sibling (codex P2, PRRT_kwDORn9KaM6rLI28)
			// installs by the verified move lane straight off its real
			// source, mirroring the deferred primary: the admission identity
			// is re-proven at consumption, the MoveBack inverse is journaled
			// pre-move, and the install binds the identity the move lane
			// produced rather than re-deriving it from the destination name.
			if _, statErr := s.fs.Stat(target); statErr == nil {
				continue
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("inspect deferred publication sibling: %w", statErr)
			}
			if _, armErr := batch.BeforePublish(ctx, target, false); armErr != nil {
				return armErr
			}
			if o.revertLog != nil && opID != "" {
				if err := o.revertLog.RecordMoveIntent(ctx, opID, s.directoryOriginalPath(sibling.sourcePath), target); err != nil {
					return fmt.Errorf("journal deferred sibling move intent: %w", err)
				}
			}
			if err := s.revalidateAdmittedSource(sibling.sourcePath, sibling.identity); err != nil {
				return err
			}
			proof := deferredSiblingProofs[filepath.Clean(sibling.sourcePath)]
			if proof == nil {
				return fmt.Errorf("publish deferred sibling from source %s: admission identity unavailable", sibling.sourcePath)
			}
			movedID, moveErr := fsutil.MoveFileNoReplaceVerifiedMode(s.fs, sibling.sourcePath, target, proof, sibling.identity.perm)
			if primaryResident && (moveErr == nil || fsutil.PublishCompleted(moveErr)) {
				s.residentSidecarMutation = true
			}
			if moveErr == nil || (fsutil.PublishCompleted(moveErr) && !fsutil.PublishSuccessorUnproven(moveErr)) {
				if armErr := batch.SetRollbackOriginBound(target, sibling.sourcePath, movedID); armErr != nil {
					rollbackProof := fsutil.VerifiedSourceProofFromBoundInstallIdentity(s.fs, movedID)
					mode := sibling.identity.perm
					if movedID != nil && movedID.FileInfo() != nil {
						mode = movedID.FileInfo().Mode().Perm()
					}
					if _, rollbackErr := fsutil.MoveFileNoReplaceVerifiedMode(s.fs, target, sibling.sourcePath, rollbackProof, mode); rollbackErr != nil {
						return errors.Join(moveErr, armErr, fmt.Errorf("restore deferred sibling after rollback arm refusal: %w", rollbackErr))
					}
					return errors.Join(moveErr, armErr)
				}
			}
			if moveErr != nil {
				return fmt.Errorf("publish deferred sibling from source: %w", moveErr)
			}
			if confirmErr := batch.ConfirmPublish(ctx, target); confirmErr != nil {
				return confirmErr
			}
		}

		if s.videoDeferred {
			// The direct publication already journaled the inverse and armed
			// rollback; the move consumed the original, so there is no
			// source-path removal here.
		} else {
			// Persist the final-path inverse before deleting any source. A crash
			// after this point leaves history with every published path.
			if o.revertLog != nil && opID != "" {
				partial := &ApplyResult{OrganizeResult: finalResult, Movie: state.movie, DownloadPaths: state.downloadPaths, NFOPath: state.nfoPath, FoundNFOPath: state.foundNFOPath, Merged: state.merged, OperationID: opID}
				if err := o.revertLog.Complete(ctx, opID, partial); err != nil {
					return fmt.Errorf("persist inverse before source cleanup: %w", err)
				}
			}
			// The staged payload already landed; consuming the original must
			// still refuse a source that changed since admission: deleting a
			// foreign replacement is not "move" semantics.
			if err := s.revalidateAdmittedSource(s.sourcePath, s.sourceIdentity); err != nil {
				return err
			}
			s.sourceCleanupArmed = true
			if err := batch.SetRollbackOrigin(finalResult.NewPath, s.sourcePath); err != nil {
				return err
			}
			if err := s.fs.Remove(s.sourcePath); err != nil {
				_ = batch.SetRollbackOrigin(finalResult.NewPath, "")
				return fmt.Errorf("remove original after artifact publication: %w", err)
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
	if s.directoryMove != nil && finalResult != nil {
		for i := range finalResult.Subtitles {
			finalResult.Subtitles[i].OriginalPath = s.directoryOriginalPath(finalResult.Subtitles[i].OriginalPath)
		}
	}
	committed = true
	return nil
}

// refusedSidecarExcludedSiblings preserves the organizer's refusal for each
// admitted subtitle source. A skipped or failed source must never re-enter publication
// through the generic tree, even if its destination later disappears, is a
// dangling symlink, or cannot be inspected. Re-probing occupancy would lose
// that refusal and allow a later foreign file to be treated as replaceable.
// Match sources rather than target names because language normalization can
// produce a different destination from the generic sibling fallback.
func (s *artifactStage) refusedSidecarExcludedSiblings(finalResult *organizer.OrganizeResult) map[string]bool {
	if !s.videoDeferred || s.original.Organize.MoveFiles || s.original.Organize.LinkMode != organizer.LinkModeNone || finalResult == nil {
		return nil
	}
	skipped := make(map[string]bool, len(finalResult.Subtitles))
	for _, sr := range finalResult.Subtitles {
		if (sr.Skipped || (sr.Error != nil && !fsutil.PublishCompleted(sr.Error))) && sr.OriginalPath != "" {
			skipped[filepath.Clean(sr.OriginalPath)] = true
		}
	}
	if len(skipped) == 0 {
		return nil
	}
	excluded := make(map[string]bool)
	for _, sibling := range s.siblings {
		if skipped[filepath.Clean(sibling.sourcePath)] {
			excluded[filepath.Clean(sibling.stagedPath)] = true
		}
	}
	return excluded
}

// installedSidecarExcludedSiblings names the staged sibling copies the rehome
// leg must NOT pull into the install tree because the organizer lane ALREADY
// delivered that source's bytes to the destination (codex P2,
// PRRT_kwDORn9KaM6m7CBi): a Copied seat, or its publish-completed error twin
// (round-28: the post-publish leg failed AFTER the bytes landed — the same
// classification that feeds copiedSidecarTargets above), proves the
// destination holds this apply's install. Rehoming the staged duplicate would
// republish the endpoint through installPaths, where a foreign writer that
// swapped the destination after the organizer copy fails the sameBytes skip
// and reads as replaceable — an overwrite of bytes the publication no longer
// owns. Matching is by SOURCE identity (the seat's OriginalPath against the
// admitted sibling's sourcePath), never by computed target name: language
// normalization can re-derive a leaf stagedArtifactSiblingName never
// reproduces, so the exclusion must follow the source the organizer actually
// installed.
func (s *artifactStage) installedSidecarExcludedSiblings(finalResult *organizer.OrganizeResult) map[string]bool {
	if !s.videoDeferred || s.original.Organize.MoveFiles || s.original.Organize.LinkMode != organizer.LinkModeNone || finalResult == nil {
		return nil
	}
	var installed map[string]bool
	for _, sr := range finalResult.Subtitles {
		if sr.NewPath != "" && sr.OriginalPath != "" && (sr.Copied || fsutil.PublishCompleted(sr.Error)) {
			if installed == nil {
				installed = map[string]bool{}
			}
			installed[filepath.Clean(sr.OriginalPath)] = true
		}
	}
	if installed == nil {
		return nil
	}
	var excluded map[string]bool
	for _, sibling := range s.siblings {
		if installed[filepath.Clean(sibling.sourcePath)] {
			if excluded == nil {
				excluded = map[string]bool{}
			}
			excluded[filepath.Clean(sibling.stagedPath)] = true
		}
	}
	return excluded
}

// retractCopiedSidecarOwnership withdraws every ownership claim this apply
// recorded for a copied subtitle seat after a bound observation proved another
// writer replaced the installed bytes (codex P1, PRRT_kwDORn9KaM6p3Dq8):
// releasing only the in-process batch leg would leave the durable claim
// standing — the target stays in the set the completion reconcile KEEPS, and
// the seat later graduates into the ledger's unconditional Delete list, so
// reverting this otherwise-successful apply would remove the explicitly
// unproven successor. The target leaves both in-process sets (the reconcile
// keep-filter and the identity map) and the seat is marked SuccessorRefused so
// the completion ledger skips it — while Copied stays SET: the rehome/install
// exclusion reads that flag to keep the staged duplicate OUT of the tree, and
// a cleared flag would let installPaths republish over the retained occupant.
// The result is non-nil by construction: only the copied-seat path — whose
// copied set a non-nil result produced — reaches this refusal.
func retractCopiedSidecarOwnership(result *organizer.OrganizeResult, targets map[string]bool, identities map[string]*fsutil.BoundInstallIdentity, target string) {
	key := filepath.Clean(target)
	delete(targets, key)
	delete(identities, key)
	for i := range result.Subtitles {
		seat := &result.Subtitles[i]
		if filepath.Clean(seat.NewPath) != key {
			continue
		}
		seat.SuccessorRefused = true
	}
}

func (s *artifactStage) rehomeRemainingSiblings(stagedVideo string, excluded map[string]bool) error {
	if stagedVideo == "" || s.original.Organize.LinkMode != organizer.LinkModeNone || s.stagedSource == "" {
		return nil
	}
	targetDir := filepath.Dir(stagedVideo)
	for _, sibling := range s.siblings {
		if sibling.deferred {
			// No staged bytes to rehome: the deferred sibling is published
			// straight off its real source by the verified move lane.
			continue
		}
		if sibling.subtitle || excluded[filepath.Clean(sibling.stagedPath)] || s.excludedStagedSubtitleInputs[filepath.Clean(sibling.stagedPath)] {
			continue
		}
		if _, err := s.fs.Stat(sibling.stagedPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect staged sidecar %s: %w", sibling.stagedPath, err)
		}
		target := filepath.Join(targetDir, s.siblingTargetName(filepath.Base(s.stagedSource), filepath.Base(stagedVideo), filepath.Base(sibling.stagedPath)))
		if filepath.Clean(target) == filepath.Clean(sibling.stagedPath) {
			continue
		}
		if artifactSameRegularFile(s.fs, sibling.stagedPath, target) {
			continue
		}
		if err := s.fs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create staged sidecar directory: %w", err)
		}
		if _, err := s.fs.Stat(target); err == nil {
			if strings.EqualFold(filepath.Clean(target), filepath.Clean(sibling.stagedPath)) {
				return fmt.Errorf("cannot prove staged sidecar spelling alias %s: %w", target, errArtifactSourceChanged)
			}
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

// siblingTargetName keeps raw native basenames when file renaming is disabled.
func (s *artifactStage) siblingTargetName(sourceName, targetName, siblingName string) string {
	if s.preserveSiblingNames {
		return siblingName
	}
	return stagedArtifactSiblingName(sourceName, targetName, siblingName)
}

func stagedArtifactSiblingName(sourceName, targetName, siblingName string) string {
	sourceName, sourceExt := artifactMatchName(sourceName)
	siblingNormalized, siblingExt := artifactMatchName(siblingName)
	targetName, targetExt := artifactMatchName(targetName)
	sourceStem := strings.TrimSuffix(sourceName, sourceExt)
	siblingStem := strings.TrimSuffix(siblingNormalized, siblingExt)
	targetStem := strings.TrimSuffix(targetName, targetExt)
	foldedSource := foldArtifactASCII(sourceStem)
	foldedSibling := foldArtifactASCII(siblingStem)
	if strings.EqualFold(foldedSibling, foldedSource) {
		return targetStem + siblingExt
	}
	// Compare normalized identities, but split the original suffix by rune
	// count: fullwidth and ASCII prefixes have different byte lengths.
	sourceRunes := len([]rune(sourceStem))
	siblingRunes := []rune(siblingStem)
	if len(siblingRunes) > sourceRunes && strings.EqualFold(foldArtifactASCII(string(siblingRunes[:sourceRunes])), foldedSource) {
		separator := []rune(foldedSibling)[sourceRunes]
		if separator == '.' || separator == '-' || separator == '_' {
			return targetStem + string(siblingRunes[sourceRunes:]) + siblingExt
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
		if info.IsDir() || stagedBusyReservation(info.Name()) {
			return nil
		}
		clean := filepath.Clean(path)
		if s.excludedStagedSubtitleInputs[clean] || s.isStagedSubtitleOutput(clean) {
			return nil
		}
		if info.Name() == artifactStageManifestName && filepath.Clean(filepath.Dir(path)) == filepath.Clean(s.root) {
			return nil
		}
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

func (s *artifactStage) installTree(skipFile, skipDir string, preserve []string, stagedArtifactDir, finalArtifactDir string, journalDeleteIntent func([]models.DeleteEntry) error) (bool, error) {
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
		if info.IsDir() || stagedBusyReservation(info.Name()) {
			return nil
		}
		clean := filepath.Clean(path)
		if s.excludedStagedSubtitleInputs[clean] || s.isStagedSubtitleOutput(clean) {
			return nil
		}
		if info.Name() == artifactStageManifestName && filepath.Clean(filepath.Dir(path)) == filepath.Clean(s.root) {
			return nil
		}
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
	return s.installPaths(paths, preserve, stagedArtifactDir, finalArtifactDir, journalDeleteIntent)
}

func (s *artifactStage) installPaths(paths, preserve []string, stagedArtifactDir, finalArtifactDir string, journalDeleteIntent func([]models.DeleteEntry) error) (bool, error) {
	type installPath struct {
		source, target string
		skip           bool
		replace        bool
		sharedOwner    bool
		video          bool
		primary        bool
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
		registered, primary, video, roleErr := s.stagedVideoOutput(source)
		if roleErr != nil {
			return false, roleErr
		}
		plan.primary, plan.video = primary, video
		if plan.video {
			resident, err := s.stagedVideoResident(registered, target, plan.primary)
			if err != nil {
				return false, err
			}
			if resident {
				plan.skip = true
				s.markUnownedSkipped(target)
				plans = append(plans, plan)
				continue
			}
		}
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
			s.markUnownedSkipped(target)
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
			if plan.video && (!plan.primary || !s.original.Organize.ForceUpdate) {
				return false, fmt.Errorf("%w: staged video destination %s", fsutil.ErrPublishCollision, target)
			}
			if digestEq, eqErr := sameBytes(s.fs, source, target); !plan.video && eqErr == nil && digestEq {
				// Byte-identical (e.g. this operation just copy-installed through the
				// memberdBatch route): no duplicate install, no replacement journal.
				// Track the skip: this apply landed nothing at the destination, so
				// the pre-existing bytes stay foreign to it and the delete-ledger
				// mapping must never condemn them on revert.
				plan.skip = true
				s.markUnownedSkipped(target)
				plans = append(plans, plan)
				continue
			}
			if !s.original.OverwriteExistingMedia && containsPath(preserve, source) {
				plan.skip = true
				s.markUnownedSkipped(target)
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
		if plan.video {
			if err := s.publishStagedVideo(plan.source, plan.target, plan.primary && s.original.Organize.ForceUpdate); err != nil {
				return false, err
			}
			continue
		}

		// The verdict that matters is the ARMING one: preflight's stale
		// occupation snapshot can say replace where BeforePublish finds nothing
		// (armed as a create) and vice versa — journal deletion intent based on
		// what BeforePublish actually did, not the stale classification.
		replaced := false
		if s.publishBatch != nil {
			var prepErr error
			replaced, prepErr = s.publishBatch.BeforePublish(s.publishCtx, plan.target, plan.replace)
			if prepErr != nil {
				return false, prepErr
			}
		} else if plan.replace {
			if removeErr := s.fs.Remove(plan.target); removeErr != nil {
				return false, fmt.Errorf("replace artifact destination %s: %w", plan.target, removeErr)
			}
			replaced = true
		}
		// Journal the pending deletion BEFORE the install lands, pinned to the
		// staged payload's digest: a crash anywhere from here on is covered, and
		// a revert deletes the destination only while its bytes still match what
		// this operation meant to publish (never unrelated later content).
		// Replacement paths are already journaled via their replacement legs.
		if journalDeleteIntent != nil && !replaced {
			digest, digestErr := artifactDigest(s.fs, plan.source)
			if digestErr != nil {
				return false, digestErr
			}
			if err := journalDeleteIntent([]models.DeleteEntry{{Path: plan.target, SHA256: digest}}); err != nil {
				return false, err
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

// sameBytes reports whether two files hold identical content, surfacing any
// read failure as an error (the caller treats that as not-equal).
func sameBytes(fs afero.Fs, a, b string) (bool, error) {
	da, errA := artifactDigest(fs, a)
	if errA != nil {
		return false, errA
	}
	db, errB := artifactDigest(fs, b)
	if errB != nil {
		return false, errB
	}
	return da == db, nil
}

// symlinkLinkTargetFn is the test seam over organizer.SymlinkLinkTarget for
// the soft-link pin arm of deferredPrimaryDeleteEntry (the same discipline as
// organizer's filepathAbsFn): the helper's only error leg is the
// source-absolutization failure on a relative path — a cwd failure this
// package cannot induce deterministically — so fault tests replay it here.
var symlinkLinkTargetFn = organizer.SymlinkLinkTarget

// deferredPrimaryDeleteEntry derives the durable delete-intent pin for the
// deferred primary publication WITHOUT a streaming read of the payload
// (codex P2, PRRT_kwDORn9KaM6nBUrF — the superseded content-hash pin cost a
// second full pass over a multi-gigabyte source before the copy it merely
// anticipated): the proof shape keys on the publication's link mode.
//
// LinkModeSoft: the install is the link OBJECT, whose entire payload is its
// target string — the pin carries exactly the string the strategy's symlink
// leg installs (organizer.SymlinkLinkTarget computes both sides through one
// route), and recovery authenticates by readlink. A regular-file hash pin
// could never serve here: the planned-delete leg retains every non-regular
// entry (m5HF7), so the link install would survive recovery orphaned (codex
// P2, PRRT_kwDORn9KaM6nBUq8).
//
// LinkModeHard: the linked destination IS the admitted source's object
// (link(2) shares the volume/index), so the admission identity tuple pinned
// at preparation IS the ownership certificate — dev/inode where the platform
// exposes one, plus size+mtime as the metadata legs, recovered through the
// same no-follow identity route the admission proofs use. The plan must name
// the admitted source; any other shape is refused closed.
//
// Default (byte-streaming copy): the bounded head+tail interim digest of
// fsutil.PartialCopyDigest — the only content marker available before the
// stream exists. It is intent evidence, NOT removal power (codex P1,
// PRRT_kwDORn9KaM6novbT): an entry still in this shape at recovery retains
// the destination, because the crash-surviving bounded proof can no longer
// tell the landed copy from a payload edited between the digest windows. The
// verified copy leg tees the full sha256 off its single publish stream and
// FinalizeDeleteIntentCopyDigest seals this entry once the install lands —
// only the sealed shape authorizes the removal.
func (s *artifactStage) deferredPrimaryDeleteEntry(plan *organizer.OrganizePlan) (models.DeleteEntry, error) {
	switch s.original.Organize.LinkMode {
	case organizer.LinkModeSoft:
		target, err := symlinkLinkTargetFn(plan.SourcePath)
		if err != nil {
			return models.DeleteEntry{}, fmt.Errorf("pin deferred primary symlink target %s: %w", plan.TargetPath, err)
		}
		return models.DeleteEntry{Path: plan.TargetPath, LinkTarget: target}, nil
	case organizer.LinkModeHard:
		if filepath.Clean(plan.SourcePath) != filepath.Clean(s.sourcePath) {
			return models.DeleteEntry{}, fmt.Errorf("pin deferred primary hardlink identity %s: plan source %s is not the admitted deferred source", plan.TargetPath, plan.SourcePath)
		}
		identity := s.sourceIdentity
		if !identity.known {
			return models.DeleteEntry{}, fmt.Errorf("pin deferred primary hardlink identity %s: the deferred source carries no admitted identity", plan.TargetPath)
		}
		entry := models.DeleteEntry{
			Path:            plan.TargetPath,
			IdentityPinned:  true,
			IdentitySize:    identity.size,
			IdentityModUnix: identity.modTime.Unix(),
		}
		if identity.hasDevIno {
			entry.IdentityStrong = true
			entry.IdentityDev = identity.dev
			entry.IdentityIno = identity.ino
		}
		return entry, nil
	default:
		info, digest, err := fsutil.PartialCopyDigest(s.fs, plan.SourcePath)
		if err != nil {
			return models.DeleteEntry{}, fmt.Errorf("pin deferred primary copy digest %s: %w", plan.TargetPath, err)
		}
		return models.DeleteEntry{Path: plan.TargetPath, CopySize: info.Size(), CopyPartialSHA256: digest}, nil
	}
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

func (s *artifactStage) markUnownedSkipped(target string) {
	if s.unownedSkips == nil {
		s.unownedSkips = map[string]bool{}
	}
	s.unownedSkips[filepath.Clean(target)] = true
}

func (s *artifactStage) skippedUnowned(target string) bool {
	return s.unownedSkips[filepath.Clean(target)]
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

// A successful batch rollback is not enough if a sidecar failed before its
// inverse could be armed. Re-prove admitted source identities before treating
// a resident-only publication as fully compensated.
func (s *artifactStage) clearRestoredResidentSidecars() {
	if !s.residentSidecarMutation {
		return
	}
	for _, sibling := range s.siblings {
		if sibling.subtitle || sibling.deferred {
			if err := s.revalidateAdmittedSource(sibling.sourcePath, sibling.identity); err != nil {
				return
			}
		}
	}
	s.residentSidecarMutation = false
}
