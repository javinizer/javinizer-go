package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// Per ADR-0034: generatedFilesJSON and fileMove moved to internal/models/revert_types.go.
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
}

var (
	// ErrBatchAlreadyReverted is returned when a batch or operation is already reverted.
	ErrBatchAlreadyReverted = errors.New("batch already reverted")
	// ErrCopyModeNotRevertible is returned when attempting to revert a copy/hardlink/symlink operation.
	ErrCopyModeNotRevertible = errors.New("copy-mode operations cannot be reverted")
	// ErrNoOperationsFound is returned when no operations exist for the given batch.
	ErrNoOperationsFound = errors.New("no operations found for batch")
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
	fsReverter      fileSystemReverter // filesystem operations seam
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

func (r *Reverter) revertFile(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	logging.Debugf("Reverting operation %d: movie=%s type=%s original=%s new=%s revert_status=%s",
		op.ID, op.MovieID, op.OperationType, op.OriginalPath, op.NewPath, op.RevertStatus)

	if result, err := r.guardDoubleRevert(ctx, op); result != nil || err != nil {
		return result, err
	}
	if result, err := r.checkAnchor(ctx, op); result != nil || err != nil {
		return result, err
	}

	isUpdate := op.OperationType == models.OperationTypeUpdate
	if isUpdate {
		r.fsReverter.cleanupGeneratedFiles(op, filepath.Dir(filepath.Dir(op.OriginalPath)))
	} else {
		if result, err := r.fsReverter.revertPrimaryFile(ctx, op); result != nil || err != nil {
			return result, err
		}
		destRoot := filepath.Dir(filepath.Dir(op.NewPath))
		r.fsReverter.cleanupGeneratedFiles(op, destRoot)
		if !op.InPlaceRenamed {
			r.fsReverter.cleanupEmptyDir(filepath.Dir(op.NewPath), destRoot)
		}
	}

	nfoWarning, failedResult := r.fsReverter.restoreNFO(ctx, op, isUpdate)
	if failedResult != nil {
		return failedResult, nil
	}

	if err := r.batchFileOpRepo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted); err != nil {
		return nil, fmt.Errorf("filesystem reverted but failed to persist revert status for op %d: %w", op.ID, err)
	}

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
	if isUpdate {
		logging.Infof("Reverted update operation %d: movie=%s at %s", op.ID, op.MovieID, op.OriginalPath)
	} else {
		logging.Infof("Reverted operation %d: movie=%s moved from %s back to %s", op.ID, op.MovieID, op.NewPath, op.OriginalPath)
	}
	return result, nil
}

func (r *Reverter) guardDoubleRevert(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	if op.RevertStatus == models.RevertStatusReverted {
		return nil, ErrBatchAlreadyReverted
	}

	if op.RevertStatus != models.RevertStatusApplied && op.RevertStatus != models.RevertStatusFailed {
		return nil, fmt.Errorf("operation has unexpected revert status: %s", op.RevertStatus)
	}

	if op.OperationType != models.OperationTypeMove && op.OperationType != models.OperationTypeUpdate {
		return failRevert(ctx, r.batchFileOpRepo, op, models.RevertReasonUnexpectedPathState, ErrCopyModeNotRevertible.Error()), nil
	}

	return nil, nil
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
	canonicalNfoDir, err := fsutil.CanonicalizePath(nfoDir)
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
// deletes files in the Delete list and moves back files in the MoveBack list.
// After deleting files, it removes empty parent directories left behind,
// stopping at stopAt boundary to prevent removing shared ancestor directories.
// Best-effort: missing files are skipped (os.IsNotExist), errors don't fail the revert.
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
	// Delete files in the Delete array (best-effort — skip IsNotExist)
	for _, path := range gf.Delete {
		if err := fs.Remove(path); err != nil && !os.IsNotExist(err) {
			logging.Debugf("cleanupGeneratedFiles: failed to remove %s: %v", path, err)
		}
		dirsToCheck[filepath.Dir(path)] = true
	}
	// Move back files in the MoveBack array (best-effort)
	for _, fm := range gf.MoveBack {
		if err := fs.Rename(fm.NewPath, fm.OriginalPath); err != nil {
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
	var outcomes []RevertFileResult
	for i := range ops {
		op := &ops[i]
		res, sysErr := revertFn(ctx, op)
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
		outcomes = append(outcomes, *res)
	}
	return outcomes
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

	// Filter to processable operations (applied + failed)
	var processable []models.BatchFileOperation
	revertedCount := 0
	for i := range ops {
		switch ops[i].RevertStatus {
		case models.RevertStatusReverted:
			revertedCount++
		case models.RevertStatusApplied, models.RevertStatusFailed:
			processable = append(processable, ops[i])
		}
	}

	// If no processable ops, determine which error to return
	if len(processable) == 0 {
		if revertedCount > 0 {
			return nil, ErrBatchAlreadyReverted
		}
		return nil, ErrNoOperationsFound
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
	for i := range ops {
		if ops[i].MovieID == movieID &&
			(ops[i].RevertStatus == models.RevertStatusApplied || ops[i].RevertStatus == models.RevertStatusFailed) {
			matching = append(matching, ops[i])
		}
	}

	if len(matching) == 0 {
		return nil, fmt.Errorf("no processable operations found for movie %s in batch %s", movieID, batchJobID)
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
