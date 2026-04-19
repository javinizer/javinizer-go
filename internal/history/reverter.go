package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// GeneratedFilesJSON is the JSON structure stored in BatchFileOperation.GeneratedFiles.
// Phase 3 (Organize Integration) populates this during organize; Reverter consumes it.
type GeneratedFilesJSON struct {
	Delete   []string   `json:"delete,omitempty"`    // Files to delete on revert (NFO, images, screenshots)
	MoveBack []FileMove `json:"move_back,omitempty"` // Files to move back on revert (subtitles)
}

// FileMove represents a file that was moved during organize and should be moved back on revert.
type FileMove struct {
	OriginalPath string `json:"original_path"` // Where the file was before organize
	NewPath      string `json:"new_path"`      // Where the file is after organize
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
	Outcome      string // RevertOutcome: reverted, skipped, or failed
	Reason       string // RevertReason: why the outcome occurred (empty for success)
	Error        string // Error message for failed outcomes
}

var (
	// ErrBatchAlreadyReverted is returned when a batch or operation is already reverted.
	ErrBatchAlreadyReverted = errors.New("batch already reverted")
	// ErrCopyModeNotRevertible is returned when attempting to revert a copy/hardlink/symlink operation.
	ErrCopyModeNotRevertible = errors.New("copy-mode operations cannot be reverted")
	// ErrNoOperationsFound is returned when no operations exist for the given batch.
	ErrNoOperationsFound = errors.New("no operations found for batch")
)

// Reverter handles reverting file organization operations.
// It reads BatchFileOperation records from the database, performs inverse file
// operations via afero, and tracks per-operation revert status.
type Reverter struct {
	fs              afero.Fs
	batchFileOpRepo database.BatchFileOperationRepositoryInterface
}

func fsPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
		p = p[2:]
	}
	return p
}

// NewReverter creates a new Reverter with the given filesystem and repository.
func NewReverter(fs afero.Fs, batchFileOpRepo database.BatchFileOperationRepositoryInterface) *Reverter {
	return &Reverter{
		fs:              fs,
		batchFileOpRepo: batchFileOpRepo,
	}
}

// revertFile reverts a single BatchFileOperation.
// It returns a RevertFileResult with the outcome details. The outer error is only
// for system-level failures (DB errors); per-operation outcomes are in RevertFileResult.
//
// Checks performed (in order):
// 1. Double-revert guard (D-09)
// 2. Anchor check — skip if video file missing (D-02)
// 3. Destination conflict — fail if OriginalPath already occupied (D-04)
// 4. Main revert operation (move/rename/NFO restore)
// 5. Conditional cleanup of generated files (D-03) — only after successful primary revert
// 6. NFO restore with proper error handling per mode (D-05)
func (r *Reverter) revertFile(ctx context.Context, op *models.BatchFileOperation) (*RevertFileResult, error) {
	logging.Debugf("Reverting operation %d: movie=%s type=%s original=%s new=%s revert_status=%s",
		op.ID, op.MovieID, op.OperationType, op.OriginalPath, op.NewPath, op.RevertStatus)

	// Guard: double-revert protection (D-09)
	if op.RevertStatus == models.RevertStatusReverted {
		return nil, ErrBatchAlreadyReverted
	}

	// Only applied and failed operations are processable
	if op.RevertStatus != models.RevertStatusApplied && op.RevertStatus != models.RevertStatusFailed {
		return nil, fmt.Errorf("operation has unexpected revert status: %s", op.RevertStatus)
	}

	// --- Copy/hardlink/symlink guard (D-11) — check BEFORE anchor check ---
	if op.OperationType != models.OperationTypeMove && op.OperationType != models.OperationTypeUpdate {
		if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
			logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
		}
		return &RevertFileResult{
			OperationID:  op.ID,
			MovieID:      op.MovieID,
			OriginalPath: op.OriginalPath,
			NewPath:      op.NewPath,
			Outcome:      models.RevertOutcomeFailed,
			Reason:       models.RevertReasonUnexpectedPathState,
			Error:        ErrCopyModeNotRevertible.Error(),
		}, nil
	}

	// --- Anchor-based skip check (D-02) ---
	// In update-mode: check if video file exists at OriginalPath (the anchor)
	// In move-mode: check if video file exists at NewPath (the anchor)
	anchorPath := op.NewPath
	if op.OperationType == models.OperationTypeUpdate {
		anchorPath = op.OriginalPath
	}
	if _, err := r.fs.Stat(anchorPath); err != nil {
		if os.IsNotExist(err) {
			logging.Warnf("Anchor file missing for op %d at %s: skipping revert (anchor_missing)", op.ID, anchorPath)
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeSkipped,
				Reason:       models.RevertReasonAnchorMissing,
			}, nil
		}
		if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
			logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
		}
		logging.Errorf("Cannot access anchor file for op %d at %s: %v (access_denied)", op.ID, anchorPath, err)
		return &RevertFileResult{
			OperationID:  op.ID,
			MovieID:      op.MovieID,
			OriginalPath: op.OriginalPath,
			NewPath:      op.NewPath,
			Outcome:      models.RevertOutcomeFailed,
			Reason:       models.RevertReasonAccessDenied,
			Error:        fmt.Sprintf("cannot access anchor file: %v", err),
		}, nil
	}

	// --- Update-mode revert path (HIST-05) ---
	if op.OperationType == models.OperationTypeUpdate {
		return r.revertUpdateMode(op)
	}

	// --- Move-mode revert path ---
	return r.revertMoveMode(op)
}

// revertUpdateMode handles update-mode revert (HIST-05):
// Video file is not moved, so we only restore NFO and delete generated files.
// Anchor check already passed — video file exists at OriginalPath.
func (r *Reverter) revertUpdateMode(op *models.BatchFileOperation) (*RevertFileResult, error) {
	destRoot := filepath.Dir(filepath.Dir(op.OriginalPath))

	// In update-mode, it's safe to delete generated files BEFORE NFO restore
	// because the video anchor still exists (D-03)
	r.handleGeneratedFiles(op, destRoot)

	// NFO restore — in update-mode, NFO IS the operation (D-05)
	if op.NFOSnapshot != "" {
		nfoPath := op.NFOPath
		if nfoPath == "" && op.MovieID != "" {
			nfoPath = filepath.Join(filepath.Dir(op.OriginalPath), op.MovieID+".nfo")
		}
		if nfoPath != "" {
			nfoDir := filepath.Dir(nfoPath)
			canonicalNfoDir, err := filepath.Abs(filepath.Clean(nfoDir))
			if err != nil {
				// Path error — fail the operation
				if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
					logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
				}
				return &RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Reason:       models.RevertReasonNFORestoreFailed,
					Error:        fmt.Sprintf("failed to canonicalize NFO path: %v", err),
				}, nil
			}
			if err := r.fs.MkdirAll(fsPath(canonicalNfoDir), 0755); err != nil {
				if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
					logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
				}
				return &RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Reason:       models.RevertReasonNFORestoreFailed,
					Error:        fmt.Sprintf("failed to create NFO directory: %v", err),
				}, nil
			}
			canonicalNfoPath := fsPath(filepath.Join(canonicalNfoDir, filepath.Base(nfoPath)))
			if err := afero.WriteFile(r.fs, canonicalNfoPath, []byte(op.NFOSnapshot), 0666); err != nil {
				// NFO restore IS the operation in update-mode — this is a hard failure (D-05)
				if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
					logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
				}
				return &RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Reason:       models.RevertReasonNFORestoreFailed,
					Error:        fmt.Sprintf("failed to restore NFO: %v", err),
				}, nil
			}
		}
	}

	if err := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusReverted); err != nil {
		return nil, fmt.Errorf("filesystem reverted but failed to persist revert status for op %d: %w", op.ID, err)
	}
	logging.Infof("Reverted update operation %d: movie=%s at %s", op.ID, op.MovieID, op.OriginalPath)
	return &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeReverted,
	}, nil
}

// revertMoveMode handles move-mode revert with anchor check, destination conflict check,
// conditional cleanup, and NFO error handling.
func (r *Reverter) revertMoveMode(op *models.BatchFileOperation) (*RevertFileResult, error) {
	var sourcePath string

	// In-place rename handling (D-08)
	if op.InPlaceRenamed && op.OriginalDirPath != "" {
		currentDir := filepath.Dir(op.NewPath)

		// --- Destination conflict check for in-place rename (D-04) ---
		if _, err := r.fs.Stat(op.OriginalDirPath); err == nil {
			// OriginalDirPath already exists — destination conflict
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       models.RevertReasonDestinationConflict,
				Error:        fmt.Sprintf("directory %s already exists (destination conflict)", op.OriginalDirPath),
			}, nil
		}

		// Rename the current directory back to the original directory path
		if err := r.fs.Rename(currentDir, op.OriginalDirPath); err != nil {
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			reason := models.RevertReasonUnexpectedPathState
			if os.IsPermission(err) {
				reason = models.RevertReasonAccessDenied
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       reason,
				Error:        fmt.Sprintf("failed to rename directory back: %v", err),
			}, nil
		}

		// After dir rename, the file is now at OriginalDirPath/Base(NewPath)
		sourcePath = filepath.Join(op.OriginalDirPath, filepath.Base(op.NewPath))

		// If OriginalPath differs from current location, rename the file within the directory
		if sourcePath != op.OriginalPath {
			targetDir := filepath.Dir(op.OriginalPath)
			if err := r.fs.MkdirAll(targetDir, 0755); err != nil {
				if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
					logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
				}
				return &RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Reason:       models.RevertReasonUnexpectedPathState,
					Error:        fmt.Sprintf("failed to create directory for file rename: %v", err),
				}, nil
			}
			if err := r.fs.Rename(sourcePath, op.OriginalPath); err != nil {
				if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
					logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
				}
				reason := models.RevertReasonUnexpectedPathState
				if os.IsPermission(err) {
					reason = models.RevertReasonAccessDenied
				}
				return &RevertFileResult{
					OperationID:  op.ID,
					MovieID:      op.MovieID,
					OriginalPath: op.OriginalPath,
					NewPath:      op.NewPath,
					Outcome:      models.RevertOutcomeFailed,
					Reason:       reason,
					Error:        fmt.Sprintf("failed to rename file within directory: %v", err),
				}, nil
			}
		}
	} else {
		// --- Destination conflict check for standard move (D-04) ---
		if _, err := r.fs.Stat(op.OriginalPath); err == nil {
			// OriginalPath already exists — destination conflict
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       models.RevertReasonDestinationConflict,
				Error:        fmt.Sprintf("file %s already exists (destination conflict)", op.OriginalPath),
			}, nil
		}

		targetDir := filepath.Dir(op.OriginalPath)
		canonicalDir, err := filepath.Abs(filepath.Clean(targetDir))
		if err != nil {
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       models.RevertReasonUnexpectedPathState,
				Error:        fmt.Sprintf("failed to canonicalize directory path: %v", err),
			}, nil
		}
		if err := r.fs.MkdirAll(fsPath(canonicalDir), 0755); err != nil {
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       models.RevertReasonAccessDenied,
				Error:        fmt.Sprintf("failed to recreate original directory: %v", err),
			}, nil
		}

		sourcePath = op.NewPath
		if err := r.fs.Rename(sourcePath, op.OriginalPath); err != nil {
			if dbErr := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusFailed); dbErr != nil {
				logging.Warnf("Failed to update revert status for op %d: %v", op.ID, dbErr)
			}
			reason := models.RevertReasonUnexpectedPathState
			if os.IsPermission(err) {
				reason = models.RevertReasonAccessDenied
			}
			return &RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Reason:       reason,
				Error:        fmt.Sprintf("failed to revert move: %v", err),
			}, nil
		}

		destRoot := filepath.Dir(filepath.Dir(op.NewPath))
		r.cleanupEmptyDir(filepath.Dir(op.NewPath), destRoot)
	}

	// --- Conditional cleanup (D-03): generated files cleanup AFTER successful primary move ---
	destRoot := filepath.Dir(filepath.Dir(op.NewPath))
	r.handleGeneratedFiles(op, destRoot)

	// Re-check if generated file deletion made the directory empty
	if !op.InPlaceRenamed {
		destRoot := filepath.Dir(filepath.Dir(op.NewPath))
		r.cleanupEmptyDir(filepath.Dir(op.NewPath), destRoot)
	}

	// --- NFO restore (D-05): in move-mode, NFO failure is a warning, not a hard failure ---
	var nfoWarning string
	if op.NFOSnapshot != "" {
		nfoPath := op.NFOPath
		if nfoPath == "" && op.MovieID != "" {
			nfoPath = filepath.Join(filepath.Dir(op.OriginalPath), op.MovieID+".nfo")
		}
		if nfoPath != "" {
			nfoDir := filepath.Dir(op.OriginalPath)
			canonicalNfoDir, err := filepath.Abs(filepath.Clean(nfoDir))
			if err == nil {
				_ = r.fs.MkdirAll(fsPath(canonicalNfoDir), 0755)
				restorePath := fsPath(filepath.Join(canonicalNfoDir, filepath.Base(nfoPath)))
				if err := afero.WriteFile(r.fs, restorePath, []byte(op.NFOSnapshot), 0666); err != nil {
					// NFO restore failed — log warning, but the primary revert succeeded (D-05)
					logging.Warnf("Failed to restore NFO for op %d: %v (move-mode: treating as warning)", op.ID, err)
					nfoWarning = fmt.Sprintf("NFO restore failed: %v", err)
				}
			}
		}
	}

	if err := r.batchFileOpRepo.UpdateRevertStatus(op.ID, models.RevertStatusReverted); err != nil {
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
	logging.Infof("Reverted operation %d: movie=%s moved from %s back to %s", op.ID, op.MovieID, op.NewPath, op.OriginalPath)
	return result, nil
}

// cleanupEmptyDir removes the directory at dirPath if it is empty.
// Best-effort: errors are logged but not returned. Does not remove non-empty directories.
// Walks up parent directories removing empty ones until hitting stopAt or a non-empty directory.
func (r *Reverter) cleanupEmptyDir(dirPath string, stopAt string) {
	current := filepath.Clean(dirPath)
	stop := filepath.Clean(stopAt)

	for current != "" && current != "." && current != "/" && current != stop {
		// Read directory entries to check if empty
		entries, err := afero.ReadDir(r.fs, current)
		if err != nil {
			// Directory doesn't exist or can't be read — nothing to clean up
			return
		}
		if len(entries) > 0 {
			// Directory is not empty — stop walking up
			return
		}
		// Directory is empty — remove it
		if err := r.fs.Remove(current); err != nil {
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

// handleGeneratedFiles processes the GeneratedFiles JSON on a BatchFileOperation:
// deletes files in the Delete list and moves back files in the MoveBack list.
// After deleting files, it removes empty parent directories left behind,
// stopping at stopAt boundary to prevent removing shared ancestor directories.
// Best-effort: missing files are skipped (os.IsNotExist), errors don't fail the revert.
func (r *Reverter) handleGeneratedFiles(op *models.BatchFileOperation, stopAt string) {
	if op.GeneratedFiles == "" {
		return
	}
	var gf GeneratedFilesJSON
	if err := json.Unmarshal([]byte(op.GeneratedFiles), &gf); err != nil {
		return
	}
	// Track parent directories of deleted files for cleanup
	dirsToCheck := make(map[string]bool)
	// Delete files in the Delete array (best-effort — skip IsNotExist)
	for _, path := range gf.Delete {
		if err := r.fs.Remove(path); err != nil && !os.IsNotExist(err) {
			_ = err
		}
		dirsToCheck[filepath.Dir(path)] = true
	}
	// Move back files in the MoveBack array (best-effort)
	for _, fm := range gf.MoveBack {
		if err := r.fs.Rename(fm.NewPath, fm.OriginalPath); err != nil {
			_ = err
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
		r.cleanupEmptyDirDownward(cleanDir, stopAt)
	}
}

// isDescendant checks if path is inside parentDir (or equal to it).
// Returns true if path has parentDir as a prefix when both are cleaned.
func isDescendant(path string, parentDir string) bool {
	if path == parentDir {
		return true
	}
	if len(path) > len(parentDir) && path[:len(parentDir)+1] == parentDir+string(filepath.Separator) {
		return true
	}
	return false
}

// cleanupEmptyDirDownward removes empty directories starting from dirPath,
// walking up through parents until hitting stopAt boundary.
// Unlike cleanupEmptyDir (which walks UP from a starting dir), this handles
// the case where deleting files leaves empty subdirectories.
func (r *Reverter) cleanupEmptyDirDownward(dirPath string, stopAt string) {
	current := filepath.Clean(dirPath)
	stop := filepath.Clean(stopAt)

	for {
		entries, err := afero.ReadDir(r.fs, current)
		if err != nil {
			return
		}
		if len(entries) > 0 {
			return
		}
		if current == stop {
			return
		}
		if err := r.fs.Remove(current); err != nil {
			return
		}
		parent := filepath.Dir(current)
		if parent == current || parent == "." || parent == "/" {
			return
		}
		current = parent
	}
}

// RevertBatch reverts all operations in a batch (D-02, D-04).
// It uses best-effort processing: individual failures don't abort the batch.
// After all operations are processed, it sweeps empty destination directories.
func (r *Reverter) RevertBatch(ctx context.Context, batchJobID string) (*RevertBatchResult, error) {
	ops, err := r.batchFileOpRepo.FindByBatchJobID(batchJobID)
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

	// Process each operation (D-04: best-effort, continue on failure)
	result := &RevertBatchResult{
		Total: len(processable),
	}

	// Pre-collect destRoots for batch-level cleanup. We track these before
	// processing so that even if individual revertFile calls fail (e.g., DB
	// status persistence failure after filesystem success), the batch sweep
	// still attempts to clean up empty directories. cleanupEmptyDir is safe
	// to call on any directory — it only removes actually-empty ones.
	destRoots := make(map[string]bool)
	for i := range processable {
		if !processable[i].InPlaceRenamed && processable[i].NewPath != "" {
			destRoots[filepath.Dir(filepath.Dir(processable[i].NewPath))] = true
		}
	}

	for i := range processable {
		op := &processable[i]
		res, sysErr := r.revertFile(ctx, op)
		if sysErr != nil {
			result.Failed++
			result.Outcomes = append(result.Outcomes, RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Error:        sysErr.Error(),
			})
			continue
		}

		switch res.Outcome {
		case models.RevertOutcomeReverted:
			result.Succeeded++
		case models.RevertOutcomeSkipped:
			result.Skipped++
		case models.RevertOutcomeFailed:
			result.Failed++
		}
		result.Outcomes = append(result.Outcomes, *res)
	}

	// Batch-level cleanup: per-file cleanupEmptyDir uses destRoot as a stop
	// boundary, which leaves intermediate parent directories behind (e.g.,
	// out/ABP-880/ when the file was at out/ABP-880/dir/ABP-880.mp4).
	// Sweep each destRoot with stopAt="" so cleanupEmptyDir walks all the
	// way up. It stops automatically at non-empty directories, so populated
	// ancestors (including the top-level output directory if it contains other
	// files) are preserved. An empty output directory will be removed, which
	// is the correct behavior after a full batch revert.
	for dirPath := range destRoots {
		r.cleanupEmptyDir(filepath.Clean(dirPath), "")
	}

	return result, nil
}

// RevertScrape reverts only the operations for a specific movie within a batch (HIST-04).
func (r *Reverter) RevertScrape(ctx context.Context, batchJobID string, movieID string) (*RevertBatchResult, error) {
	ops, err := r.batchFileOpRepo.FindByBatchJobID(batchJobID)
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

	// Process each matching operation
	result := &RevertBatchResult{
		Total: len(matching),
	}

	destRoots := make(map[string]bool)
	for i := range matching {
		if !matching[i].InPlaceRenamed && matching[i].NewPath != "" {
			destRoots[filepath.Dir(filepath.Dir(matching[i].NewPath))] = true
		}
	}

	for i := range matching {
		op := &matching[i]
		res, sysErr := r.revertFile(ctx, op)
		if sysErr != nil {
			result.Failed++
			result.Outcomes = append(result.Outcomes, RevertFileResult{
				OperationID:  op.ID,
				MovieID:      op.MovieID,
				OriginalPath: op.OriginalPath,
				NewPath:      op.NewPath,
				Outcome:      models.RevertOutcomeFailed,
				Error:        sysErr.Error(),
			})
			continue
		}

		switch res.Outcome {
		case models.RevertOutcomeReverted:
			result.Succeeded++
		case models.RevertOutcomeSkipped:
			result.Skipped++
		case models.RevertOutcomeFailed:
			result.Failed++
		}
		result.Outcomes = append(result.Outcomes, *res)
	}

	for dirPath := range destRoots {
		r.cleanupEmptyDir(filepath.Clean(dirPath), "")
	}

	return result, nil
}
