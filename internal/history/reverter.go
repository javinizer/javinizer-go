package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// GeneratedFilesJSON and fileMove moved to internal/models/revert_types.go.
// The history package imports models.GeneratedFilesJSON and models.FileMove instead of
// defining duplicate types.

// BatchReverter is the narrow interface for batch revert operations.
// Per D-10: decoupled from the concrete *Reverter so that callers (API handlers,
// CLI commands) depend on behavior, not implementation. The concrete Reverter
// satisfies this interface implicitly.
//
// This interface lives in the history package alongside the concrete type,
// following the Go convention of defining interfaces where they're implemented
// when the interface represents a core domain behavior.
type BatchReverter interface {
	RevertBatch(ctx context.Context, batchJobID string) (*RevertBatchResult, error)
	RevertScrape(ctx context.Context, batchJobID string, movieID string) (*RevertBatchResult, error)
}

// RevertBatchResult summarizes the outcome of a batch-level revert.
type RevertBatchResult struct {
	Total     int                // Total operations processed
	Succeeded int                // Successfully reverted
	Skipped   int                // Skipped (e.g., anchor missing)
	Failed    int                // Failed to revert
	Outcomes  []RevertFileResult // Per-operation outcomes (includes skipped and failed)
}

// RevertFileResult records a per-operation revert outcome with reason tracking (D-06).
type RevertFileResult struct {
	OperationID  uint   // BatchFileOperation.ID
	MovieID      string // Movie identifier
	OriginalPath string
	NewPath      string
	Outcome      models.RevertOutcomeEnum // RevertOutcome: reverted, skipped, or failed
	Reason       models.RevertReasonEnum  // RevertReason: why the outcome occurred (empty for success)
	Error        string                   // Error message for failed outcomes

	// orderRetryable marks cross-operation ORDER rejections: the run retries
	// the operation once its blocker's journal entries are consumed (R6-1).
	orderRetryable bool
}

var (
	// ErrBatchAlreadyReverted is returned when a batch or operation is already reverted.
	ErrBatchAlreadyReverted = errors.New("batch already reverted")
	// ErrNoOperationsFound is returned when no operations exist for the given batch.
	ErrNoOperationsFound = errors.New("no operations found for batch")

	// canonicalizeNFOPathFunc is a narrow seam for the soft NFO restore fallback.
	// filepath.Abs normally succeeds for ordinary test paths, so forcing its
	// error without changing the process working directory is otherwise brittle.
	canonicalizeNFOPathFunc = fsutil.CanonicalizePath
)

// fileSystemReverter abstracts filesystem revert operations so that Reverter.revertFile
// becomes a thin orchestrator. Production code uses the afero-based implementation;
// tests can inject a mock to verify orchestration without touching the filesystem.
//
// Per W-2: cleanupEmptyDir, cleanupGeneratedFiles, and restoreNFO are methods on
// this interface so that RevertBatch/RevertScrape/revertFile call through the seam
// instead of reaching for the standalone FS functions directly. The standalone
// functions (cleanupEmptyDirFS, cleanupGeneratedFilesFS, RestoreNFO) become
// unexported helpers used only by aferoFSReverter.
type fileSystemReverter interface {
	// revertPrimaryFile moves the primary file back to its original location.
	revertPrimaryFile(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error)
	// cleanupGeneratedFiles removes generated artifacts (NFO, images) for an operation.
	cleanupGeneratedFiles(op *models.BatchFileOperation, stopAt string)
	// cleanupEmptyDir removes empty directories, walking up to stopAt.
	cleanupEmptyDir(dirPath string, stopAt string)
	// restoreNFO restores the NFO snapshot for a reverted operation.
	// Returns a warning string (soft failure) or a failed RevertFileResult (hard failure).
	restoreNFO(ctx context.Context, op *models.BatchFileOperation, hardFailure bool) (string, *RevertFileResult)
}

// Reverter handles reverting file organization operations.
// It reads BatchFileOperation records from the database, performs inverse file
// operations via afero, and tracks per-operation revert status.
type Reverter struct {
	fs              afero.Fs
	batchFileOpRepo database.BatchFileOperationRepositoryInterface
	fsReverter      fileSystemReverter  // filesystem operations seam
	sweeper         *ReplacementSweeper // P3 crash-window sweep before revert
}

// failRevert records a failed revert in the database and returns a RevertFileResult
// with the given reason and error message. The DB status update is best-effort;
// a DB failure is logged but does not override the revert result.
func failRevert(ctx context.Context, batchFileOpRepo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, reason models.RevertReasonEnum, errMsg string) *RevertFileResult {
	if dbErr := batchFileOpRepo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusFailed); dbErr != nil {
		logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
	}
	return &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeFailed,
		Reason:       reason,
		Error:        errMsg,
	}
}

// skipRevert returns a RevertFileResult indicating the revert was skipped with
// the given reason. No DB status update is performed — skipped operations remain
// in their current status so they can be retried if the anchor reappears.
func (r *Reverter) skipRevert(op *models.BatchFileOperation, reason models.RevertReasonEnum) *RevertFileResult {
	return &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeSkipped,
		Reason:       reason,
	}
}

// NewReverter creates a new Reverter with the given filesystem and repository.
func NewReverter(fs afero.Fs, batchFileOpRepo database.BatchFileOperationRepositoryInterface) *Reverter {
	r := &Reverter{
		fs:              fs,
		batchFileOpRepo: batchFileOpRepo,
	}
	r.fsReverter = &aferoFSReverter{fs: fs, batchFileOpRepo: batchFileOpRepo}
	if fs != nil && batchFileOpRepo != nil {
		r.sweeper = NewReplacementSweeper(fs, batchFileOpRepo)
	}
	return r
}

// aferoFSReverter implements fileSystemReverter using the afero filesystem.
// This is the production implementation; tests can substitute a mock.
type aferoFSReverter struct {
	fs              afero.Fs
	batchFileOpRepo database.BatchFileOperationRepositoryInterface
}

func (a *aferoFSReverter) revertPrimaryFile(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	// Delegate to the existing standalone function, routing cleanup through the seam
	return revertPrimaryFileFS(ctx, a.fs, a.batchFileOpRepo, op, a.cleanupEmptyDir)
}

func (a *aferoFSReverter) cleanupGeneratedFiles(op *models.BatchFileOperation, stopAt string) {
	cleanupGeneratedFilesFS(a.fs, op, stopAt)
}

func (a *aferoFSReverter) cleanupEmptyDir(dirPath string, stopAt string) {
	cleanupEmptyDirFS(a.fs, dirPath, stopAt)
}

func (a *aferoFSReverter) restoreNFO(ctx context.Context, op *models.BatchFileOperation, hardFailure bool) (string, *RevertFileResult) {
	return restoreNFOFS(ctx, a.fs, a.batchFileOpRepo, op, hardFailure)
}

// operationRevertLockRegistry serializes the complete revert flow for one
// operation, including journal replay and the primary file move. The keyed
// registry keeps unrelated operations parallel; active is only contention
// accounting because it is incremented before the keyed lock is acquired.
type operationRevertLockRegistry struct {
	registry *fsutil.KeyedLockRegistry
	mu       sync.Mutex
	active   map[string]int

	// Optional synchronization seams make the active-count/keyed-lock gap
	// deterministic in concurrency regression tests without changing the
	// production lock order.
	afterActiveIncrement func(key string)
	afterAcquire         func(key string, waited bool)
}

func newOperationRevertLockRegistry() *operationRevertLockRegistry {
	return &operationRevertLockRegistry{
		registry: fsutil.NewKeyedLockRegistry(),
		active:   make(map[string]int),
	}
}

func (r *operationRevertLockRegistry) acquire(key string) (func(), bool) {
	r.mu.Lock()
	waited := r.active[key] > 0
	r.active[key]++
	r.mu.Unlock()

	if r.afterActiveIncrement != nil {
		r.afterActiveIncrement(key)
	}
	releaseKey := r.registry.Acquire(key)
	if r.afterAcquire != nil {
		r.afterAcquire(key, waited)
	}
	return func() {
		releaseKey()
		r.mu.Lock()
		r.active[key]--
		if r.active[key] == 0 {
			delete(r.active, key)
		}
		r.mu.Unlock()
	}, waited
}

// revertOperationLocks is process-wide so separate Reverter instances (for
// example, concurrent API requests) still serialize the same operation.
var revertOperationLocks = newOperationRevertLockRegistry()

func (r *Reverter) revertFile(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	release, _ := revertOperationLocks.acquire(fmt.Sprintf("rev-op:%d", op.ID))
	defer release()

	// Refresh after every keyed-lock acquisition. The active count is bumped
	// before registry.Acquire, so a caller that reports waited=false can still
	// have been preempted while another caller acquired the keyed lock first.
	// The fresh row is authoritative for both resume and already-reverted
	// abort paths before any journal, primary, cleanup, or NFO leg runs.
	fresh, err := r.batchFileOpRepo.FindByID(ctx, op.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read operation %d after acquiring revert lock: %w", op.ID, err)
	}
	if fresh == nil {
		return nil, fmt.Errorf("failed to re-read operation %d after acquiring revert lock: row not found", op.ID)
	}
	*op = *fresh

	logging.Debugf("Reverting operation %d: movie=%s type=%s original=%s new=%s revert_status=%s",
		op.ID, op.MovieID, op.OperationType, op.OriginalPath, op.NewPath, op.RevertStatus)

	if result, err := r.guardDoubleRevert(ctx, op); result != nil || err != nil {
		return result, err
	}

	// only a hydrated pending intent gets the unexecuted-intent settle below.
	hydratedPendingIntent := false
	if op.NewPath == "" && op.OperationType == models.OperationTypeMove {
		// A deferred-move row that crashed between publish and completion
		// carries its primary endpoints ONLY as a pending MoveBack intent:
		// hydrate BEFORE classification/replay — the replacement restore must
		// never overwrite the freshly moved destination with its pre-overwrite
		// backup (that would clobber the moved source's only remaining copy).
		pendingMoveIntentAnchor(op)
		hydratedPendingIntent = op.NewPath != ""
	}

	// P3: replay the replacement journal BEFORE the anchor check AND before
	// any operation-type leg (codex P3 R2-1/R6-in-2): a deleted primary
	// anchor must not strand independently recoverable overwritten media —
	// least of all for copy-mode operations whose only forward-recoverable
	// state IS the journal. Restored destinations are structurally excluded
	// from the generated-file Delete list. Order rejections are tagged
	// retryable for this run's fixpoint.
	primaryReplacement := op.OperationType == models.OperationTypeMove && replacementJournalContainsDestination(op, op.NewPath)
	restored, rejErr := r.restoreReplacementJournalWhere(ctx, op, func(destination string) bool {
		return !primaryReplacement || filepath.Clean(destination) != filepath.Clean(op.NewPath)
	})
	if rejErr != nil {
		logging.Warnf("Replacement journal restore refused/failed for op %d: %v", op.ID, rejErr)
		return rejectedRevert(op, rejErr).withRetryable(rejErr), nil
	}
	if len(restored) > 0 {
		logging.Debugf("Reverted %d journaled replacement(s) for op %d ahead of the %s leg", len(restored), op.ID, op.OperationType)
	}

	// Only after the journal replay may an unexecuted intent settle a no-op,
	// because a crash-severed overwrite has already restored its prior bytes by
	// now — destination-absent + source-present proves the move never ran.
	if hydratedPendingIntent && op.OperationType == models.OperationTypeMove {
		_, dstErr := r.fs.Stat(op.NewPath)
		// The source probe must NOT follow symlinks: a dangling symlink at the
		// source pathname is an existing directory entry the move never
		// consumed, and plain Stat would misread it as absent — the row then
		// skips the anchor check unsettled and every retry stays incomplete.
		_, srcErr := lstatRestoreSource(r.fs, op.OriginalPath)
		if os.IsNotExist(dstErr) && srcErr == nil {
			// The replay exclusion above stays correct OUTSIDE this guard: a
			// hydrated intent whose destination is present can mean the publish
			// DID land, and restoring the backup there would clobber the moved
			// source's only remaining copy. Inside this guard the absent
			// destination + present source prove the publish never ran, so the
			// excluded entry is exactly the stranded forced-overwrite backup:
			// restore it BEFORE the no-op verdict, or the terminal settle leaves
			// the user's prior bytes at the backup path with the row no longer
			// retryable.
			if primaryReplacement {
				primaryRestored, restoreErr := r.restoreReplacementJournalWhere(ctx, op, func(destination string) bool {
					return filepath.Clean(destination) == filepath.Clean(op.NewPath)
				})
				if restoreErr != nil {
					return rejectedRevert(op, restoreErr).withRetryable(restoreErr), nil
				}
				for path := range primaryRestored {
					restored[path] = true
				}
			}
			if uerr := r.batchFileOpRepo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusNoOp); uerr != nil {
				return failRevert(ctx, r.batchFileOpRepo, op, models.RevertReasonUnexpectedPathState, fmt.Sprintf("settle unexecuted pending intent for op %d: %v", op.ID, uerr)), nil
			}
			op.RevertStatus = models.RevertStatusNoOp
			return &RevertFileResult{OperationID: op.ID, MovieID: op.MovieID, OriginalPath: op.OriginalPath, NewPath: op.NewPath, Outcome: models.RevertOutcomeSkipped, Reason: models.RevertReasonAnchorMissing}, nil
		}
	}

	// Journal replay may have refreshed a stale caller snapshot. Re-check the
	// status before touching the primary path so a request that entered just
	// after another request finished cannot run the primary revert twice.
	if result, err := r.guardDoubleRevert(ctx, op); result != nil || err != nil {
		return result, err
	}

	isUpdate := op.OperationType == models.OperationTypeUpdate

	// codex P2 (PR #241 F1): anchor semantics apply ONLY to the legs that
	// address the primary — the move-back (installed primary returns to the
	// source tree) and update rows (OriginalPath is where artifacts are
	// regenerated). Copy/hardlink/symlink rows never gave up their primary:
	// the installed destination is the user's own copy, so its absence
	// (user deleted it after organizing) must NOT anchor-gate the
	// GeneratedFiles cleanup — gating here orphaned every journaled copied
	// subtitle/NFO/download behind an anchor_missing skip. Those rows run
	// cleanup independently of the primary anchor below.
	if op.OperationType == models.OperationTypeMove || isUpdate {
		if primaryReplacement {
			if _, statErr := r.fs.Stat(op.NewPath); os.IsNotExist(statErr) {
				primaryRestored, restoreErr := r.restoreReplacementJournalWhere(ctx, op, func(destination string) bool {
					return filepath.Clean(destination) == filepath.Clean(op.NewPath)
				})
				if restoreErr != nil {
					return rejectedRevert(op, restoreErr).withRetryable(restoreErr), nil
				}
				for path := range primaryRestored {
					restored[path] = true
				}
				if err := r.batchFileOpRepo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted); err != nil {
					return nil, fmt.Errorf("prior destination restored but failed to persist revert status for op %d: %w", op.ID, err)
				}
				op.RevertStatus = models.RevertStatusReverted
				return &RevertFileResult{OperationID: op.ID, MovieID: op.MovieID, OriginalPath: op.OriginalPath, NewPath: op.NewPath, Outcome: models.RevertOutcomeReverted, Error: "installed primary was missing; prior destination restored, source could not be reconstructed"}, nil
			}
		}
		if result, err := r.checkAnchor(ctx, op); result != nil || err != nil {
			return result, err
		}
	}

	switch op.OperationType {
	case models.OperationTypeUpdate:
		r.fsReverter.cleanupGeneratedFiles(op, filepath.Dir(filepath.Dir(op.OriginalPath)))
	case models.OperationTypeMove:
		if result, err := r.fsReverter.revertPrimaryFile(ctx, op); result != nil || err != nil {
			return result, err
		}
		if primaryReplacement {
			primaryRestored, restoreErr := r.restoreReplacementJournalWhere(ctx, op, func(destination string) bool {
				return filepath.Clean(destination) == filepath.Clean(op.NewPath)
			})
			if restoreErr != nil {
				return rejectedRevert(op, restoreErr).withRetryable(restoreErr), nil
			}
			for path := range primaryRestored {
				restored[path] = true
			}
		}
		destRoot := filepath.Dir(filepath.Dir(op.NewPath))
		r.fsReverter.cleanupGeneratedFiles(op, destRoot)
		if !op.InPlaceRenamed {
			r.fsReverter.cleanupEmptyDir(filepath.Dir(op.NewPath), destRoot)
		}
	case models.OperationTypeCopy, models.OperationTypeHardlink, models.OperationTypeSymlink:
		// codex P2 (PR #241 F2): copy/link ops never gave up their source —
		// the forward install was non-destructive — so there is no primary
		// move-back to perform (the retired blanket rejection here also
		// stranded their journals). Their generated-files ledger DOES carry
		// Delete entries whose ONLY consumer is cleanupGeneratedFiles below:
		// the NFO, the downloads, and COPIED subtitles (workflow/revert_log.go
		// journals a copy-installed subtitle into Delete precisely because
		// its source survives — reverting deletes the installed copy, never
		// the source). Deleting exactly those artifacts is the complete,
		// correct, non-destructive inverse: sources stay, the installed
		// primary stays (it is the user's copy), and the move leg above
		// keeps its full move-back semantics untouched. The leg is
		// deliberately anchorless (codex P2, PR #241 F1): a user-deleted
		// installed primary skips nothing — this cleanup still runs.
		r.fsReverter.cleanupGeneratedFiles(op, filepath.Dir(filepath.Dir(op.NewPath)))
	default:
		return failRevert(ctx, r.batchFileOpRepo, op, models.RevertReasonUnexpectedPathState, fmt.Sprintf("unknown operation type %q cannot be reverted", op.OperationType)), nil
	}

	nfoWarning, failedResult := r.fsReverter.restoreNFO(ctx, op, isUpdate)
	if failedResult != nil {
		return failedResult, nil
	}

	if err := r.batchFileOpRepo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted); err != nil {
		return nil, fmt.Errorf("filesystem reverted but failed to persist revert status for op %d: %w", op.ID, err)
	}
	op.RevertStatus = models.RevertStatusReverted

	result := &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeReverted,
	}
	if nfoWarning != "" {
		result.Error = nfoWarning
	}
	switch {
	case isUpdate:
		logging.Infof("Reverted update operation %d: movie=%s at %s", op.ID, op.MovieID, op.OriginalPath)
	case op.OperationType == models.OperationTypeMove:
		logging.Infof("Reverted operation %d: movie=%s moved from %s back to %s", op.ID, op.MovieID, op.NewPath, op.OriginalPath)
	default:
		logging.Infof("Reverted copy-mode operation %d: movie=%s deleted generated artifacts under %s (sources and installed primary retained)", op.ID, op.MovieID, op.NewPath)
	}
	return result, nil
}

func (r *Reverter) guardDoubleRevert(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	if op.RevertStatus == models.RevertStatusReverted {
		return nil, ErrBatchAlreadyReverted
	}

	// codex P2 (PR #241 F2): a completed-noop row (authorized duplicate skip)
	// is terminal with nothing to unwind — it must never reach the primary
	// legs (its NewPath is empty by construction), so it rejects like an
	// already-reverted row instead of probing a "" anchor forever.
	if op.RevertStatus == models.RevertStatusNoOp {
		return nil, ErrBatchAlreadyReverted
	}

	if op.RevertStatus != models.RevertStatusApplied && op.RevertStatus != models.RevertStatusFailed {
		return nil, fmt.Errorf("operation has unexpected revert status: %s", op.RevertStatus)
	}

	return nil, nil
}

// pendingMoveIntentAnchor recovers a move row's primary destination from its
// pending MoveBack intent ledger and stamps the row's column-shaped fields, so
// later revert legs (checkAnchor, revertPrimaryFile, cleanup) see a coherent
// operation. Only the intent whose source matches the row's OriginalPath can
// pose as the primary move.
func pendingMoveIntentAnchor(op *models.BatchFileOperation) string {
	if op.OriginalPath == "" || op.GeneratedFiles == "" {
		return ""
	}
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return ""
	}
	for _, fm := range gf.MoveBack {
		if fm.OriginalPath == op.OriginalPath && fm.NewPath != "" {
			op.NewPath = fm.NewPath
			return fm.NewPath
		}
	}
	return ""
}

func (r *Reverter) checkAnchor(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	anchorPath := op.NewPath
	if op.OperationType == models.OperationTypeUpdate {
		anchorPath = op.OriginalPath
	}

	if _, err := r.fs.Stat(anchorPath); err != nil {
		if os.IsNotExist(err) {
			logging.Warnf("Anchor file missing for op %d at %s: skipping revert (anchor_missing)", op.ID, anchorPath)
			return r.skipRevert(op, models.RevertReasonAnchorMissing), nil
		}
		logging.Errorf("Cannot access anchor file for op %d at %s: %v (access_denied)", op.ID, anchorPath, err)
		return failRevert(ctx, r.batchFileOpRepo, op, models.RevertReasonAccessDenied, fmt.Sprintf("cannot access anchor file: %v", err)), nil
	}

	return nil, nil
}

// revertPrimaryPaths computes the source path and target directory for reverting
// a primary file move, without touching the filesystem. This separation makes the
// path logic testable independently of FS operations.
type revertPrimaryPaths struct {
	SourcePath      string // file to rename/move back
	TargetDir       string // directory that must exist before the rename
	OriginalDirPath string // for InPlaceRenamed: the original directory path to restore
	CurrentDir      string // for InPlaceRenamed: the current directory path being renamed
	DestRoot        string // boundary for empty-dir cleanup
}

// computeRevertPrimaryPaths calculates the paths needed to revert a primary file move.
func computeRevertPrimaryPaths(op *models.BatchFileOperation) revertPrimaryPaths {
	if op.InPlaceRenamed && op.OriginalDirPath != "" {
		currentDir := filepath.Dir(op.NewPath)
		sourcePath := filepath.Join(op.OriginalDirPath, filepath.Base(op.NewPath))
		return revertPrimaryPaths{
			SourcePath:      sourcePath,
			TargetDir:       filepath.Dir(op.OriginalPath),
			OriginalDirPath: op.OriginalDirPath,
			CurrentDir:      currentDir,
			DestRoot:        filepath.Dir(filepath.Dir(op.NewPath)),
		}
	}
	return revertPrimaryPaths{
		SourcePath: op.NewPath,
		TargetDir:  filepath.Dir(op.OriginalPath),
		DestRoot:   filepath.Dir(filepath.Dir(op.NewPath)),
	}
}

// revertPrimaryFileFS is the standalone filesystem implementation of revertPrimaryFile.
// cleanupDirFn routes empty-directory cleanup through the caller's seam (typically
// aferoFSReverter.cleanupEmptyDir) instead of calling cleanupEmptyDirFS directly.
func revertPrimaryFileFS(ctx context.Context, fs afero.Fs, batchFileOpRepo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, cleanupDirFn func(dirPath, stopAt string)) (*RevertFileResult, error) {
	paths := computeRevertPrimaryPaths(op)

	if op.InPlaceRenamed && op.OriginalDirPath != "" {
		if _, err := fs.Stat(paths.OriginalDirPath); err == nil {
			return failRevert(ctx, batchFileOpRepo, op, models.RevertReasonDestinationConflict, fmt.Sprintf("directory %s already exists (destination conflict)", paths.OriginalDirPath)), nil
		}

		if err := fs.Rename(paths.CurrentDir, paths.OriginalDirPath); err != nil {
			reason := models.RevertReasonUnexpectedPathState
			if os.IsPermission(err) {
				reason = models.RevertReasonAccessDenied
			}
			return failRevert(ctx, batchFileOpRepo, op, reason, fmt.Sprintf("failed to rename directory back: %v", err)), nil
		}

		if paths.SourcePath != op.OriginalPath {
			if err := fs.MkdirAll(paths.TargetDir, config.DirPerm); err != nil {
				return failRevert(ctx, batchFileOpRepo, op, models.RevertReasonUnexpectedPathState, fmt.Sprintf("failed to create directory for file rename: %v", err)), nil
			}
			if err := fs.Rename(paths.SourcePath, op.OriginalPath); err != nil {
				reason := models.RevertReasonUnexpectedPathState
				if os.IsPermission(err) {
					reason = models.RevertReasonAccessDenied
				}
				return failRevert(ctx, batchFileOpRepo, op, reason, fmt.Sprintf("failed to rename file within directory: %v", err)), nil
			}
		}
	} else {
		if _, err := fs.Stat(op.OriginalPath); err == nil {
			return failRevert(ctx, batchFileOpRepo, op, models.RevertReasonDestinationConflict, fmt.Sprintf("file %s already exists (destination conflict)", op.OriginalPath)), nil
		}

		targetDir := paths.TargetDir
		canonicalDir, err := fsutil.CanonicalizePath(targetDir)
		if err != nil {
			return failRevert(ctx, batchFileOpRepo, op, models.RevertReasonUnexpectedPathState, fmt.Sprintf("failed to canonicalize directory path: %v", err)), nil
		}
		if err := fs.MkdirAll(canonicalDir, config.DirPerm); err != nil {
			return failRevert(ctx, batchFileOpRepo, op, models.RevertReasonAccessDenied, fmt.Sprintf("failed to recreate original directory: %v", err)), nil
		}

		if err := fs.Rename(paths.SourcePath, op.OriginalPath); err != nil {
			reason := models.RevertReasonUnexpectedPathState
			if os.IsPermission(err) {
				reason = models.RevertReasonAccessDenied
			}
			return failRevert(ctx, batchFileOpRepo, op, reason, fmt.Sprintf("failed to revert move: %v", err)), nil
		}

		cleanupDirFn(filepath.Dir(op.NewPath), paths.DestRoot)
	}

	return nil, nil
}

// restoreNFOFS is the standalone filesystem implementation that restores the NFO
// snapshot for a reverted operation. It is an unexported helper used only by
// aferoFSReverter.restoreNFO. Extracted from Reverter so it can be tested
// independently and called from different contexts.
func restoreNFOFS(ctx context.Context, fs afero.Fs, batchFileOpRepo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, hardFailure bool) (string, *RevertFileResult) {
	if op.NFOSnapshot == "" {
		return "", nil
	}

	nfoPath := op.NFOPath
	if nfoPath == "" && op.MovieID != "" {
		nfoPath = filepath.Join(filepath.Dir(op.OriginalPath), op.MovieID+".nfo")
	}
	if nfoPath == "" {
		return "", nil
	}

	if hardFailure {
		return restoreNFOHardFailure(ctx, fs, batchFileOpRepo, op, nfoPath)
	}
	return restoreNFOSoftFailure(fs, op, nfoPath)
}

func restoreNFOHardFailure(ctx context.Context, fs afero.Fs, batchFileOpRepo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, nfoPath string) (string, *RevertFileResult) {
	nfoDir := filepath.Dir(nfoPath)
	canonicalNfoDir, err := fsutil.CanonicalizePath(nfoDir)
	if err != nil {
		return "", failRevert(ctx, batchFileOpRepo, op, models.RevertReasonNFORestoreFailed, fmt.Sprintf("failed to canonicalize NFO path: %v", err))
	}
	if err := fs.MkdirAll(canonicalNfoDir, config.DirPerm); err != nil {
		return "", failRevert(ctx, batchFileOpRepo, op, models.RevertReasonNFORestoreFailed, fmt.Sprintf("failed to create NFO directory: %v", err))
	}
	canonicalNfoPath := fsutil.NormalizePath(filepath.Join(canonicalNfoDir, filepath.Base(nfoPath)))
	if err := afero.WriteFile(fs, canonicalNfoPath, []byte(op.NFOSnapshot), config.FilePerm); err != nil {
		return "", failRevert(ctx, batchFileOpRepo, op, models.RevertReasonNFORestoreFailed, fmt.Sprintf("failed to restore NFO: %v", err))
	}

	return "", nil
}

func restoreNFOSoftFailure(fs afero.Fs, op *models.BatchFileOperation, nfoPath string) (string, *RevertFileResult) {
	nfoDir := filepath.Dir(op.OriginalPath)
	canonicalNfoDir, err := canonicalizeNFOPathFunc(nfoDir)
	if err != nil {
		logging.Warnf("restoreNFOSoftFailure: failed to resolve absolute path for %q: %v", nfoDir, err)
		canonicalNfoDir = filepath.Clean(nfoDir)
	}
	_ = fs.MkdirAll(canonicalNfoDir, config.DirPerm)
	restorePath := fsutil.NormalizePath(filepath.Join(canonicalNfoDir, filepath.Base(nfoPath)))
	if err := afero.WriteFile(fs, restorePath, []byte(op.NFOSnapshot), config.FilePerm); err != nil {
		logging.Warnf("Failed to restore NFO for op %d: %v (move-mode: treating as warning)", op.ID, err)
		return fmt.Sprintf("NFO restore failed: %v", err), nil
	}

	return "", nil
}

// cleanupEmptyDir removes the directory at dirPath if it is empty.
// Best-effort: errors are logged but not returned. Does not remove non-empty directories.
// Walks up parent directories removing empty ones until hitting stopAt or a non-empty directory.
// cleanupEmptyDirFS removes the directory at dirPath if it is empty.
// Best-effort: errors are logged but not returned. Does not remove non-empty directories.
// Walks up parent directories removing empty ones until hitting stopAt or a non-empty directory.
func cleanupEmptyDirFS(fs afero.Fs, dirPath string, stopAt string) {
	current := filepath.Clean(dirPath)
	stop := filepath.Clean(stopAt)

	for current != "" && current != "." && current != "/" && current != filepath.ToSlash(filepath.VolumeName(current)+"/") && current != stop {
		// Read directory entries to check if empty
		entries, err := afero.ReadDir(fs, current)
		if err != nil {
			// Directory doesn't exist or can't be read — nothing to clean up
			return
		}
		if len(entries) > 0 {
			// Directory is not empty — stop walking up
			return
		}
		// Directory is empty — remove it
		if err := fs.Remove(current); err != nil {
			// Failed to remove (e.g., permission denied) — stop walking up
			return
		}
		// Walk up to parent
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			return
		}
		current = parent
	}
}

// cleanupGeneratedFilesFS processes the GeneratedFiles JSON on a BatchFileOperation:
// deletes files in the Delete list and executes the MoveBack list.
// After deleting files, it removes empty parent directories left behind,
// stopping at stopAt boundary to prevent removing shared ancestor directories.
// Best-effort: missing files are skipped (os.IsNotExist), errors don't fail the revert.
//
// MoveBack semantics are mode-gated (codex P1, PR #241): a rename-back is only
// ever valid for MOVE-mode rows, whose move-installed subtitles genuinely left
// their source tree. Rows of every other operation type (copy, hardlink,
// symlink, update) retained their originals — the forward install there was
// non-destructively copy-based — so a MoveBack entry on such rows can only be a
// LEGACY journal (pre-#224 phase E journaled every installed subtitle as
// MoveBack regardless of mode; today's format expresses the same install as a
// plain Delete of the new path, see buildGeneratedFilesJSON). Renaming the
// installed path over the original on POSIX would REPLACE that retained
// original, destroying any edits the user made to it after the copy. Those
// legacy entries therefore execute with the modern delete-only semantic:
// remove the installed copy at NewPath, never touch OriginalPath.
func cleanupGeneratedFilesFS(fs afero.Fs, op *models.BatchFileOperation, stopAt string) {
	if op.GeneratedFiles == "" {
		return
	}
	var gf models.GeneratedFilesJSON
	if err := json.Unmarshal([]byte(op.GeneratedFiles), &gf); err != nil {
		return
	}
	// Track parent directories of deleted files for cleanup
	dirsToCheck := make(map[string]bool)
	// Delete is populated at journal-write time from op-created paths only;
	// replaced destinations are journaled separately in Replacements. Restored
	// pre-existing destinations are therefore never members of Delete, so this
	// cleanup cannot delete anything put back by replacement restore.
	for _, path := range gf.Delete {
		if err := fs.Remove(path); err != nil && !os.IsNotExist(err) {
			logging.Debugf("cleanupGeneratedFiles: failed to remove %s: %v", path, err)
		}
		dirsToCheck[filepath.Dir(path)] = true
	}
	// A MoveBack arm supersedes any pending delete pinned to the same
	// destination in BOTH vacancy outcomes (rows journaled before the intent
	// promoted with its arm): with the source absent the rename-back
	// restores those bytes onto their source, so the pinned delete must
	// never fire first and destroy them; with the source PRESENT the rename
	// is suppressed and the pin must still not fire — source-present is
	// ambiguous between "the move never consumed its source" and "the move
	// consumed it and a foreign file reappeared afterwards", and without a
	// durable source-consumed record the pin cannot tell those apart, so
	// both paths are retained (a duplicate, never a lost last copy)
	// (codex P1, PRRT_kwDORn9KaM6m5kmF).
	moveBackTargets := make(map[string]bool, len(gf.MoveBack))
	for _, fm := range gf.MoveBack {
		moveBackTargets[fm.NewPath] = true
	}
	// A MoveBack entry whose ORIGINAL still exists names a move whose source
	// was never consumed (an exit between the pending intent commit and the
	// source removal) or whose source reappeared afterwards: running the
	// rename-back would REPLACE those retained/foreign bytes on POSIX
	// (codex P1, PRRT_kwDORn9KaM6m3ujI). Suppress the rename for such targets;
	// the PlannedDeletes leg below retains a pinned copy of the same target as
	// well — the pin would fire identically in the never-consumed shape and
	// the consumed-then-foreignly-recreated one, so only retention protects
	// both (codex P1, PRRT_kwDORn9KaM6m5kmF). A source whose state cannot be
	// PROVEN absent suppresses too: uncertainty licenses neither a
	// rename-over nor the delete. The vacancy probe never
	// follows a final symlink (codex P2, PRRT_kwDORn9KaM6m5HFu): a link planted
	// at the source — even a DANGLING one, which Stat reports as ENOENT — is
	// itself a directory entry the POSIX rename back would REPLACE, so any
	// occupant type suppresses exactly like a present file.
	moveMode := op.OperationType == models.OperationTypeMove
	renameSuppressed := make(map[string]bool, len(gf.MoveBack))
	if moveMode {
		for _, fm := range gf.MoveBack {
			if fm.OriginalPath == "" || fm.NewPath == "" {
				continue
			}
			if _, statErr := lstatRestoreSource(fs, fm.OriginalPath); statErr == nil || !os.IsNotExist(statErr) {
				renameSuppressed[fm.NewPath] = true
			}
		}
	}
	// PlannedDeletes are intent entries pinned to the publisher's payload:
	// delete only while the destination still carries exactly that payload —
	// absent paths are consumed, rebuilt/touched or foreign occupants are
	// kept. The pin shape keys on how the payload installed (models.DeleteEntry):
	// a LinkTarget pin authenticates the link OBJECT by readlink, an Identity
	// pin the hard-linked object by its admitted identity tuple, and a
	// CopySize/CopyPartialSHA256 pin an in-flight copy by size plus a bounded
	// head+tail digest; none of those ever hashes or follows a non-regular
	// entry, so the full-hash leg's nonregular-retain rule (m5HF7) is
	// preserved in every shape. Dispatch precedence is deterministic:
	// LinkTarget, then the SHA-cleared identity/partial shapes, then the
	// full-hash leg (an entry carrying SHA256 always hashes).
	for _, entry := range gf.PlannedDeletes {
		path := entry.Path
		if moveBackTargets[path] {
			continue
		}
		if entry.LinkTarget != "" {
			// Soft-link pin (codex P2, PRRT_kwDORn9KaM6nBUq8): the install is
			// the link OBJECT, authenticated solely by its readback payload.
			// Any occupant that is not a symlink — a regular file holding any
			// bytes at all, a directory — is foreign to this install shape and
			// retains untouched, the m5HF7 partition applied from the other
			// side (the hash leg retains every non-regular; this leg retains
			// every non-link).
			linkInfo, linkLstatErr := lstatRestoreSource(fs, path)
			if os.IsNotExist(linkLstatErr) {
				dirsToCheck[filepath.Dir(path)] = true
				continue
			}
			if linkLstatErr != nil {
				logging.Debugf("cleanupGeneratedFiles: pending symlink delete probe failed for %s: %v", path, linkLstatErr)
				continue
			}
			if linkInfo.Mode()&os.ModeSymlink == 0 {
				logging.Debugf("cleanupGeneratedFiles: pending symlink delete %s is not the pinned link object (mode %v) — retained", path, linkInfo.Mode())
				continue
			}
			// The removal re-authenticates post-vacate (readlink of the
			// terminal object), so a plant swapped onto path inside the
			// probe→vacate window is rewound byte-intact, never unlinked.
			if err := fsutil.UnlinkSymlinkVerified(fs, path, entry.LinkTarget); err != nil {
				if errors.Is(err, fsutil.ErrTakeAsideVanished) {
					dirsToCheck[filepath.Dir(path)] = true
					continue
				}
				logging.Debugf("cleanupGeneratedFiles: pending symlink delete %s could not be removed link-verified — retained: %v", path, err)
				continue
			}
			dirsToCheck[filepath.Dir(path)] = true
			continue
		}
		if entry.SHA256 == "" && entry.IdentityModUnix != 0 {
			// Hard-link pin: the published destination must BE the admitted
			// source's object — link(2) shares the volume/index, so the
			// identity tuple authenticates without reading a byte. The
			// metadata legs (size + mtime-seconds) always run; the dev/inode
			// legs run only against a strong pin and never degrade for one
			// (a platform that re-probed no identity cannot authenticate a
			// strong claim — retain, the admission-proof posture). Any
			// non-regular occupant retains untouched (m5HF7 unchanged).
			idInfo, idLstatErr := lstatRestoreSource(fs, path)
			if os.IsNotExist(idLstatErr) {
				dirsToCheck[filepath.Dir(path)] = true
				continue
			}
			if idLstatErr != nil {
				logging.Debugf("cleanupGeneratedFiles: pending identity delete probe failed for %s: %v", path, idLstatErr)
				continue
			}
			if !idInfo.Mode().IsRegular() {
				logging.Debugf("cleanupGeneratedFiles: pending identity delete %s is not a regular file (mode %v) — retained", path, idInfo.Mode())
				continue
			}
			if entry.IdentityStrong {
				dev, ino, identityOK := fsutil.BoundObjectIdentity(fs, path, idInfo)
				if !identityOK {
					logging.Debugf("cleanupGeneratedFiles: pending identity delete %s exposes no kernel identity for a strong pin — retained", path)
					continue
				}
				if dev != entry.IdentityDev || ino != entry.IdentityIno {
					logging.Debugf("cleanupGeneratedFiles: pending identity delete %s names a different object than the admitted source — retained", path)
					continue
				}
			}
			if idInfo.Size() != entry.IdentitySize || idInfo.ModTime().Unix() != entry.IdentityModUnix {
				logging.Debugf("cleanupGeneratedFiles: pending identity delete %s no longer matches the pinned identity tuple — retained", path)
				continue
			}
			// The lstat identity binds the verified unlink: a swap inside the
			// probe→unlink window rides the vacate, fails the rebind, and is
			// rewound byte-intact — never a pathname Remove of an unproven
			// occupant.
			if err := fsutil.UnlinkVerified(fs, path, idInfo); err != nil {
				if errors.Is(err, fsutil.ErrTakeAsideVanished) {
					dirsToCheck[filepath.Dir(path)] = true
					continue
				}
				logging.Debugf("cleanupGeneratedFiles: pending identity delete %s could not be removed identity-verified — retained: %v", path, err)
				continue
			}
			dirsToCheck[filepath.Dir(path)] = true
			continue
		}
		if entry.SHA256 == "" && entry.CopyPartialSHA256 != "" {
			// Interim copy pin (the execute→seal crash window of a streaming
			// copy install — see fsutil.PartialCopyDigest's threat model):
			// size equality plus the bounded head+tail digest. The
			// regularity probe never follows a final symlink (m5HF7), and the
			// digest re-derives from ONE open handle whose own Stat supplies
			// the unlink identity — a link planted inside the lstat→open
			// window reads the TARGET through the handle, and the verified
			// unlink's rebind then refuses the vacated link object.
			partialInfo, partialLstatErr := lstatRestoreSource(fs, path)
			if os.IsNotExist(partialLstatErr) {
				dirsToCheck[filepath.Dir(path)] = true
				continue
			}
			if partialLstatErr != nil {
				logging.Debugf("cleanupGeneratedFiles: pending partial delete probe failed for %s: %v", path, partialLstatErr)
				continue
			}
			if !partialInfo.Mode().IsRegular() {
				logging.Debugf("cleanupGeneratedFiles: pending partial delete %s is not a regular file (mode %v) — retained", path, partialInfo.Mode())
				continue
			}
			pinned, digest, probeErr := fsutil.PartialCopyDigest(fs, path)
			if errors.Is(probeErr, os.ErrNotExist) {
				dirsToCheck[filepath.Dir(path)] = true
				continue
			}
			if probeErr != nil {
				logging.Debugf("cleanupGeneratedFiles: pending partial delete digest failed for %s: %v", path, probeErr)
				continue
			}
			if pinned.Size() != entry.CopySize || digest != strings.ToLower(entry.CopyPartialSHA256) {
				logging.Debugf("cleanupGeneratedFiles: pending partial delete %s no longer matches the interim pin — retained", path)
				continue
			}
			if err := fsutil.UnlinkVerified(fs, path, pinned); err != nil {
				if errors.Is(err, fsutil.ErrTakeAsideVanished) {
					dirsToCheck[filepath.Dir(path)] = true
					continue
				}
				logging.Debugf("cleanupGeneratedFiles: pending partial delete %s could not be removed identity-verified — retained: %v", path, err)
				continue
			}
			dirsToCheck[filepath.Dir(path)] = true
			continue
		}
		// The pin certifies the previously published REGULAR file only, so the
		// occupancy check never follows a final symlink (codex P2,
		// PRRT_kwDORn9KaM6m5HF7): a successor link whose TARGET happens to hold
		// the pinned bytes is a foreign directory entry — hashing through it
		// and removing the link would unlink an unrelated object.
		info, lstatErr := lstatRestoreSource(fs, path)
		if os.IsNotExist(lstatErr) {
			dirsToCheck[filepath.Dir(path)] = true
			continue
		}
		if lstatErr != nil {
			logging.Debugf("cleanupGeneratedFiles: pending delete probe failed for %s: %v", path, lstatErr)
			continue
		}
		if !info.Mode().IsRegular() {
			logging.Debugf("cleanupGeneratedFiles: pending delete %s is not the pinned regular file (mode %v) — retained", path, info.Mode())
			continue
		}
		file, openErr := fs.Open(path)
		if os.IsNotExist(openErr) {
			dirsToCheck[filepath.Dir(path)] = true
			continue
		}
		if openErr != nil {
			logging.Debugf("cleanupGeneratedFiles: pending delete probe failed for %s: %v", path, openErr)
			continue
		}
		// The identity is captured from the OPEN HANDLE, never the pathname:
		// the digest below authenticates exactly this object's bytes, so the
		// removal must bind to the same object (codex P1,
		// PRRT_kwDORn9KaM6m6WAj). A capture failure leaves nothing to bind the
		// unlink to — retain rather than pathname-remove an unproven name.
		pinned, pinnedErr := file.Stat()
		h := sha256.New()
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if pinnedErr != nil {
			logging.Debugf("cleanupGeneratedFiles: pending delete %s identity could not be captured — retained: %v", path, pinnedErr)
			continue
		}
		if copyErr != nil || closeErr != nil {
			logging.Debugf("cleanupGeneratedFiles: pending delete digest failed for %s: %v/%v", path, copyErr, closeErr)
			continue
		}
		if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(entry.SHA256) {
			logging.Debugf("cleanupGeneratedFiles: pending delete %s no longer carries the pinned bytes — retained", path)
			continue
		}
		// Identity-verified unlink (fsutil.UnlinkVerified — the same
		// bound-unlink construction the quarantine holds carry): the hashed
		// object vacates onto a fresh crypto-claimed terminal sibling, the
		// terminal re-binds to the pinned identity, and ONLY the re-bound
		// terminal is unlinked. A foreign occupant rename-swapped onto path
		// inside the hash→unlink window rides the vacate onto the terminal,
		// fails the rebind, and is rewound byte-intact — never a pathname
		// Remove of an unproven occupant.
		if err := fsutil.UnlinkVerified(fs, path, pinned); err != nil {
			if errors.Is(err, fsutil.ErrTakeAsideVanished) {
				// The pinned bytes vanished under the verified unlink — the
				// entry consumed itself; prune the empty parent as consumed.
				dirsToCheck[filepath.Dir(path)] = true
				continue
			}
			logging.Debugf("cleanupGeneratedFiles: pending delete %s could not be removed identity-verified — retained: %v", path, err)
			continue
		}
		dirsToCheck[filepath.Dir(path)] = true
	}

	// Execute the MoveBack array (best-effort): rename-back for move-mode rows
	// whose source is GONE (the move consumed it); delete-the-installed-copy
	// for every other mode's legacy entries (rename-over must NEVER run
	// against a retained original — see the function doc above). A suppressed
	// entry (source still present) does neither: the PlannedDeletes leg above
	// already retained its published copy, pinned or not, alongside the
	// occupied source — nothing is lost in either crash shape.
	for _, fm := range gf.MoveBack {
		if fm.NewPath == op.NewPath && fm.OriginalPath == op.OriginalPath {
			// A pending move intent equal to the row columns: the primary move is
			// already reverted by the column-driven arm — never double-drive it.
			continue
		}
		if !moveMode {
			if err := fs.Remove(fm.NewPath); err != nil && !os.IsNotExist(err) {
				logging.Debugf("cleanupGeneratedFiles: failed to delete copy-installed artifact %s (original at %s retained): %v", fm.NewPath, fm.OriginalPath, err)
			}
		} else if renameSuppressed[fm.NewPath] {
			logging.Debugf("cleanupGeneratedFiles: move-back %s → %s suppressed — the original still exists (the move intent was never consumed or the source reappeared); the destination, pinned or not, is retained alongside it", fm.NewPath, fm.OriginalPath)
		} else if err := fs.Rename(fm.NewPath, fm.OriginalPath); err != nil {
			logging.Debugf("cleanupGeneratedFiles: failed to move back %s → %s: %v", fm.NewPath, fm.OriginalPath, err)
		}
		dirsToCheck[filepath.Dir(fm.NewPath)] = true
	}
	// Clean up empty parent directories left behind after file deletion/move.
	// Validate each directory is inside the batch tree before removing.
	batchRoot := filepath.Clean(stopAt)
	for dir := range dirsToCheck {
		cleanDir := filepath.Clean(dir)
		if !isDescendant(cleanDir, batchRoot) {
			logging.Warnf("Skipping cleanup of directory %q: outside batch root %q", cleanDir, batchRoot)
			continue
		}
		cleanupEmptyDirDownwardFS(fs, cleanDir, stopAt)
	}
}

// isDescendant checks if path is inside parentDir (or equal to it).
// Returns true if path has parentDir as a prefix when both are cleaned.
func isDescendant(path string, parentDir string) bool {
	normPath := filepath.ToSlash(filepath.Clean(path))
	normParent := filepath.ToSlash(filepath.Clean(parentDir))
	if normPath == normParent {
		return true
	}
	if len(normPath) > len(normParent) && normPath[:len(normParent)+1] == normParent+"/" {
		return true
	}
	return false
}

// cleanupEmptyDirDownward removes empty directories starting from dirPath,
// walking up through parents until hitting stopAt boundary.
// Unlike cleanupEmptyDir (which walks UP from a starting dir), this handles
// the case where deleting files leaves empty subdirectories.
// cleanupEmptyDirDownwardFS removes empty directories starting from dirPath,
// walking up through parents until hitting stopAt boundary.
// Unlike cleanupEmptyDirFS (which walks UP from a starting dir), this handles
// the case where deleting files leaves empty subdirectories.
func cleanupEmptyDirDownwardFS(fs afero.Fs, dirPath string, stopAt string) {
	current := filepath.Clean(dirPath)
	stop := filepath.Clean(stopAt)

	for {
		entries, err := afero.ReadDir(fs, current)
		if err != nil {
			return
		}
		if len(entries) > 0 {
			return
		}
		if current == stop {
			return
		}
		if err := fs.Remove(current); err != nil {
			return
		}
		parent := filepath.Dir(current)
		if parent == current || parent == "." || parent == "/" || parent == filepath.VolumeName(current)+string(filepath.Separator) {
			return
		}
		current = parent
	}
}

// revertOperations processes a slice of operations using the given revert function,
// tracking per-operation outcomes. Each operation is processed independently
// (best-effort: individual failures don't abort the batch). Returns the ordered
// list of per-operation results.
func (r *Reverter) revertOperations(ctx context.Context, ops []models.BatchFileOperation, revertFn func(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error)) []RevertFileResult {
	// P3 + codex R6-1: per-destination sequences are not chronologically
	// comparable ACROSS destinations, so newest-first per-op maxima are only
	// an approximation. A newer-applied rejection is therefore RETRIED once
	// its blocker's entries are consumed — passes iterate to a fixpoint and
	// terminate because consumption shrinks journals monotonically.
	remaining := append([]models.BatchFileOperation(nil), ops...)
	var outcomes []RevertFileResult
	for {
		sort.SliceStable(remaining, func(i, j int) bool {
			return maxJournalSeq(&remaining[i]) > maxJournalSeq(&remaining[j])
		})
		progressed := false
		var retryNext []models.BatchFileOperation
		for i := range remaining {
			op := &remaining[i]
			// R18-1: CONSUMPTION is progress too — a blocker whose journal got
			// replayed but whose primary anchor was missing lands Skipped;
			// counted as progress, deeper chains (A←B←C) keep unwinding.
			journaledBefore := len(mustJournal(op))
			res, sysErr := revertFn(ctx, op)
			if journaledAfter := len(mustJournal(op)); journaledAfter < journaledBefore {
				progressed = true
			}
			if sysErr != nil {
				outcomes = append(outcomes, RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Error:        sysErr.Error(),
				})
				continue
			}
			if res.orderRetryable {
				retryNext = append(retryNext, *op)
				continue
			}
			outcomes = append(outcomes, *res)
			if res.Outcome == models.RevertOutcomeReverted {
				progressed = true
			}
		}
		if len(retryNext) == 0 {
			break
		}
		if !progressed {
			// No blocker reverted this pass — re-run once so the standing
			// rejection is reported as a normal (final) outcome.
			for i := range retryNext {
				op := &retryNext[i]
				res, sysErr := revertFn(ctx, op)
				if sysErr != nil {
					outcomes = append(outcomes, RevertFileResult{OperationID: op.ID, MovieID: op.MovieID, OriginalPath: op.OriginalPath, NewPath: op.NewPath, Outcome: models.RevertOutcomeFailed, Error: sysErr.Error()})
					continue
				}
				res.orderRetryable = false
				outcomes = append(outcomes, *res)
			}
			break
		}
		remaining = retryNext
	}
	return outcomes
}

// mustJournal optimistically parses an op's replacement journal (zero on
// malformed/absent).
func mustJournal(op *models.BatchFileOperation) []models.ReplacementEntry {
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return nil
	}
	return gf.Replacements
}

// summarizeOutcomes computes aggregate succeeded/skipped/failed counts from
// per-operation outcomes.
func summarizeOutcomes(outcomes []RevertFileResult) (succeeded, skipped, failed int) {
	for _, o := range outcomes {
		switch o.Outcome {
		case models.RevertOutcomeReverted:
			succeeded++
		case models.RevertOutcomeSkipped:
			skipped++
		case models.RevertOutcomeFailed:
			failed++
		}
	}
	return
}

// collectDestRoots extracts destination root directories from operations for
// batch-level empty-dir cleanup after revert.
func collectDestRoots(ops []models.BatchFileOperation) map[string]bool {
	destRoots := make(map[string]bool)
	for i := range ops {
		if !ops[i].InPlaceRenamed && ops[i].NewPath != "" {
			destRoots[filepath.Dir(filepath.Dir(ops[i].NewPath))] = true
		}
	}
	return destRoots
}

// RevertBatch reverts all operations in a batch (D-02, D-04).
// It uses best-effort processing: individual failures don't abort the batch.
// After all operations are processed, it sweeps empty destination directories.
func (r *Reverter) RevertBatch(ctx context.Context, batchJobID string) (*RevertBatchResult, error) {
	ops, err := r.batchFileOpRepo.FindByBatchJobID(ctx, batchJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch batch operations: %w", err)
	}

	if len(ops) == 0 {
		return nil, ErrNoOperationsFound
	}

	// Filter to processable operations (applied + failed). Completed-noop rows
	// (authorized duplicate skips, codex P2 PR #241 F2) are terminal with
	// nothing to unwind: like reverted rows they never enter the selection,
	// but they are NOT already-reverted work — an all-noop batch completes
	// trivially success-shaped (never anchor_missing-skipped), letting the job
	// report fully reverted.
	var processable []models.BatchFileOperation
	revertedCount := 0
	noopCount := 0
	for i := range ops {
		switch ops[i].RevertStatus {
		case models.RevertStatusReverted:
			revertedCount++
		case models.RevertStatusNoOp:
			noopCount++
		case models.RevertStatusApplied, models.RevertStatusFailed:
			processable = append(processable, ops[i])
		}
	}

	// If no processable ops, determine which result/error to return
	if len(processable) == 0 {
		if revertedCount > 0 {
			return nil, ErrBatchAlreadyReverted
		}
		if noopCount > 0 {
			return &RevertBatchResult{}, nil
		}
		return nil, ErrNoOperationsFound
	}

	r.sweepJournaledDestinations(ctx, processable)

	// leniently-promote reload: the sweep consumed/persisted; re-read the rows so
	// the revert's in-memory journal can't chase backups the sweep deleted.
	if len(processable) > 0 {
		fresh, freshErr := r.batchFileOpRepo.FindByBatchJobID(ctx, processable[0].BatchJobID)
		if freshErr == nil {
			byID := make(map[uint]models.BatchFileOperation, len(fresh))
			for i := range fresh {
				byID[fresh[i].ID] = fresh[i]
			}
			for i := range processable {
				if freshRow, ok := byID[processable[i].ID]; ok {
					processable[i] = freshRow
				}
			}
		}
	}

	outcomes := r.revertOperations(ctx, processable, r.revertFile)
	succeeded, skipped, failed := summarizeOutcomes(outcomes)

	// Batch-level cleanup: per-file cleanupEmptyDir uses destRoot as a stop
	// boundary, which leaves intermediate parent directories behind (e.g.,
	// out/ABP-880/ when the file was at out/ABP-880/dir/ABP-880.mp4).
	// Sweep each destRoot with stopAt="" so cleanupEmptyDir walks all the
	// way up. It stops automatically at non-empty directories, so populated
	// ancestors (including the top-level output directory if it contains other
	// files) are preserved. An empty output directory will be removed, which
	// is the correct behavior after a full batch revert.
	for dirPath := range collectDestRoots(processable) {
		r.fsReverter.cleanupEmptyDir(filepath.Clean(dirPath), "")
	}

	return &RevertBatchResult{
		Total:     len(processable),
		Succeeded: succeeded,
		Skipped:   skipped,
		Failed:    failed,
		Outcomes:  outcomes,
	}, nil
}

// RevertScrape reverts only the operations for a specific movie within a batch (HIST-04).
func (r *Reverter) RevertScrape(ctx context.Context, batchJobID string, movieID string) (*RevertBatchResult, error) {
	ops, err := r.batchFileOpRepo.FindByBatchJobID(ctx, batchJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch batch operations: %w", err)
	}

	if len(ops) == 0 {
		return nil, ErrNoOperationsFound
	}

	// Filter to matching movieID AND processable status
	var matching []models.BatchFileOperation
	noopMatching := 0
	for i := range ops {
		if ops[i].MovieID != movieID {
			continue
		}
		// codex P2 (PR #241 F2): a completed-noop row (authorized duplicate
		// skip mutated nothing) is terminal — it never enters the revert
		// selection, and a movie whose ONLY rows are noop completes trivially
		// success-shaped so the batch's fully-reverted accounting can close.
		if ops[i].RevertStatus == models.RevertStatusNoOp {
			noopMatching++
			continue
		}
		if ops[i].RevertStatus == models.RevertStatusApplied || ops[i].RevertStatus == models.RevertStatusFailed {
			matching = append(matching, ops[i])
		}
	}

	if len(matching) == 0 {
		if noopMatching > 0 {
			return &RevertBatchResult{}, nil
		}
		return nil, fmt.Errorf("no processable operations found for movie %s in batch %s", movieID, batchJobID)
	}

	r.sweepJournaledDestinations(ctx, matching)

	if len(matching) > 0 {
		fresh, freshErr := r.batchFileOpRepo.FindByBatchJobID(ctx, matching[0].BatchJobID)
		if freshErr == nil {
			byID := make(map[uint]models.BatchFileOperation, len(fresh))
			for i := range fresh {
				byID[fresh[i].ID] = fresh[i]
			}
			for i := range matching {
				if freshRow, ok := byID[matching[i].ID]; ok {
					matching[i] = freshRow
				}
			}
		}
	}

	outcomes := r.revertOperations(ctx, matching, r.revertFile)
	succeeded, skipped, failed := summarizeOutcomes(outcomes)

	for dirPath := range collectDestRoots(matching) {
		r.fsReverter.cleanupEmptyDir(filepath.Clean(dirPath), "")
	}

	return &RevertBatchResult{
		Total:     len(matching),
		Succeeded: succeeded,
		Skipped:   skipped,
		Failed:    failed,
		Outcomes:  outcomes,
	}, nil
}
