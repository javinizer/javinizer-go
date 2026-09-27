package fsutil

import "time"

// ProcessLiveness is a cross-platform process-existence verdict: the probe can
// prove the owner alive, prove it dead, or be undecidable (permissions,
// platform limits), in which case callers must treat the owner as alive.
type ProcessLiveness uint8

const (
	// ProcessDead proves the PID no longer names a live process.
	ProcessDead ProcessLiveness = iota
	// ProcessAlive proves the PID names a live process.
	ProcessAlive
	// ProcessUnknown means the probe could not decide; ownership is retained
	// (fail-closed).
	ProcessUnknown
)

// ProbeProcessLiveness reports whether pid names a live process. Unknown means
// the probe could not decide and ownership must be retained. Shares the
// replacement-marker platform probe (signal-0 / OpenProcess) so owner
// classification stays consistent across fsutil.
func ProbeProcessLiveness(pid int) ProcessLiveness {
	switch replacementProbePIDAliveAware(pid) {
	case replacementPIDAlive:
		return ProcessAlive
	case replacementPIDDead:
		return ProcessDead
	default:
		return ProcessUnknown
	}
}

// ProbeProcessStartTime returns the process creation stamp for pid, or nil
// when the platform cannot provide one (callers fall back to liveness only).
// Shares the replacement-marker platform probe (/proc starttime /
// K32GetProcessTimes).
func ProbeProcessStartTime(pid int) *time.Time {
	return replacementProcessStartTime(pid)
}
