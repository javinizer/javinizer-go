package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

var stagedVideoMove = fsutil.MoveFileNoReplaceVerifiedMode
var stagedVideoPartialDigest = fsutil.PartialCopyDigest
var stagedVideoDigest = artifactDigest
var stagedVideoBeforePublish = (*downloader.ReplacementBatch).BeforePublish
var stagedVideoBindPublish = (*downloader.ReplacementBatch).SetRollbackOriginBound
var stagedVideoConfirmPublish = (*downloader.ReplacementBatch).ConfirmPublish

// registerStagedVideoOutputs follows admitted source artifacts through the
// stage's rename/rehome. Extension alone never enrolls downloaded trailers or
// other generated media in this lane.
func (s *artifactStage) registerStagedVideoOutputs(stagedVideo, primary string) {
	s.stagedVideoOutputs = make(map[string]bool)
	if primary != "" {
		s.stagedVideoOutputs[filepath.Clean(primary)] = true
	}
	for _, sibling := range s.siblings {
		if sibling.deferred || sibling.stagedPath == "" || !isStagedArtifactVideoExtension(strings.ToLower(filepath.Ext(sibling.sourcePath))) {
			continue
		}
		path := sibling.stagedPath
		if stagedVideo != "" && s.stagedSource != "" && s.original.Organize.LinkMode == organizer.LinkModeNone {
			path = filepath.Join(filepath.Dir(stagedVideo), stagedArtifactSiblingName(filepath.Base(s.stagedSource), filepath.Base(stagedVideo), filepath.Base(path)))
		}
		s.stagedVideoOutputs[filepath.Clean(path)] = false
	}
}

// An already-normalized in-place source needs no publication at all. Prove
// that its final name still holds the admitted source; matching bytes alone
// cannot grant ownership of a foreign file or authorize a delete intent.
func (s *artifactStage) stagedVideoResident(target string, primary bool) (bool, error) {
	if primary {
		if s.sourceIdentity.known && filepath.Clean(target) == filepath.Clean(s.sourcePath) {
			return true, s.revalidateAdmittedSource(target, s.sourceIdentity)
		}
		return false, nil
	}
	for _, sibling := range s.siblings {
		if sibling.identity.known && filepath.Clean(target) == filepath.Clean(sibling.sourcePath) {
			return true, s.revalidateAdmittedSource(target, sibling.identity)
		}
	}
	return false, nil
}

// publishStagedVideo preserves the source-video no-replace contract at the
// final tree boundary. A requested primary may first displace a journaled
// occupant under ForceUpdate; automatically included multipart files never
// acquire that authority. The actual install is always atomic no-replace,
// including a late arrival after the replacement arm.
func (s *artifactStage) publishStagedVideo(source, target string, allowReplace bool) error {
	if s.publishBatch == nil {
		return fmt.Errorf("staged video publication requires an armed batch")
	}
	info, err := lstatArtifactSource(s.fs, source)
	if err != nil {
		return fmt.Errorf("inspect staged video %s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staged video is not a regular file: %s", source)
	}
	admitted := captureArtifactSourceIdentity(s.fs, source, info)
	replaced, err := stagedVideoBeforePublish(s.publishBatch, s.publishCtx, target, allowReplace)
	if err != nil {
		return err
	}
	durable := !replaced && s.publishVideoLog != nil && s.publishVideoOpID != ""
	digest := ""
	if durable {
		pinned, partial, err := stagedVideoPartialDigest(s.fs, source)
		if err != nil {
			return fmt.Errorf("pin staged video: %w", err)
		}
		if !admitted.matches(s.fs, source, pinned) {
			return fmt.Errorf("%w: staged video %s", errArtifactSourceChanged, source)
		}
		// Intent alone cannot delete a same-content foreign winner. It is
		// sealed only after the verified move proves this apply installed.
		if err := s.publishVideoLog.RecordDeleteIntent(s.publishCtx, s.publishVideoOpID, []models.DeleteEntry{{Path: target, CopySize: pinned.Size(), CopyPartialSHA256: partial}}); err != nil {
			return fmt.Errorf("journal staged video intent: %w", err)
		}
		digest, err = stagedVideoDigest(s.fs, source)
		if err != nil {
			return fmt.Errorf("digest staged video: %w", err)
		}
	}
	proof := func(path string, current os.FileInfo) error {
		if !admitted.matches(s.fs, path, current) {
			return fmt.Errorf("%w: staged video %s", errArtifactSourceChanged, path)
		}
		return nil
	}
	// Same-volume staging stays a rename, rather than copying a large video
	// again. The verified move returns the identity of its own output.
	installed, moveErr := stagedVideoMove(s.fs, source, target, proof, info.Mode().Perm())
	if fsutil.PublishSuccessorUnproven(moveErr) || (moveErr != nil && installed == nil) {
		return fmt.Errorf("publish staged video %s: %w", target, moveErr)
	}
	// Empty rollback origin means removal of our exact install. A later
	// confirmation never replaces this bound identity with a name-only one.
	if err := stagedVideoBindPublish(s.publishBatch, target, "", installed); err != nil {
		return fmt.Errorf("bind staged video %s: %w", target, err)
	}
	if moveErr != nil {
		return fmt.Errorf("publish staged video %s: %w", target, moveErr)
	}
	if err := stagedVideoConfirmPublish(s.publishBatch, s.publishCtx, target); err != nil {
		return err
	}
	observed, observeErr := fsutil.ObserveVerifiedInstall(s.fs, target, installed)
	if observed == nil || observeErr != nil {
		return errors.Join(fmt.Errorf("confirm bound staged video %s", target), observeErr)
	}
	if durable {
		if err := s.publishVideoLog.FinalizeDeleteIntentCopyDigest(s.publishCtx, s.publishVideoOpID, target, digest); err != nil {
			return fmt.Errorf("seal staged video intent: %w", err)
		}
	}
	return nil
}
