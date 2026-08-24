package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// POSTER-WRITE-HARDENING P3 — orphan/decree sweep for the revert-ledger's
// destination-adjacent backups (<dest>.dlbak.<opID-hash16>).
//
// Sweep rules (conservative ownership):
//   - Only names matching the exact ownership marker `*.dlbak.<16 lowercase
//     hex>` are eligible; foreign lookalikes are never touched.
//   - A backup journaled on ANY operation row (applied OR failed) is always
//     retained — it is the only copy of that row's pre-replace bytes.
//   - Before arbitrating any candidate, the sweeper claims the durable
//     destination-adjacent `<dest>.dlbusy` marker. A live owner (including a
//     marker from this boot) makes the candidate stay untouched; dead-PID
//     markers are reclaimed. Malformed markers are retained because age alone
//     does not prove ownership.
//   - A journaled backup whose destination went missing belongs to the crash
//     window between set-aside and install: the new bytes never landed, so
//     the old bytes are restored to the destination and the journal entry is
//     consumed (the row otherwise stays failed/unrevertable forever).
//   - A backup journaled NOWHERE is not ownership-proven by its name alone:
//     with the destination present it is retained and warned about; with the
//     destination missing it is the last copy of somebody's bytes (restored),
//     but the marker file is retained for manual inspection.

// R12-3: end-anchored — a destination legitimately named with a
// marker-shaped substring must not confuse the suffix detector; only the
// FINAL marker counts. R4-4's ordinal tail is the optional trailing group.
var replacementBackupName = regexp.MustCompile(`\.dlbak\.[0-9a-f]{16}(\.[0-9a-f]{1,16})?$`)

// acquireReplacementBusyExFn is the busy-marker acquisition seam behind
// sweepOne/consumeRearmRefusedPending (same discipline as
// rearmPublishFn/restoreStagingOwnershipFn in reverter_replacements_p3.go):
// production acquires through fsutil.AcquireReplacementBusyEx; tests drive
// the wave-56 (finding F2) provenance-unavailable refusal leg — an acquire
// that yields a non-nil release with an empty token and a nil error. The
// token is always non-empty on a nil error in production, so the refusal
// leg is only reachable through this seam.
var acquireReplacementBusyExFn = fsutil.AcquireReplacementBusyEx

// journalEntryInstalled reports whether the row journals this backup with
// the install-confirm marker set.
func journalEntryInstalled(row *models.BatchFileOperation, backupSlash string) bool {
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return false // unparseable row — treat as armed (conservative restore posture)
	}
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			return rep.Installed
		}
	}
	return false
}

func journalEntryRestorePending(row *models.BatchFileOperation, backupSlash string) bool {
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return false
	}
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			return rep.RestorePending
		}
	}
	return false
}

// journalEntryPendingKind reports the journaled entry's NORMALIZED
// restore-pending kind ("" when the entry is missing or not pending). Sweep
// legs route on this: the wave-19 rearm-refused kind must never drive any
// path operation against the (unowned) backup name.
func journalEntryPendingKind(row *models.BatchFileOperation, backupSlash string) string {
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return ""
	}
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			return rep.PendingKind()
		}
	}
	return ""
}

// sweepSlash normalizes a path for journal comparison via the probe-aware
// destination key: backslash separators normalize only under the Windows
// seam, and case folds only on an insensitive/tolerant destination root.
// Audit: POSIX sweep journals carry `/` spellings, so literal `\\` names stay
// distinct; Windows legacy backup spellings continue to cross-match.
func sweepSlash(p string) string { return fsutil.DestKey(p) }

// IsReplacementBackupName reports whether name carries the revert-ledger
// ownership marker (destination-adjacent backup from downloader overwrites).
func IsReplacementBackupName(name string) bool {
	return replacementBackupName.MatchString(name)
}

// warnRetainedUnjournaledBackup keeps name-only matches inspectable. Marker
// shape is not ownership proof, so the conservative restore posture leaves
// manual deletion to the user when no journal entry proves Javinizer created it.
func warnRetainedUnjournaledBackup(backup string) {
	absoluteBackup, _ := filepath.Abs(backup)
	logging.Warnf("replacement sweep retained unjournaled marker-shaped file %s: no journal entry proves ownership; user can delete it manually", absoluteBackup)
}

// BackupRemovalRefusedError reports a refused backup removal (wave-25,
// codex P3 PR#215 finding 2): the object currently occupying the journaled
// backup name failed the ownership binding, so it must be a foreign plant —
// unlinking it would destroy bytes nobody journaled AND consume the only
// journal record of the restore. The entry stays live for retry/inspection.
type BackupRemovalRefusedError struct {
	Backup string // journaled backup path whose removal was refused
	Reason string // which ownership fact failed
}

func (e *BackupRemovalRefusedError) Error() string {
	return fmt.Sprintf("backup removal %s refused: %s", e.Backup, e.Reason)
}

func refuseReplacementBackupRemoval(backup, phase, reason string) error {
	absoluteBackup, _ := filepath.Abs(backup)
	logging.Warnf("%s refused to remove backup %s: %s; journal entry retained live", phase, absoluteBackup, reason)
	return &BackupRemovalRefusedError{Backup: backup, Reason: reason}
}

// journaledEntryFacts returns a COPY of the journal entry naming backupSlash
// in row's (possibly index-time) ledger, or nil when the ledger has no such
// entry / cannot be parsed. Entry facts are written once at journal-append
// time and never rewritten, so an index-time snapshot's facts stay valid for
// a later removal gate (wave-25).
func journaledEntryFacts(row *models.BatchFileOperation, backupSlash string) *models.ReplacementEntry {
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return nil
	}
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			entry := rep
			return &entry
		}
	}
	return nil
}

// quarantineReplacementBackupForRemoval runs removeReplacementBackup's
// ownership-binding legs plus the wave-26 verified quarantine move, then
// STOPS before the only unlink (wave-32, codex local review round 2, PR#215
// finding R1): the caller re-proves its destination gate against the hold
// between the move and the unlink, and either finishes with the hold's
// removeVerified or puts the verified object back with the hold's restore.
// Errors are exactly removeReplacementBackup's (nil == the name was already
// gone, *BackupRemovalRefusedError for a proven-foreign occupant, plain
// errors for indeterminate states, plus the wave-32 vanished sentinel for
// unownable post-move loss).
func quarantineReplacementBackupForRemoval(fs afero.Fs, backup, phase string, entry *models.ReplacementEntry, copiedFrom os.FileInfo) (*replacementBackupQuarantine, error) {
	handle, verified, err := openVerifiedReplacementBackup(fs, backup, phase, entry, copiedFrom)
	if err != nil {
		return nil, err
	}
	if handle == nil {
		// The journaled name was genuinely absent — removed already.
		return &replacementBackupQuarantine{fs: fs, backup: backup, phase: phase, unlinked: true}, nil
	}
	defer func() { _ = handle.Close() }()
	hold, err := quarantineVerifiedBackup(fs, backup, phase, handle, verified)
	if err != nil {
		return nil, err
	}
	return hold, nil
}

// quarantineReplacementBackupForPrune preserves a partially moved quarantine
// hold for the prune path; generic sweep callers retain their established
// nil-hold error contract.
func quarantineReplacementBackupForPrune(fs afero.Fs, backup, phase string, entry *models.ReplacementEntry, copiedFrom os.FileInfo) (*replacementBackupQuarantine, error) {
	handle, verified, err := openVerifiedReplacementBackup(fs, backup, phase, entry, copiedFrom)
	if err != nil {
		return nil, err
	}
	if handle == nil {
		return &replacementBackupQuarantine{fs: fs, backup: backup, phase: phase, unlinked: true}, nil
	}
	defer func() { _ = handle.Close() }()
	return quarantineVerifiedBackup(fs, backup, phase, handle, verified)
}

// openVerifiedReplacementBackup carries removeReplacementBackup's gate legs:
// Lstat the occupant no-follow and gate its shape + journaled facts +
// restore-read binding, then reopen the name NO-FOLLOW and require the
// opened object to be the Lstat object. (nil, nil, nil) reports a genuinely
// absent name — "already gone == removed" (the established ownership rule).
func openVerifiedReplacementBackup(fs afero.Fs, backup, phase string, entry *models.ReplacementEntry, copiedFrom os.FileInfo) (afero.File, os.FileInfo, error) {
	absoluteBackup, _ := filepath.Abs(backup)
	info, lerr := lstatRestoreSource(fs, backup)
	if lerr != nil {
		if errors.Is(lerr, afero.ErrFileNotFound) {
			return nil, nil, nil // already gone == removed (established ownership rule)
		}
		logging.Warnf("%s failed to inspect backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, lerr)
		return nil, nil, lerr
	}
	if info == nil {
		err := fmt.Errorf("filesystem returned no file information for %s", backup)
		logging.Warnf("%s failed to inspect backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, err)
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, refuseReplacementBackupRemoval(backup, phase, "name names a symlink, never the owned set-aside")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("name names a non-regular object (mode %s)", info.Mode()))
	}
	if entry != nil && entry.BackupFactsStamped() {
		if info.Size() != entry.BackupSize || info.ModTime().Unix() != entry.BackupModUnix {
			return nil, nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("occupant identity mismatch — journaled %d bytes @ %d, found %d bytes @ %d", entry.BackupSize, entry.BackupModUnix, info.Size(), info.ModTime().Unix()))
		}
	}
	if copiedFrom != nil {
		if srcDev, srcIno, srcOK := restoreSourceIdentity(copiedFrom); srcOK {
			if curDev, curIno, curOK := restoreSourceIdentity(info); curOK && (srcDev != curDev || srcIno != curIno) {
				return nil, nil, refuseReplacementBackupRemoval(backup, phase, "occupant is not the object the restore read (dev/inode mismatch)")
			}
		}
		if info.Size() != copiedFrom.Size() || !info.ModTime().Equal(copiedFrom.ModTime()) {
			return nil, nil, refuseReplacementBackupRemoval(backup, phase, "occupant metadata differs from the object the restore read")
		}
	}
	handle, oerr := restoreOpenReplacementSource(fs, backup)
	if oerr != nil {
		if os.IsNotExist(oerr) {
			return nil, nil, nil // vanished under us == removed
		}
		logging.Warnf("%s failed to reopen backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, oerr)
		return nil, nil, oerr
	}
	openedInfo, serr := handle.Stat()
	if serr != nil {
		_ = handle.Close()
		logging.Warnf("%s failed to stat opened backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, serr)
		return nil, nil, serr
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		_ = handle.Close()
		return nil, nil, refuseReplacementBackupRemoval(backup, phase, "opened object is not the regular file Lstat verified")
	}
	if curDev, curIno, curOK := restoreSourceIdentity(info); curOK {
		if opDev, opIno, opOK := restoreSourceIdentity(openedInfo); opOK && (curDev != opDev || curIno != opIno) {
			_ = handle.Close()
			return nil, nil, refuseReplacementBackupRemoval(backup, phase, "opened object differs from the Lstat object")
		}
	}
	// Wave-63 (codex P2, PR#215 finding F2): size+mtime are forgeable — an
	// owned backup replaced by same-size+same-mtime foreign bytes survived the
	// gates above and the journal entry got consumed. Hash the opened handle
	// and compare to the journaled digest; mismatch refuses (quarantine never
	// starts, entry live, foreign preserved). An empty sha keeps wave-25.
	if entry != nil && entry.BackupSHA256 != "" {
		h := sha256.New()
		if _, herr := io.Copy(h, handle); herr != nil {
			_ = handle.Close()
			return nil, nil, fmt.Errorf("hash backup %s before removal: %w", backup, herr)
		}
		if hex.EncodeToString(h.Sum(nil)) != entry.BackupSHA256 {
			_ = handle.Close()
			return nil, nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("occupant sha256 mismatch — journaled %s, found %s", entry.BackupSHA256, hex.EncodeToString(h.Sum(nil))))
		}
	}
	// Wave-26: the handle stays OPEN past the fstat — the quarantine move
	// keeps it open through the rename on POSIX so the moved object is
	// provably the opened one (Windows posture closes it first inside
	// moveVerifiedBackupToQuarantine; the re-verify still binds).
	return handle, openedInfo, nil
}

// markReplacementRestorePendingKind records that restore bytes are installed while
// the backup cleanup still needs a retry — with an explicit pending kind.
// explicit pending kind (wave-19). The merge discipline lives in
// models.ReplacementEntry.SetRestorePending: identical re-marks no-op, and a
// rearm-refused kind upgrade is one-way (a name once proven unowned never
// re-enters the removal path).
func markReplacementRestorePendingKind(gf *models.GeneratedFilesJSON, backupSlash, kind string) bool {
	for i := range gf.Replacements {
		if sweepSlash(gf.Replacements[i].Backup) != backupSlash {
			continue
		}
		return gf.Replacements[i].SetRestorePending(kind)
	}
	return false
}

// markReplacementEntryRestorePendingKind is markReplacementEntryRestorePending
// with an explicit pending kind (wave-19): the explicit reverter's refused
// re-arm compensation marks rearm-refused, certifying the destination bytes
// while keeping every retry off the unowned backup name.
func markReplacementEntryRestorePendingKind(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, rowID uint, backupSlash, kind string) error {
	release := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer release()
	txErr := repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !markReplacementRestorePendingKind(&gf, backupSlash, kind) {
			return gf, false, nil
		}
		return gf, true, nil
	})
	if errors.Is(txErr, database.ErrNotFound) {
		return fmt.Errorf("owner row %d not found", rowID)
	}
	return txErr
}

// rearmOccupiedClass reports whether a re-arm failure carries the typed
// occupied-name classes — fsutil.ErrPublishCollision (a foreign writer owns
// the backup name now) or fsutil.ErrPublishNoReplaceUnsupported (the volume
// cannot express an atomic no-replace publish at all). In both classes the
// journaled backup name is unreclaimable, so a journal entry left ARMED would
// point later restores at content this operation does not own: the retry must
// be driven by the durable RestorePending marker, never by the occupied path.
// Wave-19: the classifier itself is fsutil.PublishRefusal, shared with the
// downloader's rollback re-arm refusal handling.
func rearmOccupiedClass(err error) bool {
	return fsutil.PublishRefusal(err)
}

// rearmPendingKind classifies a FAILED re-arm for the restore-pending marker
// every compensation leg persists (wave-20, codex P2, PR#215). ANY re-arm
// failure must disarm the entry — one left ARMED against an absent backup
// name wedges every explicit retry at the source stat forever while sweeps
// see an ordinary armed row with a present destination and repair nothing.
// Only the KIND varies with backup-name ownership:
//   - failures AFTER the staged copy definitely PUBLISHED — exactly the
//     fsutil.PublishCompleted class (fsutil.ErrPublishCompleted, the
//     hard-link fallback's cleanup-plus-failed-rollback leg) shared with the
//     downloader so both packages read one publish error the same (wave-21,
//     codex P2) — mean the name carries THIS operation's own bytes →
//     RestorePendingKindClean: the pending retry removes the owned name and
//     consumes the entry;
//   - everything else proves NOTHING about the name and takes
//     RestorePendingKindRearmRefused: the publish REFUSAL classes
//     (rearmOccupiedClass: occupied name, or a volume that cannot express
//     no-replace) leaving it foreign or absent, AND failures BEFORE any
//     publish completed (re-arm source open, staging open/write/close,
//     pre-publish metadata fix-ups — wave-21 applies mode/times/ownership
//     to the staged inode, never the published name, so their failure means
//     nothing published).
func rearmPendingKind(rearmErr error) string {
	if fsutil.PublishCompleted(rearmErr) {
		return models.RestorePendingKindClean
	}
	return models.RestorePendingKindRearmRefused
}

// pendingKindForRemovalError routes a FAILED wave-32 quarantine removal onto
// the durable pending kind the retry shape requires. The vanished class
// (finding R4) is special: the verified object moved to its quarantine name
// and then vanished unownably — the journaled name is ABSENT by
// construction, so no retry may ever stat/copy/remove it (the clean-kind
// retry legs would stitch to a forever-absent source). The rearm-refused
// kind's journal-only consumption is the exact fit: the marker still
// certifies the destination carries the restored bytes, and the wave-29
// ledger-enumerated rearm-refused retry converges without any file. Every
// other failure class leaves the name's ownership live (attempt retained,
// foreign plant restored back, or nothing moved at all) and keeps the
// ordinary clean marker. Wave-36 (codex local review round 6, PR#215
// finding F3): a FAILED wedge-compensation move-back
// (errBackupQuarantineRestoreFailed, joined into the removal error chain)
// takes the same rearm-refused routing — the journaled name is UNOWNED
// there too (foreign-claimed while the verified bytes stayed recoverable at
// the quarantine name), so no retry may stat/copy/remove it either.
func pendingKindForRemovalError(rmErr error) string {
	if errors.Is(rmErr, errReplacementBackupQuarantineVanished) || errors.Is(rmErr, errBackupQuarantineRestoreFailed) {
		return models.RestorePendingKindRearmRefused
	}
	return models.RestorePendingKindClean
}

// rearmReplacementBackup recreates the backup from the destination's bytes
// when a journal consumption fails AFTER the backup was removed, restoring
// the armed retry posture (callers keep info's permission bits and mtime).
// A refused no-replace publish (fsutil.PublishRefusal classes) leaves the
// name foreign-occupied or absent — wave-19 callers mark the entry
// rearm-refused restore-pending instead of leaving it armed against the
// unowned name. Wave-20 (codex P2): EVERY failure class is disarmed by the
// callers now — the kind comes from rearmPendingKind, where ONLY the
// definitely-published class (fsutil.PublishCompleted, shared with the
// downloader) resolves to the clean kind because the name provably carries
// this operation's own bytes.
// Wave-10 codex follow-up: the destination used to be copied via a plain
// fs.Open — an attacker swapping dest for a symlink in the removal→re-arm
// window got a protected file copied into the media-dir backup, armed for a
// later restore (privilege escalation). The open now runs through the same
// no-follow + regular-file + identity discipline as the restore source open
// (openRearmSource) and the copy streams from THAT handle.
// Wave-21 (codex P1, PR#215): copyRearmSourceBytes threads the WHOLE
// metadata application — mode at the O_EXCL staging create, times and the
// ownership seam on the staged name — INSIDE, strictly BEFORE the no-replace
// publish. The pre-wave-21 tail below chmod'ed/chtimes'ed/chown'ed the
// PUBLISHED backup path: in a directory writable by another user the name
// could be swapped for a symlink inside the publish→metadata window, and the
// path-based calls would follow the link to an arbitrary target. No
// post-publish metadata calls remain on this path.
func rearmReplacementBackup(fs afero.Fs, dest, backup string, info os.FileInfo) error {
	if filepath.Clean(dest) == filepath.Clean(backup) {
		return nil // CopyFileFs parity (wave-9): identical paths are a no-op
	}
	src, err := openRearmSource(fs, dest)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	// The backup spelling can arrive slash-normalized (journal legacy spellings,
	// sweep enumeration paths) while OS metadata calls need the native form:
	// afero MemMapFs indexes filepath.Clean'd names but its Chmod performs a RAW
	// lookup, so the slash spelling missed the just-renamed entry with
	// "chmod ...: file does not exist" on the Windows runner. The conversion is
	// restoreOSPath's single seam; dest stays verbatim for the no-follow seam
	// (its own calls normalize internally, and the w10 test pins the observed
	// opened-path string). See restoreOSPath for the POSIX no-op posture.
	osBackup := restoreOSPath(backup)
	return copyRearmSourceBytes(fs, src, osBackup, info)
}

// ReplacementSweeper reaps replacement backups under conservative ownership.
type ReplacementSweeper struct {
	fs              afero.Fs
	repo            database.BatchFileOperationRepositoryInterface
	pendingMu       sync.Mutex        // API-triggered sweeps share pendingRemovals; never hold across fs/repo calls.
	pendingRemovals map[string]string // backup key → restore-pending kind: cleanup authorized, routing on kind (wave-19)
}

// NewReplacementSweeper constructs a sweeper whose in-flight arbitration is
// durable and destination-specific via the `.dlbusy` marker.
func NewReplacementSweeper(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) *ReplacementSweeper {
	return &ReplacementSweeper{fs: fs, repo: repo, pendingRemovals: map[string]string{}}
}

// PruneOperationBackups removes replacement backups belonging to operation rows
// that are about to be pruned. The caller keeps those rows live until this hook
// succeeds, so a crash or failed cleanup leaves durable ledger ownership.
func (s *ReplacementSweeper) PruneOperationBackups(ctx context.Context, ops []models.BatchFileOperation) error {
	if len(ops) == 0 {
		return nil
	}
	if s == nil || s.fs == nil || s.repo == nil {
		return fmt.Errorf("prune operation backups: sweeper is not configured")
	}

	candidateIDs := make(map[uint]struct{}, len(ops))
	for _, op := range ops {
		candidateIDs[op.ID] = struct{}{}
	}
	var errs []error
	consumed := make(map[uint]map[string]struct{})
	markConsumed := func(opID uint, backup string) {
		if consumed[opID] == nil {
			consumed[opID] = make(map[string]struct{})
		}
		consumed[opID][backup] = struct{}{}
	}
pruneEntries:
	for i := range ops {
		gf, err := models.ParseGeneratedFiles(ops[i].GeneratedFiles)
		if err != nil {
			errs = append(errs, fmt.Errorf("operation %d ledger parse: %w", ops[i].ID, err))
			continue
		}
		for j := range gf.Replacements {
			entry := gf.Replacements[j]
			if strings.TrimSpace(entry.Backup) == "" || strings.TrimSpace(entry.Destination) == "" {
				errs = append(errs, fmt.Errorf("operation %d has an incomplete replacement entry", ops[i].ID))
				continue
			}
			if !entry.Installed {
				errs = append(errs, fmt.Errorf("operation %d backup %s: install is unconfirmed", ops[i].ID, entry.Backup))
				continue
			}
			if entry.PendingKind() == models.RestorePendingKindRearmRefused {
				errs = append(errs, fmt.Errorf("operation %d backup %s: ownership is rearm-refused", ops[i].ID, entry.Backup))
				continue
			}
			if err := ctx.Err(); err != nil {
				errs = append(errs, err)
				break pruneEntries
			}

			busyRelease, busyToken, busyErr := acquireReplacementBusyExFn(s.fs, entry.Destination)
			if busyErr != nil {
				errs = append(errs, fmt.Errorf("claim destination %s: %w", entry.Destination, busyErr))
				continue
			}
			if busyRelease == nil || busyToken == "" {
				if busyRelease != nil {
					busyRelease()
				}
				errs = append(errs, fmt.Errorf("claim destination %s: busy-marker provenance unavailable", entry.Destination))
				continue
			}
			claim, untrack := recordSweepBusyClaim(ctx, s.fs, entry.Destination, busyToken, busyRelease)
			rawRelease := fsutil.SharedDestLocks().Acquire(entry.Destination)
			if !claim.bindDestLock(rawRelease) {
				rawRelease()
				untrack()
				claim.releaseAdmit()
				busyRelease()
				errs = append(errs, fmt.Errorf("claim destination %s was revoked before cleanup", entry.Destination))
				continue
			}
			wasConsumed, pruneErr := s.pruneOperationBackup(ctx, ops[i].ID, entry, candidateIDs, claim)
			claim.releaseAdmit()
			claim.releaseDestLock()
			untrack()
			busyRelease()
			if wasConsumed {
				markConsumed(ops[i].ID, entry.Backup)
			}
			if pruneErr != nil {
				errs = append(errs, fmt.Errorf("operation %d backup %s: %w", ops[i].ID, entry.Backup, pruneErr))
			}
		}
	}
	retractCtx := ctx
	if retractCtx.Err() != nil {
		retractCtx = context.WithoutCancel(retractCtx)
	}
	if len(errs) == 0 {
		return s.retractConsumedEntries(retractCtx, consumed, candidateIDs)
	}
	if err := s.retractConsumedEntries(retractCtx, consumed, candidateIDs); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// retractConsumedEntries removes already-consumed backup entries from every
// candidate row, including another row that shared the same backup spelling.
// The journal RMW is durable and serialized with ordinary journal writers.
func (s *ReplacementSweeper) retractConsumedEntries(parentCtx context.Context, consumed map[uint]map[string]struct{}, candidateIDs map[uint]struct{}) error {
	if len(consumed) == 0 || len(candidateIDs) == 0 {
		return nil
	}
	want := make(map[string]struct{})
	for _, backups := range consumed {
		for backup := range backups {
			want[sweepSlash(backup)] = struct{}{}
		}
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()
	var errs []error
	for opID := range candidateIDs {
		release, lockErr := acquireJournalLockWithin(ctx, strconv.Itoa(int(opID)))
		if lockErr != nil {
			errs = append(errs, fmt.Errorf("retract consumed entries lock for operation %d: %w", opID, lockErr))
			continue
		}
		err := s.repo.UpdateJournalInTx(ctx, opID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
			if err != nil {
				return models.GeneratedFilesJSON{}, false, err
			}
			kept := gf.Replacements[:0]
			for _, entry := range gf.Replacements {
				if _, remove := want[sweepSlash(entry.Backup)]; !remove {
					kept = append(kept, entry)
				}
			}
			if len(kept) == len(gf.Replacements) {
				return gf, false, nil
			}
			gf.Replacements = kept
			return gf, true, nil
		})
		release()
		if errors.Is(err, database.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("retract consumed entries for operation %d: %w", opID, err))
		}
	}
	return errors.Join(errs...)
}

// acquireJournalLockWithin bounds the wait for the process-local journal lock.
// If the context expires, the eventual acquirer releases immediately without
// touching the row.
func acquireJournalLockWithin(ctx context.Context, key string) (func(), error) {
	result := make(chan func(), 1)
	go func() { result <- fsutil.SharedJournalLocks().Acquire(key) }()
	select {
	case release := <-result:
		return release, nil
	case <-ctx.Done():
		go func() { (<-result)() }()
		return nil, ctx.Err()
	}
}

// pruneOperationBackup rechecks all live ledger rows while the destination
// locks are held. Candidate rows are ignored because they are the rows this
// pre-delete cleanup is about to retire; any other row keeps the backup alive.
func (s *ReplacementSweeper) pruneOperationBackup(ctx context.Context, opID uint, entry models.ReplacementEntry, candidateIDs map[uint]struct{}, claim *sweepBusyMarkerClaim) (bool, error) {
	defer func() {
		if claim != nil {
			claim.releaseAdmit()
		}
	}()
	rows, err := s.repo.FindOperationsWithLedger(ctx)
	if err != nil {
		return false, fmt.Errorf("read live ledger: %w", err)
	}
	want := sweepSlash(entry.Backup)
	for i := range rows {
		if _, candidate := candidateIDs[rows[i].ID]; candidate {
			continue
		}
		gf, parseErr := models.ParseGeneratedFiles(rows[i].GeneratedFiles)
		if parseErr != nil {
			return false, fmt.Errorf("cannot prove backup %s is unreferenced by operation %d: %w", entry.Backup, rows[i].ID, parseErr)
		}
		for _, live := range gf.Replacements {
			if sweepSlash(live.Backup) == want {
				return false, nil
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if claim != nil && claim.abandonIfRevoked("prune backup quarantine", entry.Backup, entry.Destination) {
		return false, fsutil.ErrReplacementBusy
	}
	if err := s.markPruneCleanupPending(opID, entry); err != nil {
		return false, err
	}
	hold, err := quarantineReplacementBackupForPrune(s.fs, entry.Backup, "organized-job prune", &entry, nil)
	if err != nil {
		if hold != nil && hold.moved {
			return false, errors.Join(err, s.persistPruneQuarantine(opID, entry, hold.quarantine))
		}
		return false, err
	}
	if hold == nil || hold.unlinked {
		return false, fmt.Errorf("backup %s is absent during prune", entry.Backup)
	}
	if err := ctx.Err(); err != nil {
		return false, s.joinPruneRestoreFailure(opID, entry, hold, err)
	}
	if claim != nil && claim.abandonIfRevoked("prune backup unlink", entry.Backup, entry.Destination) {
		return false, errors.Join(fsutil.ErrReplacementBusy, s.persistPruneQuarantine(opID, entry, hold.quarantine))
	}
	if err := hold.removeVerified(); err != nil {
		if hold.moved {
			return false, errors.Join(err, s.persistPruneQuarantine(opID, entry, hold.quarantine))
		}
		return false, err
	}
	return true, nil
}

// joinPruneRestoreFailure retains a durable ledger pointer to the quarantine
// name when compensation cannot restore the original backup name.
func (s *ReplacementSweeper) joinPruneRestoreFailure(opID uint, entry models.ReplacementEntry, hold *replacementBackupQuarantine, cause error) error {
	if restoreErr := hold.restore(); restoreErr != nil {
		return errors.Join(cause, restoreErr, s.persistPruneQuarantine(opID, entry, hold.quarantine))
	}
	return cause
}

// markPruneCleanupPending durably records the two-phase cleanup intent
// before any backup bytes move. A crash leaves a retryable pending entry.
func (s *ReplacementSweeper) markPruneCleanupPending(opID uint, entry models.ReplacementEntry) error {
	ctx, cancel := context.WithTimeout(database.WithPruneMaintenance(context.Background()), 30*time.Second)
	defer cancel()
	return markReplacementEntryRestorePendingKind(ctx, s.repo, opID, sweepSlash(entry.Backup), models.RestorePendingKindPrune)
}

// persistPruneQuarantine moves the durable ledger pointer to the recoverable
// quarantine object and marks cleanup pending so a later retry never treats a
// missing original name as already consumed.
func (s *ReplacementSweeper) persistPruneQuarantine(opID uint, original models.ReplacementEntry, quarantine string) error {
	release := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(opID)))
	defer release()
	return s.persistPruneQuarantineLocked(opID, original, quarantine)
}

func (s *ReplacementSweeper) persistPruneQuarantineLocked(opID uint, original models.ReplacementEntry, quarantine string) error {
	ctx, cancel := context.WithTimeout(database.WithPruneMaintenance(context.Background()), 30*time.Second)
	defer cancel()
	err := s.repo.UpdateJournalInTx(ctx, opID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		for i := range gf.Replacements {
			if sweepSlash(gf.Replacements[i].Backup) != sweepSlash(original.Backup) || sweepSlash(gf.Replacements[i].Destination) != sweepSlash(original.Destination) {
				continue
			}
			gf.Replacements[i].Backup = quarantine
			gf.Replacements[i].SetRestorePending(models.RestorePendingKindPrune)
			return gf, true, nil
		}
		return gf, false, nil
	})
	return err
}

// rememberPendingRemoval records the in-process cleanup authorization with
// the legacy CLEAN kind (the backup name still holds the operation's own
// bytes — e.g. its removal just failed).
func (s *ReplacementSweeper) rememberPendingRemoval(backupKey string) {
	s.rememberPendingRemovalKind(backupKey, models.RestorePendingKindClean)
}

// rememberPendingRemovalKind is rememberPendingRemoval with a kind (wave-19):
// the compensation legs that watched a no-replace re-arm get refused record
// the rearm-refused kind so a fallback-authorized retry also stays off the
// unowned backup name. A remembered rearm-refused kind is never downgraded by
// a later clean-kind memory for the same key.
func (s *ReplacementSweeper) rememberPendingRemovalKind(backupKey, kind string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingRemovals == nil {
		s.pendingRemovals = map[string]string{}
	}
	if s.pendingRemovals[backupKey] == models.RestorePendingKindRearmRefused {
		return
	}
	s.pendingRemovals[backupKey] = kind
}

func (s *ReplacementSweeper) hasPendingRemoval(backupKey string) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	_, ok := s.pendingRemovals[backupKey]
	return ok
}

// pendingRemovalKind reports the in-process fallback's restore-pending kind
// for a key; the rearm-refused kind dominates a durable clean marker on
// routing (a refused re-arm in THIS process is fresher ownership evidence
// than the committed ledger's clean posture).
func (s *ReplacementSweeper) pendingRemovalKind(backupKey string) (string, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	kind, ok := s.pendingRemovals[backupKey]
	return kind, ok
}

func (s *ReplacementSweeper) forgetPendingRemoval(backupKey string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingRemovals != nil {
		delete(s.pendingRemovals, backupKey)
	}
}

// persistRestorePendingMarkerKind is persistRestorePendingMarker with an
// explicit pending kind (wave-19): refused re-arm compensation passes the
// rearm-refused kind so the durable marker itself keeps every later retry
// off the unowned backup name.
func (s *ReplacementSweeper) persistRestorePendingMarkerKind(ctx context.Context, rowID uint, backupSlash, kind string) error {
	return s.repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !markReplacementRestorePendingKind(&gf, backupSlash, kind) {
			return gf, false, nil
		}
		return gf, true, nil
	})
}

// replacementLedgerIndex maps journaled backup paths to their owning row for
// sweep arbitration, and destinations to the owning rows.
type replacementLedgerIndex struct {
	// journaled is the sweep-local MUTABLE view of LIVE ledger state:
	// consumption legs REMOVE each entry's (backup → row) mapping the moment
	// the journal transaction commits (sweepOne after restoreAndConsume;
	// Sweep's refusedPendings ledger leg), so a candidate marker file scanned
	// later in the SAME sweep is never routed through an owner whose entry
	// was consumed mid-sweep (wave-33, codex local review round 3, PR#215
	// finding R1). A mapping left behind would drive the consumed-entry
	// removal legs on stale evidence alone — an ownership decision only the
	// LIVE ledger may authorize (the orphan posture retains + warns instead).
	journaled       map[string]*models.BatchFileOperation // backup path → owning row
	dirs            map[string]bool                       // directories holding journaled destinations
	refusedPendings []rearmRefusedLedgerEntry
	prunePendings   []prunePendingLedgerEntry // wave-29: ledger-only rearm-refused pendings
}

// rearmRefusedLedgerEntry is one journaled restore-pending entry of the
// REARM-REFUSED kind as read from the ledger index (wave-29, codex P2,
// PR#215). Its backup name is UNOWNED — foreign-occupied or outright ABSENT
// after a refused no-replace re-arm — so it never materializes as a marker
// file in any directory scan. Only the recorded spellings are carried; the
// consumption leg re-verifies everything against the live journal.
type rearmRefusedLedgerEntry struct {
	rowID       uint
	backup      string // recorded spelling — never a path-operation target here
	dest        string // recorded spelling of the certified destination
	backupSlash string // probe-aware journal key
}

// prunePendingLedgerEntry is a durable retention intent that may need a
// ledger-only retry after the process crashed between backup quarantine and
// journal consumption. backup is the current filesystem path to clean;
// journalBackup is the spelling stored in the ledger.
type prunePendingLedgerEntry struct {
	rowID         uint
	backup        string
	journalBackup string
	dest          string
	backupSlash   string
}

func (s *ReplacementSweeper) index(ctx context.Context) (*replacementLedgerIndex, error) {
	rows, err := s.repo.FindOperationsWithReplacements(ctx)
	if err != nil {
		return nil, err
	}
	ledgerRows, lerr := s.repo.FindOperationsWithLedger(ctx)
	if lerr != nil {
		return nil, lerr
	}
	rows = append(rows, ledgerRows...)
	idx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{},
		dirs:      map[string]bool{},
	}
	// rows = FindOperationsWithReplacements + FindOperationsWithLedger — the
	// two queries overlap (a journaled row satisfies both), so the
	// ledger-enumerated pending leg dedupes per (row, backup) exactly like the
	// journaled map does.
	seenRefusedPendings := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
		if perr != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			backupSlash := sweepSlash(rep.Backup)
			idx.journaled[backupSlash] = row
			if rep.RestorePending && rep.PendingKind() == models.RestorePendingKindPrune {
				idx.prunePendings = append(idx.prunePendings, prunePendingLedgerEntry{
					rowID: row.ID, backup: rep.Backup, journalBackup: rep.Backup, dest: rep.Destination, backupSlash: backupSlash,
				})
			}
			// Dirs are FS ENUMERATION paths — keep their recorded case (the
			// probe-aware key is only a comparison form).
			idx.dirs[filepath.ToSlash(filepath.Clean(filepath.Dir(rep.Destination)))] = true
			// wave-29 (codex P2): enumerate rearm-refused pendings straight from
			// the LEDGER — see Sweep for the consumption leg. ONLY the
			// rearm-refused kind qualifies: clean-kind pendings still authorize a
			// backup-name removal and must be driven by the marker file the
			// directory scans find, never by this ledger-only leg.
			if rep.RestorePending && rep.PendingKind() == models.RestorePendingKindRearmRefused {
				key := strconv.Itoa(int(row.ID)) + "\x00" + sweepSlash(rep.Backup)
				if seenRefusedPendings[key] {
					continue
				}
				seenRefusedPendings[key] = true
				idx.refusedPendings = append(idx.refusedPendings, rearmRefusedLedgerEntry{
					rowID:       row.ID,
					backup:      rep.Backup,
					dest:        rep.Destination,
					backupSlash: sweepSlash(rep.Backup),
				})
			}
		}
		// R2-3: delete-listed paths name download destinations even when NO
		// replacement was (yet) journaled — the crash window between
		// backup-aside and RecordReplacement leaves the backup in exactly such
		// a directory.
		for _, delPath := range gf.Delete {
			idx.dirs[filepath.ToSlash(filepath.Clean(filepath.Dir(delPath)))] = true
		}
		// R3-3: Begin-seeded roots name the destination dir directly (a root
		// is a destination directory, not a file path).
		for _, root := range gf.Roots {
			idx.dirs[filepath.ToSlash(filepath.Clean(root))] = true
		}
	}
	return idx, nil
}

// consumePrunePending retries one durable retention intent under the same
// destination/busy-marker claim discipline as ordinary sweep legs. Prune
// cleanup is intentionally allowed to consume journal-only when its backup
// path is already absent: that absence means unlink completed before the
// process crashed, not that destination bytes were restored.
func (s *ReplacementSweeper) consumePrunePending(ctx context.Context, idx *replacementLedgerIndex, entry prunePendingLedgerEntry) int {
	busyRelease, busyToken, busyErr := acquireReplacementBusyExFn(s.fs, entry.dest)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		return 0
	}
	if busyErr != nil || busyToken == "" {
		if busyErr == nil {
			busyRelease()
		}
		return 0
	}
	defer busyRelease()
	claim, untrack := recordSweepBusyClaim(ctx, s.fs, entry.dest, busyToken, busyRelease)
	defer untrack()
	defer claim.releaseAdmit()
	rawDestRelease := fsutil.SharedDestLocks().Acquire(entry.dest)
	if !claim.bindDestLock(rawDestRelease) {
		rawDestRelease()
		claim.abandonIfRevoked("destination lock acquisition", entry.backup, entry.dest)
		return 0
	}
	defer claim.releaseDestLock()
	if s.retryPendingRemovalClaimed(database.WithPruneMaintenance(ctx), entry.rowID, entry.backup, entry.dest, entry.backupSlash, claim) {
		delete(idx.journaled, entry.backupSlash)
		return 1
	}
	return 0
}

// sweepPruneQuarantine routes a .dlq object back to the prune intent that
// owns its original .dlbak name. This closes the crash window between the
// verified quarantine move and persistence of the new ledger pointer.
func (s *ReplacementSweeper) sweepPruneQuarantine(ctx context.Context, idx *replacementLedgerIndex, dirSlash string, e os.FileInfo) int {
	quarantine := filepath.FromSlash(dirSlash + "/" + e.Name())
	original := originalReplacementBackupFromQuarantine(quarantine)
	journalBackup := original
	owner, ok := idx.journaled[sweepSlash(journalBackup)]
	if !ok {
		journalBackup = quarantine
		owner, ok = idx.journaled[sweepSlash(journalBackup)]
	}
	if !ok || journalEntryPendingKind(owner, sweepSlash(journalBackup)) != models.RestorePendingKindPrune {
		return 0
	}
	return s.consumePrunePending(ctx, idx, prunePendingLedgerEntry{
		rowID: owner.ID, backup: quarantine, journalBackup: journalBackup,
		dest: journalEntryDestination(owner, sweepSlash(journalBackup)), backupSlash: sweepSlash(journalBackup),
	})
}

func journalEntryDestination(row *models.BatchFileOperation, backupSlash string) string {
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return ""
	}
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			return rep.Destination
		}
	}
	return ""
}

// Sweep runs a full startup sweep: every directory that holds a journaled
// destination is scanned for ownership-marker backups.
//
// wave-29 (codex P2, PR#215): the sweep ALSO retries restore-pending entries
// of the REARM-REFUSED kind enumerated straight from the LEDGER (index()). An
// absent backup name never appears as a marker file in the directory scans,
// so these entries were invisible to every sweep — they stayed live
// indefinitely and blocked older replacement chains (checkedDestBlocking
// still sees the armed journal row). The ledger leg consumes them
// journal-only (no path operation against the unowned backup name) while the
// wave-19 contracts are upheld: the certified destination must still be
// present, and a clean-kind pending untouched by this leg keeps needing the
// marker file. Scoped/targeted sweeps (SweepDirs / SweepDestinations)
// deliberately keep their marker-file semantics and never run the ledger leg.
func (s *ReplacementSweeper) Sweep(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	// The ledger leg runs BEFORE the directory scans, and every entry it
	// consumes is retracted from idx.journaled on the spot (wave-33, finding
	// R1): a marker planted at a just-consumed name after its absent-proof
	// must arbitrate through the orphan leg against the LIVE ledger — never
	// through this index's stale owner copy.
	healed := 0
	for _, entry := range idx.refusedPendings {
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		healed += s.consumeRearmRefusedPending(ctx, idx, entry)
	}
	for dir := range idx.dirs {
		// Cancellation (e.g. the CLI revert's sweep deadline) wins over
		// progress: a hung root must not chain-delay the caller.
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		// R11-2: FLAT scans only — no recursion into media libraries. Crash
		// windows are covered exactly by the orchestrator's per-organize leaf
		// seed (R7-3), so startup cost stays O(ledgered dirs), independent of
		// library size.
		entries, rdErr := afero.ReadDir(s.fs, filepath.FromSlash(dir))
		if rdErr != nil {
			continue
		}
		// Wave-46 (codex P2): recheck cancellation the moment a stalled
		// ReadDir returns. The wave-8 bounded sweep abandons its goroutine
		// at the deadline: entries a post-deadline ReadDir surfaces must
		// NEVER feed sweepOne, whose busy-marker claims would collide with
		// the revert that already continued (ErrReplacementBusy).
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch {
			case IsReplacementBackupName(e.Name()):
				healed += s.sweepOne(ctx, idx, dir, e)
			case isReplacementBackupQuarantineName(e.Name()):
				healed += s.sweepPruneQuarantine(ctx, idx, dir, e)
			}
		}
	}
	// A prune intent whose original name is absent and whose quarantine move
	// was not visible to the directory scan still converges journal-only.
	for _, entry := range idx.prunePendings {
		if _, ok := idx.journaled[entry.backupSlash]; !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		healed += s.consumePrunePending(ctx, idx, entry)
	}
	return healed, nil
}

// SweepDirs runs a SCOPED sweep (codex P2 CLI bound): only the named
// directories are scanned for ownership-marker backups, while ownership
// arbitration still consults the FULL journal index (a scoped scan is about
// bounding filesystem work — a journaled backup found in scope is restored
// against its owner's ledger wherever that owner lives). The CLI revert uses
// this to heal exactly the roots the target batch can touch rather than every
// journaled root, so a hung network share unrelated to the revert can never
// block it. Duplicate and unreadable directories collapse harmlessly.
func (s *ReplacementSweeper) SweepDirs(ctx context.Context, dirs []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	healed := 0
	seen := map[string]bool{}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		cleaned := filepath.ToSlash(filepath.Clean(dir))
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		// As in Sweep: FLAT scans only, unreadable dirs are skipped.
		entries, rdErr := afero.ReadDir(s.fs, filepath.FromSlash(cleaned))
		if rdErr != nil {
			continue
		}
		// Wave-46 (codex P2): same post-ReadDir cancellation gate as Sweep —
		// an abandoned post-deadline scan processes NOTHING from this dir.
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		for _, e := range entries {
			if e.IsDir() || !IsReplacementBackupName(e.Name()) {
				continue
			}
			healed += s.sweepOne(ctx, idx, cleaned, e)
		}
	}
	return healed, nil
}

// SweepDestinations runs the targeted pre-revert sweep over the destinations
// an operation's journal names: crash-window restores complete BEFORE the
// revert's rejection/restore checks evaluate the destination state.
//
// Requested destinations are grouped by directory up front: when several
// destinations share one folder (cover.jpg + poster.jpg), the folder is
// scanned once and every candidate is tested against the WHOLE group —
// otherwise only the first destination's backups would arbitrate and a
// crash-window backup for a later destination would leak past the revert's
// destination-conflict checks (codex P2 review 4960491781). The group key is
// the probe-aware destination key (sweepSlash), so two case-folded spellings
// of one insensitive folder share a single scan; the enumeration path keeps
// the first-seen recorded case for afero.ReadDir.
func (s *ReplacementSweeper) SweepDestinations(ctx context.Context, destinations []string) (int, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	type destGroup struct {
		enumDir  string   // first-seen enumeration spelling (recorded case kept for ReadDir)
		destKeys []string // probe-aware keys of every requested destination in the folder
	}
	groups := map[string]*destGroup{}
	var order []string // first-appearance order of each directory group (matches the pre-grouping scan order)
	for _, dest := range destinations {
		dir := filepath.ToSlash(filepath.Clean(filepath.Dir(dest)))
		groupKey := sweepSlash(dir)
		g, ok := groups[groupKey]
		if !ok {
			g = &destGroup{enumDir: dir}
			groups[groupKey] = g
			order = append(order, groupKey)
		}
		g.destKeys = append(g.destKeys, sweepSlash(dest))
	}
	healed := 0
	for _, groupKey := range order {
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		g := groups[groupKey]
		entries, rdErr := afero.ReadDir(s.fs, filepath.FromSlash(g.enumDir))
		if rdErr != nil {
			continue
		}
		// Wave-46 (codex P2): this targeted scan feeds the reverter's wave-8
		// goroutine discipline directly — a ReadDir answering past the
		// deadline is already abandoned, so recheck before any arbitration.
		if err := ctx.Err(); err != nil {
			return healed, err
		}
		for _, e := range entries {
			if e.IsDir() || !IsReplacementBackupName(e.Name()) {
				continue
			}
			// Targeted sweep only arbitrates backups of the named destinations;
			// test the candidate against EVERY destination in the group.
			// Wave-17 (codex P2): derive the candidate's destination by
			// STRIPPING the validated `.dlbak.<16hex>` ownership marker (the
			// same derivation sweepOne/journal spelling comparisons use) and
			// require EXACT equality with a requested destination. The
			// pre-wave-17 prefix test admitted sibling-name decoys — e.g.
			// 'cover.jpg.old.dlbak.<hex>' prefixed a targeted 'cover.jpg', so
			// the sweep would arbitrate (and possibly restore/delete) a backup
			// belonging to a destination the caller never named. Both sides are
			// compared under the probe-aware sweepSlash key, so separator and
			// case normalization stay exactly DestKey-consistent.
			candidateDest := sweepSlash(filepath.Join(g.enumDir,
				strings.TrimSuffix(e.Name(), replacementBackupName.FindString(e.Name()))))
			matched := false
			for _, destKey := range g.destKeys {
				if candidateDest == destKey {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			healed += s.sweepOne(ctx, idx, g.enumDir, e)
		}
	}
	return healed, nil
}

// sweepOne arbitrates one ownership-marker backup file.
func (s *ReplacementSweeper) sweepOne(ctx context.Context, idx *replacementLedgerIndex, dirSlash string, e os.FileInfo) int {
	// Wave-46 (codex P2): an abandoned post-deadline sweep is a STRICT
	// no-op. Without this gate a sweep whose ctx died mid-scan (the wave-8
	// bounded goroutine outliving its budget) would claim the
	// destination's .dlbusy marker — and journal ops — for a restore
	// nobody waits on, colliding with the revert that already continued at
	// the deadline (ErrReplacementBusy). The gate precedes EVERY busy
	// claim and journal operation below.
	if err := ctx.Err(); err != nil {
		return 0
	}
	backup := filepath.FromSlash(dirSlash + "/" + e.Name())
	// Journal comparisons run under the probe-aware key (separator normalization
	// plus conditional case folding); actual fs paths keep their recorded case.
	backupKey := sweepSlash(backup)

	dest := strings.TrimSuffix(backup, replacementBackupName.FindString(e.Name()))

	// The marker is an on-disk cross-process exclusion. Acquire it before
	// reading or changing ownership state: the downloader creates the same
	// marker before moving the destination aside, so a live API install cannot
	// be mistaken for a stale crash window by this process (or at startup).
	busyRelease, busyToken, busyErr := acquireReplacementBusyExFn(s.fs, dest)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		return 0
	}
	if busyErr != nil {
		logging.Warnf("replacement sweep %s: busy-marker arbitration failed (%v) — kept", backup, busyErr)
		return 0
	}
	if busyToken == "" {
		// Wave-56 (finding F2): provenance unavailable — refuse to record the
		// claim (treat as a failed acquire on the sweep side).
		busyRelease()
		logging.Warnf("replacement sweep %s: busy-marker token provenance unavailable — kept", backup)
		return 0
	}
	defer busyRelease()
	// Wave-52 (codex local review round 7, PR#215 finding F2): journal the
	// claim in the in-process, ctx-scoped ledger at BUSY-MARKER ACQUIRE TIME —
	// BEFORE the blocking destination-lock wait below. The wave-50 order
	// (dest lock first, claim record second) left a ctx expiring DURING the
	// wait with an OWNED marker no ledger record named: the continued
	// revert's pre-acquisition reclaim consult found nothing, and the
	// destination kept its busy refusal for the whole stranding. The record
	// is born binding the marker release and carrying a pending cell for the
	// dest-lock release (bindDestLock fills it below), so the wave-49 ledger
	// sees the incoming claim for the ENTIRE wait and no held lock is ever
	// untracked. The untrack runs before the marker release (LIFO), and a
	// reclaim by the continued revert consumes the once-guarded releases,
	// making this goroutine's deferred releases no-ops (never a double-free
	// of a successor marker or lock).
	claim, untrackSweepClaim := recordSweepBusyClaim(ctx, s.fs, dest, busyToken, busyRelease)
	defer untrackSweepClaim()
	defer claim.releaseAdmit() // wave-56 (finding F1): release the last admit gate at return
	rawDestRelease := fsutil.SharedDestLocks().Acquire(dest)
	if !claim.bindDestLock(rawDestRelease) {
		// The claim was reclaimed DURING the dest-lock wait (the reclaim ran
		// against the empty cell): this wait then completing means the
		// just-acquired lock belongs to this goroutine alone — the
		// once-guard never became visible to the reclaim — so the release is
		// this goroutine's direct responsibility. The revocation flag is
		// already set (the reclaim revokes before it releases); log through
		// the gate seam and abandon before any further classification read or
		// mutation — the wave-51 stage gates below would stop the first
		// mutation anyway.
		rawDestRelease()
		claim.abandonIfRevoked("destination lock acquisition", backup, dest)
		return 0
	}
	defer claim.releaseDestLock()

	if owner, ok := idx.journaled[backupKey]; ok {
		// Journaled — handled inside restoreAndConsume, which RECHECKS the
		// destination state in the critical section: a downloader can install
		// new bytes between the classification read and the lock (codex P3
		// R3-2), and the backup must never clobber those freshly-installed
		// artifacts. The dest lock is already held (bound to claim above), so
		// restoreAndConsume must NOT re-acquire it — SharedDestLocks mutexes
		// are not reentrant. The idx mapping is the sweep-local LIVE view
		// (wave-33, finding R1): a successful consumption retracts it here
		// exactly like the ledger leg retracts its own — every later candidate
		// consults only state still present in the ledger.
		if s.restoreAndConsume(ctx, owner, backup, dest, backupKey, claim) {
			delete(idx.journaled, backupKey)
			return 1
		}
		return 0
	}

	// Orphan: no row journals this backup anymore. CLASSIFY FRESH INSIDE THE
	// LOCK (codex P3 R4-1): the index snapshot may predate the downloader's
	// RecordReplacement — deleting a just-journaled backup because the
	// destination exists would leave that row permanently unrevertable. (The
	// dest lock is the one bound to the claim above; released by the deferred
	// claim release on return.)
	fresh, fErr := s.repo.FindOperationsByDestination(ctx, dest)
	if fErr != nil {
		// R7-2/R4-1: an unreadable ownership answer is NEVER absence — keep
		// the backup; the next sweep retries with a live view.
		return 0
	}
	for i := range fresh {
		gf, pErr := models.ParseGeneratedFiles(fresh[i].GeneratedFiles)
		if pErr != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			if sweepSlash(rep.Backup) == backupKey {
				return 0 // freshly journaled — keep; next sweep arbitrates it as journaled
			}
		}
	}
	// codex P2: same Lstat-first classification as restoreAndConsume — an
	// unjournaled marker-shaped file is the more dangerous case for the
	// Stat-misclassification: Stat on a dangling symlink reports ENOENT, and
	// copyRestoreBytes would then REPLACE THE LINK OBJECT, destroying a
	// directory entry no journal rows describe. Lstat success (any mode) means
	// the destination is present and the conservative retain leg applies.
	_, lstatErr := lstatRestoreSource(s.fs, dest)
	if errors.Is(lstatErr, afero.ErrFileNotFound) {
		// Wave-51 (codex P1, PR#215) — epoch-ownership gate at the orphan
		// restore publish: a claim reclaimed while this worker was parked in
		// the orphan classification reads above must not publish now — the
		// continued revert owns the destination's arbitration. Abandoning
		// leaves dest absent and the unjournaled marker file byte-intact (the
		// next live sweep re-arbitrates it from scratch).
		if claim.abandonIfRevoked("orphan destination publish", backup, dest) {
			return 0
		}
		// Wave-16 (codex P2): the destination was proven ABSENT, so the
		// restore publishes no-replace — a foreign writer claiming the name
		// mid-window collides into the kept leg below (typed
		// fsutil.ErrPublishCollision): racer bytes intact, backup retained,
		// nothing journaled or removed.
		if rnErr := copyRestoreBytesNoReplace(s.fs, backup, dest); rnErr != nil {
			logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
			return 0
		}
		// The restore repairs the missing destination, but marker shape alone is
		// not ownership proof. Retain the source for inspection rather than
		// deleting a possible user-owned file; the user can remove it manually.
		warnRetainedUnjournaledBackup(backup)
		return 1
	}
	if lstatErr != nil {
		// R8-1: indeterminate destination state (permission/IO) must NEVER
		// read as "present" — the unjournaled backup may be the ONLY copy of
		// the pre-replace bytes. Touch nothing; retry next sweep.
		logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, lstatErr)
		return 0
	}
	// Marker shape alone is not ownership proof. Retain this unjournaled file
	// for inspection rather than permanently deleting user-owned data; the
	// conservative restore posture leaves manual deletion to the user.
	warnRetainedUnjournaledBackup(backup)
	return 0
}

// restoreAndConsume moves a journaled crash-window backup back onto its
// destination under the destination lock. Backup removal is completed before
// the journal entry is consumed; a failed removal leaves ownership armed.
//
// Wave-50 (codex P2, PR#215 finding F1): production callers (sweepOne) hand
// in the ctx-scoped claim whose record already BINDS this destination's lock
// — the lock is held across this call and released through the claim's
// once-guarded release. A nil claim is the direct-caller (test/legacy)
// posture: the lock is self-acquired and self-released here.
func (s *ReplacementSweeper) restoreAndConsume(ctx context.Context, row *models.BatchFileOperation, backup, dest, backupSlash string, claim *sweepBusyMarkerClaim) bool {
	if claim == nil {
		release := fsutil.SharedDestLocks().Acquire(dest)
		defer release()
	}
	defer claim.releaseAdmit() // wave-56 (finding F1): release the last admit gate at return

	// A prior restore can leave the destination present when backup cleanup
	// failed. Only the explicit pending marker (or the same-process fallback)
	// authorizes cleanup here; an ordinary armed/installed row remains retained.
	//
	// codex P2: classify with Lstat, NOT Stat — a DANGLING SYMLINK at dest
	// reads as ENOENT under Stat (the target is gone), so the restore below
	// would then rename over the link object itself and destroy an unjournaled
	// directory entry. Any Lstat-success object — symlink included — is PRESENT
	// and follows the present-dest paths; only a genuine Lstat-ENOENT may flow
	// into the absent-dest restore; other Lstat errors stay indeterminate (kept).
	if _, lstatErr := lstatRestoreSource(s.fs, dest); lstatErr == nil {
		if !s.hasPendingRemoval(backupSlash) && !journalEntryRestorePending(row, backupSlash) {
			return false
		}
		// Wave-33 (codex local review round 3, PR#215 finding R1): when the
		// cleanup authorization comes from the INDEX-TIME row copy alone (no
		// in-process memory from this process's own failed removal), re-verify
		// the entry STILL exists restore-pending in the live ledger before any
		// pending-retry removal may run — the snapshot may predate a mid-sweep
		// or concurrent consumption, and the retry's consumed-entry legs would
		// otherwise remove a byte-string ownership no longer authorizes.
		// A consumed (or otherwise de-pended) entry makes the occupant's
		// ownership unproven: ORPHAN posture — retain the bytes and warn,
		// exactly like a never-journaled marker.
		if !s.hasPendingRemoval(backupSlash) {
			freshOwner, ferErr := s.repo.FindByID(ctx, row.ID)
			switch {
			case ferErr != nil || freshOwner == nil:
				logging.Warnf("replacement sweep %s: owner row unreadable before cleanup authorization (%v) — kept", backup, ferErr)
				return false
			case !journalEntryRestorePending(freshOwner, backupSlash):
				warnRetainedUnjournaledBackup(backup)
				return false
			}
		}
		retryCtx := ctx
		if journalEntryPendingKind(row, backupSlash) == models.RestorePendingKindPrune {
			retryCtx = database.WithPruneMaintenance(ctx)
		}
		return s.retryPendingRemovalClaimed(retryCtx, row.ID, backup, dest, backupSlash, claim)
	} else if !errors.Is(lstatErr, afero.ErrFileNotFound) {
		logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, lstatErr)
		return false
	}

	// R11-1: re-read the journal FRESH under the lock — the index snapshot
	// may predate the install-confirm; an armed flag read stale would
	// misclassify a deleted-afterwards destination as an interrupted install.
	freshRow, frErr := s.repo.FindByID(ctx, row.ID)
	if frErr != nil || freshRow == nil {
		logging.Warnf("replacement sweep %s: owner row unreadable (%v) — kept", backup, frErr)
		return false
	}
	if freshRow.RevertStatus == models.RevertStatusReverted {
		return false // consumed meanwhile
	}
	if journalEntryInstalled(freshRow, backupSlash) {
		logging.Infof("replacement sweep %s: destination missing but install was confirmed — backup retained, no auto-restore", backup)
		return false
	}
	// Wave-19 (codex P2): a rearm-refused pending entry may NEVER be restored
	// FROM its backup name — the name is foreign-occupied or absent (a refused
	// no-replace re-arm left it unowned), and the restored destination the
	// marker certified is now gone. Copying the path could install foreign
	// bytes; consuming the entry would erase the only record that the restore
	// happened. Retain both untouched for manual recovery.
	if journalEntryPendingKind(freshRow, backupSlash) == models.RestorePendingKindRearmRefused {
		logging.Warnf("replacement sweep %s: destination missing but the journal entry is rearm-refused restore-pending — restored bytes are gone and the backup name is unowned; entry and name retained untouched", backup)
		return false
	}
	// Wave-16 (codex P2): the destination was proven ABSENT above, so the
	// restore publishes no-replace — a foreign writer claiming the name
	// mid-window collides into this kept/warn leg (typed
	// fsutil.ErrPublishCollision) with the racer's bytes intact; on collision
	// the backup is retained and the journal entry is NOT consumed (the
	// removal and consumption below never run).
	// Wave-25 (codex P3 PR#215 finding 2): capture the restore SOURCE's
	// identity before the copy so the backup removals below can refuse any
	// object that no longer matches what this leg actually restored.
	backupInfoBeforeCopy, _ := lstatRestoreSource(s.fs, backup)
	// Wave-51 (codex P1, PR#215) — epoch-ownership gate at the destination
	// publish: a worker RESUMING from a wedged fs read above (the fs call
	// answered only after the wave-8 deadline proceeded with the revert and
	// the reclaim flipped this claim's revocation flag) must not publish now
	// — the continued revert already owns this destination's arbitration, and
	// a publish here would fork a second restore sequence against the same
	// backup. Abandoning leaves every classification pre-mutation: dest stays
	// absent, the backup stays at its journaled name, and the journal entry
	// keeps its armed classification for the reclaiming revert (or the next
	// live sweep) to heal.
	if claim.abandonIfRevoked("destination publish", backup, dest) {
		return false
	}
	restoredID, rnErr := copyRestoreBytesNoReplaceIdentityFacts(s.fs, backup, dest, journaledEntryFacts(freshRow, backupSlash))
	if rnErr != nil {
		logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
		return false
	}
	// Wave-31 (codex local round 1, PR#215 finding L1): before ANY backup
	// removal or journal consumption below (the already-consumed shortcut and
	// the armed consumption alike), the destination must STILL name the exact
	// object this leg just published. A foreign writer swapping or deleting
	// dest in the publish→remove window no longer gets the backup (the sole
	// remaining copy of the pre-replacement bytes) unlinked nor the entry
	// consumed: both stay retained, the destination is left untouched, and
	// the entry stays ARMED — deliberately NOT marked restore-pending, whose
	// marker would certify dest carries restored bytes (unproven now) and
	// drive a backup removal + consumption without a restore.
	if !restoredDestStillOurs(s.fs, dest, restoredID) {
		logging.Warnf("replacement sweep %s: restored destination %s no longer names the published restore object (foreign swap or deletion in the restore window) — backup retained, journal entry left armed, destination untouched", backup, dest)
		return false
	}

	// R15-1 + review 4960250562: the journal section runs entirely through
	// UpdateJournalInTx — each round is a BEGIN IMMEDIATE transaction whose
	// in-transaction re-read merges against the latest committed ledger, so an
	// apply in ANOTHER process arming a DIFFERENT destination of this row can
	// neither clobber this consumption nor be resurrected by it. The marker
	// shape of the classification below (armed entry present vs already
	// consumed) tracks the state fn observed, never an index-time snapshot.
	// Lock order dest→journal matches the reverter's consumption.
	// Wave-34 (codex local review round 4, PR#215 finding F1): the undo unlink
	// is IDENTITY-BOUND to the object this leg published — a pathname remove
	// after the wave-31/wave-32 re-gates succeeded would delete a foreign
	// occupant swapped onto dest inside the gate→undo window. The seam
	// re-derives the no-follow identity and requires SameFile; on divergence
	// (or an indeterminate verdict) the destination AND the backup stay
	// retained and the journal entry stays live, exactly like the publish-time
	// refusal legs.
	// Wave-35 (codex local review round 5, PR#215): the unlink itself runs
	// through the destination quarantine (restore_dest_quarantine_w35) — the
	// seam verdict's check→Remove window closes: the verified object moves
	// aside under an O_EXCL-reserved sibling name, is re-proven, and only
	// the quarantine name is unlinked; wedge legs compensate NO-REPLACE so a
	// racer's occupant at dest is never clobbered or deleted.
	undoRestore := func() {
		if !restoredDestStillOurs(s.fs, dest, restoredID) {
			logging.Warnf("replacement sweep %s: restore undo REFUSED — restored destination %s no longer names the published restore object (foreign swap or deletion in the undo window); destination and backup retained, journal entry left live", backup, dest)
			return
		}
		if rmErr := removeRestoredDestQuarantined(s.fs, dest, "replacement sweep", restoredID); rmErr != nil {
			logging.Warnf("replacement sweep %s: restore undo failed: %v", backup, rmErr)
		}
	}
	// Wave-51 (codex P1, PR#215) — epoch-ownership gate at the ENTRY of the
	// backup-removal + journal-consumption unit: a claim reclaimed while this
	// worker was parked in the publish or verification legs above means the
	// continued revert owns the arbitration now, so the quarantine move, the
	// verified unlink, the entry-presence transaction, the consumption, and
	// every pending-persist compensation below must not even START.
	// Abandoning here leaves a coherent armed shape: the restored bytes may
	// already stand at dest (a complete publish), the backup stays at its
	// journaled name, and the journal entry keeps its pre-mutation
	// classification — never clobbered with partially-mutated data.
	// Wave-52 (codex local review round 7, PR#215 finding F1 — "the entry
	// gate is not the unit fence"): a claim revoked AFTER passing this gate
	// let the unit run on: its consume/persist legs then fail under the dead
	// ctx and the compensations (re-arm publishes, undo unlinks, marker
	// persists) moved or destroyed names against arbitration the continued
	// revert already owns — and the row surfaced armed against whatever shape
	// that interlude left. The unit is now fenced PER STAGE: every mutation
	// stage below — (b) the quarantine claim/move/unlink arms on either
	// classification leg, (c) each pending persist, (d) the consumption
	// update, (e) each compensation leg that destroys or moves names —
	// consults the claim's revocation flag IMMEDIATELY BEFORE its first
	// mutation and abandons with zero further mutation. The entry
	// classification stays exactly where the last completed stage left it, and
	// a stage whose syscall sequence already began completes through the
	// audited wave-26/32/34/35/36 identity bindings (the next stage's gate is
	// what stops the unit — the in-stage syscalls are never reopened). Two
	// resting shapes matter:
	//   - publish NOT yet consumed but the backup unlinked (stages d/e below):
	//     the wave-19 unlandable-consume contract — the entry restages to the
	//     rearm-refused restore-pending kind (destination certified, backup
	//     name unowned), NEVER a re-arm publish or a restore undo, so the
	//     reclaiming revert consumes it journal-only;
	//   - every earlier stage: a bare stop, classification untouched.
	// The reverter's reclaim→arbitrate→fresh-read→mutate ordering (documented
	// at restoreReplacementJournal) resolves the commit-vs-restore race into
	// the completed-consume posture on top.
	if claim.abandonIfRevoked("backup removal and journal consumption", backup, dest) {
		return false
	}
	jrel := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(row.ID)))
	defer jrel()

	entryPresent := false
	pErr := s.repo.UpdateJournalInTx(ctx, row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		for _, rep := range gf.Replacements {
			if sweepSlash(rep.Backup) == backupSlash {
				entryPresent = true
				break
			}
		}
		return gf, false, nil
	})
	if pErr != nil {
		// Owner row unreadable (or journal unparseable) inside the
		// transaction — the restore is undone exactly as the pre-transaction
		// live-row read failure did.
		// Wave-52 (round 7, finding F1, stage e): the undo UNLINKS the
		// published destination — a name-destroying compensation a reclaimed
		// claim must never run. The revoked stop leaves the completed publish
		// exactly where it landed (destination present with the restored
		// bytes, backup at its journaled name, entry armed) for the
		// reclaiming arbitration to converge.
		if claim.abandonIfRevoked("journal-read failure compensation (restore undo)", backup, dest) {
			return false
		}
		undoRestore()
		return false
	}
	if !entryPresent {
		// Already consumed (e.g. a reverter raced us) — the backup is no longer
		// journal-owned, so remove it before reporting the restore complete.
		// No journal entry survives to stamp facts: the removal binds solely to
		// the object this leg restored from (wave-25).
		//
		// Wave-32 (codex local review round 2, PR#215 finding R1): the wave-31
		// destination check above ran BEFORE the removal; a foreign swap or
		// deletion landing between the two used to get the recoverable bytes
		// unlinked with consumption already done elsewhere. The removal
		// therefore runs through the split quarantine: the verified backup
		// object moves aside, the destination is re-proven to STILL name the
		// object this leg published, and only then is the quarantined object
		// unlinked. On divergence the object moves back onto the journaled
		// name and nothing is removed.
		// Wave-52 (round 7, finding F1, stage b): the quarantine
		// claim/move/unlink arms are ONE gated stage — a reclaimed claim stops
		// BEFORE the verified object moves: the completed publish stands, the
		// backup keeps its journaled name, the already-consumed journal is
		// untouched, and the next live arbitration re-classifies the leftover
		// name as an ordinary orphan.
		if claim.abandonIfRevoked("consumed-entry backup quarantine removal", backup, dest) {
			return false
		}
		hold, rmErr := quarantineReplacementBackupForRemoval(s.fs, backup, "replacement sweep", nil, backupInfoBeforeCopy)
		if rmErr == nil && !restoredDestStillOurs(s.fs, dest, restoredID) {
			if rerr := hold.restore(); rerr != nil {
				// Wave-36 (codex local review round 6, PR#215 finding F3): the
				// move-back failed — the backup name is unowned (a foreign claimant
				// holds it or the publish wedged) while the verified bytes stay
				// recoverable at the quarantine name. No journal entry survives to
				// mark on this leg (consumed elsewhere), so the failure only surfaces:
				// the foreign occupant and the quarantined bytes both stay intact.
				logging.Warnf("replacement sweep %s: restored destination %s diverged after the backup was quarantined AND the verified move-back failed (%v) — the unowned backup name keeps its occupant, verified bytes recoverable at the quarantine name %s; nothing consumed, nothing removed", backup, dest, rerr, hold.quarantine)
				return false
			}
			logging.Warnf("replacement sweep %s: restored destination %s diverged after the backup was quarantined (foreign swap or deletion inside the check-to-delete window) — backup restored to its journaled name, destination untouched", backup, dest)
			return false
		}
		if rmErr == nil {
			rmErr = hold.removeVerified()
		}
		if rmErr != nil {
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}

	// Removal is the ownership boundary. Keep the row armed when it fails and
	// persist a distinct marker so a later sweep can clean a present destination
	// without mistaking an ordinary armed apply for a completed restore.
	// Capture the backup metadata without following a swapped-in symlink so a
	// failed consume can re-arm only the original object metadata. Wave-25:
	// the removal gate verifies those bindings — the journaled arm-time facts
	// (index-time snapshot; facts are written once) AND the object this leg
	// restored from — before unlinking, so a foreign occupant swapped onto
	// the backup name after our copy is retained instead of deleted.
	backupInfo, _ := lstatRestoreSource(s.fs, backup)
	// Wave-32 (codex local review round 2, PR#215 finding R1): same split as
	// the already-consumed leg above — quarantine the verified backup object,
	// re-prove the destination STILL names the published restore object, and
	// only then unlink the quarantined name. A divergence between the wave-31
	// check and the unlink can no longer destroy the recoverable bytes with
	// consumption pending: the object moves back onto the journaled name and
	// the entry stays ARMED — deliberately NOT marked restore-pending, whose
	// marker certifies destination bytes that are no longer provably in
	// place.
	// Wave-52 (round 7, finding F1, stage b): the armed entry's quarantine
	// arms — reclaimed ⇒ the publish stage's result stands (destination
	// carries the restored bytes), the backup never leaves its journaled
	// name, and the entry keeps its armed classification for the reclaiming
	// arbitration to re-run this unit from scratch.
	if claim.abandonIfRevoked("armed-entry backup quarantine removal", backup, dest) {
		return false
	}
	hold, rmErr := quarantineReplacementBackupForRemoval(s.fs, backup, "replacement sweep", journaledEntryFacts(row, backupSlash), backupInfoBeforeCopy)
	if rmErr == nil && !restoredDestStillOurs(s.fs, dest, restoredID) {
		if rerr := hold.restore(); rerr != nil {
			// Wave-36 (codex local review round 6, PR#215 finding F3): the
			// move-back failed — the journaled name is unowned (a foreign
			// claimant holds it or the publish wedged) while the verified bytes
			// sit at the quarantine name. The entry must NOT stay armed against
			// that name (a later leg would restore from or remove the foreign
			// occupant): persist the rearm-refused restore-pending kind — the
			// marker still certifies the destination carries the restored bytes
			// and every retry runs journal-only. The quarantined bytes stay
			// retained for manual recovery in every persist outcome.
			// Wave-52 (round 7, finding F1, stage c): the pending-persist
			// classification update is revocation-fenced — a reclaimed claim
			// stops bare: the entry KEEPS its armed classification (the wave-36
			// persist-failure posture), the foreign occupant at the journaled
			// name stays byte-intact, and the verified bytes stay recoverable at
			// the quarantine name.
			if claim.abandonIfRevoked("divergence recovery restore-pending persist", backup, dest) {
				return false
			}
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
			if markErr := s.persistRestorePendingMarkerKind(ctx, row.ID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
				logging.Warnf("replacement sweep %s: restored destination %s diverged after the backup was quarantined, the verified move-back failed (%v), AND the rearm-refused marker could not be persisted (%v) — entry left armed, foreign name untouched, verified bytes recoverable at %s", backup, dest, rerr, markErr, hold.quarantine)
			} else {
				logging.Warnf("replacement sweep %s: restored destination %s diverged after the backup was quarantined and the verified move-back failed (%v) — entry marked restore-pending (rearm-refused), foreign name untouched, verified bytes recoverable at %s", backup, dest, rerr, hold.quarantine)
			}
			return false
		}
		logging.Warnf("replacement sweep %s: restored destination %s diverged after the backup was quarantined (foreign swap or deletion inside the check-to-delete window) — backup restored to its journaled name, journal entry left armed, destination untouched", backup, dest)
		return false
	}
	if rmErr == nil {
		rmErr = hold.removeVerified()
	}
	if rmErr != nil {
		// Wave-52 (round 7, finding F1, stages c+e): the failed removal's
		// pending persist — and its persist-failure undo compensation — are
		// fenced together: a reclaimed claim stops bare with the entry's armed
		// classification untouched and the fs exactly as the removal attempt
		// left it (destination published, backup name in the post-attempt
		// shape).
		if claim.abandonIfRevoked("removal-failure pending persist", backup, dest) {
			return false
		}
		// Wave-32 (finding R4): the vanished class marks the rearm-refused
		// kind (journal-only retry — the name is absent by construction);
		// every other class keeps the clean marker for the file-driven retry.
		persistKind := pendingKindForRemovalError(rmErr)
		s.rememberPendingRemovalKind(backupSlash, persistKind)
		markErr := s.persistRestorePendingMarkerKind(ctx, row.ID, backupSlash, persistKind)
		if markErr != nil {
			absoluteBackup, _ := filepath.Abs(backup)
			logging.Warnf("replacement sweep failed to retain cleanup marker for backup %s: %v", absoluteBackup, markErr)
			// Without a durable marker, do not leave the restored destination
			// behind: restore the armed, backup-present retry state instead.
			s.forgetPendingRemoval(backupSlash)
			undoRestore()
		}
		return false
	}

	// Wave-52 (round 7, finding F1, stage d): the consumption update is the
	// unit's last mutation stage. A claim revoked here — the restore ALREADY
	// published and verified, the backup ALREADY unlinked — is never
	// compensated backward (no re-arm publish, no restore undo): the wave-19
	// unlandable-consume contract supplies the RESTING classification
	// instead — the rearm-refused restore-pending marker (best-effort
	// durable + in-process fallback): the destination is certified carrying
	// the restored bytes and the backup name is unowned (absent), so the
	// reclaiming revert consumes the entry journal-only.
	if claim.abandonIfRevoked("journal consumption", backup, dest) {
		s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
		if markErr := s.persistRestorePendingMarkerKind(ctx, row.ID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
			logging.Warnf("replacement sweep %s: claim reclaimed after the backup removal — the consumption was skipped AND the rearm-refused restore-pending restage could not be persisted (%v) — restored destination retained, absent backup name untouched", backup, markErr)
		}
		return false
	}
	uErr := s.repo.UpdateJournalInTx(ctx, row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return consumeSweepJournalEntry(current, backupSlash)
	})
	if uErr != nil {
		// Wave-52 (round 7, finding F1, stage e): the consumption-failure
		// compensation (re-arm publish → undo unlink) moves or destroys names
		// a reclaimed claim owns no longer — both legs are fenced. The resting
		// classification is the same wave-19 unlandable-consume contract: the
		// rearm-refused restore-pending restage (the re-arm's refusal outcome
		// WITHOUT the re-arm itself — no publish ever ran here, so the absent
		// name is unowned), destination retained, backup path untouched.
		if claim.abandonIfRevoked("consumption failure compensation (re-arm)", backup, dest) {
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
			if markErr := s.persistRestorePendingMarkerKind(ctx, row.ID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
				logging.Warnf("replacement sweep %s: consumption failed (%v) under a reclaimed claim AND the rearm-refused restore-pending restage could not be persisted (%v) — restored destination retained, no re-arm attempted, absent backup name untouched", backup, uErr, markErr)
			}
			return false
		}
		// The backup was removed first, so re-arm it before undoing the
		// restore. ONLY a SUCCEEDED re-arm re-establishes the armed,
		// backup-present retry posture that licenses undoing the restore (codex
		// P2, PR#215 round 18): with the re-arm failed — a foreign writer
		// occupying the backup name (typed fsutil.ErrPublishCollision /
		// ErrPublishNoReplaceUnsupported) or any other cause — the restored
		// destination is the ONLY remaining copy of the pre-crash bytes, and
		// removing it would lose those bytes forever while leaving the journal
		// armed against whatever occupies the backup path (a later restore
		// would install those foreign bytes over the destination and then
		// delete the occupant). Retain the destination instead, drive the
		// recovery through the durable RestorePending marker (a retry runs only
		// cleanup + consumption and never restores from the occupied path),
		// warn with BOTH causes, and leave the occupant untouched.
		if rearmErr := rearmReplacementBackup(s.fs, dest, backup, backupInfo); rearmErr != nil {
			// Wave-19 (codex P2): a failed re-arm leaves the backup name UNOWNED
			// in every failure mode — foreign-occupied on the published-refusal
			// classes, absent on a plain failure. Wave-20 (codex P2): the kind is
			// classified by rearmPendingKind instead of unconditionally — a
			// failure AFTER the staged copy definitely published (fsutil's
			// hard-link fallback completed-despite-error leg — the shared
			// fsutil.PublishCompleted class, wave-21)
			// proves the name carries THIS operation's own bytes, so the durable
			// marker and the in-process fallback record the CLEAN kind and the
			// pending retry reaps the owned name; refusal and pre-publish failure
			// classes keep the rearm-refused kind: no later retry may stat/copy/
			// remove that path (a removal would delete a foreign occupant, an
			// existence check would fail forever on the absent name).
			persistKind := rearmPendingKind(rearmErr)
			s.rememberPendingRemovalKind(backupSlash, persistKind)
			if markErr := s.persistRestorePendingMarkerKind(ctx, row.ID, backupSlash, persistKind); markErr != nil {
				logging.Warnf("replacement sweep %s: consumption failed (%v), re-arm failed (%v), and restore-pending persistence failed (%v) — restored destination retained, backup path untouched (pending kind %s)", backup, uErr, rearmErr, markErr, persistKind)
			} else {
				logging.Warnf("replacement sweep %s: consumption failed (%v) and re-arm failed (%v) — restored destination retained, cleanup marked restore-pending (%s)", backup, uErr, rearmErr, persistKind)
			}
			return false
		}
		// Wave-34 (codex local review round 4, PR#215 finding F1): the undo
		// unlink stays identity-bound even after the SUCCEEDED re-arm — a
		// foreign occupant swapped onto dest in the re-arm→undo window must
		// never be deleted by pathname. Divergence retains the destination and
		// the freshly re-armed backup with the journal entry left live; only a
		// destination still naming the published restore object is unlinked.
		// Wave-35 (codex local review round 5, PR#215): the unlink runs
		// through the destination quarantine (see undoRestore above), closing
		// the remaining verdict→Remove window against a foreign substitution.
		if !restoredDestStillOurs(s.fs, dest, restoredID) {
			logging.Warnf("replacement sweep %s: consumption failed (%v) and restore undo REFUSED — restored destination %s no longer names the published restore object (foreign swap or deletion in the re-arm→undo window); destination and re-armed backup retained, journal entry left live", backup, uErr, dest)
			return false
		}
		// Wave-52 (round 7, finding F1, stage e): the undo unlink is the
		// unit's last name-destroying leg — a claim revoked ANYWHERE past the
		// re-arm's success (including inside the identity check above, the
		// one parkable read inside this stage) leaves the fully repaired armed
		// retry shape standing (backup re-armed from the published
		// destination, entry armed, destination untouched): nothing further
		// unlinks, and the next live arbitration converges the retry. The gate
		// stands between the verdict and the unlink so no read inside the
		// stage can smuggle a revocation past it.
		if claim.abandonIfRevoked("consumption failure compensation (restore undo)", backup, dest) {
			return false
		}
		if rmErr := removeRestoredDestQuarantined(s.fs, dest, "replacement sweep", restoredID); rmErr != nil {
			logging.Warnf("replacement sweep %s: consumption failed AND restore-undo failed (%v after %v)", backup, rmErr, uErr)
		} else {
			logging.Warnf("replacement sweep %s: consumption failed (%v) — restore undone, will retry", backup, uErr)
		}
		return false
	}
	s.forgetPendingRemoval(backupSlash)
	return true
}

// consumeSweepJournalEntry drops every journal entry matching backupSlash
// from the freshly re-read row. All three sweep consumption sites share it so
// the removal posture lives exactly once (review 4960250562 transactions).
func consumeSweepJournalEntry(current *models.BatchFileOperation, backupSlash string) (models.GeneratedFilesJSON, bool, error) {
	gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
	if err != nil {
		return models.GeneratedFilesJSON{}, false, err
	}
	kept := gf.Replacements[:0]
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) != backupSlash {
			kept = append(kept, rep)
		}
	}
	gf.Replacements = kept
	return gf, true, nil
}

// retryPendingRemoval handles the present-destination state left by a failed
// backup Remove. The retry routes on the entry's restore-pending KIND
// (wave-19, codex P2): the legacy clean kind consumes only after the retry
// Remove succeeds (or reports os.IsNotExist), while the rearm-refused kind
// consumes WITHOUT any backup-path operation — the name is foreign-occupied
// or absent, so an existence check could wedge forever and a removal would
// delete a foreign file. Reads and writes run through the same journal
// transaction every other row mutator uses (review 4960250562).
//
// retryPendingRemoval is the direct-caller (test/legacy) entry — the wave-50
// nil-claim discipline: a nil claim is never revoked, so the wave-51 gates
// below never fire on this path. Production in-sweep callers hand their
// ctx-scoped claim to retryPendingRemovalClaimed.
//
//nolint:unused // test-facing entry pinned across waves 8–50; production pending legs ride the wave-51 claimed variant.
func (s *ReplacementSweeper) retryPendingRemoval(ctx context.Context, rowID uint, backup, dest, backupSlash string) bool {
	return s.retryPendingRemovalClaimed(ctx, rowID, backup, dest, backupSlash, nil)
}

// retryPendingRemovalClaimed is retryPendingRemoval under the wave-51
// epoch-ownership gate: every mutation unit below (the orphan-shaped
// cleanup, the clean-kind removal sequence, the rearm-refused journal-only
// consumption) consults the claim's revocation flag at its entry and abandons
// without further mutation once the claim was reclaimed.
func (s *ReplacementSweeper) retryPendingRemovalClaimed(ctx context.Context, rowID uint, backup, dest, backupSlash string, claim *sweepBusyMarkerClaim) bool {
	jrel := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer jrel()
	defer claim.releaseAdmit() // wave-56 (finding F1): release the last admit gate at return
	var rowReverted, targetFound, authorized bool
	var durableKind string
	var fnErr error
	// Wave-25: carry the durable entry's arm-time facts out of the read
	// transaction so the removal gate below can bind its unlink to the OWNED
	// object rather than to the pathname alone (facts are written once at
	// journal-append time and never rewritten).
	var durableEntry models.ReplacementEntry
	err := s.repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		if current.RevertStatus == models.RevertStatusReverted {
			rowReverted = true
			return models.GeneratedFilesJSON{}, false, nil
		}
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			fnErr = err
			return models.GeneratedFilesJSON{}, false, err
		}
		for i := range gf.Replacements {
			if sweepSlash(gf.Replacements[i].Backup) == backupSlash {
				targetFound = true
				authorized = gf.Replacements[i].RestorePending || s.hasPendingRemoval(backupSlash)
				durableKind = gf.Replacements[i].PendingKind()
				durableEntry = gf.Replacements[i]
				break
			}
		}
		return gf, false, nil
	})
	if err != nil {
		if fnErr != nil {
			logging.Warnf("replacement sweep %s: cleanup journal unreadable (%v) — kept", backup, err)
		} else {
			logging.Warnf("replacement sweep %s: owner row unreadable during cleanup (%v) — kept", backup, err)
		}
		return false
	}
	if rowReverted {
		return false
	}
	if !targetFound {
		// Wave-19: a consumed entry whose in-process fallback still remembers a
		// rearm-refused kind can leave a FOREIGN occupant at the backup name —
		// the consumption proved the journal only, never the path's ownership.
		// Keep the name (the next orphan arbitration retains+warns it) instead
		// of deleting bytes nobody journaled.
		if kind, ok := s.pendingRemovalKind(backupSlash); ok && kind == models.RestorePendingKindRearmRefused {
			s.forgetPendingRemoval(backupSlash)
			return true
		}
		// No durable entry carries facts for this name anymore (a prior leg
		// consumed it): the pre-wave-25 pathname posture applies with its
		// documented residual swap window.
		//
		// Wave-51 (codex P1, PR#215) — revocation gate before the
		// no-longer-journaled pending cleanup mutates: a reclaimed claim means
		// nothing below (quarantine move, verified unlink, destination
		// re-gate) may even start. Abandoning leaves the backup byte-intact
		// at its name, the fallback memory recorded, and no journal record
		// touched.
		if claim.abandonIfRevoked("unowned pending-entry backup cleanup", backup, dest) {
			return false
		}
		// Wave-32 (codex local review round 2, PR#215 finding R1): the pending
		// retry's identity gate = destination PRESENCE re-proven AFTER the
		// verified backup object moved to its quarantine name (facts are
		// absent on this legacy leg by construction). A missing or
		// indeterminate destination puts the bytes back and keeps the entry
		// live for the next arbitration instead of consuming the record of a
		// restore whose destination no longer stands.
		hold, rmErr := quarantineReplacementBackupForRemoval(s.fs, backup, "replacement sweep", nil, nil)
		if rmErr == nil {
			if _, derr := lstatRestoreSource(s.fs, dest); derr != nil {
				if rerr := hold.restore(); rerr != nil {
					// Wave-36 (codex local review round 6, PR#215 finding F3): the
					// move-back failed and no durable entry survives on this legacy leg
					// — the failure only surfaces: the unowned backup name keeps its
					// occupant, the verified bytes stay at the quarantine name.
					logging.Warnf("replacement sweep %s: pending cleanup kept — destination %s not present after the backup was quarantined (%v) AND the verified move-back failed (%v); the unowned backup name keeps its occupant, verified bytes recoverable at the quarantine name %s", backup, dest, derr, rerr, hold.quarantine)
					return false
				}
				logging.Warnf("replacement sweep %s: pending cleanup kept — destination %s not present after the backup was quarantined (%v); backup restored to its journaled name", backup, dest, derr)
				return false
			}
			rmErr = hold.removeVerified()
		}
		if rmErr != nil {
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}
	if !authorized {
		return false
	}
	// Wave-51 (codex P1, PR#215) — revocation gate covering the pending
	// entry's cleanup unit on EITHER kind: the rearm-refused leg below is a
	// pure journal consumption (nothing fs-side precedes it, so abandoning is
	// exact), and the clean leg's quarantine→unlink→consume sequence keeps
	// the same entry-gated unit posture as restoreAndConsume's. A revocation
	// observed here leaves the durable pending marker and its kind untouched
	// (the journal stays), the backup wherever it already stood, and the retry
	// converges through the wave-19/29 legs when the caller is live again.
	if claim.abandonIfRevoked("pending-entry cleanup and journal consumption", backup, dest) {
		return false
	}

	// Routing kind: the durable marker is authoritative, but an in-process
	// rearm-refused memory DOMINATES — a refused re-arm this process watched
	// is fresher ownership evidence than the committed (clean) posture.
	effectiveKind := durableKind
	if fallbackKind, ok := s.pendingRemovalKind(backupSlash); ok && (fallbackKind == models.RestorePendingKindRearmRefused || fallbackKind == models.RestorePendingKindPrune) {
		effectiveKind = fallbackKind
	}
	if effectiveKind == models.RestorePendingKindPrune {
		// A prune intent deliberately removes the old bytes while the
		// organized destination remains in place. If the process crashed after
		// unlink but before journal consumption, the absent path is evidence of
		// completed prune cleanup, so consume journal-only; never re-arm from
		// the organized destination.
		if _, backupErr := lstatRestoreSource(s.fs, backup); errors.Is(backupErr, afero.ErrFileNotFound) {
			if claim.abandonIfRevoked("prune pending journal consumption", backup, dest) {
				return false
			}
			consumeCtx := database.WithPruneMaintenance(ctx)
			uErr := s.repo.UpdateJournalInTx(consumeCtx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				return consumeSweepJournalEntry(current, backupSlash)
			})
			if uErr == nil {
				s.forgetPendingRemoval(backupSlash)
				return true
			}
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindPrune)
			logging.Warnf("replacement sweep %s: prune-pending journal consumption failed after the backup was already absent: %v", backup, uErr)
			return false
		} else if backupErr != nil {
			return false
		}
	}

	if effectiveKind == models.RestorePendingKindRearmRefused {
		// Rearm-refused pending (wave-19): destination bytes were certified in
		// place when the marker was set and the backup name is unowned — skip
		// the metadata capture AND the removal entirely. Consumption failure
		// needs no re-arm compensation (nothing was removed); the durable and
		// in-process markers simply persist the same retry posture again.
		uErr := s.repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return consumeSweepJournalEntry(current, backupSlash)
		})
		if uErr != nil {
			// Wave-52 (round 7, finding F1, stage c): the failed consumption's
			// pending re-persist is revocation-fenced — a reclaimed claim stops
			// bare; the durable rearm-refused marker stands unchanged for the
			// next live retry (nothing fs-side ever moved on this leg).
			if claim.abandonIfRevoked("rearm-refused pending re-persist", backup, dest) {
				return false
			}
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
			if markErr := s.persistRestorePendingMarkerKind(ctx, rowID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
				logging.Warnf("replacement sweep %s: rearm-refused pending consumption failed (%v) and restore-pending persistence failed (%v) — restored destination retained, unowned backup name untouched", backup, uErr, markErr)
			} else {
				logging.Warnf("replacement sweep %s: rearm-refused pending consumption failed: %v — restored destination retained, unowned backup name untouched", backup, uErr)
			}
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}

	// Capture metadata without following a swapped-in symlink; consumption
	// failure below must recreate the original permission bits and timestamps.
	// Wave-25: the removal gate verifies the durable entry's arm-time facts
	// plus this just-captured identity before unlinking — a foreign occupant
	// swapped onto the backup name is retained, never deleted-then-consumed.
	backupInfo, _ := lstatRestoreSource(s.fs, backup)
	// Wave-32 (codex local review round 2, PR#215 finding R1): the pending
	// retry's identity gate = the recorded backup-facts binding (wave-25,
	// applied inside the quarantine through durableEntry) + destination
	// PRESENCE re-proven AFTER the verified object moved aside; only then
	// does the quarantined unlink run. A destination that vanished or turned
	// indeterminate mid-window keeps the bytes at their journaled name and
	// the pending marker live.
	// Wave-52 (round 7, finding F1, stage b): the wedged-Lstat park above is
	// exactly where a reclaim must catch the unit before its quarantine arms
	// move — a reclaimed claim stops bare: the durable clean marker stands,
	// the backup never leaves its journaled name, and the unit re-enters from
	// its entry on the next live retry.
	if claim.abandonIfRevoked("clean pending backup quarantine removal", backup, dest) {
		return false
	}
	var hold *replacementBackupQuarantine
	var rmErr error
	if effectiveKind == models.RestorePendingKindPrune {
		hold, rmErr = quarantineReplacementBackupForPrune(s.fs, backup, "replacement sweep", &durableEntry, backupInfo)
	} else {
		hold, rmErr = quarantineReplacementBackupForRemoval(s.fs, backup, "replacement sweep", &durableEntry, backupInfo)
	}
	if rmErr != nil && effectiveKind == models.RestorePendingKindPrune && hold != nil && hold.moved {
		_ = s.persistPruneQuarantineLocked(rowID, durableEntry, hold.quarantine)
	}
	if rmErr == nil && effectiveKind != models.RestorePendingKindPrune {
		if _, derr := lstatRestoreSource(s.fs, dest); derr != nil {
			if rerr := hold.restore(); rerr != nil {
				// Wave-36 (codex local review round 6, PR#215 finding F3): the
				// move-back failed — the journaled name is unowned while the
				// verified bytes stay recoverable at the quarantine name, so the
				// durable pending entry upgrades to the rearm-refused kind
				// (journal-only retry; no leg touches the foreign name again).
				// Wave-52 (round 7, finding F1, stage c): the rearm-refused upgrade
				// persist is revocation-fenced — a reclaimed claim stops bare: the
				// durable CLEAN marker (this leg's entry point) stands unchanged,
				// the in-process fallback stays unset, the journaled name keeps
				// whatever occupant the failed move-back left, and the verified
				// bytes stay recoverable at the quarantine name.
				if claim.abandonIfRevoked("clean pending divergence marker persist", backup, dest) {
					return false
				}
				s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
				if markErr := s.persistRestorePendingMarkerKind(ctx, rowID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
					logging.Warnf("replacement sweep %s: pending cleanup kept — destination %s not present after the backup was quarantined (%v), the verified move-back failed (%v), AND the rearm-refused marker could not be persisted (%v); verified bytes recoverable at the quarantine name %s", backup, dest, derr, rerr, markErr, hold.quarantine)
				} else {
					logging.Warnf("replacement sweep %s: pending cleanup kept — destination %s not present after the backup was quarantined (%v) and the verified move-back failed (%v); entry upgraded to the rearm-refused pending kind, verified bytes recoverable at the quarantine name %s", backup, dest, derr, rerr, hold.quarantine)
				}
				return false
			}
			logging.Warnf("replacement sweep %s: pending cleanup kept — destination %s not present after the backup was quarantined (%v); backup restored to its journaled name", backup, dest, derr)
			s.rememberPendingRemoval(backupSlash)
			return false
		}
	}
	if rmErr == nil {
		rmErr = hold.removeVerified()
	}
	if rmErr != nil {
		// Wave-52 (round 7, finding F1, stage c): the removal-failure marker
		// updates (the vanished-class rearm-refused upgrade persist, or the
		// in-process clean re-authorization) are revocation-fenced — a
		// reclaimed claim stops bare: the durable CLEAN marker stands
		// unchanged and the post-attempt fs shape is exactly what the stage
		// left.
		if claim.abandonIfRevoked("clean pending removal-failure marker persist", backup, dest) {
			return false
		}
		// Wave-32 (finding R4): the vanished class leaves the journaled name
		// absent by construction, so the pending entry converts to the
		// rearm-refused (journal-only) kind — durable and in-process alike —
		// or the clean-kind retry would wait forever on a file that can
		// never reappear.
		if errors.Is(rmErr, errReplacementBackupQuarantineVanished) && effectiveKind != models.RestorePendingKindPrune {
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindRearmRefused)
			if markErr := s.persistRestorePendingMarkerKind(ctx, rowID, backupSlash, models.RestorePendingKindRearmRefused); markErr != nil {
				logging.Warnf("replacement sweep %s: vanished-quarantine removal could not persist the rearm-refused upgrade (%v after %v) — entry stays clean-pending for later arbitration", backup, markErr, rmErr)
			}
			return false
		}
		if effectiveKind == models.RestorePendingKindPrune {
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindPrune)
		} else {
			s.rememberPendingRemoval(backupSlash)
		}
		return false
	}
	// Wave-52 (round 7, finding F1, stage d): the consumption update — a
	// reclaimed claim stops bare AFTER the completed removal stage: the
	// backup name is already absent (the clean-kind retry tolerates that),
	// the destination stands untouched, and the durable CLEAN restore-pending
	// marker remains the retry's authorization — nothing further mutates.
	if claim.abandonIfRevoked("clean pending journal consumption", backup, dest) {
		return false
	}
	consumeCtx := ctx
	if effectiveKind == models.RestorePendingKindPrune {
		consumeCtx = database.WithPruneMaintenance(ctx)
	}
	uErr := s.repo.UpdateJournalInTx(consumeCtx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return consumeSweepJournalEntry(current, backupSlash)
	})
	if uErr != nil {
		// Wave-52 (round 7, finding F1, stage e): the re-arm compensation
		// PUBLISHES the backup name — a reclaimed claim never runs it. The
		// durable clean restore-pending marker still stands (this leg's entry
		// point), and the clean-kind retry tolerates the absent name — the
		// resting shape needs nothing else.
		if claim.abandonIfRevoked("clean pending consumption compensation (re-arm)", backup, dest) {
			return false
		}
		if effectiveKind == models.RestorePendingKindPrune {
			s.rememberPendingRemovalKind(backupSlash, models.RestorePendingKindPrune)
			if markErr := s.persistRestorePendingMarkerKind(database.WithPruneMaintenance(ctx), rowID, backupSlash, models.RestorePendingKindPrune); markErr != nil {
				logging.Warnf("replacement sweep %s: prune-pending consumption failed (%v) and marker persistence failed (%v)", backup, uErr, markErr)
			}
			logging.Warnf("replacement sweep %s: prune-pending consumption failed: %v — backup cleanup will retry without re-arming from organized bytes", backup, uErr)
			return false
		}
		fallbackKind := models.RestorePendingKindClean
		if rearmErr := rearmReplacementBackup(s.fs, dest, backup, backupInfo); rearmErr != nil {
			// codex P2 (PR#215 round 18): identical recovery contract as
			// restoreAndConsume's consumption-failure leg. The destination
			// already holds the restored bytes and never gets undone here, so
			// the entry must not stay ARMED against a backup name the re-arm
			// could not reclaim (a foreign occupant, or any other cause):
			// persist the durable RestorePending marker so the retry runs only
			// removal + consumption. The marker merge is a no-op when the entry
			// already carries the marker (the usual entry point into this leg).
			// Wave-19: the occupied-name refusal classes mean the just-removed
			// name raced a FOREIGN claimant — the marker (and the in-process
			// fallback below) upgrades to the rearm-refused kind so the next
			// retry never touches that path.
			// Wave-20 (codex P2): the kind now comes from rearmPendingKind for
			// EVERY failure class — a plain pre-publish failure leaves the name
			// ABSENT (unproven), which the rearm-refused kind covers: this
			// sweep's own removal leg tolerates a missing name, but an explicit
			// revert reading a clean-pending entry with an absent backup wedges
			// at the source stat forever. Only a failure after the staged copy
			// definitely published keeps the clean posture (owned bytes at the
			// name; the retry reaps it).
			persistKind := rearmPendingKind(rearmErr)
			fallbackKind = persistKind
			if markErr := s.persistRestorePendingMarkerKind(ctx, rowID, backupSlash, persistKind); markErr != nil {
				absoluteBackup, _ := filepath.Abs(backup)
				logging.Warnf("replacement sweep failed to re-arm backup %s after pending cleanup failure (%v), re-arm failed (%v), and restore-pending persistence failed (%v) — restored destination retained, backup path untouched", absoluteBackup, uErr, rearmErr, markErr)
			} else {
				absoluteBackup, _ := filepath.Abs(backup)
				logging.Warnf("replacement sweep failed to re-arm backup %s after pending cleanup failure: %v — entry marked restore-pending", absoluteBackup, rearmErr)
			}
		}
		s.rememberPendingRemovalKind(backupSlash, fallbackKind)
		logging.Warnf("replacement sweep %s: pending cleanup consumption failed: %v", backup, uErr)
		return false
	}
	s.forgetPendingRemoval(backupSlash)
	return true
}

// consumeRearmRefusedPending retries ONE ledger-enumerated rearm-refused
// restore-pending entry during a FULL sweep (wave-29, codex P2, PR#215). Such
// an entry's backup name is UNOWNED — foreign-occupied or absent after a
// refused no-replace re-arm — so no directory scan will ever produce it as a
// marker file; without this leg it would stay live forever and its operation
// row would keep blocking older replacement chains. The retry is JOURNAL-ONLY
// through retryPendingRemoval's wave-19 kind routing (re-verified fresh
// inside the journal transaction): the destination bytes were certified in
// place when the marker was written, so consumption must never touch the
// backup path — an existence check would wedge forever on the absent name and
// a removal could delete a foreign occupant.
//
// Pre-conditions verified here, conservatively:
//   - the destination must classify PRESENT by Lstat (any object — the
//     wave-12 posture: only a genuine ENOENT is missing, and a missing or
//     indeterminate destination breaks the wave-19 consumption contract:
//     instead of consuming the only record that a restore happened, the entry
//     is kept and warned);
//   - the backup NAME must classify ABSENT. An occupant at the name is the
//     directory scan's arbitration (the wave-19 journaled/orphan legs reach
//     it through the marker file); this leg exists exactly for the names a
//     scan cannot see.
//
// A consumption failure keeps the entry live (retryPendingRemoval re-persists
// the durable marker and warns). A live busy marker keeps the name untouched
// for the owner process's own lifecycle, mirroring sweepOne's posture.
func (s *ReplacementSweeper) consumeRearmRefusedPending(ctx context.Context, idx *replacementLedgerIndex, entry rearmRefusedLedgerEntry) int {
	busyRelease, busyToken, busyErr := acquireReplacementBusyExFn(s.fs, entry.dest)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		return 0
	}
	if busyErr != nil {
		logging.Warnf("replacement sweep %s: busy-marker arbitration failed (%v) — rearm-refused pending kept", entry.backup, busyErr)
		return 0
	}
	if busyToken == "" {
		// Wave-56 (finding F2): provenance unavailable — refuse to record the
		// claim (treat as a failed acquire on the sweep side).
		busyRelease()
		logging.Warnf("replacement sweep %s: busy-marker token provenance unavailable — rearm-refused pending kept", entry.backup)
		return 0
	}
	defer busyRelease()
	// Wave-49 (codex P2): same ctx-scoped claim ledger as sweepOne — an
	// abandoned full sweep must not strand this marker against the
	// continued revert either. Wave-52 (codex local review round 7, PR#215
	// finding F2): same record-at-marker-acquire-time discipline as sweepOne
	// — the record lands BEFORE the dest-lock wait carrying a pending cell
	// for the lock release, so the reverter's reclaim consult sees the claim
	// for the whole wait and frees both holds once bound.
	claim, untrackSweepClaim := recordSweepBusyClaim(ctx, s.fs, entry.dest, busyToken, busyRelease)
	defer untrackSweepClaim()
	defer claim.releaseAdmit() // wave-56 (finding F1): release the last admit gate at return
	rawDestRelease := fsutil.SharedDestLocks().Acquire(entry.dest)
	if !claim.bindDestLock(rawDestRelease) {
		// Reclaimed during the wait (empty cell): the just-acquired lock is
		// this goroutine's alone to release; then abandon — the revocation
		// flag is already set and every stage gate below reads it.
		rawDestRelease()
		claim.abandonIfRevoked("destination lock acquisition", entry.backup, entry.dest)
		return 0
	}
	defer claim.releaseDestLock()

	// The destination the marker certified must still be present before the
	// only journal record of the restore is consumed.
	if _, lstatErr := lstatRestoreSource(s.fs, entry.dest); lstatErr != nil {
		logging.Warnf("replacement sweep %s: rearm-refused pending kept — the restore-certified destination is not present (%v); entry retained for later arbitration", entry.backup, lstatErr)
		return 0
	}
	// This leg exists for the names a directory scan cannot see: the backup
	// name must be ABSENT. An occupant (or an indeterminate answer) defers to
	// the marker-file scans — a removal decision here could destroy foreign
	// bytes.
	if _, backupErr := lstatRestoreSource(s.fs, entry.backup); backupErr == nil {
		return 0
	} else if !errors.Is(backupErr, afero.ErrFileNotFound) {
		logging.Warnf("replacement sweep %s: rearm-refused pending kept — backup name state indeterminate (%v)", entry.backup, backupErr)
		return 0
	}
	if s.retryPendingRemovalClaimed(ctx, entry.rowID, entry.backup, entry.dest, entry.backupSlash, claim) {
		// Wave-33 (codex local review round 3, PR#215 finding R1): the
		// journal-only consumption removed the entry from the LIVE ledger —
		// retract its idx.journaled mapping NOW so a later directory scan of
		// this sweep never routes a candidate marker at this name through the
		// stale owner copy (a marker planted here after the absent-proof above
		// must arbitrate ORPHAN — retain + warn — never owned-removable). A
		// FAILED consumption leaves the entry live, so the mapping correctly
		// stays.
		delete(idx.journaled, entry.backupSlash)
		return 1
	}
	return 0
}
