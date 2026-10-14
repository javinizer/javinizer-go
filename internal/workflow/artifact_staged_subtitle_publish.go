package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

var stagedSubtitleCopy = fsutil.CopyFileNoReplaceVerifiedDigestMode
var stagedSubtitlePartialDigest = fsutil.PartialCopyDigest
var stagedSubtitleBeforePublish = (*downloader.ReplacementBatch).BeforePublish
var stagedSubtitleConfirmPublish = (*downloader.ReplacementBatch).ConfirmPublish
var stagedSubtitleBindPublish = func(batch *downloader.ReplacementBatch, target string, installed *fsutil.BoundInstallIdentity) error {
	return batch.SetRollbackOriginBound(target, "", installed)
}

// Staging-only subtitle results describe temporary copies, not ownership of
// their final destinations. Keep them out of the generic replacement tree and
// translate their outcomes only after the no-replace publisher runs.
func (s *artifactStage) collectStagedSubtitleOutputs(result *organizer.OrganizeResult) error {
	if !s.inPlace || s.videoDeferred || result == nil {
		return nil
	}
	// Every admitted subtitle input stays out of generic artwork replacement,
	// including failed or omitted organizer seats. Only the canonical outputs
	// enrolled below may publish, through their no-replace ownership lane.
	for _, sibling := range s.siblings {
		if sibling.subtitle && sibling.stagedPath != "" {
			if s.excludedStagedSubtitleInputs == nil {
				s.excludedStagedSubtitleInputs = map[string]bool{}
			}
			s.excludedStagedSubtitleInputs[filepath.Clean(sibling.stagedPath)] = true
		}
	}
	s.stagedSubtitleResults = append([]organizer.SubtitleResult{}, result.Subtitles...)
	s.stagedSubtitleOutputs = map[string]int{}
	for i := range s.stagedSubtitleResults {
		sr := &s.stagedSubtitleResults[i]
		selfPath := sr.OriginalPath != "" && filepath.Clean(sr.OriginalPath) == filepath.Clean(sr.NewPath)
		source := ""
		for _, sibling := range s.siblings {
			if sibling.subtitle && filepath.Clean(sibling.stagedPath) == filepath.Clean(sr.OriginalPath) {
				source = sibling.sourcePath
				break
			}
		}
		if source != "" {
			sr.OriginalPath = source
		}
		if sr.NewPath == "" {
			continue
		}
		staged := filepath.Clean(sr.NewPath)
		rel, err := filepath.Rel(s.root, staged)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("staged subtitle output escapes staging area: %s", staged)
		}
		sr.NewPath = filepath.Join(s.finalRoot, rel)
		// A self-path skip owns a private canonical input, not the final target.
		// It still needs final no-replace publication when the folder changes.
		if sr.Copied || sr.Moved || sr.Planned || fsutil.PublishCompleted(sr.Error) || (selfPath && sr.Error == nil) {
			if source == "" {
				return fmt.Errorf("staged subtitle output has no admitted source: %s", staged)
			}
			s.stagedSubtitleOutputs[staged] = i
			sr.Copied, sr.Moved, sr.Planned = false, false, false
			sr.Skipped = true
			sr.InstalledIdentity = nil
		}
	}
	return nil
}

func (s *artifactStage) isStagedSubtitleOutput(path string) bool {
	_, ok := s.stagedSubtitleOutputs[filepath.Clean(path)]
	return ok
}

func (s *artifactStage) publishStagedSubtitles(ctx context.Context, o *applyOrchImpl, opID OperationID, result *organizer.OrganizeResult) error {
	if result != nil && s.stagedSubtitleResults != nil {
		result.Subtitles = s.stagedSubtitleResults
	}
	if len(s.stagedSubtitleOutputs) == 0 {
		return nil
	}
	if s.publishBatch == nil || result == nil {
		return fmt.Errorf("staged subtitle publication requires an armed batch and organize result")
	}
	sources := make([]string, 0, len(s.stagedSubtitleOutputs))
	for source := range s.stagedSubtitleOutputs {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	// Share this backing slice with the final result so skipped and refused
	// destinations cannot graduate into a copied-subtitle deletion ledger.
	result.Subtitles = s.stagedSubtitleResults
	for _, source := range sources {
		sr := &result.Subtitles[s.stagedSubtitleOutputs[source]]
		if err := s.publishStagedSubtitle(ctx, o, opID, source, sr); err != nil {
			return err
		}
	}
	return nil
}

func (s *artifactStage) publishStagedSubtitle(ctx context.Context, o *applyOrchImpl, opID OperationID, source string, sr *organizer.SubtitleResult) error {
	target := sr.NewPath
	if _, err := lstatArtifactSource(s.fs, target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect final subtitle %s: %w", target, err)
	}
	info, err := lstatArtifactSource(s.fs, source)
	if err != nil {
		return fmt.Errorf("inspect staged subtitle %s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staged subtitle is not a regular file: %s", source)
	}
	admitted := captureArtifactSourceIdentity(s.fs, source, info)
	if err := s.fs.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("create final subtitle directory: %w", err)
	}
	if _, err := stagedSubtitleBeforePublish(s.publishBatch, ctx, target, false); err != nil {
		// An occupant may arrive after the initial check. BeforePublish(false)
		// never vacates it; retain the organizer's skip-on-exists behavior.
		if _, occupiedErr := lstatArtifactSource(s.fs, target); occupiedErr == nil {
			return nil
		}
		return err
	}
	pinned, partial, err := stagedSubtitlePartialDigest(s.fs, source)
	if err != nil {
		return fmt.Errorf("pin staged subtitle: %w", err)
	}
	if !admitted.matches(s.fs, source, pinned) {
		return fmt.Errorf("%w: staged subtitle %s", errArtifactSourceChanged, source)
	}
	durable := o.revertLog != nil && opID != ""
	if durable {
		// An unsealed copy pin is intent only, never authority to delete a
		// same-content foreign file that wins the no-replace race.
		if err := o.revertLog.RecordDeleteIntent(ctx, opID, []models.DeleteEntry{{Path: target, CopySize: pinned.Size(), CopyPartialSHA256: partial}}); err != nil {
			return fmt.Errorf("journal staged subtitle intent: %w", err)
		}
	}
	proof := func(path string, current os.FileInfo) error {
		if !admitted.matches(s.fs, path, current) {
			return fmt.Errorf("%w: staged subtitle %s", errArtifactSourceChanged, path)
		}
		return nil
	}
	digest, installed, copyErr := stagedSubtitleCopy(s.fs, source, target, proof, info.Mode().Perm())
	if fsutil.PublishRefusal(copyErr) || fsutil.PublishSuccessorUnproven(copyErr) {
		sr.SuccessorRefused = fsutil.PublishSuccessorUnproven(copyErr)
		return s.publishBatch.ReleaseUninstalled(target)
	}
	if copyErr != nil && installed == nil {
		return fmt.Errorf("publish staged subtitle %s: %w", target, copyErr)
	}
	if err := stagedSubtitleBindPublish(s.publishBatch, target, installed); err != nil {
		if fsutil.PublishSuccessorUnproven(err) {
			sr.SuccessorRefused = true
			return s.publishBatch.ReleaseUninstalled(target)
		}
		return fmt.Errorf("bind staged subtitle %s: %w", target, err)
	}
	// A completed-with-error copy still owns its bound install. Observe it
	// before returning so rollback removes only that object, never a successor.
	if copyErr != nil {
		return fmt.Errorf("publish staged subtitle %s: %w", target, copyErr)
	}
	if err := stagedSubtitleConfirmPublish(s.publishBatch, ctx, target); err != nil {
		return err
	}
	// Confirmation must not promote an absent or substituted output into
	// durable ownership. Its rollback identity stays the publisher's bound
	// object even when a later writer claims the destination name.
	observed, observeErr := fsutil.ObserveVerifiedInstall(s.fs, target, installed)
	if observed == nil || observeErr != nil {
		return errors.Join(fmt.Errorf("confirm bound staged subtitle %s", target), observeErr)
	}
	if durable {
		if err := o.revertLog.FinalizeDeleteIntentCopyDigest(ctx, opID, target, digest); err != nil {
			return fmt.Errorf("seal staged subtitle intent: %w", err)
		}
	}
	sr.Copied, sr.Skipped = true, false
	sr.InstalledIdentity = installed
	sr.Error = nil
	return nil
}
