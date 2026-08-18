package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

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

// removeReplacementBackup is the ownership gate for every journal consume path:
// a non-missing removal error leaves the journal entry armed and retryable.
func removeReplacementBackup(fs afero.Fs, backup, phase string) error {
	if err := fs.Remove(backup); err != nil && !os.IsNotExist(err) {
		absoluteBackup, _ := filepath.Abs(backup)
		logging.Warnf("%s failed to remove backup %s: %v", phase, absoluteBackup, err)
		return err
	}
	return nil
}

// markReplacementRestorePending records that restore bytes are installed while
// the backup cleanup still needs a retry. Installed remains the downloader's
// confirmation bit; this marker only disambiguates a repaired destination from
// an armed apply whose destination happened to be present.
func markReplacementRestorePending(gf *models.GeneratedFilesJSON, backupSlash string) bool {
	for i := range gf.Replacements {
		if sweepSlash(gf.Replacements[i].Backup) != backupSlash {
			continue
		}
		if gf.Replacements[i].RestorePending {
			return false
		}
		gf.Replacements[i].RestorePending = true
		return true
	}
	return false
}

// markReplacementEntryRestorePending applies the marker to a fresh row. It is
// used by the explicit reverter when backup removal fails before consumption.
func markReplacementEntryRestorePending(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, rowID uint, backupSlash string) error {
	release := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer release()
	liveRow, err := repo.FindByID(ctx, rowID)
	if err != nil {
		return err
	}
	if liveRow == nil {
		return fmt.Errorf("owner row %d not found", rowID)
	}
	gf, err := models.ParseGeneratedFiles(liveRow.GeneratedFiles)
	if err != nil {
		return err
	}
	if !markReplacementRestorePending(&gf, backupSlash) {
		return nil
	}
	liveRow.GeneratedFiles = models.MarshalLedgerJSON(gf)
	return repo.Update(ctx, liveRow)
}

func rearmReplacementBackup(fs afero.Fs, dest, backup string, info os.FileInfo) error {
	if err := fsutil.CopyFileFs(fs, dest, backup); err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	if err := fs.Chmod(backup, info.Mode().Perm()); err != nil {
		return err
	}
	return fs.Chtimes(backup, info.ModTime(), info.ModTime())
}

// ReplacementSweeper reaps replacement backups under conservative ownership.
type ReplacementSweeper struct {
	fs              afero.Fs
	repo            database.BatchFileOperationRepositoryInterface
	pendingMu       sync.Mutex      // API-triggered sweeps share pendingRemovals; never hold across fs/repo calls.
	pendingRemovals map[string]bool // backup key → restore installed, cleanup pending
}

// NewReplacementSweeper constructs a sweeper whose in-flight arbitration is
// durable and destination-specific via the `.dlbusy` marker.
func NewReplacementSweeper(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) *ReplacementSweeper {
	return &ReplacementSweeper{fs: fs, repo: repo, pendingRemovals: map[string]bool{}}
}

func (s *ReplacementSweeper) rememberPendingRemoval(backupKey string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingRemovals == nil {
		s.pendingRemovals = map[string]bool{}
	}
	s.pendingRemovals[backupKey] = true
}

func (s *ReplacementSweeper) hasPendingRemoval(backupKey string) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return s.pendingRemovals != nil && s.pendingRemovals[backupKey]
}

func (s *ReplacementSweeper) forgetPendingRemoval(backupKey string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingRemovals != nil {
		delete(s.pendingRemovals, backupKey)
	}
}

// replacementLedgerIndex maps journaled backup paths to their owning row for
// sweep arbitration, and destinations to the owning rows.
type replacementLedgerIndex struct {
	journaled map[string]*models.BatchFileOperation // backup path → owning row
	dirs      map[string]bool                       // directories holding journaled destinations
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
	for i := range rows {
		row := &rows[i]
		gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
		if perr != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			idx.journaled[sweepSlash(rep.Backup)] = row
			// Dirs are FS ENUMERATION paths — keep their recorded case (the
			// probe-aware key is only a comparison form).
			idx.dirs[filepath.ToSlash(filepath.Clean(filepath.Dir(rep.Destination)))] = true
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

// Sweep runs a full startup sweep: every directory that holds a journaled
// destination is scanned for ownership-marker backups.
func (s *ReplacementSweeper) Sweep(ctx context.Context) (int, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	healed := 0
	for dir := range idx.dirs {
		// R11-2: FLAT scans only — no recursion into media libraries. Crash
		// windows are covered exactly by the orchestrator's per-organize leaf
		// seed (R7-3), so startup cost stays O(ledgered dirs), independent of
		// library size.
		entries, rdErr := afero.ReadDir(s.fs, filepath.FromSlash(dir))
		if rdErr != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !IsReplacementBackupName(e.Name()) {
				continue
			}
			healed += s.sweepOne(ctx, idx, dir, e)
		}
	}
	return healed, nil
}

// SweepDestinations runs the targeted pre-revert sweep over the destinations
// an operation's journal names: crash-window restores complete BEFORE the
// revert's rejection/restore checks evaluate the destination state.
func (s *ReplacementSweeper) SweepDestinations(ctx context.Context, destinations []string) (int, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	seen := map[string]bool{}
	healed := 0
	for _, dest := range destinations {
		dir := filepath.ToSlash(filepath.Clean(filepath.Dir(dest)))
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, rdErr := afero.ReadDir(s.fs, filepath.FromSlash(dir))
		if rdErr != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !IsReplacementBackupName(e.Name()) {
				continue
			}
			// Targeted sweep only arbitrates backups of the named destinations.
			candidate := sweepSlash(filepath.Join(dir, e.Name()))
			destSlash := sweepSlash(dest)
			if !strings.HasPrefix(candidate, destSlash) {
				continue
			}
			healed += s.sweepOne(ctx, idx, dir, e)
		}
	}
	return healed, nil
}

// sweepOne arbitrates one ownership-marker backup file.
func (s *ReplacementSweeper) sweepOne(ctx context.Context, idx *replacementLedgerIndex, dirSlash string, e os.FileInfo) int {
	backup := filepath.FromSlash(dirSlash + "/" + e.Name())
	// Journal comparisons run under the probe-aware key (separator normalization
	// plus conditional case folding); actual fs paths keep their recorded case.
	backupKey := sweepSlash(backup)

	dest := strings.TrimSuffix(backup, replacementBackupName.FindString(e.Name()))

	// The marker is an on-disk cross-process exclusion. Acquire it before
	// reading or changing ownership state: the downloader creates the same
	// marker before moving the destination aside, so a live API install cannot
	// be mistaken for a stale crash window by this process (or at startup).
	busyRelease, busyErr := fsutil.AcquireReplacementBusy(s.fs, dest)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		return 0
	}
	if busyErr != nil {
		logging.Warnf("replacement sweep %s: busy-marker arbitration failed (%v) — kept", backup, busyErr)
		return 0
	}
	defer busyRelease()

	if owner, ok := idx.journaled[backupKey]; ok {
		// Journaled — handled under the lock inside restoreAndConsume, which
		// RECHECKS the destination state in the critical section: a downloader
		// can install new bytes between the classification read and the lock
		// (codex P3 R3-2), and the backup must never clobber those
		// freshly-installed artifacts.
		if s.restoreAndConsume(ctx, owner, backup, dest, backupKey) {
			delete(idx.journaled, backupKey)
			return 1
		}
		return 0
	}

	// Orphan: no row journals this backup anymore. CLASSIFY FRESH INSIDE THE
	// LOCK (codex P3 R4-1): the index snapshot may predate the downloader's
	// RecordReplacement — deleting a just-journaled backup because the
	// destination exists would leave that row permanently unrevertable.
	release := fsutil.SharedDestLocks().Acquire(dest)
	defer release()
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
	statErr := func() error { _, err := s.fs.Stat(dest); return err }()
	if errors.Is(statErr, afero.ErrFileNotFound) {
		if rnErr := copyRestoreBytes(s.fs, backup, dest); rnErr != nil {
			logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
			return 0
		}
		// The restore repairs the missing destination, but marker shape alone is
		// not ownership proof. Retain the source for inspection rather than
		// deleting a possible user-owned file; the user can remove it manually.
		warnRetainedUnjournaledBackup(backup)
		return 1
	}
	if statErr != nil {
		// R8-1: indeterminate destination state (permission/IO) must NEVER
		// read as "present" — the unjournaled backup may be the ONLY copy of
		// the pre-replace bytes. Touch nothing; retry next sweep.
		logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, statErr)
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
func (s *ReplacementSweeper) restoreAndConsume(ctx context.Context, row *models.BatchFileOperation, backup, dest, backupSlash string) bool {
	release := fsutil.SharedDestLocks().Acquire(dest)
	defer release()

	// A prior restore can leave the destination present when backup cleanup
	// failed. Only the explicit pending marker (or the same-process fallback)
	// authorizes cleanup here; an ordinary armed/installed row remains retained.
	if _, statErr := s.fs.Stat(dest); statErr == nil {
		if !s.hasPendingRemoval(backupSlash) && !journalEntryRestorePending(row, backupSlash) {
			return false
		}
		return s.retryPendingRemoval(ctx, row.ID, backup, dest, backupSlash)
	} else if !errors.Is(statErr, afero.ErrFileNotFound) {
		logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, statErr)
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
	if rnErr := copyRestoreBytes(s.fs, backup, dest); rnErr != nil {
		logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
		return false
	}

	// R15-1: consume from a row re-read under the shared journal lock — an
	// index-time snapshot can overwrite an entry recorded or confirmed
	// meanwhile. Lock order dest→journal matches the reverter's consumption.
	undoRestore := func() {
		if rmErr := s.fs.Remove(dest); rmErr != nil {
			logging.Warnf("replacement sweep %s: restore undo failed: %v", backup, rmErr)
		}
	}
	jrel := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(row.ID)))
	defer jrel()
	liveRow, lErr := s.repo.FindByID(ctx, row.ID)
	if lErr != nil || liveRow == nil {
		undoRestore()
		return false
	}
	gf, err := models.ParseGeneratedFiles(liveRow.GeneratedFiles)
	if err != nil {
		undoRestore()
		return false
	}
	removedSelf := false
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == backupSlash {
			removedSelf = true
			break
		}
	}
	if !removedSelf {
		// Already consumed (e.g. a reverter raced us) — the backup is no longer
		// journal-owned, so remove it before reporting the restore complete.
		if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}

	// Removal is the ownership boundary. Keep the row armed when it fails and
	// persist a distinct marker so a later sweep can clean a present destination
	// without mistaking an ordinary armed apply for a completed restore.
	// Capture the backup metadata without following a swapped-in symlink so a
	// failed consume can re-arm only the original object metadata.
	backupInfo, _, _ := lstatRestoreSource(s.fs, backup)
	if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
		s.rememberPendingRemoval(backupSlash)
		if markReplacementRestorePending(&gf, backupSlash) {
			liveRow.GeneratedFiles = models.MarshalLedgerJSON(gf)
			if markErr := s.repo.Update(ctx, liveRow); markErr != nil {
				absoluteBackup, _ := filepath.Abs(backup)
				logging.Warnf("replacement sweep failed to retain cleanup marker for backup %s: %v", absoluteBackup, markErr)
				// Without a durable marker, do not leave the restored destination
				// behind: restore the armed, backup-present retry state instead.
				s.forgetPendingRemoval(backupSlash)
				undoRestore()
			}
		}
		return false
	}

	kept := gf.Replacements[:0]
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) != backupSlash {
			kept = append(kept, rep)
		}
	}
	gf.Replacements = kept
	liveRow.GeneratedFiles = models.MarshalLedgerJSON(gf)
	if uErr := s.repo.Update(ctx, liveRow); uErr != nil {
		// The backup was removed first, so re-arm it before undoing the restore.
		// This preserves the old retry posture even when journal persistence is
		// temporarily unavailable.
		if rearmErr := rearmReplacementBackup(s.fs, dest, backup, backupInfo); rearmErr != nil {
			absoluteBackup, _ := filepath.Abs(backup)
			logging.Warnf("replacement sweep failed to re-arm backup %s after consumption failure: %v", absoluteBackup, rearmErr)
		}
		if rmErr := s.fs.Remove(dest); rmErr != nil {
			logging.Warnf("replacement sweep %s: consumption failed AND restore-undo failed (%v after %v)", backup, rmErr, uErr)
		} else {
			logging.Warnf("replacement sweep %s: consumption failed (%v) — restore undone, will retry", backup, uErr)
		}
		return false
	}
	s.forgetPendingRemoval(backupSlash)
	return true
}

// retryPendingRemoval handles the present-destination state left by a failed
// backup Remove. It consumes only after the retry Remove succeeds (or reports
// os.IsNotExist), and uses the live row under the journal lock.
func (s *ReplacementSweeper) retryPendingRemoval(ctx context.Context, rowID uint, backup, dest, backupSlash string) bool {
	jrel := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer jrel()
	liveRow, err := s.repo.FindByID(ctx, rowID)
	if err != nil || liveRow == nil {
		logging.Warnf("replacement sweep %s: owner row unreadable during cleanup (%v) — kept", backup, err)
		return false
	}
	if liveRow.RevertStatus == models.RevertStatusReverted {
		return false
	}
	gf, err := models.ParseGeneratedFiles(liveRow.GeneratedFiles)
	if err != nil {
		logging.Warnf("replacement sweep %s: cleanup journal unreadable (%v) — kept", backup, err)
		return false
	}
	target := -1
	for i := range gf.Replacements {
		if sweepSlash(gf.Replacements[i].Backup) == backupSlash {
			target = i
			break
		}
	}
	if target < 0 {
		if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}
	if !gf.Replacements[target].RestorePending && !s.hasPendingRemoval(backupSlash) {
		return false
	}
	// Capture metadata without following a swapped-in symlink; consumption
	// failure below must recreate the original permission bits and timestamps.
	backupInfo, _, _ := lstatRestoreSource(s.fs, backup)
	if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
		s.rememberPendingRemoval(backupSlash)
		return false
	}
	kept := gf.Replacements[:0]
	for i, rep := range gf.Replacements {
		if i != target {
			kept = append(kept, rep)
		}
	}
	gf.Replacements = kept
	liveRow.GeneratedFiles = models.MarshalLedgerJSON(gf)
	if err := s.repo.Update(ctx, liveRow); err != nil {
		if rearmErr := rearmReplacementBackup(s.fs, dest, backup, backupInfo); rearmErr != nil {
			absoluteBackup, _ := filepath.Abs(backup)
			logging.Warnf("replacement sweep failed to re-arm backup %s after pending cleanup failure: %v", absoluteBackup, rearmErr)
		}
		s.rememberPendingRemoval(backupSlash)
		logging.Warnf("replacement sweep %s: pending cleanup consumption failed: %v", backup, err)
		return false
	}
	s.forgetPendingRemoval(backupSlash)
	return true
}
