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

// markReplacementEntryRestorePending applies the marker to a fresh row inside
// the journal read-modify-write transaction (review 4960250562: atomic across
// processes, not just this process's lock registry). It is used by the
// explicit reverter when backup removal fails before consumption.
func markReplacementEntryRestorePending(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, rowID uint, backupSlash string) error {
	release := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer release()
	txErr := repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !markReplacementRestorePending(&gf, backupSlash) {
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
func rearmOccupiedClass(err error) bool {
	return errors.Is(err, fsutil.ErrPublishCollision) || errors.Is(err, fsutil.ErrPublishNoReplaceUnsupported)
}

// rearmReplacementBackup recreates the backup from the destination's bytes
// when a journal consumption fails AFTER the backup was removed, restoring
// the armed retry posture (callers keep info's permission bits and mtime).
// Wave-10 codex follow-up: the destination used to be copied via a plain
// fs.Open — an attacker swapping dest for a symlink in the removal→re-arm
// window got a protected file copied into the media-dir backup, armed for a
// later restore (privilege escalation). The open now runs through the same
// no-follow + regular-file + identity discipline as the restore source open
// (openRearmSource) and the copy streams from THAT handle.
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
	if err := copyRearmSourceBytes(fs, src, osBackup); err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	// Codex P2 (w14): the re-armed backup must carry the ORIGINAL ownership
	// too — otherwise a privileged sweep/revert whose consumption update
	// failed would leave a Javinizer-owned backup, and the next retry's
	// copyRestoreBytes would derive uid/gid from it, permanently losing the
	// original owner once the backup is consumed. Best-effort semantics ride
	// the helper (EPERM swallowed; windows no-op), same as the restore path.
	restoreStagingOwnershipFn(fs, osBackup, info)
	if err := fs.Chmod(osBackup, info.Mode().Perm()); err != nil {
		return err
	}
	return fs.Chtimes(osBackup, info.ModTime(), info.ModTime())
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

// persistRestorePendingMarker durably records the RestorePending cleanup
// marker for the journaled backup through the row's journal transaction. The
// caller MUST already hold the row's SharedJournalLocks lock — the sweep legs
// below run their whole journal section under it. The reverter's equivalent
// outside the journal lock is markReplacementEntryRestorePending, which
// acquires the lock itself (the registry is not reentrant).
func (s *ReplacementSweeper) persistRestorePendingMarker(ctx context.Context, rowID uint, backupSlash string) error {
	return s.repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !markReplacementRestorePending(&gf, backupSlash) {
			return gf, false, nil
		}
		return gf, true, nil
	})
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
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	idx, err := s.index(ctx)
	if err != nil {
		return 0, fmt.Errorf("replacement sweep: row scan failed: %w", err)
	}
	healed := 0
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
		for _, e := range entries {
			if e.IsDir() || !IsReplacementBackupName(e.Name()) {
				continue
			}
			healed += s.sweepOne(ctx, idx, dir, e)
		}
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
	// codex P2: same Lstat-first classification as restoreAndConsume — an
	// unjournaled marker-shaped file is the more dangerous case for the
	// Stat-misclassification: Stat on a dangling symlink reports ENOENT, and
	// copyRestoreBytes would then REPLACE THE LINK OBJECT, destroying a
	// directory entry no journal rows describe. Lstat success (any mode) means
	// the destination is present and the conservative retain leg applies.
	_, lstatErr := lstatRestoreSource(s.fs, dest)
	if errors.Is(lstatErr, afero.ErrFileNotFound) {
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
func (s *ReplacementSweeper) restoreAndConsume(ctx context.Context, row *models.BatchFileOperation, backup, dest, backupSlash string) bool {
	release := fsutil.SharedDestLocks().Acquire(dest)
	defer release()

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
		return s.retryPendingRemoval(ctx, row.ID, backup, dest, backupSlash)
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
	// Wave-16 (codex P2): the destination was proven ABSENT above, so the
	// restore publishes no-replace — a foreign writer claiming the name
	// mid-window collides into this kept/warn leg (typed
	// fsutil.ErrPublishCollision) with the racer's bytes intact; on collision
	// the backup is retained and the journal entry is NOT consumed (the
	// removal and consumption below never run).
	if rnErr := copyRestoreBytesNoReplace(s.fs, backup, dest); rnErr != nil {
		logging.Warnf("replacement sweep restore %s→%s: %v", backup, dest, rnErr)
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
	undoRestore := func() {
		if rmErr := s.fs.Remove(dest); rmErr != nil {
			logging.Warnf("replacement sweep %s: restore undo failed: %v", backup, rmErr)
		}
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
		undoRestore()
		return false
	}
	if !entryPresent {
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
	backupInfo, _ := lstatRestoreSource(s.fs, backup)
	if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
		s.rememberPendingRemoval(backupSlash)
		markErr := s.persistRestorePendingMarker(ctx, row.ID, backupSlash)
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

	uErr := s.repo.UpdateJournalInTx(ctx, row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
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
	})
	if uErr != nil {
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
			s.rememberPendingRemoval(backupSlash)
			if markErr := s.persistRestorePendingMarker(ctx, row.ID, backupSlash); markErr != nil {
				logging.Warnf("replacement sweep %s: consumption failed (%v), re-arm failed (%v), and restore-pending persistence failed (%v) — restored destination retained, backup path untouched", backup, uErr, rearmErr, markErr)
			} else {
				logging.Warnf("replacement sweep %s: consumption failed (%v) and re-arm failed (%v) — restored destination retained, cleanup marked restore-pending", backup, uErr, rearmErr)
			}
			return false
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
// os.IsNotExist), reading and writing through the same journal transaction
// every other row mutator uses (review 4960250562).
func (s *ReplacementSweeper) retryPendingRemoval(ctx context.Context, rowID uint, backup, dest, backupSlash string) bool {
	jrel := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(rowID)))
	defer jrel()
	var rowReverted, targetFound, authorized bool
	var fnErr error
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
		if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
			return false
		}
		s.forgetPendingRemoval(backupSlash)
		return true
	}
	if !authorized {
		return false
	}
	// Capture metadata without following a swapped-in symlink; consumption
	// failure below must recreate the original permission bits and timestamps.
	backupInfo, _ := lstatRestoreSource(s.fs, backup)
	if rmErr := removeReplacementBackup(s.fs, backup, "replacement sweep"); rmErr != nil {
		s.rememberPendingRemoval(backupSlash)
		return false
	}
	uErr := s.repo.UpdateJournalInTx(ctx, rowID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
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
	})
	if uErr != nil {
		if rearmErr := rearmReplacementBackup(s.fs, dest, backup, backupInfo); rearmErr != nil {
			// codex P2 (PR#215 round 18): identical recovery contract as
			// restoreAndConsume's consumption-failure leg. The destination
			// already holds the restored bytes and never gets undone here, so
			// the entry must not stay ARMED against a backup name the re-arm
			// could not reclaim (a foreign occupant, or any other cause):
			// persist the durable RestorePending marker so the retry runs only
			// removal + consumption. The marker merge is a no-op when the entry
			// already carries the marker (the usual entry point into this leg).
			if markErr := s.persistRestorePendingMarker(ctx, rowID, backupSlash); markErr != nil {
				absoluteBackup, _ := filepath.Abs(backup)
				logging.Warnf("replacement sweep failed to re-arm backup %s after pending cleanup failure (%v), re-arm failed (%v), and restore-pending persistence failed (%v) — restored destination retained, backup path untouched", absoluteBackup, uErr, rearmErr, markErr)
			} else {
				absoluteBackup, _ := filepath.Abs(backup)
				logging.Warnf("replacement sweep failed to re-arm backup %s after pending cleanup failure: %v — entry marked restore-pending", absoluteBackup, rearmErr)
			}
		}
		s.rememberPendingRemoval(backupSlash)
		logging.Warnf("replacement sweep %s: pending cleanup consumption failed: %v", backup, uErr)
		return false
	}
	s.forgetPendingRemoval(backupSlash)
	return true
}
