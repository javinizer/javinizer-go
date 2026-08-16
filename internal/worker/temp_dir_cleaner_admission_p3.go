package worker

import "github.com/javinizer/javinizer-go/internal/models"

// POSTER-WRITE-HARDENING P3 — admission-lease awareness for stale temp
// cleanup. A job with an in-flight edit (shared lease), queued/running
// delete-drain, or queued phase start must never have its temp poster staging
// wiped mid-surgery: cleanup of that job's dir parks until the edit
// completes, and a cancelled in-flight edit is ordered the same way (the
// cancel releases the lease; the next sweep sees the dir cleanly).

// AdmissionProbe holds a job's EXCLUSIVE admission lease for the duration
// of its removal decision and deletion; (nil, false) means another operation
// holds or queues on the lease and the directory must be skipped. Holding
// the lease across the removal closes the point-in-time gap where a new
// edit admits between probe and delete (codex P3 R3-4).
type AdmissionProbe func(jobID string) (release func(), ok bool)

// WithAdmissionProbe installs the lease prober consulted before any per-job
// removal decision.
func WithAdmissionProbe(probe AdmissionProbe) func(*TempDirCleaner) {
	return func(c *TempDirCleaner) { c.admissionProbe = probe }
}

// admissionBusy is the JobStore's lease prober: in-memory jobs carry the
// admission barrier, so only live leases are observable (a job absent from
// the store has no barrier to consult — the cleanup lease is vacuous).
func (s *JobStore) admissionBusy(jobID string) (release func(), ok bool) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(jobID)]
	s.mu.RUnlock()
	if !ok || job == nil || job.admission == nil {
		return func() {}, true
	}
	return job.admission.TryAdmitExclusive()
}
