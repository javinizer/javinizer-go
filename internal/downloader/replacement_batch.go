package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// ReplacementBatch extends the downloader replacement ledger across a staged
// multi-file publication. Every occupied destination is moved to an owned,
// same-directory backup and journaled before the publisher is allowed to run.
// Rollback unwinds all published legs in reverse order.
type ReplacementBatch struct {
	fs       afero.Fs
	opID     string
	recorder ReplacementRecorder
	legs     []*replacementBatchLeg
}

type replacementBatchLeg struct {
	destination    string
	backup         string
	replaced       bool
	installed      bool
	installedID    *fsutil.BoundInstallIdentity
	rollbackOrigin string
	releaseLock    func()
	releaseBusy    func()
}

// NewReplacementBatch creates a staged publication transaction.
func NewReplacementBatch(fs afero.Fs, opID string, recorder ReplacementRecorder) (*ReplacementBatch, error) {
	if fs == nil {
		return nil, errors.New("staged replacement publication requires a filesystem")
	}
	return &ReplacementBatch{fs: fs, opID: strings.TrimSpace(opID), recorder: recorder}, nil
}

// Preflight rejects every non-regular occupied destination before any final is
// touched. Lstat semantics deliberately refuse symlink objects, including
// dangling links.
func (b *ReplacementBatch) Preflight(destinations []string) error {
	seen := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		clean := filepath.Clean(destination)
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("duplicate staged publication destination: %s", destination)
		}
		seen[clean] = struct{}{}
		info, err := lstatBackupCandidate(b.fs, destination)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect staged publication destination %s: %w", destination, err)
		}
		if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("staged publication destination is not a regular file: %s", destination)
		}
	}
	return nil
}

// BeforePublish arms one destination. The returned boolean reports whether a
// pre-existing regular file was journaled; an absent destination is tracked as
// created and removed on rollback.
func (b *ReplacementBatch) BeforePublish(ctx context.Context, destination string, allowReplace bool) (bool, error) {
	leg := &replacementBatchLeg{destination: filepath.Clean(destination)}
	leg.releaseLock = fsutil.SharedDestLocks().Acquire(leg.destination)
	info, err := lstatBackupCandidate(b.fs, leg.destination)
	if os.IsNotExist(err) {
		if _, parentErr := b.fs.Stat(filepath.Dir(leg.destination)); os.IsNotExist(parentErr) {
			b.legs = append(b.legs, leg)
			return false, nil
		}
	}
	if err == nil || os.IsNotExist(err) {
		busyRelease, busyErr := fsutil.AcquireReplacementBusy(b.fs, leg.destination)
		if busyErr != nil {
			leg.release()
			return false, fmt.Errorf("arm staged publication destination %s: %w", destination, busyErr)
		}
		leg.releaseBusy = busyRelease
		// The marker acquisition is a blocking boundary. Reclassify while the
		// process lock is still held before moving any bytes aside.
		info, err = lstatBackupCandidate(b.fs, leg.destination)
	}
	if os.IsNotExist(err) {
		b.legs = append(b.legs, leg)
		return false, nil
	}
	if err != nil {
		leg.release()
		return false, fmt.Errorf("inspect staged publication destination %s: %w", destination, err)
	}
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		leg.release()
		return false, fmt.Errorf("staged publication destination is not a regular file: %s", destination)
	}
	if !allowReplace {
		leg.release()
		return false, fmt.Errorf("staged publication destination exists and overwrite is disabled: %s", destination)
	}
	if b.recorder == nil || b.opID == "" {
		leg.release()
		return false, fmt.Errorf("replace artifact destination %s requires an armed durable recorder", destination)
	}
	var claim os.FileInfo
	var claimErr error
	for attempt := 0; attempt < backupNameClaimTries && leg.backup == ""; attempt++ {
		candidate, reservation, claimError := claimOverwriteBackupPath(b.fs, leg.destination, b.opID)
		if claimError != nil {
			claimErr = claimError
			break
		}
		if verifyErr := overwriteBackupReservationStillOurs(b.fs, candidate, reservation); verifyErr != nil {
			claimErr = verifyErr
			continue
		}
		leg.backup, claim = candidate, reservation
	}
	if leg.backup == "" {
		leg.release()
		return false, fmt.Errorf("claim staged replacement backup for %s: %w", destination, claimErr)
	}
	if err := handoffToReservedBackup(b.fs, leg.destination, leg.backup, claim); err != nil {
		leg.release()
		return false, fmt.Errorf("set aside staged replacement destination %s: %w", destination, err)
	}
	facts, err := captureReplacementBackupFacts(b.fs, leg.backup)
	if err == nil {
		err = b.recorder.RecordReplacement(ctx, b.opID, leg.destination, leg.backup, facts)
	}
	if err != nil {
		restored, restoreErr := restoreAsideBackup(b.fs, leg.destination, leg.backup)
		leg.release()
		if restoreErr != nil || !restored {
			return false, fmt.Errorf("journal staged replacement %s: %w (backup retained at %s; restore error: %v)", destination, err, leg.backup, restoreErr)
		}
		return false, fmt.Errorf("journal staged replacement %s: %w", destination, err)
	}
	leg.replaced = true
	b.legs = append(b.legs, leg)
	return true, nil
}

// YieldToLockedPublisher releases the in-process lock when the publisher has
// its own destination-locking critical section (the organizer video lane).
func (b *ReplacementBatch) YieldToLockedPublisher(destination string) {
	if leg := b.find(destination); leg != nil {
		leg.unlock()
	}
}

// ObservePublishResult binds an output that may have landed even when its
// publisher returned an error. The caller invokes this immediately on return
// while the cross-process busy marker is still held.
func (b *ReplacementBatch) ObservePublishResult(destination string) {
	leg := b.find(destination)
	if leg == nil || leg.installed {
		return
	}
	if info, err := lstatBackupCandidate(b.fs, leg.destination); err == nil {
		leg.installed, leg.installedID = true, fsutil.NewWeakBoundInstallIdentity(info)
	}
}

// ObservePublishResultBound is ObservePublishResult with the observation
// bound to the identity the publish operation PRODUCED (codex P1, PR #276,
// finding ntCe6) instead of whatever the destination name resolves to at
// observation time: a publisher that released its destination lock before
// this observation (the organizer's copied-sidecar lane) leaves a window in
// which an external writer can replace the freshly installed bytes, and a
// name-derived observation would record the foreign SUCCESSOR's identity as
// this batch's install — arming rollback's UnlinkVerified against it (an
// identity-verified delete of bytes the batch never wrote, executed before
// any displaced-backup restore). The re-proof rides
// fsutil.ObserveVerifiedInstall, the same predicate the rollback unlink
// applies, so observation and deletion can never disagree on which object is
// ours:
//
//   - match: the leg adopts the publish-time identity;
//   - vacant/indeterminate lookup: nothing is adopted and no error is
//     returned (the caller's ConfirmPublish surfaces the legacy failure);
//   - affirmative divergence: the leg stays UNINSTALLED and the typed
//     successor refusal propagates (ErrPublishSuccessorUnproven joined with
//     ErrPublishCompleted) — the occupant is retained byte-intact, exactly
//     the verified hard-link leg's post-link successor contract
//     (PRRT_kwDORn9KaM6nsX9a) applied to the copy lane's record;
//   - a nil installed identity is a caller bug and fails closed.
func (b *ReplacementBatch) ObservePublishResultBound(destination string, installed *fsutil.BoundInstallIdentity) error {
	leg := b.find(destination)
	if leg == nil || leg.installed {
		return nil
	}
	info, err := fsutil.ObserveVerifiedInstall(b.fs, leg.destination, installed)
	if err != nil {
		return err
	}
	if info != nil {
		leg.installed, leg.installedID = true, installed
	}
	return nil
}

// ConfirmPublish marks the most recently armed destination installed only
// after the publisher has landed its output.
func (b *ReplacementBatch) ConfirmPublish(ctx context.Context, destination string) error {
	leg := b.find(destination)
	if leg == nil {
		return fmt.Errorf("staged publication destination was not armed: %s", destination)
	}
	info, err := lstatBackupCandidate(b.fs, leg.destination)
	if err != nil {
		return fmt.Errorf("inspect staged publication result %s: %w", destination, err)
	}
	if info == nil || info.IsDir() {
		return fmt.Errorf("staged publication did not install a file at %s", destination)
	}
	leg.installed = true
	// An identity an earlier observation bound to the publish's own output
	// (ObservePublishResult's record or ObservePublishResultBound's proven
	// install) outranks a fresh name lookup here: re-deriving the identity
	// at confirmation time would re-anchor rollback to whatever occupies the
	// name AFTER the observation window closed — exactly the successor
	// substitution the bound observation exists to refuse (finding ntCe6).
	if leg.installedID == nil {
		leg.installedID = fsutil.NewWeakBoundInstallIdentity(info)
	}
	if leg.replaced {
		var confirmErr error
		if factual, ok := b.recorder.(ReplacementInstalledFactsRecorder); ok {
			facts, factsErr := captureInstalledReplacementFacts(b.fs, leg.destination)
			if factsErr != nil {
				return fmt.Errorf("capture staged replacement result %s: %w", destination, factsErr)
			}
			confirmErr = factual.ConfirmReplacementInstalled(ctx, b.opID, leg.destination, leg.backup, facts)
		} else {
			confirmErr = b.recorder.ConfirmReplacement(ctx, b.opID, leg.destination, leg.backup)
		}
		if confirmErr != nil {
			return fmt.Errorf("confirm staged replacement %s: %w", destination, confirmErr)
		}
	}
	leg.release()
	return nil
}

// SetRollbackOrigin preserves an installed output as the source-side inverse
// during rollback instead of deleting it. The origin must already have been
// removed by the caller; rollback uses no-replace move semantics.
func (b *ReplacementBatch) SetRollbackOrigin(destination, origin string) error {
	leg := b.find(destination)
	if leg == nil {
		leg = &replacementBatchLeg{destination: filepath.Clean(destination)}
		leg.releaseLock = fsutil.SharedDestLocks().Acquire(leg.destination)
		busy, err := fsutil.AcquireReplacementBusy(b.fs, leg.destination)
		if err != nil {
			leg.release()
			return fmt.Errorf("track staged publication destination %s: %w", destination, err)
		}
		leg.releaseBusy = busy
		info, err := lstatBackupCandidate(b.fs, leg.destination)
		if err != nil || info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			leg.release()
			return fmt.Errorf("staged publication destination has no regular installed output: %s: %w", destination, err)
		}
		leg.installed, leg.installedID = true, fsutil.NewWeakBoundInstallIdentity(info)
		b.legs = append(b.legs, leg)
		leg.release()
	}
	if !leg.installed {
		return fmt.Errorf("staged publication destination has no installed output: %s", destination)
	}
	if strings.TrimSpace(origin) == "" {
		leg.rollbackOrigin = ""
		return nil
	}
	leg.rollbackOrigin = filepath.Clean(origin)
	return nil
}

// Rollback restores replacements and removes created outputs in reverse order.
// A failed restore keeps the backup and its journal entry actionable.
func (b *ReplacementBatch) Rollback(ctx context.Context) error { return b.rollback(ctx, true) }

func (b *ReplacementBatch) rollback(ctx context.Context, releaseJournal bool) error {
	var joined error
	for i := len(b.legs) - 1; i >= 0; i-- {
		leg := b.legs[i]
		if leg.releaseLock == nil {
			leg.releaseLock = fsutil.SharedDestLocks().Acquire(leg.destination)
		}
		if leg.releaseBusy == nil {
			busy, err := fsutil.AcquireReplacementBusy(b.fs, leg.destination)
			if err != nil {
				joined = errors.Join(joined, fmt.Errorf("reacquire rollback destination %s: %w", leg.destination, err))
				leg.unlock()
				continue
			}
			leg.releaseBusy = busy
		}
		if leg.installed {
			var err error
			if leg.rollbackOrigin != "" {
				// Identity-bound move-back (codex P1, PRRT_kwDORn9KaM6qJY2g): the
				// destination name is ours only while it still names the object this
				// leg installed. ConfirmPublish released the destination marker, so
				// another writer can rename-swap the target, and a path-only
				// MoveFileNoReplace would then consume that foreign successor and
				// relocate it onto the original source path. The observation rides
				// the same predicate the unlink branch applies below (and fails
				// closed on a vacant or unreadable name), so a divergent occupant is
				// retained byte-intact and this leg reports the skipped compensation
				// instead of moving foreign bytes.
				if _, oerr := fsutil.ObserveVerifiedInstall(b.fs, leg.destination, leg.installedID); oerr != nil {
					joined = errors.Join(joined, fmt.Errorf("skipped rollback move-back of %s: the destination no longer provably names this leg's installed object: %w", leg.destination, oerr))
					leg.release()
					continue
				}
				err = fsutil.MoveFileNoReplace(b.fs, leg.destination, leg.rollbackOrigin)
			} else {
				err = fsutil.UnlinkVerifiedInstall(b.fs, leg.destination, leg.installedID)
			}
			if err != nil {
				joined = errors.Join(joined, fmt.Errorf("reverse staged publication %s during rollback: %w", leg.destination, err))
				leg.release()
				continue
			}
			leg.installed = false
		}
		if leg.replaced {
			restored, err := restoreAsideBackup(b.fs, leg.destination, leg.backup)
			if err != nil || !restored {
				joined = errors.Join(joined, fmt.Errorf("restore staged replacement %s from %s: %v", leg.destination, leg.backup, err))
				leg.release()
				continue
			}
			if releaseJournal {
				if err := b.recorder.ReleaseReplacement(ctx, b.opID, leg.destination, leg.backup); err != nil {
					if rearmErr := rearmReplacementBackup(b.fs, leg.destination, leg.backup); rearmErr != nil {
						markRollbackRearmFailed(ctx, downloadLedger{opID: b.opID, recorder: b.recorder}, leg.destination, leg.backup, rearmErr)
					}
					joined = errors.Join(joined, fmt.Errorf("release rolled-back replacement %s: %w", leg.destination, err))
				}
			}
		}
		leg.release()
	}
	return joined
}

func captureInstalledReplacementFacts(fs afero.Fs, path string) (models.ReplacementBackupFacts, error) {
	info, err := lstatBackupCandidate(fs, path)
	if err != nil {
		return models.ReplacementBackupFacts{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return captureReplacementBackupFacts(fs, path)
	}
	lr, ok := fs.(afero.LinkReader)
	if !ok {
		return models.ReplacementBackupFacts{}, fmt.Errorf("filesystem cannot read installed symlink %s", path)
	}
	target, err := lr.ReadlinkIfPossible(path)
	if err != nil {
		return models.ReplacementBackupFacts{}, err
	}
	sum := sha256.Sum256([]byte(target))
	return models.ReplacementBackupFacts{Size: int64(len(target)), ModUnix: info.ModTime().Unix(), SHA256: hex.EncodeToString(sum[:])}, nil
}

// IsReplacement reports whether destination displaced pre-existing bytes.
func (b *ReplacementBatch) IsReplacement(destination string) bool {
	leg := b.find(destination)
	return leg != nil && leg.replaced
}

func (b *ReplacementBatch) find(destination string) *replacementBatchLeg {
	clean := filepath.Clean(destination)
	for i := len(b.legs) - 1; i >= 0; i-- {
		if b.legs[i].destination == clean {
			return b.legs[i]
		}
	}
	return nil
}

func (l *replacementBatchLeg) unlock() {
	if l.releaseLock != nil {
		l.releaseLock()
		l.releaseLock = nil
	}
}

// ReleaseUninstalled frees a destination leg whose publish never landed
// (armed but never confirmed): the destination lock and busy marker release,
// and the leg drops out of the batch, so nothing observes ErrReplacementBusy
// for the server's lifetime.
func (b *ReplacementBatch) ReleaseUninstalled(destination string) error {
	target := filepath.Clean(destination)
	for i, leg := range b.legs {
		if leg.destination != target {
			continue
		}
		if leg.installed {
			return fmt.Errorf("cannot release installed staged publication destination %s", target)
		}
		leg.release()
		b.legs = append(b.legs[:i], b.legs[i+1:]...)
		return nil
	}
	return nil
}

func (l *replacementBatchLeg) release() {
	if l.releaseBusy != nil {
		l.releaseBusy()
		l.releaseBusy = nil
	}
	l.unlock()
}
