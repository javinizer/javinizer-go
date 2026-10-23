package worker

import (
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// recordApplyRetryFailure runs under the file family's write-back lock. The
// normal phase-envelope persist durably stores this separate ledger, without
// bumping the protected result's revision (poster recovery uses that revision).
func (job *BatchJob) recordApplyRetryFailure(path string, admitted *resultstore.MovieResult, message string) {
	if admitted == nil || admitted.ResultID == "" {
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.applyRetryFailures == nil {
		job.applyRetryFailures = make(map[string]jobpersist.ApplyRetryFailure)
	}
	job.applyRetryFailures[path] = jobpersist.ApplyRetryFailure{ResultID: admitted.ResultID, Revision: admitted.Revision, Error: message}
}

// projectApplyRetryFailures changes only a read snapshot. The durable domain
// result and provenance remain untouched, so reconciliation cannot mistake a
// failed receipt for a committed poster mutation. Existing REST/UI retry paths
// see failed rows; a newer result revision or identity cannot inherit the error.
func projectApplyRetryFailures(snapshot *batchJobSnapshot) {
	delta := jobpersist.ProjectApplyRetryFailures(snapshot.results, snapshot.Excluded, snapshot.applyRetryFailures)
	snapshot.Completed -= delta
	snapshot.Failed += delta
}

func (job *BatchJob) applyRetryFailurePending(path string, current *resultstore.MovieResult) bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	failure, ok := job.applyRetryFailures[path]
	return ok && failure.Matches(current)
}
