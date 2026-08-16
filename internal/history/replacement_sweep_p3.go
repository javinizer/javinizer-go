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

var replacementBackupName = regexp.MustCompile(`\.dlbak\.[0-9a-f]{16}(\.[0-9a-f]{1,16})?`)

// sweepSlash normalizes a path for journal comparison: the journaled ledger
// stores paths however the downloader recorded them (slash form), while the
// enumerator joins with OS separators — on Windows the two spellings differ.
func sweepSlash(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

// IsReplacementBackupName reports whether name carries the revert-ledger
// ownership marker (destination-adjacent backup from downloader overwrites).
func IsReplacementBackupName(name string) bool {
	// Anchored: the marker must reach the end of the name.
	m := replacementBackupName.FindString(name)
	return m != "" && strings.HasSuffix(name, m)
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
			idx.dirs[sweepSlash(filepath.Dir(rep.Destination))] = true
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
		dir := sweepSlash(filepath.Dir(dest))
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
	backupSlash := sweepSlash(backup)

	// Younger than this process: plausibly in-flight under a live downloader
	// lock — never arbitrate what we cannot see journaled yet.
	if !e.ModTime().Before(s.startedAt) {
		return 0
	}
	dest := strings.TrimSuffix(backupSlash, replacementBackupName.FindString(backupSlash))

	if owner, ok := idx.journaled[backupSlash]; ok {
		// Journaled — retained by default. Crash-window: destination missing
		// means the new bytes NEVER landed; restore old bytes and consume.
		if _, statErr := s.fs.Stat(dest); errors.Is(statErr, afero.ErrFileNotFound) {
			if s.restoreAndConsume(ctx, owner, backup, dest) {
				delete(idx.journaled, backup)
				return 1
			}
		} else if statErr != nil {
			logging.Warnf("replacement sweep %s: destination indeterminate (%v) — kept", backup, statErr)
		}
		return 0
	}

	// Orphan: no row journals this backup anymore (entry consumed post-revert
	// or the row was pruned).
	if _, statErr := s.fs.Stat(dest); errors.Is(statErr, afero.ErrFileNotFound) {
		release := fsutil.SharedDestLocks().Acquire(dest)
		defer release()
		if rnErr := s.fs.Rename(backup, dest); rnErr != nil {
			logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
			return 0
		}
		return 1
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
func (s *ReplacementSweeper) restoreAndConsume(ctx context.Context, row *models.BatchFileOperation, backup, dest string) bool {
	release := fsutil.SharedDestLocks().Acquire(dest)
	defer release()
	if rnErr := s.fs.Rename(backup, dest); rnErr != nil {
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
		logging.Warnf("replacement sweep: journal consumption failed for op %d: %v", row.ID, uErr)
	}
	return true
}
