package worker

import (
	"fmt"
	"sort"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ResultSnapshot is a point-in-time clone of all result data.
type ResultSnapshot struct {
	Results       map[string]*MovieResult
	Files         []string
	Excluded      map[string]bool
	FileMatchInfo map[string]models.FileMatchInfo
	Provenance    map[string]*ProvenanceData
	ResultIDIndex map[string]string
}

// ProgressSnapshot holds the progress counters from ResultTracker.
// Per ADR-0041/0042: BatchJob consumes its own sub-manager interfaces
// instead of reaching into internals.
type ProgressSnapshot struct {
	TotalFiles int
	Completed  int
	Failed     int
	Progress   float64
}

// resultReadStore owns all read methods for result tracking.
// It holds a pointer to the shared resultTrackerState, providing
// locality for reads — a change to read logic doesn't require
// understanding write logic and vice versa.
//
// Per DEEP-3: extracted from ResultTracker to split the 38-method
// monolith into independently understandable halves.
type resultReadStore struct {
	*resultTrackerState
}

// --- ResultMapAccessor read methods ---

// IsGone returns true if the job is deleted or transitioned to a terminal state.
func (rr *resultReadStore) IsGone() bool {
	if rr.goneChecker != nil {
		return rr.goneChecker()
	}
	return false
}

// GetFileMatchInfo returns match metadata for a file.
func (rr *resultReadStore) GetFileMatchInfo(filePath string) (models.FileMatchInfo, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	info, ok := rr.FileMatchInfo[filePath]
	return info, ok
}

// GetCurrentMovieID returns the movie ID for a file's current result.
// Returns empty string if no result or no movie.
func (rr *resultReadStore) GetCurrentMovieID(filePath string) string {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if r := rr.Results[filePath]; r != nil {
		if r.Movie != nil && r.Movie.ID != "" {
			return r.Movie.ID
		}
		return r.FileMatchInfo.MovieID
	}
	return ""
}

// OtherResultUsesMovieID checks if any result OTHER than excludePath uses the given movieID.
func (rr *resultReadStore) OtherResultUsesMovieID(excludePath string, movieID string) bool {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePaths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	for _, fp := range filePaths {
		if fp != excludePath {
			return true
		}
	}
	return false
}

// GetMovieResult returns a clone of the MovieResult for a file path.
func (rr *resultReadStore) GetMovieResult(filePath string) (*MovieResult, error) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	result, ok := rr.Results[filePath]
	if !ok || result == nil {
		return nil, fmt.Errorf("file result not found: %s", filePath)
	}
	return result.Clone(), nil
}

// GetProvenance returns provenance metadata for a file path.
func (rr *resultReadStore) GetProvenance(filePath string) *ProvenanceData {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if rr.Provenance == nil {
		return nil
	}
	if p := rr.Provenance[filePath]; p != nil {
		return p.Clone()
	}
	return nil
}

// GetResults returns a flat slice of all non-nil MovieResult clones.
func (rr *resultReadStore) GetResults() []MovieResult {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	results := make([]MovieResult, 0, len(rr.Results))
	for _, v := range rr.Results {
		if v != nil {
			copied := v.Clone()
			results = append(results, *copied)
		}
	}
	return results
}

// GetFiles returns a clone of the file list.
func (rr *resultReadStore) GetFiles() []string {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	files := make([]string, len(rr.Files))
	copy(files, rr.Files)
	return files
}

// CloneResults returns a deep clone of the Results map.
func (rr *resultReadStore) CloneResults() map[string]*MovieResult {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return stateCloneResultsLocked(rr.resultTrackerState)
}

// CloneFileMatchInfo returns a shallow clone of the FileMatchInfo map.
func (rr *resultReadStore) CloneFileMatchInfo() map[string]models.FileMatchInfo {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return stateCloneFileMatchInfoLocked(rr.resultTrackerState)
}

// IsAllExcluded returns true if all files in the result set are excluded.
// Per DEEP-5: exported so ResultMapAccessor can include it in its interface surface.
func (rr *resultReadStore) IsAllExcluded() bool {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return rr.allFilesExcludedLocked()
}

func (rr *resultReadStore) allFilesExcludedLocked() bool {
	if len(rr.Results) == 0 {
		return len(rr.Files) > 0 && len(rr.Excluded) >= len(rr.Files)
	}
	for filePath := range rr.Results {
		if !rr.Excluded[filePath] {
			return false
		}
	}
	return true
}

// --- FileFinder methods ---

// FindFileForMovieID resolves a movie ID to its primary file path and revision.
// Per RACE-2 fix: holds a single RLock for the entire method so the index
// and result are read atomically, closing the TOCTOU gap that existed when
// two separate RLock acquisitions allowed the index to change between reads.
func (rr *resultReadStore) FindFileForMovieID(movieID string) (*FileLookupResult, error) {
	// movieID normalization (lowercase) is centralized inside
	// stateLookupFilePathsForMovieIDLocked via indexKey(), so every public
	// lookup — FindFileForMovieID, OtherResultUsesMovieID, and the rest —
	// shares one consistent normalized path. Do not pre-normalize here.
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePaths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("movie ID %q not found in results", movieID)
	}
	sorted := make([]string, len(filePaths))
	copy(sorted, filePaths)
	sort.Strings(sorted)
	foundFilePath := sorted[0]
	result, ok := rr.Results[foundFilePath]
	if !ok || result == nil {
		return nil, fmt.Errorf("result not found for movie ID %q at path %q", movieID, foundFilePath)
	}
	var capturedRevision uint64
	var oldMovieID string
	capturedRevision = result.Revision
	if result.Movie != nil {
		oldMovieID = result.Movie.ID
	} else {
		oldMovieID = result.FileMatchInfo.MovieID
	}
	return &FileLookupResult{
		FilePath:         foundFilePath,
		OldMovieID:       oldMovieID,
		CapturedRevision: capturedRevision,
	}, nil
}

// GetRevision returns the current revision for a file's result.
func (rr *resultReadStore) GetRevision(filePath string) uint64 {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if r := rr.Results[filePath]; r != nil {
		return r.Revision
	}
	return 0
}

// --- MovieLookup methods ---

// FindFilePathsForMovieID returns all file paths associated with a movie ID.
func (rr *resultReadStore) FindFilePathsForMovieID(movieID string) []string {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	paths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	if len(paths) == 0 {
		return nil
	}
	cp := make([]string, len(paths))
	copy(cp, paths)
	return cp
}

// FindMovieResultForMovieID returns the MovieResult for the primary file path
// associated with a movie ID.
func (rr *resultReadStore) FindMovieResultForMovieID(movieID string) (*MovieResult, error) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePaths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("movie ID %q not found in results", movieID)
	}
	sorted := make([]string, len(filePaths))
	copy(sorted, filePaths)
	sort.Strings(sorted)
	if r := rr.Results[sorted[0]]; r != nil {
		if r.Movie == nil {
			return nil, fmt.Errorf("no movie result found for movie ID %q at path %q", movieID, sorted[0])
		}
		return r.Clone(), nil
	}
	return nil, fmt.Errorf("no result for movie ID %q at path %q", movieID, sorted[0])
}

// GetMovieResultsForMovieID returns all MovieResults for a given movie ID.
func (rr *resultReadStore) GetMovieResultsForMovieID(movieID string) []*MovieResult {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePaths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	results := make([]*MovieResult, 0, len(filePaths))
	for _, fp := range filePaths {
		if r := rr.Results[fp]; r != nil {
			cloned := r.Clone()
			results = append(results, cloned)
		}
	}
	return results
}

// GetFileMatchInfosForMovieID returns FileMatchInfo for all files associated
// with a movie ID.
func (rr *resultReadStore) GetFileMatchInfosForMovieID(movieID string) []models.FileMatchInfo {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePaths := stateLookupFilePathsForMovieIDLocked(rr.resultTrackerState, movieID)
	var infos []models.FileMatchInfo
	for _, filePath := range filePaths {
		result := rr.Results[filePath]
		if result != nil {
			infos = append(infos, result.FileMatchInfo)
		}
	}
	return infos
}

// GetFileResultByResultID returns the MovieResult and its file path for
// a given stable ResultID (thread-safe). Returns (result, filePath, found).
func (rr *resultReadStore) GetFileResultByResultID(resultID string) (*MovieResult, string, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	filePath, found := stateLookupFilePathForResultIDLocked(rr.resultTrackerState, resultID)
	if !found {
		return nil, "", false
	}
	if r := rr.Results[filePath]; r != nil {
		return r.Clone(), filePath, true
	}
	return nil, "", false
}

// --- Snapshot methods ---

// SnapshotForStatus returns a point-in-time snapshot of result data
// alongside progress counters. The caller must NOT be holding results.mu
// when calling this method (it acquires the lock internally).
func (rr *resultReadStore) SnapshotForStatus() (ResultSnapshot, ProgressSnapshot) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return rr.snapshotDataLocked(), rr.progressSnapshotLocked()
}

// progressSnapshotLocked returns progress counters.
// The caller MUST be holding rr.mu when calling this method.
func (rr *resultReadStore) progressSnapshotLocked() ProgressSnapshot {
	return ProgressSnapshot{
		TotalFiles: rr.TotalFiles,
		Completed:  rr.Completed,
		Failed:     rr.Failed,
		Progress:   rr.Progress,
	}
}

// snapshotForStatusLocked returns both result data and progress counters.
// The caller MUST be holding rr.mu when calling this method.
func (rr *resultReadStore) snapshotForStatusLocked() (ResultSnapshot, ProgressSnapshot) {
	return rr.snapshotDataLocked(), rr.progressSnapshotLocked()
}

// SnapshotData returns a point-in-time clone of all result data.
func (rr *resultReadStore) SnapshotData() ResultSnapshot {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return rr.snapshotDataLocked()
}

func (rr *resultReadStore) snapshotDataLocked() ResultSnapshot {
	return ResultSnapshot{
		Results: stateCloneResultsLocked(rr.resultTrackerState),
		Files: func() []string {
			clone := make([]string, len(rr.Files))
			copy(clone, rr.Files)
			return clone
		}(),
		Excluded: func() map[string]bool {
			clone := make(map[string]bool, len(rr.Excluded))
			for k, v := range rr.Excluded {
				clone[k] = v
			}
			return clone
		}(),
		FileMatchInfo: stateCloneFileMatchInfoLocked(rr.resultTrackerState),
		Provenance: func() map[string]*ProvenanceData {
			clone := make(map[string]*ProvenanceData, len(rr.Provenance))
			for k, v := range rr.Provenance {
				clone[k] = v.Clone()
			}
			return clone
		}(),
		ResultIDIndex: func() map[string]string {
			if rr.resultIDIndex == nil {
				return nil
			}
			clone := make(map[string]string, len(rr.resultIDIndex))
			for k, v := range rr.resultIDIndex {
				clone[k] = v
			}
			return clone
		}(),
	}
}
