package resultstore

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// ResultTracker manages the shared mutable state for result tracking.
// It satisfies the Store interface (and all narrow sub-interfaces) defined in
// store.go. Callers that only need a narrow surface should accept the relevant
// sub-interface directly.
//
// Per DEEP-3: ResultTracker uses Go embedding to promote all methods from
// resultUpdater (write methods) and resultReadStore (read methods) without
// boilerplate delegation. Both halves hold the same *resultTrackerState, so
// reads and writes operate on the same shared state protected by the shared mutex.
type ResultTracker struct {
	*resultTrackerState
	*resultUpdater
	*resultReadStore
}

// New creates a fully-wired Store with empty result maps, a movie-ID index, a
// result-ID index, and zero progress.
func New(totalFiles int, files []string) Store {
	s := &resultTrackerState{
		TotalFiles:    totalFiles,
		Files:         files,
		Results:       make(map[string]*MovieResult),
		Provenance:    make(map[string]*ProvenanceData),
		FileMatchInfo: make(map[string]models.FileMatchInfo),
		Excluded:      make(map[string]bool),
		movieIDIndex:  make(map[string][]string),
	}
	return newResultTrackerFromState(s)
}

// newResultTrackerFromState wires a ResultTracker around an existing state.
func newResultTrackerFromState(s *resultTrackerState) *ResultTracker {
	return &ResultTracker{
		resultTrackerState: s,
		resultUpdater:      &resultUpdater{resultTrackerState: s},
		resultReadStore:    &resultReadStore{resultTrackerState: s},
	}
}

// NewFromSnapshot constructs a Store pre-populated with ALL fields that
// reconstructResultTracker sets directly (totalFiles, files, results,
// provenance, fileMatchInfo, excluded, completed, failed, progress). The
// movie-ID and result-ID indexes are rebuilt from the provided results.
func NewFromSnapshot(
	totalFiles int,
	files []string,
	results map[string]*MovieResult,
	provenance map[string]*ProvenanceData,
	fileMatchInfo map[string]models.FileMatchInfo,
	excluded map[string]bool,
	completed int,
	failed int,
	progress float64,
) Store {
	if results == nil {
		results = make(map[string]*MovieResult)
	}
	if provenance == nil {
		provenance = make(map[string]*ProvenanceData)
	}
	if fileMatchInfo == nil {
		fileMatchInfo = make(map[string]models.FileMatchInfo)
	}
	if excluded == nil {
		excluded = make(map[string]bool)
	}
	s := &resultTrackerState{
		TotalFiles:    totalFiles,
		Files:         files,
		Results:       results,
		Provenance:    provenance,
		FileMatchInfo: fileMatchInfo,
		Excluded:      excluded,
		Completed:     completed,
		Failed:        failed,
		Progress:      progress,
		movieIDIndex:  make(map[string][]string),
	}
	rt := newResultTrackerFromState(s)
	rt.rebuildIndexesLocked()
	return rt
}

// SetGoneChecker installs the callback consulted by IsGone. Store-only.
func (rt *ResultTracker) SetGoneChecker(fn func() bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.goneChecker = fn
}

// ForceCompleteProgress recalculates progress and, if below 100%, sets it to
// 100%. Replaces the direct mutex/field access in BatchJob.attachLifecycleCallback.
// Store-only.
func (rt *ResultTracker) ForceCompleteProgress() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	stateRecalculateProgress(rt.resultTrackerState)
	if rt.Progress < 100 {
		rt.Progress = 100
	}
}

// ReplaceResultRaw directly replaces a file's result WITHOUT recalculating
// progress counters or incrementing revisions, but DOES perform a FULL rebuild
// of both movieIDIndex and resultIDIndex. Does NOT clone the input — stores
// the caller's pointer directly. Store-only. Intended for test setup.
func (rt *ResultTracker) ReplaceResultRaw(filePath string, result *MovieResult) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.Results[filePath] = result
	stateRebuildMovieIDIndexLocked(rt.resultTrackerState)
}

// RawResults returns the internal results map without cloning. The caller
// MUST NOT add or remove keys from the returned map. Returned *MovieResult
// pointers are mutable; callers may modify fields through them. Used by
// ClearMissingTempPosters to iterate and mutate poster URLs in place. Store-only.
func (rt *ResultTracker) RawResults() map[string]*MovieResult {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.Results
}

// RebuildIndexes performs a full rebuild of both movieIDIndex and resultIDIndex
// from the current Results map. Store-only. Intended for test setup.
func (rt *ResultTracker) RebuildIndexes() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	stateRebuildMovieIDIndexLocked(rt.resultTrackerState)
}

// rebuildIndexesLocked is the unexported, lock-holding variant used by
// NewFromSnapshot (which builds state before exposing it).
func (rt *ResultTracker) rebuildIndexesLocked() {
	stateRebuildMovieIDIndexLocked(rt.resultTrackerState)
}

// RecalculateProgress recalculates progress counters from the Results map
// without forcing to 100%. Store-only.
func (rt *ResultTracker) RecalculateProgress() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	stateRecalculateProgress(rt.resultTrackerState)
}

// LoadResultsRaw bulk-replaces the results map and file match info map without
// cloning, recalculating progress, or incrementing revisions. Store-only.
// Intended for test setup — follow with RebuildIndexes().
func (rt *ResultTracker) LoadResultsRaw(results map[string]*MovieResult, fileMatchInfo map[string]models.FileMatchInfo) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if results == nil {
		results = make(map[string]*MovieResult)
	}
	if fileMatchInfo == nil {
		fileMatchInfo = make(map[string]models.FileMatchInfo)
	}
	rt.Results = results
	rt.FileMatchInfo = fileMatchInfo
}
