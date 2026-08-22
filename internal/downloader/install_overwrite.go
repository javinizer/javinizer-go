package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	backupSuffixForDest  = ".dlbak"
	backupNameClaimTries = 64
	// createPublishMaxAttempts bounds the create path's collision
	// reclassification (wave-15): every pass means a foreign writer won the
	// publish against the no-replace primitive and its bytes take the
	// armed-overwrite discipline. Churn beyond the bound fails the install
	// rather than spinning against an adversarial name.
	createPublishMaxAttempts = 8
)

// backupOrdinal gives every backup attempt a process-local tail. opID alone
// cannot disambiguate: ONE operation may overwrite the same destination
// twice (e.g. poster + cropped re-write), and without the ordinal the second
// rename would clobber the first backup while both journal entries point at
// it — revert could never recover the original bytes (codex P3 round 1).
// claimOverwriteBackupPath advances past occupied tails across restarts.
var backupOrdinal atomic.Uint64
var restoreCopyOrdinal atomic.Uint64

// stagedRepublishOrdinal names the wave-48 bound-publish re-stage copies
// (fsutil.PublishStagedBound re-stages the validated bytes FROM ITS OPEN
// HANDLE into a fresh O_EXCL name when a substitution was proven
// mid-publish); the names are destination-adjacent ".dlpub.<hex>" scratch —
// never .dlbak ownership identifiers.
var stagedRepublishOrdinal atomic.Uint64

// nextStagedRepublishOrdinal mints the next bound-publish re-stage ordinal
// (wave-53): a named func rather than a per-call literal so the minting has
// one coverable site shared by every bound-publish caller (the re-stage runs
// only in the rare proven-substitution recovery leg, which no unit test
// triggers end-to-end; a focused test covers the minting directly).
func nextStagedRepublishOrdinal() uint64 { return stagedRepublishOrdinal.Add(1) }

// publishStagedBoundFn is the wave-48 bound-publish production seam
// (fsutil.PublishStagedBoundInfo), mirroring restoreStagingOwnershipFn's
// seam discipline: production never deviates, tests wedge the
// verify→publish window by wrapping the invocation's Publish func to replay
// the directory-writer substitution finding-6 closes.
var publishStagedBoundFn = fsutil.PublishStagedBoundInfo

// restoreStagingOwnershipFn is the downloader rollback's ownership hand-off
// seam, mirroring history's same-named discipline (restoreStagingOwnershipFn
// in reverter_replacements_p3.go): production forwards to
// fsutil.RestoreStagingOwnership; tests record/replay the exact mid-flow
// instant between the staged copy and the wave-30 bound publish.
var restoreStagingOwnershipFn = fsutil.RestoreStagingOwnership

// rollbackPublishStagedBoundInfoFn is the bound-publish seam behind
// copyBackupToDestPublish (mirroring history's publishStagedBoundInfoFn):
// production forwards to fsutil.PublishStagedBoundInfo; tests replay the
// r15 completed-with-identity outcome (the ENOSYS-times-skipped leg —
// PublishStagedBoundInfo hands back the post-publish-verified destination
// stat alongside an ErrPublishCompleted-carrying error) without the
// cross-package fd-times plumbing.
var rollbackPublishStagedBoundInfoFn = fsutil.PublishStagedBoundInfo

// copyBackupToDest restores the backup bytes onto dest WITHOUT consuming the
// backup: staged adjacent write + replace-aware swap (Win-safe), streamed
// through a bounded buffer. Used by the confirm-failure rollback so the
// journal entry can never end up pointing at consumed bytes (codex P3 R9-1).
//
//nolint:unused // test-facing error-only shape pinned across waves 7–30; the confirm-failure rollback rides the wave-31 bound variant.
func copyBackupToDest(fsys afero.Fs, backup, dest string) error {
	_, err := copyBackupToDestBound(fsys, backup, dest)
	return err
}

// rollbackCopyFacts carries the two ownership bindings a confirm-failure
// rollback needs AFTER the restore copy (wave-31, codex local round 1,
// PR#215 findings L1/L2):
//
//   - restored binds the object the copy PUBLISHED at dest — the post-publish
//     destination stat fsutil.PublishStagedBoundInfo proved SameFile with the
//     staged inode (never a pre-publish staged capture, which the recovery
//     loop's re-stage would invalidate, nor a window-poisonable fresh
//     capture). The destination must still name it before the backup is
//     removed and the journal entry retracted.
//   - copied binds the backup object the copy STREAMED FROM — the validated
//     pre-open no-follow Lstat object (dev/inode when the filesystem exposes
//     it, size and mtime), the in-memory twin of history's wave-25
//     removeReplacementBackup copiedFrom binding. The backup NAME's
//     occupant is verified against it before any removal.
type rollbackCopyFacts struct {
	restored installedDestIdentity
	copied   os.FileInfo
}

// copyBackupToDestBound is copyBackupToDest with both ownership bindings
// handed back (wave-31): the confirm-failure rollback revalidates the
// destination against restored before releasing the backup, and binds the
// backup removal to copied.
// copyBackupToDestBoundFacts is copyBackupToDestBound with the ORIGINALLY
// armed backup identity threaded in (wave-62, codex P2, PR#215 finding F2):
// the confirm-failure rollback authenticates the opened backup against the
// arm-time capture (dev/ino consistency + size/mtime) BEFORE any bytes reach
// dest — a foreign file swapped onto the backup name mid-window refuses typed
// ErrTakeAsideForeign, preserving whatever currently sits at the backup name
// while leaving dest intact. A nil capture keeps the legacy posture.
func copyBackupToDestBoundFacts(fsys afero.Fs, backup, dest string, armedFacts *models.ReplacementBackupFacts) (rollbackCopyFacts, error) {
	return copyBackupToDestPublish(fsys, backup, dest, fsutil.ReplaceFile, false, armedFacts)
}

//nolint:unused // test-facing bound shape; the confirm-failure rollback rides the wave-62 facts variant (copyBackupToDestBoundFacts) which authenticates the opened backup against the originally armed identity before any bytes reach dest.
func copyBackupToDestBound(fsys afero.Fs, backup, dest string) (rollbackCopyFacts, error) {
	return copyBackupToDestPublish(fsys, backup, dest, fsutil.ReplaceFile, false, nil)
}

// copyBackupToDestNoReplace is copyBackupToDest whose staged publish NEVER
// replaces an occupied destination: callers who copy onto a name their own
// rollback just vacated (the re-arm direction) must not clobber a foreign
// object that claimed the name mid-window. A collision drops the staged copy
// and returns the typed fsutil.ErrPublishCollision (see wave-15).
func copyBackupToDestNoReplace(fsys afero.Fs, backup, dest string) error {
	_, err := copyBackupToDestPublish(fsys, backup, dest, fsutil.PublishNoReplace, true, nil)
	return err
}

func copyBackupToDestPublish(fsys afero.Fs, backup, dest string, publish func(afero.Fs, string, string) error, noReplace bool, armedFacts *models.ReplacementBackupFacts) (rollbackCopyFacts, error) {
	// Validate the path before opening it: Stat/Open would follow a hostile
	// backup symlink and copy its target into the media directory.
	sourceInfo, err := lstatRestoreSource(fsys, backup)
	if err != nil {
		return rollbackCopyFacts{}, fmt.Errorf("open backup: %w", err)
	}
	if sourceInfo == nil {
		return rollbackCopyFacts{}, refuseRestoreSource(backup, "filesystem returned no file information")
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return rollbackCopyFacts{}, refuseRestoreSource(backup, "backup is a symlink")
	}
	if !sourceInfo.Mode().IsRegular() {
		return rollbackCopyFacts{}, refuseRestoreSource(backup, fmt.Sprintf("backup is not a regular file (mode %s)", sourceInfo.Mode()))
	}

	// The open itself is platform-specific (see downloader.go's
	// restoreOpenReplacementSource): POSIX passes O_NOFOLLOW through OsFs to
	// os.OpenFile; Windows opens with FILE_FLAG_OPEN_REPARSE_POINT and refuses
	// a reparse point on the returned handle. MemMapFs has no symlink
	// representation and safely ignores the protection; the Lstat gate above
	// remains its available protection.
	src, err := restoreOpenReplacementSource(fsys, backup)
	if err != nil {
		// A no-follow open can report the race before a handle exists. Recheck
		// with Lstat so a path that became a symlink is reported as the same
		// safe refusal posture as the pre-open gate; unrelated open errors keep
		// their original classification for callers and retries.
		if currentInfo, lerr := lstatRestoreSource(fsys, backup); lerr == nil && currentInfo != nil && currentInfo.Mode()&os.ModeSymlink != 0 {
			return rollbackCopyFacts{}, refuseRestoreSource(backup, "backup became a symlink before open")
		}
		return rollbackCopyFacts{}, fmt.Errorf("open backup: %w", err)
	}
	defer func() { _ = src.Close() }()

	// File.Stat is fstat for afero.OsFs. Verify the object actually opened is
	// still regular, and compare identity when the platform exposes Dev/Ino.
	openedInfo, err := src.Stat()
	if err != nil {
		return rollbackCopyFacts{}, fmt.Errorf("stat opened backup: %w", err)
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		return rollbackCopyFacts{}, refuseRestoreSource(backup, "opened object is not a regular file")
	}
	if sourceDev, sourceIno, sourceOK := restoreSourceIdentity(sourceInfo); sourceOK {
		if openedDev, openedIno, openedOK := restoreSourceIdentity(openedInfo); openedOK && (sourceDev != openedDev || sourceIno != openedIno) {
			return rollbackCopyFacts{}, refuseRestoreSource(backup, "opened object differs from the Lstat object")
		}
	}
	// Wave-62 (codex P2, PR#215 finding F2): authenticate the OPENED backup
	// AGAINST the ORIGINALLY armed identity captured pre-RecordReplacement
	// (the dev/ino consistency above + the arm-time size/mtime) BEFORE any
	// bytes reach dest. copyBackupToDestBound re-proved only the CURRENT
	// backup name, so a foreign file swapped onto the name mid-window was
	// streamed into the media directory. The copy now refuses up front on a
	// stamped-facts mismatch (typed ErrTakeAsideForeign — foreign bytes
	// preserved): whatever currently sits at the backup name stays put and
	// dest is left intact. A nil/unstamped capture keeps the legacy posture.
	if armedFacts != nil && armedFacts.ModUnix != 0 {
		if openedInfo.Size() != armedFacts.Size || openedInfo.ModTime().Unix() != armedFacts.ModUnix {
			return rollbackCopyFacts{}, fmt.Errorf("rollback backup %s no longer names the armed set-aside (identity mismatch — armed %d bytes @ %d, found %d bytes @ %d); foreign bytes preserved, dest untouched: %w", backup, armedFacts.Size, armedFacts.ModUnix, openedInfo.Size(), openedInfo.ModTime().Unix(), fsutil.ErrTakeAsideForeign)
		}
	}
	// Wave-31 (L2): the backup removal after this copy binds to the exact
	// object streamed here — history's wave-25 copiedFrom discipline: the
	// validated pre-open no-follow Lstat object (identity-proven against the
	// opened handle above), carried as the FileInfo so a foreign plant
	// swapped onto the backup NAME in the copy→remove window is never
	// unlinked in its place. On virtual filesystems the infos are live
	// views, keeping the removal-side comparison self-consistent exactly
	// like history's gate.
	facts := rollbackCopyFacts{copied: sourceInfo}

	stagedOrdinal := restoreCopyOrdinal.Add(1)
	// codex P3 R18h: keep the backup's permission bits through the swap too.
	mode := openedInfo.Mode().Perm()
	staged, dstFile, err := fsutil.CreateExclusiveStagingFile(fsys, dest, ".dlrstr", stagedOrdinal, mode)
	if err != nil {
		return rollbackCopyFacts{}, fmt.Errorf("stage rollback: %w", err)
	}
	buf := make([]byte, 256*1024)
	// Wave-63 (codex P2, PR#215 finding F1): size+mtime are forgeable, so a
	// same-size+mtime substitute rode the wave-62 gate. Tee the stream through
	// sha256 and compare to the armed digest; mismatch discards the staged
	// copy (dest untouched, foreign preserved) and refuses ErrTakeAsideForeign
	// before the publish. An unstamped capture keeps the wave-62 posture.
	digest := sha256.New()
	if _, cerr := io.CopyBuffer(dstFile, io.TeeReader(src, digest), buf); cerr != nil {
		// The staged name (dest-adjacent .dlrstr.<ordinal>) is
		// near-predictable: discard ONLY while it provably names the handle's
		// inode — a substitute planted in the copy→remove window is preserved
		// byte-intact (the wave-45 bound cleanup; it closes the handle).
		fsutil.DiscardFailedExclusiveStaging(fsys, staged, dstFile)
		return rollbackCopyFacts{}, fmt.Errorf("copy rollback: %w", cerr)
	}
	if armedFacts != nil && armedFacts.SHA256 != "" && hex.EncodeToString(digest.Sum(nil)) != armedFacts.SHA256 {
		fsutil.DiscardFailedExclusiveStaging(fsys, staged, dstFile)
		return rollbackCopyFacts{}, fmt.Errorf("rollback backup %s sha256 mismatch — armed %s, streamed %s; foreign bytes preserved, dest untouched: %w", backup, armedFacts.SHA256, hex.EncodeToString(digest.Sum(nil)), fsutil.ErrTakeAsideForeign)
	}
	// Re-apply the backup's ownership before the swap: a privileged restore of
	// another account's backup must not leave the restored bytes owned by the
	// Javinizer account once the backup is deleted. Best-effort —
	// unprivileged restores cannot chown and must still succeed.
	// wave-29 (codex P1, PR#215): like history's restore/re-arm staging, every
	// remaining metadata leg runs THROUGH THE OPEN HANDLE before the publish —
	// a directory writer renaming the staged name away mid-flow can no longer
	// redirect chmod/times/chown through a planted link.
	restoreStagingOwnershipFn(fsys, dstFile, openedInfo)
	// wave-30 (codex P1, PR#215): the staging tail runs through
	// fsutil.PublishStagedBound — history's restore/re-arm flows bind the
	// staged identity ACROSS the publish through the same helper, so the
	// verify/publish/re-verify discipline cannot drift between the three
	// funnels. The wave-29 proof alone left a verify→publish window where a
	// directory writer could plant a substitute that the path publish then
	// installed at dest before the genuine backup was consumed; the bound
	// helper holds the handle through the publish (POSIX) and re-verifies
	// the landed destination, republishing from the handle after a proven
	// substitution and refusing typed (backup retained) on exhaustion.
	// wave-31 (codex local round 1, PR#215 finding L1): the Info variant hands
	// back the post-publish-VERIFIED destination object so the caller can
	// revalidate dest against exactly the staged object as it landed —
	// recovery-loop re-stages included — before the backup is removed and the
	// journal entry retracted.
	published, pubErr := rollbackPublishStagedBoundInfoFn(fsutil.StagedPublish{
		FS:          fsys,
		Publish:     publish,
		NoReplace:   noReplace,
		Staged:      staged,
		Handle:      dstFile,
		Dest:        dest,
		Atime:       openedInfo.ModTime(),
		Mtime:       openedInfo.ModTime(),
		ApplyTimes:  true,
		Suffix:      ".dlrstr",
		NextOrdinal: func() uint64 { return restoreCopyOrdinal.Add(1) },
	})
	if pubErr != nil {
		var timesErr *fsutil.StagingTimesError
		switch {
		case errors.Is(pubErr, fsutil.ErrPublishStagedVerify):
			// Unproven staged name (possible foreign plant): never removed,
			// handle already closed by the helper — wave-29 posture.
			return rollbackCopyFacts{}, fmt.Errorf("stage rollback identity: %w", pubErr)
		case errors.As(pubErr, &timesErr):
			// Codex P2 (r26 downloader twin): the handle was closed before
			// this cleanup — pathname removal could delete foreign bytes post-
			// swap. The staged name is ordinal-salted (.rstr.<n>) so residue
			// is inert; retain with a warn, never unlink unproven.
			logging.Warnf("downloader: rollback staged name %s left in place after the times failure (unproven identity) — residue inert", staged)
			return rollbackCopyFacts{}, fmt.Errorf("stage rollback times: %w", pubErr)
		case errors.Is(pubErr, fsutil.ErrPublishStagedClose):
			logging.Warnf("downloader: rollback staged name %s left in place after the close failure (unproven identity) — residue inert", staged)
			return rollbackCopyFacts{}, fmt.Errorf("close rollback: %w", pubErr)
		default:
			// Wave-34 (codex local review round 4, PR#215 finding F3): a
			// publish failure carrying fsutil.ErrPublishCompleted proves the
			// DESTINATION already carries the staged bytes while fsutil
			// DELIBERATELY left the staged name in place — the POSIX hard-link
			// fallback's staged cleanup could not re-prove it
			// (fsutil.ErrPublishNoReplaceStagedUnverified: the name may now
			// address a foreign object swapped on mid-window) or its unlink
			// failed with the destination rollback failing too (wave-20).
			// Unlinking here could destroy those possibly-foreign bytes, so
			// only a provably-unpublished staged copy (our own) is dropped.
			// r12: the ENOSYS leg (no fd-scoped times primitive on the platform)
			// joins the completed class with the times SKIPPED after a verified
			// publish — the successful publish consumed the staged name there,
			// so the skipped Remove is a no-op on that leg as well.
			// r15 (codex P2, PR#215 — mirror r14's copyRestoreBytesPublish):
			// a completed leg carrying a VERIFIED non-nil identity (the
			// ENOSYS-times-skipped publish — PublishStagedBoundInfo hands back
			// the post-publish-verified destination stat) means the publish
			// SUCCEEDED: dest provably carries the restored bytes. Return the
			// identity so the caller's wave-31 revalidation + backup removal +
			// journal retraction run like the plain-success leg; on drift the
			// caller's wave-31 refusal fires. Pre-r15 this returned the error,
			// so the ConfirmReplacement-failure rollback never removed the backup
			// nor retracted the entry — the armed journal entry stayed against
			// the dest forever. A nil identity keeps the legacy discipline below.
			if fsutil.PublishCompleted(pubErr) && published != nil {
				facts.restored = installedIdentityFromFileInfo(published)
				return facts, nil
			}
			if fsutil.PublishCompleted(pubErr) {
				logging.Warnf("downloader: staged rollback copy %s left in place — publish completed but the staged name could not be re-proven (possibly foreign); manual cleanup advised: %v", staged, pubErr)
			} else {
				_ = fsys.Remove(staged)
			}
			return rollbackCopyFacts{}, fmt.Errorf("swap rollback: %w", pubErr)
		}
	}
	facts.restored = installedIdentityFromFileInfo(published)
	return facts, nil
}

// rearmReplacementBackup recreates a backup consumed by a rollback restore,
// using the same metadata-preserving reverse copy as the confirm rollback.
//
// Wave-16 (codex P2) — an OCCUPIED backup name is a REFUSAL, never an
// acceptance: the rollback that prompts this re-arm removed the journal's
// verified backup first, so any object occupying the name afterwards is
// FOREIGN (a rename-over/impersonation — a conflicting rollback cannot exist:
// the destination lock and busy marker serialize the whole rollback window).
// The pre-wave-16 shortcut treated a Stat-success there as success and
// silently armed the journal entry against those unrelated bytes — a later
// revert/sweep would have restored them over the destination and then deleted
// them. The refusal leaves the journal entry in its prior armed/pending
// state, keeps the foreign object byte-intact, and surfaces through the
// typed fsutil.ErrPublishCollision class so the caller's kept+warn leg logs
// the refusal reason verbatim. The reverse copy's publish is itself
// no-replace (copyBackupToDestNoReplace), closing the window between this
// classification and the staged publish for a name claimed inside it.
//
// Wave-17 (codex P2): a no-replace-UNSUPPORTED volume (typed
// fsutil.ErrPublishNoReplaceUnsupported) takes the identical conservative
// leg — the publish error propagates to the caller's kept+warn handling, the
// journal entry stays armed/pending, and replacing semantics are never used
// to force the re-arm through.
func rearmReplacementBackup(fsys afero.Fs, dest, backup string) error {
	if _, err := fsys.Stat(backup); err == nil {
		logging.Warnf("downloader: re-arm of rolled-back backup %s refused — backup name occupied by a foreign object; journal entry remains armed/pending, foreign bytes kept", backup)
		return fmt.Errorf("re-arm backup %s refused: name occupied: %w", backup, fsutil.ErrPublishCollision)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat backup for re-arm: %w", err)
	}
	return copyBackupToDestNoReplace(fsys, dest, backup)
}

// restoreAsideBackup rolls a failed install back to its set-aside backup
// WITHOUT ever renaming over a foreign writer (wave-17, codex P2): the
// window between moving the destination aside and this rollback is wide (the
// busy marker deters Javinizer participants, not foreign processes), and the
// pre-wave-17 bare os.Rename silently REPLACED a foreign file created at
// destPath inside the window — no backup, no journal entry (POSIX rename
// overwrites in place). The restore therefore publishes NO-REPLACE:
//   - success CONSUMES the backup (restored=true) and callers keep their
//     prior bookkeeping (journal release / re-arm);
//   - an occupied destination (typed fsutil.ErrPublishCollision) or a volume
//     that cannot express no-replace (typed
//     fsutil.ErrPublishNoReplaceUnsupported) REFUSES the restore: the
//     foreign file keeps destPath byte-intact, the backup is RETAINED in
//     place at backupPath as the still-armed backup — the original bytes stay
//     recoverable through the existing sweep/revert flow (journaled entry) or
//     the orphan sweep's conservative retention (record-failure leg) — and
//     the refusal surfaces as a warn while the CALLER's primary error keeps
//     surfacing unchanged;
//   - any other publish failure is a genuine restore failure (restored=false,
//     err set — bytes remain at backupPath).
func restoreAsideBackup(fsys afero.Fs, destPath, backupPath string) (bool, error) {
	switch err := fsutil.PublishNoReplace(fsys, backupPath, destPath); {
	case err == nil:
		return true, nil
	case rollbackRestoreRefused(err):
		logging.Warnf("downloader: rollback restore of %s refused — %v; backup %s retained in place (foreign/unsupported destination never clobbered, original bytes stay recoverable)", destPath, err, backupPath)
		return false, nil
	default:
		return false, err
	}
}

// rollbackRestoreRefused reports whether a failed no-replace rollback restore
// is one of the REFUSAL classes (foreign-occupied destination,
// no-replace-unsupported volume) whose conservative posture retains the
// backup and warns rather than erroring: in both classes nothing was
// attempted that could have touched the destination's bytes. Wave-19: the
// classifier itself is fsutil.PublishRefusal, shared with history's re-arm
// compensation.
func rollbackRestoreRefused(err error) bool {
	return fsutil.PublishRefusal(err)
}

// rollbackRearmPendingKind routes the downloader's failed rollback re-arm
// onto the journal's restore-pending kind (wave-21, codex P2 PR#215),
// mirroring history's wave-20 trichotomy through the SHARED fsutil
// classifiers (never duplicated): the fsutil.PublishCompleted class means
// the failed re-arm still published THIS operation's own bytes at the backup
// name — restore-pending CLEAN, the pending retry reaps the owned name and
// consumes; every other failure — the fsutil.PublishRefusal classes (a
// foreign writer owns the name, or the volume cannot express a no-replace
// publish) AND plain pre-publish failures (staging open/write/close, a
// failed publish) — leaves the name unowned (foreign or absent) and takes
// the REARM-REFUSED kind: retries consume journal-only, off the unowned name.
func rollbackRearmPendingKind(rearmErr error) string {
	if fsutil.PublishCompleted(rearmErr) {
		return models.RestorePendingKindClean
	}
	return models.RestorePendingKindRearmRefused
}

// pendingKindLabel renders the durable kind for log lines in the wave-19
// phrasing (rearm-refused / clean).
func pendingKindLabel(kind string) string {
	if kind == models.RestorePendingKindRearmRefused {
		return "rearm-refused"
	}
	return kind
}

// markRollbackQuarantineRestoreFailed disarms the ledger after a rollback
// quarantine wedge whose verified move-back FAILED (finding F2, codex P2
// PR#215 — the downloader mirror of history's wave-36 F3 caller shape): the
// journaled backup name is UNOWNED (a foreign claimant holds it or the
// no-replace publish wedged) while the restored bytes stay recoverable at
// the quarantine name. Left ARMED (or clean-pending), a later sweep/revert
// would stat/copy/remove the foreign occupant at that name, so the entry is
// durably marked restore-pending with the REARM-REFUSED kind (journal-only
// retry, the quarantined bytes recovered manually). Only a failed marker
// write keeps the armed posture (both causes logged), mirroring
// markRollbackRearmFailed's last-resort discipline.
func markRollbackQuarantineRestoreFailed(ctx context.Context, ledger downloadLedger, destPath, backupPath, quarantine string, restoreErr error) {
	if markErr := ledger.recorder.MarkReplacementRestorePendingKind(ctx, ledger.opID, destPath, backupPath, models.RestorePendingKindRearmRefused); markErr != nil {
		logging.Warnf("downloader: rollback quarantine move-back of backup %s failed (%v) AND the rearm-refused restore-pending marking failed (%v) — journal entry stays armed against an unowned name; restored bytes recoverable at the quarantine name %s", backupPath, restoreErr, markErr, quarantine)
		return
	}
	logging.Warnf("downloader: rollback quarantine move-back of backup %s failed (%v) — journal entry marked restore-pending (rearm-refused); restored bytes recoverable at the quarantine name %s", backupPath, restoreErr, quarantine)
}

// markRollbackRearmFailed disarms the ledger after a rollback whose backup
// re-arm failed — for EVERY failure class (wave-19 refusal classes,
// generalized by wave-21, codex P2 PR#215). The destination bytes are
// provably the restored pre-existing bytes (the rollback restore that
// prompts this re-arm only runs after producing exactly that state), so the
// entry is durably marked restore-pending with rollbackRearmPendingKind's
// kind instead of staying armed: left ARMED, a refusal class would point at
// an unowned name (a later revert restores foreign bytes over the
// destination and deletes the occupant), and a NON-refusal pre-publish
// failure leaves the journal ARMED against an ABSENT backupPath — every
// explicit revert wedges statting that absent source forever, and sweeps see
// an ordinary armed row with a present destination (nothing to repair). Only
// a failed marker write keeps the armed posture (last-resort logged),
// unchanged from wave-19.
func markRollbackRearmFailed(ctx context.Context, ledger downloadLedger, destPath, backupPath string, rearmErr error) {
	kind := rollbackRearmPendingKind(rearmErr)
	if markErr := ledger.recorder.MarkReplacementRestorePendingKind(ctx, ledger.opID, destPath, backupPath, kind); markErr != nil {
		logging.Warnf("downloader: re-arm of rolled-back backup %s failed (%v) and restore-pending marking failed (%v) — journal entry stays armed, backup name untouched (pending kind %s)", backupPath, rearmErr, markErr, pendingKindLabel(kind))
		return
	}
	logging.Warnf("downloader: re-arm of rolled-back backup %s failed (%v) — journal entry marked restore-pending (%s)", backupPath, rearmErr, pendingKindLabel(kind))
}

// refuseRollbackBackupRemoval keeps a proven-foreign backup occupant
// byte-intact and surfaces the refusal through the caller's
// backup-cleanup-failed leg (journal entry stays armed), mirroring history's
// refuseReplacementBackupRemoval warn+typed-error discipline.
func refuseRollbackBackupRemoval(backup, phase, reason string) error {
	logging.Warnf("downloader: %s refused to remove backup %s: %s; journal entry remains armed", phase, backup, reason)
	return fmt.Errorf("%s: rollback backup removal %s refused: %s", phase, backup, reason)
}

// captureReplacementBackupFacts reads the JUST-established set-aside's
// identity facts for the journal stamp (wave-25, codex P3 PR#215 finding 2).
// Only size + mtime (Unix seconds) are journaled — NOT dev/inode: the
// consumption-failure re-arm compensations republish the backup under a
// FRESH inode while preserving bytes, size, and mtime, so a journaled inode
// would misjudge the operation's own re-armed backup as foreign and wedge
// the removal gate forever. A failed read is returned to the caller (the
// install compensates like a record failure) rather than stamping nothing
// silently.
func captureReplacementBackupFacts(fsys afero.Fs, backupPath string) (models.ReplacementBackupFacts, error) {
	info, err := lstatBackupCandidate(fsys, backupPath)
	if err != nil {
		return models.ReplacementBackupFacts{}, err
	}
	if info == nil {
		return models.ReplacementBackupFacts{}, errors.New("filesystem returned no file information")
	}
	// Wave-63 (codex P2 PR#215): sha256 of the set-aside bytes binds the
	// restore copy to the exact content (size+mtime are forgeable — same length
	// + a coerced unix-second mtime impersonates the owned set-aside); a failed
	// read fails the capture closed. mtime is captured by value and restored on
	// close: MemMapFs re-stamps ModTime on an O_NOFOLLOW read's close (FileInfo
	// is a live view), and the deferred close restores the pre-read mtime the
	// downstream rollback/re-arm hands off — a no-op on OsFs.
	mtime := info.ModTime()
	f, err := restoreOpenReplacementSource(fsys, backupPath)
	if err != nil {
		return models.ReplacementBackupFacts{}, err
	}
	defer func() {
		_ = f.Close()
		// Codex P2 (wave-64): the deferred mtime restoreable must stay bound —
		// another directory writer may have replaced the path while the open
		// handle was active; on OsFs a planted symlink would chase the
		// Chtimes onto unrelated bytes. Verify the CURRENT name still names
		// the opened object (dev/ino via SameFile where exposed, size/mtime
		// otherwise) before touching it; MemMapFs (read-mode close re-stamping)
		// is exactly where we authored this leg, everything else keeps the
		// no-mutation posture.
		cur, lerr := lstatBackupCandidate(fsys, backupPath)
		if lerr != nil {
			return
		}
		if _, osfsOk := fsys.(*afero.OsFs); osfsOk {
			// Real filesystem: the strict kernel-identity gate — MemMapFs has
			// no inode model at all (every FileInfo there is a live view),
			// so the same check false-negatives on it.
			if !os.SameFile(cur, info) {
				return
			}
		} else if cur.Name() != info.Name() || cur.Size() != info.Size() {
			// MemMapFs has no symlink model and re-stamps ModTime on close —
			// identity here rides the byte-stable pair (name + size).
			return
		}
		_ = fsys.Chtimes(backupPath, mtime, mtime)
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return models.ReplacementBackupFacts{}, err
	}
	return models.ReplacementBackupFacts{Size: info.Size(), ModUnix: mtime.Unix(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// installedDestIdentity binds a confirm-failure rollback to the exact object
// the install published at the destination (wave-25, codex P3 PR#215
// finding 3): dev/inode when the filesystem exposes them, plus size and
// mtime. known=false records a capture failure — the rollback then has
// NOTHING verifiable to restore over and must refuse rather than overwrite
// an occupant it cannot identify. Wave-26 (codex P2): the identity is
// captured from the STAGED file BEFORE the publish (rename keeps inode,
// size, and mtime across ReplaceFile) and verified against the destination
// immediately after it, so an occupant swapped in around the publish is
// never appointed the rollback baseline.
type installedDestIdentity struct {
	known     bool
	hasDevIno bool
	dev, ino  uint64
	size      int64
	modTime   time.Time
}

// captureInstalledDestIdentity Lstats the staged install file BEFORE its
// publish (wave-26; wave-25 captured the destination AFTER the publish, which
// appointed any occupant swapped into that window as the rollback baseline).
// The same capture shape serves both the pre-publish staged read and the
// post-publish destination rechecks. Anything other than a regular,
// non-symlink object (including a read failure) yields the unknown identity.
func captureInstalledDestIdentity(fsys afero.Fs, destPath string) installedDestIdentity {
	info, err := lstatBackupCandidate(fsys, destPath)
	if err != nil {
		return installedDestIdentity{}
	}
	return installedIdentityFromFileInfo(info)
}

// installedIdentityFromFileInfo builds an installedDestIdentity from an
// already-looked-up FileInfo (wave-31): the no-follow rollback SOURCE handle's
// fstat (rollbackCopyFacts.copied) and fsutil.PublishStagedBoundInfo's
// post-publish-VERIFIED destination stat (rollbackCopyFacts.restored) both
// arrive as FileInfo. A nil, symlink, or non-regular answer yields the
// unknown identity — never a guess.
func installedIdentityFromFileInfo(info os.FileInfo) installedDestIdentity {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installedDestIdentity{}
	}
	id := installedDestIdentity{known: true, size: info.Size(), modTime: info.ModTime()}
	if dev, ino, ok := restoreSourceIdentity(info); ok {
		id.hasDevIno = true
		id.dev, id.ino = dev, ino
	}
	return id
}

// rollbackRestoredDestStillOurs is the confirm-rollback's restored-destination
// recheck seam (wave-31, codex local round 1, PR#215 finding L1): production
// wiring is destStillHoldsInstalledObject against the rollback copy's
// published identity. Wave-32 caller audit (codex local round 2, PR#215
// finding R5): an UNKNOWN identity reaches here ONLY from virtual/wrapper
// filesystem legs (fsutil reports no publish identity there) — r12 removed
// the wave-32 fsutil deferred-times legs (the ENOSYS leg skips the times
// and surfaces the completed classification, never a name-based fallback
// nor a degraded nil-identity success), and the copy leg propagates its
// errors unchanged, so a real-filesystem publish never arrives with an
// indeterminate identity to skip on. The virtual-leg unknown continues to
// SKIP (the documented wave-31 residual) rather than refusing a good
// rollback on nothing. Tests replay the foreign swap/deletion landing inside
// the publish→recheck window, an instant no afero double can reach while the
// wave-30 real-OsFs identity gate is up; the real-OsFs detection itself is
// pinned by direct unit tests.
var rollbackRestoredDestStillOurs = func(fsys afero.Fs, dest string, id installedDestIdentity) bool {
	if !id.known {
		return true
	}
	return destStillHoldsInstalledObject(fsys, dest, id)
}

// destStillHoldsInstalledObject re-derives the destination's identity the
// no-follow way and requires it to still name the object the install
// published — the wave-26 baseline captured from the STAGED file pre-publish
// and post-publish-verified: dev/inode mismatch when both sides expose it,
// then size and mtime on every platform. Any failure, substitution, or failed
// capture answers false — the confirm-failure rollback treats false as a
// REFUSAL (backup retained, journal entry armed), never a clobber.
func destStillHoldsInstalledObject(fsys afero.Fs, destPath string, id installedDestIdentity) bool {
	if !id.known {
		return false
	}
	info, err := lstatBackupCandidate(fsys, destPath)
	if err != nil || info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	if id.hasDevIno {
		dev, ino, ok := restoreSourceIdentity(info)
		if ok && (dev != id.dev || ino != id.ino) {
			return false
		}
	}
	return info.Size() == id.size && info.ModTime().Equal(id.modTime)
}

// overwriteBackupPath names the destination's backup for one replacement:
// opID folded as a hash (never a path component) plus a process-unique
// ordinal, so stacked same-op or cross-op overwrites never clobber a backup.
func overwriteBackupPath(destPath, opID string) string {
	return destPath + backupSuffixForDest + "." + sha1hex8(opID) + "." + strconv.FormatUint(backupOrdinal.Add(1), 16)
}

// sha1hex8 folds an op's identity to 16 lowercase hex chars for backup path
// naming — ledger identity must never inject path segments.
func sha1hex8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// lstatBackupCandidate treats every existing directory entry — including a
// symlink, directory, or older backup — as occupied. OsFs uses Lstat, while
// MemMapFs has no symlink model and safely falls back to Stat.
func lstatBackupCandidate(fsys afero.Fs, candidate string) (os.FileInfo, error) {
	if ls, ok := fsys.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(candidate)
		return info, err
	}
	return fsys.Stat(candidate)
}

// claimOverwriteBackupPath chooses a free destination-adjacent backup name
// and ATOMICALLY RESERVES it: after observing a free candidate the name is
// claimed with O_CREATE|O_EXCL, closing the Lstat-to-Rename window in which
// a foreign writer (one that does not honor the .dlbusy marker) could occupy
// the observed-free name — POSIX rename would then silently overwrite its
// bytes before the journal ever saw them (codex PR#215). An O_EXCL collision
// on an observed-free candidate means a racer reserved it first and the
// claim climbs to the next name. The returned placeholder is a 0-byte file;
// the caller's dest→backup handoff (wave-38: handoffToReservedBackup) is
// CONDITIONAL on it: the placeholder is first taken aside through the bound
// take-aside (Linux/OsFs parks it at dest via the atomic RENAME_EXCHANGE and
// the take-aside removes it there), then dest moves onto the freed backup
// name NO-REPLACE — no foreign claim is ever displaced and every participant
// either wins a unique name or fails closed.
// (Pre-wave-38 the handoff renamed dest onto the reserved name in place:
// POSIX rename replaced the placeholder atomically in the verify→rename
// window, while Windows rename — OsFs.Rename → MoveFileW — REFUSED the
// existing placeholder and rode fsutil.ReplaceFile instead (codex PR#215
// w12). Wave-38's dest move now targets a vacancy on every platform, riding
// fsutil.PublishNoReplace's collision-class refusal. The rollback restores
// below publish onto a path the set-aside just vacated with NO-REPLACE
// semantics (restoreAsideBackup, wave-17), so a foreign dest claimed
// mid-window is refused and kept instead of clobbered (Windows's
// MoveFileExW-without-replace refusal maps into the same retained class).)

// claimOverwriteBackupPath returns the reserved backup name AND the
// reservation's own captured identity (the open handle's pre-close Stat).
// Wave-36 (codex local review round 6, PR#215 finding F1): the claimed
// placeholder was previously DROPPED after Close with nothing binding its
// identity to the later dest→backup rename — a foreign writer could rename
// the reservation away and plant its own object at the backup name, and the
// replacing move then destroyed the occupant. The returned identity lets the
// caller re-derive the reservation immediately before the move and climb to
// a fresh name on any mismatch (overwriteBackupReservationStillOurs), never
// touching the foreign occupant.
func claimOverwriteBackupPath(fsys afero.Fs, destPath, opID string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < backupNameClaimTries; attempt++ {
		candidate := overwriteBackupPath(destPath, opID)
		if _, err := lstatBackupCandidate(fsys, candidate); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", nil, fmt.Errorf("inspect backup candidate %s: %w", candidate, err)
		}
		reservation, reserveErr := fsys.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case reserveErr == nil:
			info, statErr := reservation.Stat()
			if statErr != nil {
				// A reservation whose identity cannot even be read is in an
				// unknown on-disk state. Codex P2 (wave-62): the name's
				// identity is UNPROVEN — between our O_EXCL create and now
				// another writer may have replaced it, so unlinking the path
				// could delete foreign bytes. Retain it for manual cleanup
				// (the name stays claimed and visible; nothing here mutates
				// on doubt).
				_ = reservation.Close()
				logging.Warnf("downloader: backup reservation %s left in place — its identity could not be proven (%v); manual cleanup advised", candidate, statErr)
				return "", nil, fmt.Errorf("stat backup reservation %s: %w", candidate, statErr)
			}
			if err := reservation.Close(); err != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state, but its identity WAS captured. Wave-r19 (codex P2,
				// PR#215 finding F4): bind cleanup to the captured info — re-prove
				// the candidate still names our claimed placeholder (SameFile)
				// and unlink only when matching; retain on doubt (never a
				// pathname Remove of an unproven object).
				releaseClaimedReservation(fsys, candidate, info)
				return "", nil, fmt.Errorf("close backup reservation %s: %w", candidate, err)
			}
			return candidate, info, nil
		case os.IsExist(reserveErr):
			// A foreign claimant won the Lstat-to-OpenFile window; climb.
			continue
		default:
			return "", nil, fmt.Errorf("reserve backup candidate %s: %w", candidate, reserveErr)
		}
	}
	return "", nil, fmt.Errorf("backup names exhausted for %s after %d attempts", destPath, backupNameClaimTries)
}

// overwriteBackupReservationStillOurs re-derives the O_EXCL reservation's
// identity IMMEDIATELY BEFORE the dest→backup move (wave-36, finding F1):
// the backup name must still address THE claimed placeholder — dev/inode
// where the filesystem exposes them, plus size 0, mtime, and a regular
// non-symlink shape on every platform. Any divergence means a foreign writer
// owns the name now: the answer is the typed collision class
// (fsutil.ErrPublishCollision), the occupant is never touched, and the
// caller climbs to a fresh backup name.
func overwriteBackupReservationStillOurs(fsys afero.Fs, backupPath string, claim os.FileInfo) error {
	cur, err := lstatBackupCandidate(fsys, backupPath)
	switch {
	case err != nil:
		return fmt.Errorf("inspect backup reservation %s before the move: %w", backupPath, err)
	case cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() || cur.Size() != 0 || !cur.ModTime().Equal(claim.ModTime()):
		return fmt.Errorf("backup reservation %s no longer names the claimed empty placeholder (foreign reservation swap) — foreign bytes preserved: %w", backupPath, fsutil.ErrPublishCollision)
	}
	if claimDev, claimIno, claimOK := restoreSourceIdentity(claim); claimOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (claimDev != curDev || claimIno != curIno) {
			return fmt.Errorf("backup reservation %s no longer names the claimed placeholder (dev/inode mismatch) — foreign bytes preserved: %w", backupPath, fsutil.ErrPublishCollision)
		}
	}
	return nil
}

// stagedInstallProvenance is installOverwriting's wave-48 provenance bundle:
// the wave-45 validation-time identity record of the staged install input
// PLUS the retained open handle of the validated object the record was
// derived from. Production hands the handle down open
// (validateDownloadedMedia in http.download, or downloadPoster's post-crop
// no-follow re-open); installOverwriting OWNS it end to end — the bound
// publishes consume it (fsutil.PublishStagedBoundInfo always closes), and
// every early/skip/refusal exit closes it here, so no descriptor leaks and
// Windows tempdir cleanup never wedges on a held lock. A nil handle (or an
// unknown identity) is the legacy/recorded-only posture: the wave-45
// snapshot gate still runs and the publish legs are the plain path
// publishes.
type stagedInstallProvenance struct {
	identity installedDestIdentity
	handle   afero.File
}

// publishStagedInstall runs the staged install input's publish through the
// wave-29/30 fsutil bound-publish loop with Handle=the validated handle: the
// staged name re-proves itself against the handle's fd identity at publish
// adjacency, and the post-publish reverify catches any substitute shaping
// (POSIX restages the genuine bytes from the handle and republishes within
// the bounded budget; Windows closes before the publish and refuses typed on
// a broken snapshot). NoReplace stays FALSE even for the no-replace publish:
// a collision here is the downloader's wave-15 foreign-claim reclassification
// (the racer's bytes take the armed-overwrite discipline, preserved via
// backup+journal), never fsutil's obstacle-displacement rule.
func publishStagedInstall(fsys afero.Fs, stagedPath, destPath string, handle afero.File, publish func(afero.Fs, string, string) error) (os.FileInfo, error) {
	return publishStagedBoundFn(fsutil.StagedPublish{
		FS:          fsys,
		Publish:     publish,
		NoReplace:   false,
		Staged:      stagedPath,
		Handle:      handle,
		Dest:        destPath,
		Suffix:      ".dlpub",
		NextOrdinal: func() uint64 { return stagedRepublishOrdinal.Add(1) },
	})
}

// installedIdentityRecordsEqual is the wave-66 record-vs-record form of
// identityInfoMatchesRecord (codex P2, PR#215 — bind the candidate to the
// producer's identity): both sides are already-captured value snapshots
// (shape-checked at capture time — known identities never describe a symlink
// or non-regular object), so the comparison is dev/inode divergence when
// BOTH records carry kernel identity, then size+mtime on every platform. A
// one-sided kernel-identity absence degrades to the size+mtime legs, exactly
// like the FileInfo form's silent-Sys fallback.
func installedIdentityRecordsEqual(a, b installedDestIdentity) bool {
	if a.hasDevIno && b.hasDevIno && (a.dev != b.dev || a.ino != b.ino) {
		return false
	}
	return a.size == b.size && a.modTime.Equal(b.modTime)
}

// identityInfoMatchesRecord is the provenance comparator shared by
// classifyStagedInput (name lookups) and bindStagedProvenanceHandle (the
// re-opened fd's fstat): symlink/non-regular shapes, dev/inode divergence
// when the record carries kernel identity, and size+mtime on every platform.
func identityInfoMatchesRecord(info os.FileInfo, rec installedDestIdentity) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	if rec.hasDevIno {
		if dev, ino, ok := restoreSourceIdentity(info); ok && (dev != rec.dev || ino != rec.ino) {
			return false
		}
	}
	return info.Size() == rec.size && info.ModTime().Equal(rec.modTime)
}

// bindStagedProvenanceHandle re-opens the staged name NO-FOLLOW and returns
// its fd ONLY when the fd's fstat equals the validation record exactly
// (wave-48 — codex's "re-open no-follow + fstat==validated record + only
// that handle is used" fallback for the legs that cannot keep the original
// validation handle open: an earlier publish attempt consumed it, fsutil
// always closes). Every failure class refuses the byte flow: a vanished name
// stays the documented indeterminate posture (its own wrapped not-exist
// error, NOT the substitution sentinel — nothing at the name can ever
// substitute-install, and no publish is attempted), while an open/fstat
// failure or an fstat≠record answer means the name is UNPROVEN (possible
// substitution mid-window): the typed errStagedInputSubstituted refusal keeps
// callers off any unlink of the possibly-foreign occupant.
func bindStagedProvenanceHandle(fsys afero.Fs, stagedPath string, rec installedDestIdentity) (afero.File, error) {
	handle, oerr := restoreOpenReplacementSource(fsys, stagedPath)
	if oerr != nil {
		if errors.Is(oerr, os.ErrNotExist) {
			return nil, fmt.Errorf("staged install input %s vanished before the publish bind: %w", stagedPath, oerr)
		}
		return nil, fmt.Errorf("staged install input %s refused the no-follow publish bind open (%v) — the name is unproven (possible substitution); occupant preserved byte-intact: %w", stagedPath, oerr, errStagedInputSubstituted)
	}
	info, serr := handle.Stat()
	if serr != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("staged install input %s failed the publish bind fstat (%v) — the name is unproven; occupant preserved byte-intact: %w", stagedPath, serr, errStagedInputSubstituted)
	}
	if !identityInfoMatchesRecord(info, rec) {
		_ = handle.Close()
		return nil, fmt.Errorf("staged install input %s no longer names the validated object at the publish bind (fd fstat ≠ validation record; foreign substitution mid-window) — substitute preserved byte-intact: %w", stagedPath, errStagedInputSubstituted)
	}
	return handle, nil
}

// stagedPublishVerdict maps the fsutil bound-publish refusal classes whose
// staged name is UNPROVEN or whose destination could not be proven to name
// the validated object onto the wave-45 substitution refusal, so every
// caller keeps its preserve-the-substitute posture (never an unlink of the
// possibly-foreign name, the wave-41 PublishCompleted-carrying classes
// untouched). Publish outcomes outside those classes — collisions (handled
// by the caller's reclassification loop), plain publish/close failures
// (caller's historic error leg, staged removal still safe per fsutil's
// contract), and completed-carrying errors — return nil and ride through
// verbatim.
func stagedPublishVerdict(err error) error {
	if err == nil || fsutil.PublishCompleted(err) {
		return nil
	}
	if errors.Is(err, fsutil.ErrPublishStagedVerify) {
		return fmt.Errorf("staged install input failed the identity proof at publish adjacency — the name is unproven (possible substitution); occupant preserved byte-intact, no byte flow into the destination attempted: %w", errStagedInputSubstituted)
	}
	if errors.Is(err, fsutil.ErrPublishStagedIdentityBreak) ||
		errors.Is(err, fsutil.ErrPublishStagedExhausted) ||
		errors.Is(err, fsutil.ErrPublishStagedForeignOccupant) ||
		errors.Is(err, fsutil.ErrPublishStagedIdentityIndeterminate) {
		return fmt.Errorf("the validated-handle bound publish could not prove the destination names the validated object (%v) — foreign occupants preserved byte-intact, nothing consumed, the caller's compensation runs unchanged: %w", err, errStagedInputSubstituted)
	}
	return nil
}

// errCandidateProvenanceUnprobeable refuses byte flow when the downloadPoster
// candidate is completely unprobeable (wave-53, codex P3, PR#215 finding 3):
// BOTH the path-based identity capture (captureInstalledDestIdentity/Lstat)
// AND the no-follow re-open (restoreOpenReplacementSource) failed, so there is
// NOTHING verifiable about the name — neither an identity snapshot for the
// wave-45 classify gate nor a handle for the wave-48 bound publish. The
// pre-shape posture degraded here to an unauthenticated path-only publish
// (provenanceID.known == false, no handle → installOverwriting's else-branch
// PublishNoReplace by name), letting a substituted candidate publish
// unchallenged. The both-fail leg now fails CLOSED: nothing is recorded or
// touched, the candidate is preserved byte-intact for manual cleanup, and
// the typed refusal surfaces so the caller never reports success. A Lstat
// success (known identity) keeps the wave-48 degrade posture — the
// snapshot gate and bindStagedProvenanceHandle re-prove the name at publish
// adjacency.
var errCandidateProvenanceUnprobeable = errors.New("candidate provenance unprobeable — path identity capture and no-follow re-open both failed")

// bindCandidateProvenanceFn is the wave-53 production seam over
// bindCandidateProvenance: production never deviates, tests inject the both-fail
// refusal (finding 3) at the overwrite install path's bind step without
// staging a candidate-vanish race. Mirrors publishStagedBoundFn's discipline.
var bindCandidateProvenanceFn = bindCandidateProvenance

// bindCandidateProvenance binds the downloadPoster crop/write candidate to an
// open handle end to end (wave-48, codex P2, PR#215 finding 6 media leg):
// the crop writers (imageutil.CropPosterFromCover / CropPosterWithBounds)
// hand back no handle, so the candidate name is re-opened O_RDONLY no-follow
// IMMEDIATELY after the crop/write completes and THAT fd — identity captured
// from its own fstat — rides into installOverwriting as the bound
// provenance. A failed open or fstat degrades to the wave-47 posture (the
// post-crop no-follow name capture, no handle), never a failure: the wave-45
// snapshot gate still guards the publish legs there. Wave-53 (finding 3):
// when the path-based capture AND the no-follow re-open BOTH fail the name
// is completely unprobeable — fail closed (typed refusal) instead of
// degrading to an unauthenticated path-only publish.
// Wave-66 (codex P2, PR#215 — bind the candidate to the PRODUCER'S identity):
// the bind's own captures used to be the FIRST identity ever taken of the
// candidate name and ran AT INSTALL TIME — a foreign substitute rotated onto
// the candidate name between the producer's write and the bind then
// authenticated against ITSELF (both the Lstat and the fstat read the
// substitute, so the wave-54 Lstat-vs-fstat pair stays green). The caller
// now hands the producer's own write-time identity record down
// (wave-67: downloadPoster's full-download record filed on the result, or
// the crop producers' returned post-write FileInfo): the bind's
// Lstat capture AND the re-opened fd's fstat must BOTH equal that record or
// the bind refuses typed (errStagedInputSubstituted) — substitute preserved
// byte-intact, nothing installed, the caller's install/refusal posture
// unchanged. A zero/unknown producer record keeps the wave-53/54 legs
// verbatim (degrade + both-fail closed) for legs with no producer to bind
// against.
func bindCandidateProvenance(fsys afero.Fs, candidate string, producer installedDestIdentity) (stagedInstallProvenance, error) {
	provenance := stagedInstallProvenance{identity: captureInstalledDestIdentity(fsys, candidate)}
	// Wave-66: the install-time Lstat must equal the producer's write-time
	// record — a mismatch means the name was rotated onto a foreign substitute
	// inside the producer-write→bind window; refuse BEFORE opening a handle.
	if producer.known && provenance.identity.known && !installedIdentityRecordsEqual(provenance.identity, producer) {
		return stagedInstallProvenance{}, fmt.Errorf("candidate %s no longer names the producer-written object at the install-time bind (Lstat ≠ producer record — foreign substitution between the producer write and the bind) — substitute preserved byte-intact, nothing installed: %w", candidate, errStagedInputSubstituted)
	}
	handle, oerr := restoreOpenReplacementSource(fsys, candidate)
	if oerr != nil {
		if !provenance.identity.known {
			// BOTH the path-based identity capture AND the no-follow re-open
			// failed — there is nothing verifiable about the candidate name.
			// Fail closed: never publish unauthenticated, preserve the
			// occupant byte-intact for manual cleanup.
			return stagedInstallProvenance{}, fmt.Errorf("candidate %s could not be proven for publish (path identity capture and no-follow re-open both failed): %w", candidate, errCandidateProvenanceUnprobeable)
		}
		return provenance, nil // degrade: identity known from Lstat, no handle
	}
	info, serr := handle.Stat()
	if serr != nil {
		_ = handle.Close()
		if !provenance.identity.known {
			// Codex P2 (w57): Lstat failed AND fstat failed — neither the
			// first snapshot nor the opened handle yields an identity, so a
			// pathname-only publish would install an authenticated-nothing.
			// Fail closed with no state change inc to the name or journal.
			return stagedInstallProvenance{}, fmt.Errorf("candidate %s could not be proven for publish (path Lstat failed AND no-follow handle fstat failed: %v) — refusing to publish by pathname alone: %w", candidate, serr, errCandidateProvenanceUnprobeable)
		}
		// The Lstat snapshot already proves identity; we just lack a handle
		// — degrade per the wave-45 posture is documented in the callers.
		return provenance, nil //nolint:nilerr // identity known from Lstat; no handle available
	}
	// Wave-54 (finding 2): the no-follow open + fstat MUST equal the 1st Lstat
	// snapshot — a racer substituting the candidate before the open publishes
	// the substitute. Mismatch → typed refusal, substitute preserved, nothing installed.
	if provenance.identity.known && !identityInfoMatchesRecord(info, provenance.identity) {
		_ = handle.Close()
		return stagedInstallProvenance{}, fmt.Errorf("candidate %s no longer names the Lstat-verified object at the no-follow open (fd fstat ≠ 1st snapshot; foreign substitution mid-window) — substitute preserved byte-intact, nothing installed: %w", candidate, errStagedInputSubstituted)
	}
	// Wave-66: the re-opened fd must ALSO equal the producer's write-time
	// record — the binding leg when the Lstat capture was wedged, and the
	// closure of the producer-write→bind window the wave-54 Lstat-vs-fstat
	// pair alone cannot see (a substitute in place at BOTH captures).
	if producer.known && !identityInfoMatchesRecord(info, producer) {
		_ = handle.Close()
		return stagedInstallProvenance{}, fmt.Errorf("candidate %s no longer names the producer-written object at the install-time bind (fd fstat ≠ producer record — foreign substitution between the producer write and the bind) — substitute preserved byte-intact, nothing installed: %w", candidate, errStagedInputSubstituted)
	}
	provenance.identity = installedIdentityFromFileInfo(info)
	provenance.handle = handle
	return provenance, nil
}

// errStagedInputSubstituted refuses byte flow when the staged install input
// provably stopped naming the exact object its producer validated (wave-45,
// codex P2, PR#215 finding F1). http.download validates the downloaded bytes
// and hands the validation-time, handle-derived identity snapshot down as the
// install provenance; a directory writer rotating the staged name onto a
// foreign substitute inside the validation→install window trips this class.
// Callers never unlink the staged name for this class — the substitute is
// foreign bytes preserved byte-intact (the create path published nothing, so
// the destination is untouched; the replace path refused BEFORE any publish
// and rode the publish-failure compensation, so the set-aside restore and
// journal retraction already ran).
var errStagedInputSubstituted = errors.New("staged install input substituted after validation")

// stagedInputVerdict is the provenance gate's classification of the staged
// install input against the validation-time identity snapshot (wave-45).
type stagedInputVerdict int

const (
	// stagedInputUnrecorded keeps the pre-wave-45 posture: the caller handed
	// no provenance snapshot down. (The wave-45 residual for the poster-crop
	// candidate — produced locally from an already-validated temp, never
	// sniffed — was closed by wave-47: downloadPoster freezes the candidate's
	// post-write identity and passes it down, so an unrecorded verdict now
	// means a caller never recorded one at all.)
	stagedInputUnrecorded stagedInputVerdict = iota
	// stagedInputMatch: the staged name still addresses the validated object.
	stagedInputMatch
	// stagedInputMismatch: the staged name provably addresses a foreign
	// substitute — bytes must never publish.
	stagedInputMismatch
	// stagedInputIndeterminate: the staged lookup failed — no proof either
	// way, so the legacy publish legs keep their own error handling
	// (a vanished staged name fails the publish with its usual not-exist
	// error instead of being misreported as a substitution).
	stagedInputIndeterminate
)

// classifyStagedInput re-derives the staged install input's identity the
// no-follow way (lstatBackupCandidate — a planted symlink is seen as the
// link object, never followed) and compares it against the validation-time
// provenance snapshot: dev/inode when the filesystem exposed kernel identity
// AT VALIDATION TIME (the os.SameFile kernel carrier), then size + mtime on
// every platform — the same comparator shape as
// destStillHoldsInstalledObject, pointed back at the staged input. The
// snapshot stores VALUES (never the live FileInfo), so a virtual
// filesystem's live identity views cannot drift the record after capture.
func classifyStagedInput(fsys afero.Fs, stagedPath string, provenance installedDestIdentity) stagedInputVerdict {
	if !provenance.known {
		return stagedInputUnrecorded
	}
	cur, err := lstatBackupCandidate(fsys, stagedPath)
	if err != nil || cur == nil {
		return stagedInputIndeterminate
	}
	if !identityInfoMatchesRecord(cur, provenance) {
		return stagedInputMismatch
	}
	return stagedInputMatch
}

// installOverwriting installs staged (already-downloaded) bytes onto
// destPath under the per-destination lock with the replace-ledger discipline
// (POSTER-WRITE-HARDENING P3):
//
//  1. The durable .dlbusy marker is claimed BEFORE any existence
//     classification and held through BOTH the create and replace paths:
//     a foreign process may hold the marker while it has the destination
//     renamed aside, so an observed-absent destination must never take the
//     create path under a live owner (codex PR#215). The deferred release
//     runs on every exit, including the busy-refusal and error returns.
//  2. Existence is classified INSIDE the lock with Lstat semantics so a
//     dangling symlink is refused instead of falling into the create path
//     (os.Stat follows links and would report ENOENT, replacing the link
//     object with no ledger entry). Concurrent writers serialize; the
//     second operation measures the just-installed bytes correctly.
//  3. A create (nothing at destination) installs directly — no ledger arm.
//  4. A replace requires the revert ledger armed (operation ID recorded);
//     without it the overwrite is refused: destination preserved, skip+warn.
//  5. The pre-existing bytes are moved aside to an atomically reserved
//     per-operation backup; the record is journaled BEFORE the replace; a
//     record-or-replace failure restores the backup under the same lock.
//
// Returns (skipped, replaced, err): skipped reports a refused destructive
// overwrite (destination unchanged, no error); replaced reports the final
// in-lock classification the callers' results carry.
//
// Wave-45 (codex P2, PR#215 finding F1): provenance optionally carries the
// validation-time identity snapshot of the staged input
// (installedIdentityFromFileInfo of the handle validateDownloadedMedia read).
// Both publish legs re-derive the staged input through the no-follow
// classifier before any byte flow into destPath: the CREATE path re-proves
// before EVERY no-replace publish attempt, and the replace path refuses
// (routing through the set-aside/journal compensation like a failed publish)
// when the wave-26 baseline would otherwise capture a window substitute.
// A refusal surfaces the typed errStagedInputSubstituted — the caller must
// not unlink the staged name: its occupant is preserved foreign bytes.
// Wave-48 (codex P2, PR#215 finding 6): the wave-45 path-based provenance
// checks closed the validation→install window by NAME, but the publishes
// still mutated by PATH later — a workdir-writable process replacing
// stagedPath between the checks and the mutation got ITS bytes installed as
// ours. The provenance bundle therefore additionally carries the validated
// object's OPEN HANDLE (stagedInstallProvenance): the create path publishes
// each attempt and the replace path publishes once through
// fsutil.PublishStagedBoundInfo with Handle=the validated handle, so the
// staged name must equal the handle's fd identity at publish adjacency and
// the destination must provably name that object after the publish (POSIX
// restages the genuine bytes from the handle inside the bounded budget; the
// Windows leg closes at publish adjacency and refuses typed on a broken
// snapshot — the documented platform binding). Legs that cannot keep the
// validation handle open (an earlier publish attempt consumed it — fsutil
// always closes) RE-OPEN the staged name no-follow and bind only an fd whose
// fstat equals the validation record (bindStagedProvenanceHandle). A
// substitution at tempPath post-validation now provably reaches a refusal
// BEFORE any bytes-at-dest mutation on the create path, and on the replace
// path the refusal rides the unchanged publish-failure compensation
// (set-aside restore + journal retraction), the substitute preserved
// byte-intact either way.
func (d *Downloader) installOverwriting(ctx context.Context, stagedPath, destPath string, ledger downloadLedger, provenance ...stagedInstallProvenance) (bool, bool, error) {
	return d.installOverwritingIdentity(ctx, stagedPath, destPath, ledger, nil, provenance...)
}

// installOverwritingIdentity is installOverwriting with the installer's own
// POST-PUBLISH-VERIFIED destination identity handed back through installedOut
// (wave-67, codex P2, PR#215 — producer-side provenance binding, mirroring
// copyBackupToDestPublish's facts.restored shape): on a completed publish the
// caller files THAT record as the download's producer identity — the real
// OsFs legs carry fsutil.PublishStagedBoundInfo's SameFile-bound post-publish
// destination stat, the virtual leg its documented post-publish capture — so
// no consumer ever re-derives the mutable destination name after the
// producer returned (the window in which a substitute authenticates against
// itself). A nil installedOut keeps the legacy posture (callers that discard
// the record); every non-publishing exit reports the unknown identity.
func (d *Downloader) installOverwritingIdentity(ctx context.Context, stagedPath, destPath string, ledger downloadLedger, installedOut *installedDestIdentity, provenance ...stagedInstallProvenance) (bool, bool, error) {
	var provenanceID installedDestIdentity
	var boundHandle afero.File
	if len(provenance) > 0 {
		provenanceID = provenance[0].identity
		boundHandle = provenance[0].handle
	}
	// The retained validated-object handle is owned here end to end: the
	// bound publishes CONSUME it (fsutil always closes), and every
	// early/skip/refusal exit closes it through this defer — no descriptor
	// leak, no Windows "file in use" wedge for the caller's tempdir cleanup
	// of either the staged name or the retained substitute.
	defer func() {
		if boundHandle != nil {
			_ = boundHandle.Close()
		}
	}()
	release := d.destLocks.Acquire(destPath)
	// codex PR#215 R22-3: a caller canceled while queued on the destination
	// lock must not publish staged media after the lock is finally granted.
	if cerr := ctx.Err(); cerr != nil {
		release()
		return false, true, cerr
	}
	defer release()

	// codex PR#215: the busy marker is armed BEFORE the destination is
	// classified. Marked-absence is not proof of absence — the owner may be
	// mid-replacement — so the claim precedes both the create path and the
	// replacement path. AcquireReplacementBusy self-cleans on error, so only
	// a successful claim needs the deferred release below.
	busyRelease, busyErr := fsutil.AcquireReplacementBusy(d.fs, destPath)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		logging.Warnf("downloader: overwrite of %s refused — another process owns the replacement", destPath)
		return true, true, nil
	}
	if busyErr != nil {
		return false, true, fmt.Errorf("failed to arm replacement busy marker for %s: %w", destPath, busyErr)
	}
	defer busyRelease()

	// Codex P2 (wave-62): the blocking marker acquisition may complete only
	// after ctx canceled — a stalled-Fs wait under a running cancellation must
	// NOT publish the media. Gate the remainder on ctx.Err() like the other
	// blocking checks in this flow.
	if cerr := ctx.Err(); cerr != nil {
		return false, true, fmt.Errorf("install overwrite of %s canceled while acquiring the busy marker: %w", destPath, cerr)
	}

	// codex PR#215: classify existence with Lstat, not Stat. os.Stat follows
	// symlinks, so a DANGLING symlink at the destination reports ENOENT and
	// the create path would overwrite the link object itself — no ledger
	// entry, no symlink refusal. Lstat sees the link object directly.
	//
	// codex PR#215 wave-15 (P2): the create path publishes through
	// fsutil.PublishNoReplace. Between the Lstat-ENOENT here and a plain
	// ReplaceFile a foreign writer can claim the destination, and the replace
	// would destroy the racer's bytes with no backup and no ledger entry
	// while the download reports success. An occupied destination is therefore
	// a RECLASSIFICATION, not a failure: the destination is now present, so
	// it falls into the armed-overwrite discipline below and the racer's
	// bytes are set aside + journaled exactly like any other pre-existing
	// destination. The bounded loop also tolerates a racer whose entry is
	// created and deleted again across repeated publishes.
	//
	// Wave-17 (codex P2): a volume that cannot express an atomic no-replace
	// publish at all (renameat2 AND hard links both unsupported — FAT/exFAT)
	// answers with the typed fsutil.ErrPublishNoReplaceUnsupported, which is
	// a REFUSAL, not a reclassification: the install fails cleanly through
	// the plain publish-error leg below (destination untouched, staged file
	// retained, nothing journaled) — replacing semantics are never used to
	// paper over the missing primitive.
	info, statErr := lstatBackupCandidate(d.fs, destPath)
	for createAttempts := 0; statErr != nil && os.IsNotExist(statErr); createAttempts++ {
		if createAttempts >= createPublishMaxAttempts {
			return false, true, fmt.Errorf("failed to install %s: foreign writers claimed the destination across %d no-replace publishes: %w", destPath, createAttempts, fsutil.ErrPublishCollision)
		}
		// Wave-45 (finding F1): re-derive the staged input before EVERY
		// no-replace publish attempt — only the validated object's bytes may
		// flow into destPath on the create path.
		if classifyStagedInput(d.fs, stagedPath, provenanceID) == stagedInputMismatch {
			return false, false, fmt.Errorf("create install refused for %s — staged input %s no longer names the validated download object (foreign substitution after validation); substitute preserved, destination untouched: %w", destPath, stagedPath, errStagedInputSubstituted)
		}
		var pubErr error
		if provenanceID.known {
			publishHandle := boundHandle
			if publishHandle == nil {
				// Wave-48: an earlier publish attempt consumed the validated
				// handle (fsutil.PublishStagedBoundInfo always closes it) —
				// re-open the staged name no-follow and bind only an fd whose
				// fstat equals the validation record; every refusal class means
				// the name is unproven and no byte flow may be attempted.
				var bindErr error
				publishHandle, bindErr = bindStagedProvenanceHandle(d.fs, stagedPath, provenanceID)
				if bindErr != nil {
					return false, false, bindErr
				}
			}
			boundHandle = nil
			var published os.FileInfo
			published, pubErr = publishStagedInstall(d.fs, stagedPath, destPath, publishHandle, fsutil.PublishNoReplace)
			// Wave-48: verify / post-publish identity-break refusals surface as
			// the typed substitution refusal (staged name unproven — possibly
			// foreign — preserved byte-intact); PublishCompleted-carrying
			// errors and plain failures ride through verbatim for the caller's
			// wave-41 legs, as does the collision reclassification below.
			if subErr := stagedPublishVerdict(pubErr); subErr != nil {
				return false, false, subErr
			}
			if installedOut != nil && (pubErr == nil || (fsutil.PublishCompleted(pubErr) && published != nil)) {
				// Wave-67: the create path files the producer record the same
				// way the replace path does — fsutil's post-publish-VERIFIED
				// destination stat on the real OsFs legs, the wave-31 virtual-leg
				// post-publish capture where fsutil hands back nothing.
				// Wave-68 (codex P2, PR#215 F2): a completed publish carrying a
				// VERIFIED non-nil identity (the ENOSYS-times-skipped leg —
				// PublishStagedBoundInfo hands back the post-publish-verified
				// destination stat alongside ErrPublishCompleted) files the
				// record too, mirroring copyBackupToDestPublish's r15 / history's
				// wave-61; a completed leg with NO verified identity keeps the
				// caller's fail-closed posture (http.go refuses to certify it).
				if published != nil {
					*installedOut = installedIdentityFromFileInfo(published)
				} else {
					*installedOut = captureInstalledDestIdentity(d.fs, destPath)
				}
			}
		} else {
			pubErr = fsutil.PublishNoReplace(d.fs, stagedPath, destPath)
		}
		if !errors.Is(pubErr, fsutil.ErrPublishCollision) {
			return false, false, pubErr
		}
		logging.Warnf("downloader: create install of %s raced a foreign writer — no-replace publish preserved its bytes; reclassifying destination as present", destPath)
		info, statErr = lstatBackupCandidate(d.fs, destPath)
	}
	switch {
	case statErr != nil:
		return false, false, fmt.Errorf("failed to stat destination: %w", statErr)
	case info == nil:
		return false, false, fmt.Errorf("failed to stat destination %s: filesystem returned no file information", destPath)
	}

	// R20-1/R20-3 type-discipline: the ledger legs only model REGULAR files.
	// A non-regular destination would be moved into a .dlbak backup that the
	// restore path cannot safely consume; a symlink (Lstat never follows) is
	// refused with the link itself intact.
	// All such objects are refused pre-journal (skip+warn — existing object untouched).
	if info.Mode()&os.ModeSymlink != 0 {
		logging.Warnf("downloader: overwrite of %s refused — destination is a symlink; keeping the link intact", destPath)
		return true, true, nil
	}
	if info.IsDir() {
		logging.Warnf("downloader: overwrite of %s refused — destination is a directory; keeping it intact", destPath)
		return true, true, nil
	}
	if !info.Mode().IsRegular() {
		logging.Warnf("downloader: overwrite of %s refused — destination is not a regular file (mode %s); keeping it intact", destPath, info.Mode())
		return true, true, nil
	}

	if !ledger.armed() {
		logging.Warnf("downloader: overwrite of %s refused — no revert-ledger operation recorded; keeping existing bytes", destPath)
		return true, true, nil
	}

	// The marker armed above covers the entire claim-aside/journal/install/
	// confirm window below: it is visible to a CLI/startup sweep in another
	// process, and its PID makes dead owners reclaimable instead of retaining
	// crash leftovers forever.
	// Wave-36 (codex local review round 6, PR#215 finding F1): the claimed
	// reservation stays IDENTITY-BOUND through the handoff — immediately
	// before the dest→backup move the name must still address THE claimed
	// 0-byte placeholder (overwriteBackupReservationStillOurs). A foreign
	// writer renaming the reservation away and planting its own occupant used
	// to have its bytes silently displaced by the replacing rename; the swap
	// now climbs to a fresh backup name exactly like a claim collision, and
	// the foreign occupant is never touched.
	//
	// Wave-37/wave-38 (codex P2, PR#215): the handoff itself is ATOMIC where
	// the platform offers the primitive — renameat2(RENAME_EXCHANGE) on
	// Linux/OsFs (backup_handoff_linux.go) swaps the dest and reservation
	// dentries with no window for a plant, then removes the exchange-parked
	// placeholder through the wave-38 take-aside (dest→scratch no-replace
	// take + claim-bound unlink) — and CONDITIONAL everywhere else
	// (handoffViaVerifiedRename takes the placeholder ASIDE onto a bound
	// scratch first, moves dest onto the freed backup name NO-REPLACE, and
	// unlinks only the scratch re-bound against the claim), so neither the
	// overwrite nor any cleanup can ever name a foreign occupant on POSIX or
	// Windows.
	var backupPath string
	var backupClaim os.FileInfo
	var claimErr error
	for attempt := 0; attempt < backupNameClaimTries && backupPath == ""; attempt++ {
		candidate, reservation, err := claimOverwriteBackupPath(d.fs, destPath, ledger.opID)
		if err != nil {
			claimErr = err
			break
		}
		if verErr := overwriteBackupReservationStillOurs(d.fs, candidate, reservation); verErr != nil {
			logging.Warnf("downloader: backup reservation for %s was displaced between claim and move (%v) — climbing to a fresh backup name; the foreign occupant is never touched", destPath, verErr)
			claimErr = verErr
			continue
		}
		backupPath = candidate
		backupClaim = reservation
	}
	if backupPath == "" {
		return false, true, fmt.Errorf("failed to claim backup path for %s: %w", destPath, claimErr)
	}
	if err := handoffToReservedBackup(d.fs, destPath, backupPath, backupClaim); err != nil {
		return false, true, fmt.Errorf("failed to set aside existing bytes for %s: %w", destPath, err)
	}
	// Wave-25 (codex P3 PR#215 finding 2): stamp the journal entry with the
	// OWNED backup object's identity facts (size + mtime) captured right after
	// the handoff — history's removal gate later binds its unlink to these
	// facts, so a foreign occupant swapped onto the backup NAME is never
	// deleted in place of our set-aside. A capture failure rolls back exactly
	// like a record failure: an armed entry whose ownership facts were never
	// verifiable is not worth journaling.
	// Wave-62 (codex P2, PR#215 finding F2): retain the arm-time backup
	// identity captured pre-RecordReplacement so the confirm-failure
	// rollback can authenticate the CURRENT backup occupant against the
	// ORIGINAL capture (not just the current backup name) before any bytes
	// reach dest. Stored alongside the in-memory install state, the same
	// seam family staged provenance uses to thread the validated object's
	// identity to the publish.
	armedBackupFacts := models.ReplacementBackupFacts{}
	var armErr error
	if facts, factsErr := captureReplacementBackupFacts(d.fs, backupPath); factsErr != nil {
		armErr = fmt.Errorf("capture backup identity facts: %w", factsErr)
	} else {
		armedBackupFacts = facts
		armErr = ledger.recorder.RecordReplacement(ctx, ledger.opID, destPath, backupPath, facts)
	}
	if armErr != nil {
		restored, rErr := restoreAsideBackup(d.fs, destPath, backupPath)
		if rErr != nil {
			return false, true, fmt.Errorf("revert-ledger record failed: %w (AND backup restore failed: %v — bytes remain at %s)", armErr, rErr, backupPath)
		}
		if !restored {
			// wave-17: the rollback was REFUSED (a foreign file claimed the
			// vacated destination, or the volume cannot express no-replace).
			// The foreign file is never replaced or removed; the retained
			// backup is UNJOURNALED here (the record failed) and stays
			// recoverable through the orphan sweep's conservative retention.
			return false, true, fmt.Errorf("revert-ledger record failed for %s: %w (rollback restore refused — destination occupied or no-replace unsupported; backup retained at %s with no journal entry)", destPath, armErr, backupPath)
		}
		return false, true, fmt.Errorf("revert-ledger record failed for %s: %w", destPath, armErr)
	}
	// Wave-26 (codex P2, PR#215 finding 3): the confirm-rollback baseline is
	// captured from the STAGED file BEFORE the publish, never from the
	// destination AFTER it. Wave-25 captured the destination right after
	// ReplaceFile — an object swapped onto the destination inside that window
	// was recorded as the baseline and the later rollback then overwrote the
	// foreign bytes. POSIX rename/MoveFileEx make the destination hold the
	// STAGED inode the moment the publish completes, so the staged capture
	// IS the published object's identity; a post-publish verification proves
	// the destination still names it, and the rollback recheck below compares
	// against THAT post-publish-verified baseline rather than any fresh
	// capture. A capture that cannot read the staged file fails closed to the
	// wave-25 unknown-identity posture (no verification here, rollback
	// refusal below) instead of turning a good publish into a rollback.
	// Wave-45 (codex P2, PR#215 finding F1): the wave-26 baseline capture must
	// prove it is THE VALIDATED object, not a window substitute — a proven
	// substitute never publishes on the replace path either. The refusal
	// routes through the publish-failure compensation below unmodified: the
	// set-aside restore and journal retraction run exactly like a failed
	// ReplaceFile, and the typed sentinel rides the same %w wrap so the
	// caller keeps the staged (substitute) name intact.
	installedIdentity := installedDestIdentity{}
	var replaceErr error
	switch {
	case classifyStagedInput(d.fs, stagedPath, provenanceID) == stagedInputMismatch:
		replaceErr = fmt.Errorf("staged install input %s no longer names the validated download object before replace of %s (foreign substitution after validation) — substitute preserved: %w", stagedPath, destPath, errStagedInputSubstituted)
	case provenanceID.known:
		// Wave-48 (codex P2, PR#215 finding 6): the dest capture + ReplaceFile
		// pair rides the bound publish — the fd of the staged object MUST equal
		// the validation record at adjacency to the mutation (a retained
		// validation handle is used directly; a consumed one is re-opened
		// no-follow and fstat-bound). The wave-26 baseline is then the
		// post-publish-VERIFIED destination object fsutil hands back (the
		// wave-31 shape), with the pre-publish capture kept only as the
		// virtual-leg fallback (fsutil reports no publish identity there). A
		// mismatch on any of these is the typed substitution refusal and rides
		// the unchanged publish-failure compensation below — stuck-refusal
		// preserving all.
		bindHandle := boundHandle
		if bindHandle == nil {
			var bindErr error
			bindHandle, bindErr = bindStagedProvenanceHandle(d.fs, stagedPath, provenanceID)
			if bindErr != nil {
				replaceErr = bindErr
			}
		}
		if replaceErr == nil {
			boundHandle = nil
			published, pubErr := publishStagedInstall(d.fs, stagedPath, destPath, bindHandle, fsutil.ReplaceFile)
			if pubErr == nil {
				// The wave-26 baseline is the post-publish-VERIFIED destination
				// object fsutil hands back (the wave-31 shape — exactly
				// copyBackupToDestPublish's facts.restored discipline), never a
				// pre-publish re-derivation of the mutable name.
				installedIdentity = installedIdentityFromFileInfo(published)
				if published == nil {
					// The VIRTUAL leg hands back NO publish identity AND its close
					// re-stamps mem-file ModTimes (any pre-publish staged capture is
					// invalid by the rename instant), so the baseline is the
					// destination's own post-publish capture — the wave-25 shape,
					// self-consistent by construction on filesystems with no
					// rename-away threat model (the documented wave-29/30/31 virtual
					// posture). A failed capture stays the unknown identity, keeping
					// the confirm-failure rollback fail-closed.
					installedIdentity = captureInstalledDestIdentity(d.fs, destPath)
				}
			} else if subErr := stagedPublishVerdict(pubErr); subErr != nil {
				replaceErr = subErr
			} else if fsutil.PublishCompleted(pubErr) && published != nil {
				// Wave-68 (codex P2, PR#215 finding F1): a completed-carrying
				// publish error WITH a verified non-nil identity (the
				// ENOSYS-times-skipped leg on AIX/Solaris/illumos-shaped
				// platforms — PublishStagedBoundInfo hands back the
				// post-publish-verified destination stat alongside
				// ErrPublishCompleted) means the publish SUCCEEDED: dest provably
				// carries the staged bytes. Treat it exactly like the success leg —
				// retain installedIdentityFromFileInfo(published) and continue
				// through confirmation — mirroring the established
				// copyBackupToDestPublish seam's r15 discipline (the create path's
				// wave-68 F2 files the same record). Pre-fix this fell to the
				// plain `replaceErr = pubErr` leg: the rollback then refused (dest
				// occupied by the just-installed bytes), install was reported
				// failed while the new bytes were already installed + the journal
				// armed-unconfirmed, and reverts misfired later. The
				// rollbackPublishStagedBoundInfoFn seam is NOT involved here (the
				// replace path rides publishStagedBoundFn via publishStagedInstall);
				// the pattern is the same, so the same completed-with-identity
				// handling applies. A nil identity keeps the legacy discipline
				// below (the completed-but-unproven virtual leg refuses to
				// certify an unproven publish).
				installedIdentity = installedIdentityFromFileInfo(published)
			} else {
				replaceErr = pubErr
			}
		}
	default:
		installedIdentity = captureInstalledDestIdentity(d.fs, stagedPath)
		replaceErr = fsutil.ReplaceFile(d.fs, stagedPath, destPath)
	}
	if replaceErr == nil && installedIdentity.known && !destStillHoldsInstalledObject(d.fs, destPath, installedIdentity) {
		// The publish reported success but the destination does NOT name the
		// staged object — a foreign writer displaced it inside the publish
		// window. Route through the publish-failure rollback: the no-replace
		// restore preserves the foreign occupant (or lands the backup when
		// the destination vanished) exactly like a publish error.
		replaceErr = fmt.Errorf("post-publish identity break — destination %s does not name the staged install object: %w", destPath, fsutil.ErrPublishStagedIdentityBreak)
	}
	if replaceErr != nil {
		restored, rErr := restoreAsideBackup(d.fs, destPath, backupPath)
		if rErr != nil {
			return false, true, fmt.Errorf("failed to replace %s: %w (AND backup restore failed: %v — bytes remain at %s)", destPath, replaceErr, rErr, backupPath)
		}
		if !restored {
			// wave-17: rollback REFUSED — the foreign destination keeps its
			// bytes and the journal entry stays armed against the retained
			// backup (still at backupPath): a later sweep/revert recovers the
			// original bytes. Nothing to retract — the backup was NOT consumed.
			return false, true, fmt.Errorf("failed to replace %s: %w (rollback restore refused — destination occupied or no-replace unsupported; journal entry stays armed against the retained backup %s)", destPath, replaceErr, backupPath)
		}
		// The backup was consumed by the rollback restore — retract the journal
		// entry or the row permanently points at a vanished backup and every
		// later revert of this op fails stat-ing it (codex P3 round 1).
		if relErr := ledger.recorder.ReleaseReplacement(ctx, ledger.opID, destPath, backupPath); relErr != nil {
			logging.Warnf("downloader: release of rolled-back journal entry failed for %s: %v (destination is correct); re-arming backup", destPath, relErr)
			if rearmErr := rearmReplacementBackup(d.fs, destPath, backupPath); rearmErr != nil {
				// Wave-21: EVERY failure class disarms the entry; only the kind
				// routes on backup-name ownership (rollbackRearmPendingKind).
				markRollbackRearmFailed(ctx, ledger, destPath, backupPath, rearmErr)
			}
		}
		return false, true, fmt.Errorf("failed to replace file: %w", replaceErr)
	}
	// R4-3/R5-2/R9-1: confirm the install so the sweeper can distinguish
	// "backup journaled but install never landed" from "installed media
	// deleted afterwards". An unconfirmed entry MUST NOT outlive a
	// successful return: a transient confirm failure rolls the install back
	// WITHOUT consuming the backup (staged copy + swap). The backup is then
	// removed while the journal still owns it, before the successful retract.
	// If retract itself fails, re-arm the backup so the still-armed journal
	// remains mutually consistent for sweep/retry.
	//
	// Wave-25 (codex P3 PR#215 finding 3): the window between the install
	// publish above and a confirm-failure rollback is open to foreign writers
	// — the busy marker deters Javinizer participants, not other processes —
	// and the rollback's copy runs with REPLACE semantics, so restoring
	// unconditionally would DESTROY a foreign writer's new destination bytes.
	// The installed object's identity (dev/inode when exposed, plus size +
	// mtime; a failed capture is a refusal, never an overwrite of the
	// unverifiable) must therefore still describe the destination when the
	// rollback restores: a mismatch refuses with the backup RETAINED and the
	// journal entry left armed (a later sweep/revert arbitrates ownership),
	// while a match restores as before. Wave-26 (codex P2): the baseline is
	// the STAGED object's identity captured BEFORE the publish and verified
	// against the destination right after it (above) — the rollback recheck
	// never re-learns an occupant, so a foreign inode swapped in around the
	// publish can never be appointed baseline.
	if installedOut != nil {
		// Wave-67: the replace path's wave-26/wave-31 verified baseline IS the
		// producer record — the post-publish-VERIFIED destination object.
		*installedOut = installedIdentity
	}
	if cErr := ledger.recorder.ConfirmReplacement(ctx, ledger.opID, destPath, backupPath); cErr != nil {
		if !destStillHoldsInstalledObject(d.fs, destPath, installedIdentity) {
			logging.Warnf("downloader: rollback restore of %s refused — destination no longer names the just-installed object after confirm failure (%v); foreign bytes kept, backup %s retained in place, journal entry stays armed", destPath, cErr, backupPath)
			return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore refused — destination no longer holds the installed object; foreign bytes preserved, backup retained at %s, journal entry stays armed)", cErr, backupPath)
		}
		copyFacts, rErr := copyBackupToDestBoundFacts(d.fs, backupPath, destPath, &armedBackupFacts)
		if rErr != nil {
			return false, true, fmt.Errorf("install-confirm failed: %w (AND rollback restore failed: %w — bytes remain at %s)", cErr, rErr, backupPath)
		}
		// Wave-31 (codex local round 1, PR#215 finding L1): the copy returned
		// the destination's OWN post-publish object identity; the destination
		// must STILL name the restored object before the backup — the sole
		// remaining copy of the pre-existing bytes — is removed and the journal
		// entry retracted. A foreign writer swapping or deleting the
		// destination inside the publish→remove window otherwise had the
		// (now-foreign) destination blessed while the backup was unlinked and
		// the entry released. On mismatch: refuse — the destination stays
		// untouched, the backup is RETAINED, and the entry stays ARMED for
		// sweep/revert arbitration.
		if !rollbackRestoredDestStillOurs(d.fs, destPath, copyFacts.restored) {
			logging.Warnf("downloader: confirm-failure rollback of %s restored the pre-existing bytes but the destination no longer names the restored object — backup %s retained in place, journal entry stays armed, destination untouched", destPath, backupPath)
			return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore of %s landed but the destination no longer names the restored object; backup retained at %s, journal entry stays armed)", cErr, destPath, backupPath)
		}
		// Wave-31 (codex local round 1, PR#215 finding L2): the backup removal
		// is bound to the object the rollback COPIED (the in-memory facts
		// recorded from the no-follow source handle) — a foreign plant swapped
		// onto the backup name inside the copy→remove window is kept
		// byte-intact instead of being unlinked in the set-aside's place.
		//
		// Wave-32 (codex local review round 2, PR#215 finding R1): the wave-31
		// destination check above ran BEFORE the removal; a foreign swap or
		// deletion landing between the two used to get the backup — the sole
		// remaining copy of the pre-existing bytes — unlinked while the journal
		// release went through. The removal therefore runs through history's
		// quarantine construction (ported to rollback_backup_quarantine.go):
		// the verified backup object moves aside, the destination is re-gated,
		// and only then is the QUARANTINED object unlinked.
		hold, rmErr := quarantineRollbackBackupForRemoval(d.fs, backupPath, copyFacts.copied, "install-confirm rollback")
		if rmErr == nil && !rollbackRestoredDestStillOurs(d.fs, destPath, copyFacts.restored) {
			if rerr := hold.restore(); rerr != nil {
				// Finding F2 (codex P2 PR#215, mirroring history's wave-36 F3
				// caller shape): the move-back failed — the journaled backup name
				// is unowned (a foreign claimant holds it or the publish wedged)
				// while the restored bytes sit at the quarantine name. Propagate
				// the failure AND disarm the entry: left armed it would later
				// stat/copy/remove the foreign occupant at that name, so the
				// rearm-refused pending kind persists instead (journal-only
				// retry; the quarantined bytes stay recoverable manually).
				markRollbackQuarantineRestoreFailed(ctx, ledger, destPath, backupPath, hold.quarantine, rerr)
				return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore of %s landed but the destination diverged after the backup was quarantined and the verified move-back failed: %v — the journaled backup name is unowned; entry marked restore-pending (rearm-refused), restored bytes recoverable at the quarantine name %s)", cErr, destPath, rerr, hold.quarantine)
			}
			logging.Warnf("downloader: confirm-failure rollback of %s restored the pre-existing bytes but the destination diverged after the backup was quarantined — backup %s restored to its journaled name, journal entry stays armed, destination untouched", destPath, backupPath)
			return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore of %s landed but the destination diverged after the backup was quarantined; backup restored to %s, journal entry stays armed, destination untouched)", cErr, destPath, backupPath)
		}
		if rmErr == nil {
			rmErr = hold.removeVerified()
		}
		if rmErr != nil {
			if errors.Is(rmErr, errRollbackQuarantineRestoreFailed) {
				// Finding F2: an internal wedge leg's move-back JOINED into the
				// removal error — the journaled backup name is unowned while the
				// restored bytes sit at the quarantine name, so the entry leaves
				// the armed state as rearm-refused pending (journal-only retry).
				quarantine := "(unknown)"
				if hold != nil {
					quarantine = hold.quarantine
				}
				markRollbackQuarantineRestoreFailed(ctx, ledger, destPath, backupPath, quarantine, rmErr)
				return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes, but backup cleanup failed and the verified move-back also failed: %w (confirmation error: %v; journaled backup name unowned; entry marked restore-pending (rearm-refused), restored bytes recoverable at the quarantine name)", rmErr, cErr)
			}
			return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes, but backup cleanup failed: %w (confirmation error: %v; entry stays armed)", rmErr, cErr)
		}
		if relErr := ledger.recorder.ReleaseReplacement(ctx, ledger.opID, destPath, backupPath); relErr != nil {
			logging.Warnf("downloader: release of install-confirm rollback entry failed for %s: %v; re-arming backup", destPath, relErr)
			if rearmErr := rearmReplacementBackup(d.fs, destPath, backupPath); rearmErr != nil {
				// Wave-21: EVERY failure class disarms the entry; only the kind
				// routes on backup-name ownership (rollbackRearmPendingKind).
				markRollbackRearmFailed(ctx, ledger, destPath, backupPath, rearmErr)
			}
			return false, true, fmt.Errorf("install-confirm retract failed after rollback (%v): %w (backup %s re-arm attempted; entry stays journaled for sweep/revert recovery)", cErr, relErr, backupPath)
		}
		return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes: %w", cErr)
	}
	return false, true, nil
}
