package worker

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
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
	jobID     models.JobID
	filePath  string
	fmi       models.FileMatchInfo
	movie     *models.Movie // optional: prior scrape-phase Movie to preserve on apply panic (mirrors fix in interpretApplyResult's err branch)
	updater   resultstore.ResultUpdater
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
			// The panic write-back is a poster-state mutation like the
			// apply-failure write-back: the carried movie is an in-flight
			// snapshot, so a wholesale UpdateFileResult would erase a
			// crop/source edit — or a re-keyed live movie — that landed while
			// the worker ran. Take the LIVE movie's poster-source lock and only
			// merge the carried movie when identities match; the live movie
			// (and its FileMatchInfo) survive otherwise, and the pipeline-owned
			// status/error/timestamps still move. The legacy wholesale write
			// remains only for a vanished result (nothing live to clobber).
			snapshotID := ""
			if rc.movie != nil {
				snapshotID = rc.movie.ID
			}
			releasePosterLock := AcquirePosterSourceLock(rc.jobID.String(), liveWritebackLockKey(rc.updater, rc.filePath, snapshotID))
			writeErr := rc.updater.AtomicUpdateFileResult(rc.filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
				current.Status = models.JobStatusFailed
				current.Error = panicErr.Error()
				if !rc.startTime.IsZero() {
					current.StartedAt = rc.startTime
					current.EndedAt = &now
				}
				// Preserve the prior scrape-phase Movie on the apply panic path
				// (mirrors interpretApplyResult's err-branch fix), merging the
				// live poster state — and only when identities match (P2-5).
				if rc.movie != nil && (current.Movie == nil || current.Movie.ID == rc.movie.ID) {
					merged := rc.movie.Clone()
					mergeLivePosterState(merged, current.Movie)
					current.Movie = merged
				}
				return current, nil
			})
			if writeErr != nil {
				mr := &resultstore.MovieResult{
					FileMatchInfo: rc.fmi,
					Status:        models.JobStatusFailed,
					Error:         panicErr.Error(),
					Movie:         rc.movie,
				}
				if !rc.startTime.IsZero() {
					mr.StartedAt = rc.startTime
					mr.EndedAt = &now
				}
				rc.updater.UpdateFileResult(rc.filePath, mr)
			}
			releasePosterLock()

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
