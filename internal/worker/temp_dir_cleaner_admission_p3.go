package worker

import "github.com/javinizer/javinizer-go/internal/models"

// POSTER-WRITE-HARDENING P3 — admission-lease awareness for stale temp
// cleanup. A job with an in-flight edit (shared lease), queued/running
// delete-drain, or queued phase start must never have its temp poster staging
// wiped mid-surgery: cleanup of that job's dir parks until the edit
// completes, and a cancelled in-flight edit is ordered the same way (the
// cancel releases the lease; the next sweep sees the dir cleanly).

// AdmissionProbe reports whether a job currently holds any admission lease.
type AdmissionProbe func(jobID string) (inFlight bool)

// WithAdmissionProbe installs the lease probe consulted before any per-job
// removal decision.
func WithAdmissionProbe(probe AdmissionProbe) func(*TempDirCleaner) {
	return func(c *TempDirCleaner) { c.admissionProbe = probe }
}

// admissionBusy is the JobStore's probe: in-memory jobs carry the admission
// barrier, so only live leases are observable (a job absent from the store
// has no barrier to consult).
func (s *JobStore) admissionBusy(jobID string) bool {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(jobID)]
	s.mu.RUnlock()
	if !ok || job == nil || job.admission == nil {
		return false
	}
	return job.admission.inFlight()
}
