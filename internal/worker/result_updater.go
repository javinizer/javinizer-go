package worker

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// resultUpdater owns all write methods for result tracking.
// It holds a pointer to the shared resultTrackerState, providing
// locality for mutations — a change to update logic doesn't require
// understanding read logic and vice versa.
//
// Per DEEP-3: extracted from ResultTracker to split the 38-method
// monolith into independently understandable halves.
type resultUpdater struct {
	*resultTrackerState
}

// UpdateFileResult records a result for a file path, adjusting progress counters.
func (ru *resultUpdater) UpdateFileResult(filePath string, result *MovieResult) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	existing := ru.Results[filePath]
	stateReindexFilePathLocked(ru.resultTrackerState, filePath, existing, result)
	if existing != nil {
		// Only decrement counters if the file was not excluded — excluded files
		// were never counted, so decrementing them would cause counter drift.
		// Matches the guard pattern in AtomicUpdateFileResult.
		if !ru.Excluded[filePath] {
			switch existing.Status {
			case models.JobStatusCompleted:
				ru.Completed--
			case models.JobStatusFailed:
				ru.Failed--
			}
		}
		result.Revision = existing.Revision + 1
		if existing.ResultID != "" {
			result.ResultID = existing.ResultID
		}
	} else {
		result.Revision = 1
	}
	ru.Results[filePath] = result.Clone()
	if !ru.Excluded[filePath] {
		switch result.Status {
		case models.JobStatusCompleted:
			ru.Completed++
		case models.JobStatusFailed:
			ru.Failed++
		}
	}
	// For excluded files the Completed/Failed counters are deliberately not
	// touched above, so the counter-based helper would compute progress from
	// stale counters and could regress after a late terminal update. Trigger a
	// full recalculation instead, which iterates actual results and skips
	// excluded paths. Non-excluded updates keep the fast counter path.
	if ru.Excluded[filePath] {
		stateRecalculateProgress(ru.resultTrackerState)
	} else {
		stateUpdateProgressFromCounters(ru.resultTrackerState)
	}
}

// SetProvenance records provenance metadata for a file path.
func (ru *resultUpdater) SetProvenance(filePath string, prov *ProvenanceData) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	if ru.Provenance == nil {
		ru.Provenance = make(map[string]*ProvenanceData)
	}
	ru.Provenance[filePath] = prov.Clone()
}

// AtomicUpdateFileResult performs a locked read-modify-write on a file result.
// The updateFn receives a clone of the current result; it must return the
// updated result (which will be stored with an incremented revision).
func (ru *resultUpdater) AtomicUpdateFileResult(filePath string, updateFn func(*MovieResult) (*MovieResult, error)) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	current, exists := ru.Results[filePath]
	if !exists || current == nil {
		return fmt.Errorf("file result not found: %s", filePath)
	}
	copied := current.Clone()
	updated, err := updateFn(copied)
	if err != nil {
		return err
	}
	updated.Revision = current.Revision + 1
	stateReindexFilePathLocked(ru.resultTrackerState, filePath, current, updated)
	if !ru.Excluded[filePath] {
		switch current.Status {
		case models.JobStatusCompleted:
			ru.Completed--
		case models.JobStatusFailed:
			ru.Failed--
		}
		switch updated.Status {
		case models.JobStatusCompleted:
			ru.Completed++
		case models.JobStatusFailed:
			ru.Failed++
		}
	}
	stateUpdateProgressFromCounters(ru.resultTrackerState)
	ru.Results[filePath] = updated
	return nil
}

// UpdateMovie atomically updates the movie for a file result.
func (ru *resultUpdater) UpdateMovie(filePath string, movie *models.Movie) error {
	return ru.AtomicUpdateFileResult(filePath, func(current *MovieResult) (*MovieResult, error) {
		current.Movie = movie.Clone()
		current.FileMatchInfo.MovieID = movie.ID
		return current, nil
	})
}

// MarkExcluded marks a file as excluded and adjusts progress counters.
// If the file's result has a non-terminal status (e.g. Running), it is set to
// JobStatusCancelled so that snapshots do not report excluded files as Running.
// Per DEEP-5: exported so ResultUpdater can include it in its interface surface.
func (ru *resultUpdater) MarkExcluded(filePath string) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	if existing, ok := ru.Results[filePath]; ok && existing != nil && !ru.Excluded[filePath] {
		switch existing.Status {
		case models.JobStatusCompleted:
			ru.Completed--
		case models.JobStatusFailed:
			ru.Failed--
		case models.JobStatusRunning:
			existing.Status = models.JobStatusCancelled
		}
	}
	ru.Excluded[filePath] = true
	// Per BUG-5: use stateRecalculateProgress instead of stateUpdateProgressFromCounters
	// so that excluded files are properly excluded from the progress calculation.
	// stateUpdateProgressFromCounters uses (Completed+Failed)/TotalFiles, which doesn't
	// account for excluded files still in Running status — recalculation iterates the
	// Results map and skips Excluded entries, producing accurate intermediate progress.
	stateRecalculateProgress(ru.resultTrackerState)
}

// SetFileResultRevision sets the revision counter for a file result.
// Returns an error if the file result does not exist.
func (ru *resultUpdater) SetFileResultRevision(filePath string, revision uint64) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	r, ok := ru.Results[filePath]
	if !ok {
		return fmt.Errorf("SetFileResultRevision: file result not found: %s", filePath)
	}
	r.Revision = revision
	return nil
}

// SetFileMatchInfo records match metadata for a single file path.
func (ru *resultUpdater) SetFileMatchInfo(path string, info models.FileMatchInfo) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	ru.FileMatchInfo[path] = info
}

// SetFileMatchInfoMap records match metadata for multiple file paths.
func (ru *resultUpdater) SetFileMatchInfoMap(info map[string]models.FileMatchInfo) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	for k, v := range info {
		ru.FileMatchInfo[k] = v
	}
}

// CommitResult atomically writes a result and recalculates progress.
// Returns an error if the revision doesn't match expectedRevision (optimistic concurrency).
// Per DEEP-3: CommitResult is a write method even though it sits on the
// ResultMapAccessor interface — the split is internal, and ResultTracker
// delegates to resultUpdater for the actual write.
func (ru *resultUpdater) CommitResult(filePath string, result *MovieResult, expectedRevision uint64) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	current := ru.Results[filePath]
	currentRevision := uint64(0)
	if current != nil {
		currentRevision = current.Revision
	}
	if currentRevision != expectedRevision {
		return fmt.Errorf("conflict: expected revision %d, got %d", expectedRevision, currentRevision)
	}
	if info, ok := ru.FileMatchInfo[filePath]; ok {
		result.FileMatchInfo = info
	}
	result.Revision = expectedRevision + 1
	if current != nil && current.ResultID != "" {
		result.ResultID = current.ResultID
	}
	stateReindexFilePathLocked(ru.resultTrackerState, filePath, current, result)
	ru.Results[filePath] = result
	stateRecalculateProgress(ru.resultTrackerState)
	return nil
}

// setFileMatchInfo is the unexported alias for SetFileMatchInfoMap.
// Used internally by jobController.
func (ru *resultUpdater) setFileMatchInfo(info map[string]models.FileMatchInfo) {
	ru.SetFileMatchInfoMap(info)
}

// recalculateProgress recomputes Completed/Failed/Progress from the Results map.
func (ru *resultUpdater) recalculateProgress() {
	stateRecalculateProgress(ru.resultTrackerState)
}

// rebuildMovieIDIndexLocked rebuilds the movie ID → file path index.
// The caller MUST be holding the lock when calling this method.
func (ru *resultUpdater) rebuildMovieIDIndexLocked() {
	stateRebuildMovieIDIndexLocked(ru.resultTrackerState)
}
