package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
//   - A backup younger than this process is in-flight (another live operation
//     may own it) and is skipped.
//   - A journaled backup whose destination went missing belongs to the crash
//     window between set-aside and install: the new bytes never landed, so
//     the old bytes are restored to the destination and the journal entry is
//     consumed (the row otherwise stays failed/unrevertable forever).
//   - A backup journaled NOWHERE is an orphan: with the destination present
//     it is stale residue (deleted); with the destination missing it is the
//     last copy of somebody's bytes (restored).

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

// sweepSlash normalizes a path for journal comparison via the canonical
// dest key (separator + platform case folding — R12-1/R12-4).
func sweepSlash(p string) string { return fsutil.DestKey(p) }

// IsReplacementBackupName reports whether name carries the revert-ledger
// ownership marker (destination-adjacent backup from downloader overwrites).
func IsReplacementBackupName(name string) bool {
	return replacementBackupName.MatchString(name)
}

// ReplacementSweeper reaps replacement backups under conservative ownership.
type ReplacementSweeper struct {
	fs        afero.Fs
	repo      database.BatchFileOperationRepositoryInterface
	startedAt time.Time
}

// NewReplacementSweeper constructs the sweeper; startedAt defaults to now
// (process start) so in-flight backups created after boot are never swept.
func NewReplacementSweeper(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) *ReplacementSweeper {
	return &ReplacementSweeper{fs: fs, repo: repo, startedAt: time.Now()}
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
			// canonical key is only a comparison form).
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
	// Journal comparisons run under the canonical KEY (R12-1: separator +
	// platform case folding); the actual fs paths keep their recorded case.
	backupKey := sweepSlash(backup)

	// Younger than this process: plausibly in-flight under a live downloader
	// lock — never arbitrate what we cannot see journaled yet.
	if !e.ModTime().Before(s.startedAt) {
		return 0
	}
	dest := strings.TrimSuffix(backup, replacementBackupName.FindString(e.Name()))

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
		// Orphan: no consumption to persist — the moved copy is redundant now.
		_ = s.fs.Remove(backup)
		return 1
	}
	if statErr != nil {
		// R8-1: indeterminate destination state (permission/IO) must NEVER
		// read as "present" — the unjournaled backup may be the ONLY copy of
		// the pre-replace bytes. Touch nothing; retry next sweep.
		logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, statErr)
		return 0
	}
	if rmErr := s.fs.Remove(backup); rmErr != nil {
		logging.Warnf("replacement sweep remove %s: %v", backup, rmErr)
		return 0
	}
	return 1
}

// restoreAndConsume moves a journaled crash-window backup back onto its
// destination under the destination lock and consumes the journal entry (a
// stranded entry would otherwise poison future reverts with a missing
// backup).
func (s *ReplacementSweeper) restoreAndConsume(ctx context.Context, row *models.BatchFileOperation, backup, dest, backupSlash string) bool {
	release := fsutil.SharedDestLocks().Acquire(dest)
	defer release()

	// Recheck INSIDE the lock: if the destination now has bytes (a concurrent
	// apply installed them since classification), the crash-window premise
	// no longer holds — retain the journaled backup, touching nothing.
	if _, statErr := s.fs.Stat(dest); statErr == nil {
		return false
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
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		logging.Warnf("replacement sweep: journal re-parse failed for op %d: %v", row.ID, err)
		return true // bytes restored; entry consumption retried next sweep
	}
	kept := gf.Replacements[:0]
	for _, rep := range gf.Replacements {
		if sweepSlash(rep.Backup) == sweepSlash(backup) {
			continue
		}
		kept = append(kept, rep)
	}
	gf.Replacements = kept
	data, mErr := json.Marshal(gf)
	if mErr != nil {
		return true
	}
	row.GeneratedFiles = string(data)
	if uErr := s.repo.Update(ctx, row); uErr != nil {
		// R9-2: the repair is NOT complete while the entry persists. Undo the
		// restore (the destination was proven missing pre-restore) so the next
		// sweep reproduces the same state exactly — a lingering armed entry
		// beside a present destination would never converge, and a later user
		// deletion would re-fire the restore.
		if rmErr := s.fs.Remove(dest); rmErr != nil {
			logging.Warnf("replacement sweep %s: consumption failed AND restore-undo failed (%v after %v)", backup, rmErr, uErr)
		} else {
			logging.Warnf("replacement sweep %s: consumption failed (%v) — restore undone, will retry", backup, uErr)
		}
		return false
	}
	_ = s.fs.Remove(backup)
	return true
}
