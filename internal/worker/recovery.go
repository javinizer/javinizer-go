package worker

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
)

// recoverableOutcome is a minimal interface for panic-recovery mutation.
// Both applyFileOutcome and scrapeFileOutcome implement this so that
// withFileRecovery can set the shared fields without knowing the concrete type.
type recoverableOutcome interface {
	setPanic(msg string)
}

// recoveryContext carries the data needed to update file results on panic.
// It decouples the recovery logic from the specific phase inputs, so both
// applyFile and scrapeFile can share it.
type recoveryContext struct {
	filePath  string
	fmi       models.FileMatchInfo
	movie     *models.Movie // optional: prior scrape-phase Movie to preserve on apply panic (mirrors fix in interpretApplyResult's err branch)
	updater   ResultUpdater
	broadcast func(panicErr string) // optional: send a JobEvent on panic (apply phase uses this)
	startTime time.Time             // optional: included in MovieResult if non-zero
}

// withFileRecovery wraps a business-logic function with panic recovery.
// If the business function panics, it:
//  1. Formats the panic via panicutil
//  2. Logs the error
//  3. Updates the file result to failed
//  4. Optionally broadcasts a JobEvent (if rc.broadcast is set)
//  5. Sets outcome.Panic/PanicMsg/Failed via the recoverableOutcome interface
//
// The caller should defer the returned function at the top of the worker func:
//
//	outcome := &myOutcome{}
//	rc := recoveryContext{...}
//	defer withFileRecovery(rc, outcome)()
//	// ... business logic ...
func withFileRecovery(rc recoveryContext, outcome recoverableOutcome) func() {
	return func() {
		if r := recover(); r != nil {
			panicErr := panicutil.FormatRecover(r)
			logging.Errorf("Worker panic %s: %v", rc.filePath, panicErr)

			now := time.Now()
			mr := &MovieResult{
				FileMatchInfo: rc.fmi,
				Status:        models.JobStatusFailed,
				Error:         panicErr.Error(),
			}
			if rc.movie != nil {
				// Preserve the prior scrape-phase Movie on the apply panic path —
				// mirrors interpretApplyResult's err-branch fix. Without this,
				// /review/[jobId] failed-apply rows lose their movie payload
				// (UpdateFileResult replaces the whole struct, preserving only
				// ResultID + Revision). Same field-drop-on-failure-path pattern
				// fixed for FileMatchInfo/timestamps in commit 6249de64.
				mr.Movie = rc.movie
			}
			if !rc.startTime.IsZero() {
				mr.StartedAt = rc.startTime
				mr.EndedAt = &now
			}
			rc.updater.UpdateFileResult(rc.filePath, mr)

			if rc.broadcast != nil {
				rc.broadcast(panicErr.Error())
			}

			outcome.setPanic(panicErr.Error())
		}
	}
}

// setPanic implements recoverableOutcome for applyFileOutcome.
func (o *applyFileOutcome) setPanic(msg string) {
	o.Panic = true
	o.PanicMsg = msg
	o.Failed = true
}

// setPanic implements recoverableOutcome for scrapeFileOutcome.
func (o *scrapeFileOutcome) setPanic(msg string) {
	o.Panic = true
	o.PanicMsg = msg
	o.Failed = true
}

// broadcastFailure returns a broadcast function that sends a StepFailed
// JobEvent when a worker goroutine panics. The phase and label parameters
// distinguish scrape-phase vs apply-phase events (e.g. phase=JobEventPhaseScrape,
// label="Scrape") so the progress UI can attribute the failure correctly.
func broadcastFailure(broadcaster progressBroadcaster, jobID models.JobID, movieID string, phase JobEventPhase, label string) func(panicErr string) {
	return func(panicErr string) {
		broadcaster.Send(JobEvent{
			JobID:     jobID,
			MovieID:   movieID,
			Phase:     phase,
			Step:      StepFailed,
			Message:   fmt.Sprintf("%s %s", label, panicErr),
			Timestamp: time.Now(),
		})
	}
}
