package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"

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
	return r.BaseRepository.Create(ctx, op)
}

// CreateBatch inserts multiple batch file operation records in a single transaction.
func (r *BatchFileOperationRepository) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
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
	updates := map[string]any{
		"revert_status": status,
		"updated_at":    time.Now().UTC(),
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = time.Now().UTC()
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), err)
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
func (r *BatchFileOperationRepository) Update(ctx context.Context, op *models.BatchFileOperation) error {
	if err := r.GetDB().WithContext(ctx).Save(op).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
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
// '\\' pairs. At most two extra patterns are generated; spellings that
// rewrite to the caller's own bytes are dropped (separator-free destinations
// yield exactly the wave-7 single pattern).
func destinationLikePatterns(destination string) []string {
	patterns := make([]string, 0, 3)
	patterns = append(patterns, destinationLikePattern(destination))
	if strings.ContainsAny(destination, `/\`) {
		if v := strings.ReplaceAll(destination, "/", `\`); v != destination {
			patterns = append(patterns, destinationLikePattern(v))
		}
		if v := strings.ReplaceAll(destination, `\`, "/"); v != destination {
			patterns = append(patterns, destinationLikePattern(v))
		}
	}
	return patterns
}

// findDestinationCandidates is the bounded cross-form fallback for
// FindOperationsByDestination: it applies the escaped destination prefilter
// (plus its separator cross-spellings — see destinationLikePatterns) to
// generated_files AND new_path so only candidate rows are hydrated, then
// leaves exact matching to the caller's in-process pass. SQLite LIKE folds
// ASCII case, so differently-cased spellings of one destination still reach
// the normalized comparison; the new_path clause keeps rows discoverable
// through the organized media path recorded by the same organizer computation
// when their journal carries an equivalent-but-differently-spelled entry.
func (r *BatchFileOperationRepository) findDestinationCandidates(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	patterns := destinationLikePatterns(destination)
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
