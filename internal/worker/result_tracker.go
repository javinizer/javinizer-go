package worker

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// ResultTracker manages the shared mutable state for result tracking.
// It satisfies the ResultUpdater, ResultMapAccessor, MovieLookup, and FileFinder
// interfaces defined in phase_interfaces.go — callers that only need a narrow surface
// should accept the relevant sub-interface directly.
//
// Per DEEP-3: ResultTracker uses Go embedding to promote all methods from
// resultUpdater (write methods) and resultReadStore (read methods) without
// boilerplate delegation. Both halves hold the same *resultTrackerState, so
// reads and writes operate on the same shared state protected by the shared mutex.
// The direct *resultTrackerState embed is kept at depth 1 so that external code
// (batch_job.go, job_store_persist.go) can directly access state fields.
type ResultTracker struct {
	*resultTrackerState
	*resultUpdater
	*resultReadStore
}

// newResultTrackerFromState wires a ResultTracker around an existing state,
// used by tests that need to pre-populate state.
//
//nolint:unused // used by 12+ test cases in this package
func newResultTrackerFromState(s *resultTrackerState) *ResultTracker {
	return &ResultTracker{
		resultTrackerState: s,
		resultUpdater:      &resultUpdater{resultTrackerState: s},
		resultReadStore:    &resultReadStore{resultTrackerState: s},
	}
}

// NewResultTracker creates a fully-wired ResultTracker.
func NewResultTracker(totalFiles int, files []string) *ResultTracker {
	s := &resultTrackerState{
		TotalFiles:    totalFiles,
		Files:         files,
		Results:       make(map[string]*MovieResult),
		Provenance:    make(map[string]*ProvenanceData),
		FileMatchInfo: make(map[string]models.FileMatchInfo),
		Excluded:      make(map[string]bool),
		movieIDIndex:  make(map[string][]string),
	}
	return &ResultTracker{
		resultTrackerState: s,
		resultUpdater:      &resultUpdater{resultTrackerState: s},
		resultReadStore:    &resultReadStore{resultTrackerState: s},
	}
}

// Updater returns the write half of the ResultTracker.
// Phase inputs that only need mutation access should use this.
func (rt *ResultTracker) Updater() *resultUpdater {
	return rt.resultUpdater
}

// ReadStore returns the read half of the ResultTracker.
// Phase inputs that only need read access should use this.
func (rt *ResultTracker) ReadStore() *resultReadStore {
	return rt.resultReadStore
}
