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

// restoreStagingOwnershipFn is the downloader rollback's ownership hand-off
// seam, mirroring history's same-named discipline (restoreStagingOwnershipFn
// in reverter_replacements_p3.go): production forwards to
// fsutil.RestoreStagingOwnership; tests record/replay the exact mid-flow
// instant between the staged copy and the wave-30 bound publish.
var restoreStagingOwnershipFn = fsutil.RestoreStagingOwnership

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
func copyBackupToDestBound(fsys afero.Fs, backup, dest string) (rollbackCopyFacts, error) {
	return copyBackupToDestPublish(fsys, backup, dest, fsutil.ReplaceFile, false)
}

// copyBackupToDestNoReplace is copyBackupToDest whose staged publish NEVER
// replaces an occupied destination: callers who copy onto a name their own
// rollback just vacated (the re-arm direction) must not clobber a foreign
// object that claimed the name mid-window. A collision drops the staged copy
// and returns the typed fsutil.ErrPublishCollision (see wave-15).
func copyBackupToDestNoReplace(fsys afero.Fs, backup, dest string) error {
	_, err := copyBackupToDestPublish(fsys, backup, dest, fsutil.PublishNoReplace, true)
	return err
}

func copyBackupToDestPublish(fsys afero.Fs, backup, dest string, publish func(afero.Fs, string, string) error, noReplace bool) (rollbackCopyFacts, error) {
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
	if _, cerr := io.CopyBuffer(dstFile, src, buf); cerr != nil {
		_ = dstFile.Close()
		_ = fsys.Remove(staged)
		return rollbackCopyFacts{}, fmt.Errorf("copy rollback: %w", cerr)
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
	published, pubErr := fsutil.PublishStagedBoundInfo(fsutil.StagedPublish{
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
			_ = fsys.Remove(staged)
			return rollbackCopyFacts{}, fmt.Errorf("stage rollback times: %w", pubErr)
		case errors.Is(pubErr, fsutil.ErrPublishStagedClose):
			_ = fsys.Remove(staged)
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
	return models.ReplacementBackupFacts{Size: info.Size(), ModUnix: info.ModTime().Unix()}, nil
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
// filesystem legs (fsutil reports no publish identity there) — the wave-32
// fsutil deferred-times legs refuse an indeterminate relookup with a typed
// error instead of degrading to a nil identity, and the copy leg propagates
// that error, so a real-filesystem publish never arrives with an
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
// the caller's dest→backup handoff (moveIntoReservedBackup — replace-aware,
// ReplaceFile on Windows) safely replaces the reservation, so every
// participant either wins a unique name or fails closed.
// moveIntoReservedBackup hands the destination bytes to the atomically
// RESERVED backup name returned by claimOverwriteBackupPath: that name is
// occupied by the claim's 0-byte placeholder, so the handoff must REPLACE an
// existing target. POSIX rename does so atomically; Windows rename
// (OsFs.Rename → MoveFileW) REFUSES an existing destination, which turned
// every ledger-armed overwrite of an existing file into a set-aside failure
// on Windows (codex PR#215 w12). The Windows leg therefore routes through
// fsutil.ReplaceFile (OsFs → MoveFileExW with MOVEFILE_REPLACE_EXISTING).
// The leg is keyed on the same fsutil.PathBackslashesAreSeparators
// Windows-posture seam the history package's restoreOSPath/DestKey use —
// instead of a build tag — so the Windows branch is exercisable in host
// tests; both legs are behaviorally identical on a POSIX host because
// POSIX ReplaceFile is itself a rename. The set-aside is the only leg that
// renames into a reserved (pre-existing placeholder) target; the rollback
// restores below publish onto a path the set-aside just vacated with
// NO-REPLACE semantics (restoreAsideBackup, wave-17), so a foreign dest
// claimed mid-window is refused and kept instead of clobbered (Windows's
// MoveFileExW-without-replace refusal maps into the same retained class).
func moveIntoReservedBackup(fsys afero.Fs, src, dst string) error {
	if fsutil.PathBackslashesAreSeparators {
		return fsutil.ReplaceFile(fsys, src, dst)
	}
	return fsys.Rename(src, dst)
}

func claimOverwriteBackupPath(fsys afero.Fs, destPath, opID string) (string, error) {
	for attempt := 0; attempt < backupNameClaimTries; attempt++ {
		candidate := overwriteBackupPath(destPath, opID)
		if _, err := lstatBackupCandidate(fsys, candidate); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect backup candidate %s: %w", candidate, err)
		}
		reservation, reserveErr := fsys.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case reserveErr == nil:
			if err := reservation.Close(); err != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state — drop it rather than renaming over unverified bytes.
				_ = fsys.Remove(candidate)
				return "", fmt.Errorf("close backup reservation %s: %w", candidate, err)
			}
			return candidate, nil
		case os.IsExist(reserveErr):
			// A foreign claimant won the Lstat-to-OpenFile window; climb.
			continue
		default:
			return "", fmt.Errorf("reserve backup candidate %s: %w", candidate, reserveErr)
		}
	}
	return "", fmt.Errorf("backup names exhausted for %s after %d attempts", destPath, backupNameClaimTries)
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
func (d *Downloader) installOverwriting(ctx context.Context, stagedPath, destPath string, ledger downloadLedger) (bool, bool, error) {
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
		pubErr := fsutil.PublishNoReplace(d.fs, stagedPath, destPath)
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
	backupPath, claimErr := claimOverwriteBackupPath(d.fs, destPath, ledger.opID)
	if claimErr != nil {
		return false, true, fmt.Errorf("failed to claim backup path for %s: %w", destPath, claimErr)
	}
	if err := moveIntoReservedBackup(d.fs, destPath, backupPath); err != nil {
		// The failed handoff left our 0-byte reservation in place — release it
		// so a retry never has to climb past (or worse, journal) a placeholder.
		_ = d.fs.Remove(backupPath)
		return false, true, fmt.Errorf("failed to set aside existing bytes for %s: %w", destPath, err)
	}
	// Wave-25 (codex P3 PR#215 finding 2): stamp the journal entry with the
	// OWNED backup object's identity facts (size + mtime) captured right after
	// the handoff — history's removal gate later binds its unlink to these
	// facts, so a foreign occupant swapped onto the backup NAME is never
	// deleted in place of our set-aside. A capture failure rolls back exactly
	// like a record failure: an armed entry whose ownership facts were never
	// verifiable is not worth journaling.
	var armErr error
	if facts, factsErr := captureReplacementBackupFacts(d.fs, backupPath); factsErr != nil {
		armErr = fmt.Errorf("capture backup identity facts: %w", factsErr)
	} else {
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
	installedIdentity := captureInstalledDestIdentity(d.fs, stagedPath)
	replaceErr := fsutil.ReplaceFile(d.fs, stagedPath, destPath)
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
	if cErr := ledger.recorder.ConfirmReplacement(ctx, ledger.opID, destPath, backupPath); cErr != nil {
		if !destStillHoldsInstalledObject(d.fs, destPath, installedIdentity) {
			logging.Warnf("downloader: rollback restore of %s refused — destination no longer names the just-installed object after confirm failure (%v); foreign bytes kept, backup %s retained in place, journal entry stays armed", destPath, cErr, backupPath)
			return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore refused — destination no longer holds the installed object; foreign bytes preserved, backup retained at %s, journal entry stays armed)", cErr, backupPath)
		}
		copyFacts, rErr := copyBackupToDestBound(d.fs, backupPath, destPath)
		if rErr != nil {
			return false, true, fmt.Errorf("install-confirm failed: %w (AND rollback restore failed: %v — bytes remain at %s)", cErr, rErr, backupPath)
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
			hold.restore()
			logging.Warnf("downloader: confirm-failure rollback of %s restored the pre-existing bytes but the destination diverged after the backup was quarantined — backup %s restored to its journaled name, journal entry stays armed, destination untouched", destPath, backupPath)
			return false, true, fmt.Errorf("install-confirm failed: %w (rollback restore of %s landed but the destination diverged after the backup was quarantined; backup restored to %s, journal entry stays armed, destination untouched)", cErr, destPath, backupPath)
		}
		if rmErr == nil {
			rmErr = hold.removeVerified()
		}
		if rmErr != nil {
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
