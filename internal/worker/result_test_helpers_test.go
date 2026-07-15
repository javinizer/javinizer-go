package worker

import "github.com/javinizer/javinizer-go/internal/worker/resultstore"

// resultTestSnapshot exposes result-state snapshots for test assertions without
// reaching into resultstore internals. Per the result-store extraction, tests
// must not access resultTrackerState fields directly; these helpers mediate all
// reads through the Store interface.
//
// snap returns a deep-cloned ResultSnapshot (Results/Files/Excluded/FileMatchInfo/
// Provenance/ResultIDIndex) via SnapshotData().
func (job *BatchJob) snap() resultstore.ResultSnapshot {
	return job.results.SnapshotData()
}

// prog returns the current ProgressSnapshot (TotalFiles/Completed/Failed/Progress)
// via SnapshotForStatus().
func (job *BatchJob) prog() resultstore.ProgressSnapshot {
	_, p := job.results.SnapshotForStatus()
	return p
}
