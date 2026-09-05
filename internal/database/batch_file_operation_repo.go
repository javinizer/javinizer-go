package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/fsutil"

	"golang.org/x/text/unicode/norm"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// BatchFileOperationRepository persists batch file operation records used for revert tracking.
type BatchFileOperationRepository struct {
	*BaseRepository[models.BatchFileOperation, uint]
}

// NewBatchFileOperationRepository returns a repository backed by db for batch file operations.
func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{
		BaseRepository: NewBaseRepository[models.BatchFileOperation, uint](
			db, "batch file operation",
			func(op models.BatchFileOperation) string { return fmt.Sprintf("%d", op.ID) },
			WithNewEntity[models.BatchFileOperation, uint](func() models.BatchFileOperation { return models.BatchFileOperation{} }),
		),
	}
}

// Create inserts a single batch file operation record.
func (r *BatchFileOperationRepository) Create(ctx context.Context, op *models.BatchFileOperation) error {
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureJobWritable(tx, op.BatchJobID); err != nil {
			return err
		}
		return tx.Create(op).Error
	})
	if err != nil {
		return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}

// CreateBatch inserts multiple batch file operation records in a single transaction.
func (r *BatchFileOperationRepository) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
			if err := ensureJobWritable(tx, op.BatchJobID); err != nil {
				return err
			}
			if err := tx.Create(op).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
			}
		}
		return nil
	})
}

// FindByID returns the batch file operation with the given primary key.
func (r *BatchFileOperationRepository) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// FindByBatchJobID returns all file operations for a batch job, ordered by id.
func (r *BatchFileOperationRepository) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return ops, nil
}

// FindByBatchJobIDAndRevertStatus returns a batch job's operations filtered by revert status, ordered by id.
func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, revertStatus models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, revertStatus), err)
	}
	return ops, nil
}

// UpdateRevertStatus sets the revert status of an operation, stamping reverted_at when the status is reverted.
func (r *BatchFileOperationRepository) UpdateRevertStatus(ctx context.Context, id uint, status models.RevertStatusEnum) error {
	now := time.Now().UTC()
	updates := map[string]any{
		"revert_status": status,
		"updated_at":    now,
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = now
	}
	db := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).
		Where("id = ? AND NOT EXISTS (SELECT 1 FROM jobs WHERE jobs.id = batch_file_operations.batch_job_id AND jobs.status = ?)", id, pruningJobStatus)
	result := db.Updates(updates)
	if result.Error != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), result.Error)
	}
	if result.RowsAffected == 0 {
		if err := ensureOperationWritable(r.GetDB().WithContext(ctx), id); err != nil {
			return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), err)
		}
	}
	return nil
}

// CountByBatchJobID returns the number of file operations for a batch job.
func (r *BatchFileOperationRepository) CountByBatchJobID(ctx context.Context, batchJobID string) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return count, nil
}

// CountByBatchJobIDAndRevertStatus returns the number of operations for a batch job with the given revert status.
func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, status models.RevertStatusEnum) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, status), err)
	}
	return count, nil
}

// JournalUpdateFn computes the next generated-files ledger for one operation
// row inside UpdateJournalInTx's write transaction. current is the row state
// re-read INSIDE that transaction: ID, GeneratedFiles, and RevertStatus are
// hydrated from the latest committed state; every other field is zero, so
// merge decisions must key off the ledger and status only. Returning
// persist=false commits the transaction untouched (an idempotent no-op); a
// non-nil error rolls it back and propagates verbatim.
type JournalUpdateFn func(current *models.BatchFileOperation) (next models.GeneratedFilesJSON, persist bool, err error)

// UpdateJournalInTx serializes generated-files journal read-modify-writes
// DURABLY, including across processes sharing one SQLite file (review
// 4960250562): the process-local journal locks and per-destination .dlbusy
// markers cannot order two processes updating DIFFERENT destinations of the
// SAME row, so both used to read one GeneratedFiles snapshot and Save
// divergent results — the loser's entries were silently clobbered.
//
// BEGIN IMMEDIATE takes the SQLite write lock up front (the pool's busy
// timeout waits out a concurrent writer), so the row re-read observes every
// previously committed journal mutation, fn merges against that truth, and
// the UPDATE lands atomically with it. A deferred-transaction RMW cannot
// substitute: under WAL, a read-then-write upgrade after a concurrent commit
// dies with SQLITE_BUSY_SNAPSHOT instead of waiting.
//
// ONLY generated_files (+updated_at, Save-parity) is written — concurrent
// non-journal column writes are never clobbered. fn owns the merge
// discipline (armed/installed/pending invariants live with the callers); a
// missing row reports ErrNotFound before fn runs.
func (r *BatchFileOperationRepository) UpdateJournalInTx(ctx context.Context, id uint, fn JournalUpdateFn) error {
	label := fmt.Sprintf("batch file operation %d", id)
	if fn == nil {
		return fmt.Errorf("update journal for %s: merge function must not be nil", label)
	}
	sqlDB, err := r.GetDB().DB.DB()
	if err != nil {
		return wrapDBErr("update journal", label, err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return wrapDBErr("update journal", label, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return wrapDBErr("update journal", label, err)
	}
	committed := false
	defer func() {
		if !committed {
			// ctx may already be cancelled (it is one of the failure legs);
			// release the write lock on a fresh context before the conn re-pools.
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	if err := ensureOperationWritableConn(ctx, conn, id, true); err != nil {
		return err
	}

	var raw sql.NullString
	var status models.RevertStatusEnum
	scanErr := conn.QueryRowContext(ctx,
		"SELECT generated_files, revert_status FROM batch_file_operations WHERE id = ?", id,
	).Scan(&raw, &status)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return fmt.Errorf("update journal for %s: %w", label, ErrNotFound)
	}
	if scanErr != nil {
		return wrapDBErr("update journal", label, scanErr)
	}

	next, persist, fnErr := fn(&models.BatchFileOperation{
		ID:             id,
		GeneratedFiles: raw.String,
		RevertStatus:   status,
	})
	if fnErr != nil {
		return fnErr
	}
	if persist {
		if _, err := conn.ExecContext(ctx,
			"UPDATE batch_file_operations SET generated_files = ?, updated_at = ? WHERE id = ?",
			models.MarshalLedgerJSON(next), time.Now().UTC(), id,
		); err != nil {
			return wrapDBErr("update journal", label, err)
		}
	}
	// COMMIT is branchless: wrapDBErr maps a nil error to nil, and the deferred
	// ROLLBACK above is the compensation for any failure before committed=true.
	_, commitErr := conn.ExecContext(ctx, "COMMIT")
	committed = commitErr == nil
	return wrapDBErr("update journal", label, commitErr)
}

// Update saves all fields of the given batch file operation record.
//
// Callers carrying a generated-files journal MUST NOT use this: a full Save
// rewrites generated_files with whatever snapshot the caller hydrated, so any
// journal mutation committed after that snapshot is clobbered/resurrected.
// The journal column is owned exclusively by UpdateJournalInTx; non-journal
// completion writes go through UpdateNonJournalFields (wave-10 codex
// follow-up).
func (r *BatchFileOperationRepository) Update(ctx context.Context, op *models.BatchFileOperation) error {
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureJobWritable(tx, op.BatchJobID); err != nil {
			return err
		}
		return tx.Save(op).Error
	})
	if err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}

// ErrOperationRowReverted classifies UpdateNonJournalFields' suppressed
// revert-status write (POSTER-WRITE-HARDENING wave-15 codex P1): a concurrent
// writer (UpdateRevertStatus) committed RevertStatusReverted before this
// caller's completion-status update landed. Only the non-status columns
// persisted; the stored reverted state (and its original reverted_at stamp)
// stays authoritative so a stale Applied/Failed snapshot never resurfaces a
// reverted operation as live. Callers treat it as benign: warn through the
// logging seam and finish as if the write succeeded.
var ErrOperationRowReverted = errors.New("batch file operation row already reverted")

// UpdateNonJournalFields persists one row's non-journal columns WITHOUT
// touching generated_files (POSTER-WRITE-HARDENING wave-10 codex follow-up):
// wave-9 moved the journal read-modify-write into UpdateJournalInTx's BEGIN
// IMMEDIATE transaction, but the completion's follow-up full Save re-persisted
// tx-derived journal bytes AFTER the commit — a concurrent append (apply arm)
// or consume (revert/sweep) landing between the transaction commit and that
// Save was silently erased/resurrected. From wave-10 on, generated_files is
// written ONLY by UpdateJournalInTx.
//
// The column set is the full non-journal persisted set (Save-parity); zero
// values persist exactly like Save (the statements bind every listed column
// explicitly — a plain struct Updates would skip false/""/nil fields); the
// primary key and generated_files are excluded. updated_at is stamped like
// Save and UpdateJournalInTx.
//
// Wave-15 (codex P1) makes the revert-status columns CONDITIONAL. Callers
// carry a stale in-memory row, and a concurrent UpdateRevertStatus(Reverted)
// committing between their read and this write was silently overwritten back
// to Applied/Failed — a reverted operation looked live again. The update now
// runs as TWO statements inside one BEGIN IMMEDIATE transaction (same
// cross-process serialization as UpdateJournalInTx): (a) the non-status
// columns unconditionally, then (b) revert_status/reverted_at only WHERE the
// STORED row is not already reverted — the guard is re-evaluated against the
// locked committed row at statement time, never against the caller's
// snapshot. A suppressed (b) while the caller carried a completion status
// reports ErrOperationRowReverted AFTER the commit (columns from (a) stay
// persisted — that is the intended outcome); a suppressed (b) carrying
// Reverted itself is an idempotent no-op, and a missing row keeps the
// wave-10 no-op contract.
func (r *BatchFileOperationRepository) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	if op == nil {
		return fmt.Errorf("update non-journal fields for batch file operation: record must not be nil")
	}
	label := fmt.Sprintf("batch file operation %d", op.ID)
	sqlDB, err := r.GetDB().DB.DB()
	if err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}
	committed := false
	defer func() {
		if !committed {
			// ctx may already be cancelled (it is one of the failure legs);
			// release the write lock on a fresh context before the conn re-pools.
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err := ensureOperationWritableConn(ctx, conn, op.ID, false); err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}

	now := time.Now().UTC()
	// (a) Non-status columns unconditionally — the wave-10 Save-parity set.
	if _, err := conn.ExecContext(ctx,
		`UPDATE batch_file_operations SET batch_job_id = ?, movie_id = ?, original_path = ?, new_path = ?, operation_type = ?, nfo_snapshot = ?, nfo_path = ?, in_place_renamed = ?, original_dir_path = ?, created_at = ?, updated_at = ? WHERE id = ?`,
		op.BatchJobID, op.MovieID, op.OriginalPath, op.NewPath, op.OperationType,
		op.NFOSnapshot, op.NFOPath, op.InPlaceRenamed, op.OriginalDirPath,
		op.CreatedAt, now, op.ID,
	); err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}

	// (b) Revert-status columns only while the stored row is NOT already
	// reverted: a concurrent revert committed first must never be clobbered by
	// the caller's stale completion status.
	res, err := conn.ExecContext(ctx,
		`UPDATE batch_file_operations SET revert_status = ?, reverted_at = ?, updated_at = ? WHERE id = ? AND (revert_status IS NULL OR revert_status <> ?)`,
		op.RevertStatus, op.RevertedAt, now, op.ID, models.RevertStatusReverted,
	)
	if err != nil {
		return wrapDBErr("update non-journal fields", label, err)
	}

	var suppressedErr error
	if affected, affErr := res.RowsAffected(); affErr == nil && affected == 0 {
		// The guard suppressed the status write — or the row vanished. Re-read
		// INSIDE the transaction so the classification observes the same locked
		// state (b) did.
		var stored models.RevertStatusEnum
		scanErr := conn.QueryRowContext(ctx,
			"SELECT revert_status FROM batch_file_operations WHERE id = ?", op.ID,
		).Scan(&stored)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			// Missing row: keep the wave-10 no-op contract (Updates on a
			// missing row affects zero rows without erroring).
		case scanErr != nil:
			return wrapDBErr("update non-journal fields", label, scanErr)
		case stored == models.RevertStatusReverted && op.RevertStatus != models.RevertStatusReverted:
			// Lost the race: the non-status columns commit below and the
			// caller's stale completion status is suppressed.
			suppressedErr = fmt.Errorf("%w: %s (stale %s completion status suppressed; stored row stays reverted)", ErrOperationRowReverted, label, op.RevertStatus)
		default:
			// Stored already reverted and the caller aimed at reverted too:
			// idempotent no-op (the original reverted_at stamp is preserved).
		}
	}

	// COMMIT before surfacing the suppressed classification: the non-status
	// columns persist either way; only the status clobber is refused.
	_, commitErr := conn.ExecContext(ctx, "COMMIT")
	committed = commitErr == nil
	if commitErr != nil {
		return wrapDBErr("update non-journal fields", label, commitErr)
	}
	return suppressedErr
}

// countByBatchJobIDsResult is a GORM scan target for GROUP BY queries.
type countByBatchJobIDsResult struct {
	BatchJobID string `gorm:"column:batch_job_id"`
	Count      int64  `gorm:"column:cnt"`
}

// CountByBatchJobIDs returns a map of jobID→count for all given job IDs in a single query.
func (r *BatchFileOperationRepository) CountByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	if len(jobIDs) == 0 {
		return map[string]int64{}, nil
	}
	var results []countByBatchJobIDsResult
	err := r.GetDB().WithContext(ctx).
		Model(&models.BatchFileOperation{}).
		Select("batch_job_id, count(*) as cnt").
		Where("batch_job_id IN ?", jobIDs).
		Group("batch_job_id").
		Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count_by_batch_job_ids", "batch file operations", err)
	}
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.BatchJobID] = r.Count
	}
	return m, nil
}

// CountRevertedByBatchJobIDs returns a map of jobID→reverted count for all given job IDs.
func (r *BatchFileOperationRepository) CountRevertedByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	if len(jobIDs) == 0 {
		return map[string]int64{}, nil
	}
	var results []countByBatchJobIDsResult
	err := r.GetDB().WithContext(ctx).
		Model(&models.BatchFileOperation{}).
		Select("batch_job_id, count(*) as cnt").
		Where("batch_job_id IN ?", jobIDs).
		Where("revert_status = ?", models.RevertStatusReverted).
		Group("batch_job_id").
		Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count_reverted_by_batch_job_ids", "batch file operations", err)
	}
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.BatchJobID] = r.Count
	}
	return m, nil
}

// CountNoOpByBatchJobIDs returns a map of jobID→noop count for all given job IDs.
// Completed-noop rows (authorized duplicate skips; codex P2, PR #241 F2) are
// terminal and non-revertible — they are counted separately so revertible
// totals can exclude them exactly like reverted rows.
func (r *BatchFileOperationRepository) CountNoOpByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	if len(jobIDs) == 0 {
		return map[string]int64{}, nil
	}
	var results []countByBatchJobIDsResult
	err := r.GetDB().WithContext(ctx).
		Model(&models.BatchFileOperation{}).
		Select("batch_job_id, count(*) as cnt").
		Where("batch_job_id IN ?", jobIDs).
		Where("revert_status = ?", models.RevertStatusNoOp).
		Group("batch_job_id").
		Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count_noop_by_batch_job_ids", "batch file operations", err)
	}
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.BatchJobID] = r.Count
	}
	return m, nil
}

// destinationLikePattern builds the SQL LIKE prefilter pattern matching a
// destination as it appears inside the persisted text columns (ESCAPE '\').
// R5-1: destinations live inside JSON, whose encoder escapes `\` (and
// `"`/controls); matching the RAW path against the JSON column under-matches
// on Windows (`D:\a` is stored as `D:\\a`). Shape the pattern from the JSON
// encoding of the path first, then apply LIKE escaping on top.
func destinationLikePattern(destination string) string {
	jsonEsc := destination
	if enc, err := json.Marshal(destination); err == nil && len(enc) >= 2 {
		jsonEsc = string(enc[1 : len(enc)-1])
	}
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(jsonEsc)
	return "%" + escaped + "%"
}

// FindOperationsByDestination returns every operation whose generated-files
// ledger journals a replacement for destination. SQL LIKE pre-filters
// candidates; entries are matched exactly in-process so path substrings and
// LIKE metacharacters can neither over- nor under-match.
func (r *BatchFileOperationRepository) FindOperationsByDestination(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	likePattern := destinationLikePattern(destination)
	var candidates []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files LIKE ? ESCAPE '\\'", likePattern).
		Order("id ASC").Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations by destination", err)
	}
	// R6-4 + R12-1: match on the probe-aware destination key — backslash
	// separators normalize only under the Windows seam and case folds only on
	// an insensitive/tolerant root. The LIKE prefilter CAN'T see through a form
	// change, so form mismatches fall back to a candidate scan that re-applies
	// the same escaped prefilter to BOTH destination-bearing columns
	// (generated_files and new_path) instead of hydrating every ledger row
	// (P2 review: the unbounded FindOperationsWithLedger scan ran once per
	// destination lookup — per RecordReplacement, per revert conflict check,
	// and per orphan candidate — so large ledgers made every replacement
	// O(rows)). Audit: POSIX journal paths use `/` spellings, and the
	// normalized scan preserves literal backslashes rather than relying on
	// their translation; Windows legacy slash/backslash forms still match.
	norm := fsutil.DestKey
	seam := fallbackSeam{}
	return seam.finish(ctx, candidates, destination, norm, func(ctx2 context.Context) ([]models.BatchFileOperation, error) {
		return r.findDestinationCandidates(ctx2, destination)
	})
}

// hasCaseFoldingNonASCII reports whether s contains a non-ASCII letter whose
// simple Unicode case-fold differs from the letter itself. Those are the ONLY
// runes SQLite's built-in like() cannot fold — it folds ASCII letters only
// (SQLITE_CASE_SENSITIVE_LIKE aside, the default LIKE is ASCII-insensitive),
// so a journal row stored as `…\Ä.jpg` is invisible to an `…/ä.jpg` prefilter
// even though DestKey folds them together on an insensitive root. Pure-ASCII
// destinations need no help: LIKE already folds them, keeping wave-8 behavior.
//
// Wave-20 (codex P2): DestKey's insensitive form now runs FULL Unicode case
// folding (golang.org/x/text/cases.Fold instead of strings.ToLower), so the
// in-process exact matcher also resolves pairs simple lowering never touched
// (final sigma ς≡σ, ß≡ss, …). This gate stays as a conservative superset:
// fallback hydration plus the Fold matcher cover the whole pair space, so no
// tighter trigger (full-fold-only runes) is required — any non-ASCII cased
// letter keeps taking the unfiltered full-ledger leg.
func hasCaseFoldingNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII && (unicode.ToLower(r) != r || unicode.ToUpper(r) != r) {
			return true
		}
	}
	return false
}

// hasNormalizationVariants reports whether s differs from EITHER canonical
// normalization form (NFC or NFD). Wave-26 (codex P2, PR#215): the wave-16
// fallback gate checks cased non-ASCII letters only, but SQLite's LIKE also
// folds nothing about canonical decomposition — a destination carrying a
// DECOMPOSED spelling (e.g. `e` + COMBINING ACUTE for é-NFD) is pure ASCII
// cased letters plus a combining MARK (no case fold), so the gate stayed
// dark while the prefilter LIKE'd the literal NFD bytes against a journal
// stored in NFC: the row never hydrated and chain/conflict logic went blind
// exactly like the wave-16 failure. Any destination whose own
// normalization is unstable — an NFD spelling contains composable pairs
// (NFC(s) ≠ s), an NFC spelling contains precomposed runes (NFD(s) ≠ s) —
// therefore takes the unfiltered full-ledger fallback too: DestKey NFC-
// canonicalizes both spellings of one root-relative name on normalization-
// insensitive roots (fsutil.IsNormalizationInsensitiveRoot), so the
// in-process exact matcher equates what the SQL LIKE cannot. Pure-ASCII
// destinations are already NFC/NFD-stable and keep the bounded patterns.
func hasNormalizationVariants(s string) bool {
	return norm.NFC.String(s) != s || norm.NFD.String(s) != s
}

// destinationLikePatterns returns the bounded LIKE-pattern set for the
// fallback's prefilter: the caller's spelling plus, when the destination
// contains a path separator, BOTH cross-spellings ('/'↔'\\'). Wave-8 codex
// P2 follow-up: the wave-7 fallback keyed on the caller's literal spelling
// only, so a Windows journal written with backslashes was invisible to a
// caller supplying forward slashes (and vice versa) — the exact matcher
// accepts either form under the Windows separator seam, but the SQL prefilter
// never fetched the row. Separator rewriting happens on the RAW path and
// destinationLikePattern re-applies JSON-shaping + LIKE-escaping AFTER each
// rewrite — rewriting an escaped pattern instead would corrupt the escape
// '\\' pairs.
//
// Wave-16 codex P2 follow-up (supersedes wave-9's ToLower/ToUpper variant
// layering): when the destination contains a NON-ASCII cased letter
// (hasCaseFoldingNonASCII) the bounded set CANNOT cover the stored spelling
// space. All-lower and all-upper variants only rescue all-lower and all-upper
// STORED spellings — a mixed-case non-ASCII journal spelling (stored `äÖ`
// against a queried `ÄÖ`) is invisible to every bounded variant set because
// SQLite's LIKE folds ASCII only, and enumerating per-letter case spellings
// is exponential in the destination's cased non-ASCII letters. Hiding a live
// chain behind an incomplete prefilter corrupts sequence allocation and
// revert conflict checks, so the pattern generator returns NIL for those
// destinations and the caller takes the pre-wave-7 unfiltered full-ledger
// scan; correctness never again depends on variant completeness. Pure-ASCII
// destinations (and non-cased runes) keep the wave-8 byte-identical patterns
// and separator cross-spellings — spellings that rewrite to the caller's own
// bytes are dropped.
//
// Wave-20 (codex P2): the full-ledger fallback pairs with DestKey's
// cases.Fold exact matcher, so final-sigma-class pairs (stored ς-spelling,
// queried σ-spelling — ToLower kept them distinct) are FOUND through this
// fallback leg; the nil-pattern gate itself is unchanged.
func destinationLikePatterns(destination string) []string {
	if hasCaseFoldingNonASCII(destination) || hasNormalizationVariants(destination) {
		// See the doc comment: bounded case variants cannot be complete for
		// foldable non-ASCII destinations, and (wave-26, codex P2) byte-LIKE
		// cannot cross canonical-decomposition spellings at all, so either
		// class takes the safe full-ledger fallback (the pre-wave-7
		// unfiltered scan).
		return nil
	}
	patterns := make([]string, 0, 3)
	seen := map[string]bool{}
	spellings := []string{destination}
	if strings.ContainsAny(destination, `/\`) {
		if v := strings.ReplaceAll(destination, "/", `\`); v != destination {
			spellings = append(spellings, v)
		}
		if v := strings.ReplaceAll(destination, `\`, "/"); v != destination {
			spellings = append(spellings, v)
		}
	}
	for _, spelling := range spellings {
		p := destinationLikePattern(spelling)
		if !seen[p] {
			seen[p] = true
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// findDestinationCandidates is the bounded cross-form fallback for
// FindOperationsByDestination: it applies the escaped destination prefilter
// (plus its separator cross-spellings — see destinationLikePatterns) to
// generated_files AND new_path so only candidate rows are hydrated, then
// leaves exact matching to the caller's in-process pass. SQLite LIKE folds
// ASCII case only, so a destination carrying a foldable non-ASCII letter gets
// NO patterns (wave-16, see destinationLikePatterns) and this falls back to
// the un-prefiltered full-ledger scan — a live mixed-case chain can never
// hide behind an incomplete variant set. The new_path clause keeps rows
// discoverable through the organized media path recorded by the same
// organizer computation when their journal carries an equivalent-but-
// differently-spelled entry.
func (r *BatchFileOperationRepository) findDestinationCandidates(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	patterns := destinationLikePatterns(destination)
	if patterns == nil {
		// The destination carries a foldable non-ASCII letter: the prefilter
		// cannot safely narrow it, so hydrate the full ledger (the pre-wave-7
		// path) and let the caller's in-process exact matcher decide.
		return r.FindOperationsWithLedger(ctx)
	}
	conds := make([]string, 0, len(patterns))
	args := make([]any, 0, len(patterns)*2)
	for _, p := range patterns {
		conds = append(conds, "(generated_files LIKE ? ESCAPE '\\' OR new_path LIKE ? ESCAPE '\\')")
		args = append(args, p, p)
	}
	var rows []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where(strings.Join(conds, " OR "), args...).
		Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations by destination candidates", err)
	}
	return rows, nil
}

// fallbackSeam factors the form-mismatch fallback for direct testing: the
// prefilter match runs first; the union defers to the (P2 review: bounded)
// candidate scan, whose error MUST surface (R7-2) rather than masquerade as
// absence.
type fallbackSeam struct{}

func (fallbackSeam) finish(ctx context.Context, candidates []models.BatchFileOperation, destination string, norm func(string) string, ledgerScan func(context.Context) ([]models.BatchFileOperation, error)) ([]models.BatchFileOperation, error) {
	matched := matchOpsByDestination(candidates, destination, norm)
	// R14-1 + R10-1: ALWAYS union the normalized candidate scan — the SQL
	// prefilter keys on the caller's spelling, so a same-destination row
	// journaled under a DIFFERENT-but-equivalent spelling can hide behind a
	// partial prefilter hit and vanish from chain checks. The scan is
	// prefiltered itself now (P2 review), so the union stays cheap. Caller
	// context (deadline) rides the scan; scan failures surface, never
	// masquerade.
	fallback, ferr := ledgerScan(ctx)
	if ferr != nil {
		return nil, wrapDBErr("find", "batch file operations by destination fallback", ferr)
	}
	seen := map[uint]bool{}
	for i := range matched {
		seen[matched[i].ID] = true
	}
	for _, op := range matchOpsByDestination(fallback, destination, norm) {
		if !seen[op.ID] {
			matched = append(matched, op)
			seen[op.ID] = true
		}
	}
	return matched, nil
}

// matchOpsByDestination keeps rows journaling the destination, compared under
// the caller's normalizer.
func matchOpsByDestination(ops []models.BatchFileOperation, destination string, norm func(string) string) []models.BatchFileOperation {
	want := norm(destination)
	matched := make([]models.BatchFileOperation, 0, len(ops))
	for _, op := range ops {
		gf, perr := models.ParseGeneratedFiles(op.GeneratedFiles)
		if perr != nil {
			continue // unparsable legacy rows never match a destination journal
		}
		for _, rep := range gf.Replacements {
			if norm(rep.Destination) == want {
				matched = append(matched, op)
				break
			}
		}
	}
	return matched
}

// FindOperationsWithReplacements returns every operation whose generated-files
// ledger journals at least one replacement entry (any revert status — the
// sweeper must see applied and failed rows alike).
func (r *BatchFileOperationRepository) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	var candidates []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files LIKE ?", "%\"replacements\"%").
		Order("id ASC").Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations with replacements", err)
	}
	matched := make([]models.BatchFileOperation, 0, len(candidates))
	for _, op := range candidates {
		gf, perr := models.ParseGeneratedFiles(op.GeneratedFiles)
		if perr != nil {
			continue
		}
		if len(gf.Replacements) > 0 {
			matched = append(matched, op)
		}
	}
	return matched, nil
}

// FindOperationsWithLedger returns every operation carrying a non-empty
// generated-files ledger of any shape.
func (r *BatchFileOperationRepository) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	var rows []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files IS NOT NULL AND generated_files <> ''").
		Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations with ledger", err)
	}
	return rows, nil
}
