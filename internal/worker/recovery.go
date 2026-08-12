package worker

import (
	"fmt"
	"strings"
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
	filePath   string
	fmi        models.FileMatchInfo
	editLockFn func(movieIDs ...string) func() // optional: serializes write-back with review edits (codex r11; variadic per codex r42 total-order rule)
	movie      *models.Movie                   // optional: prior scrape-phase Movie to preserve on apply panic (mirrors fix in interpretApplyResult's err branch)
	// promoteWitnessFn reports an unresolved .promote- witness for the family
	// (codex P2: panics never funnel through the interpret failure branch —
	// this recovery write-back is the ONLY panic publication, so it must
	// fence or its revision bump flips startup arbitration to committed).
	promoteWitnessFn func(posterID string) bool
	updater          resultstore.ResultUpdater
	broadcast        func(panicErr string) // optional: send a JobEvent on panic (apply phase uses this)
	startTime        time.Time             // optional: included in MovieResult if non-zero
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
			// Live-session merge on the panic path too (codex P6-B): an edit
			// committed mid-phase must beat the frozen pre-phase movie for
			// review-editable fields, while phase-side state stays intact.
			// Falls back to whole-struct write when no prior result exists.
			unlock := func() {}
			if rc.editLockFn != nil && rc.fmi.MovieID != "" {
				unlock = rc.editLockFn(rc.fmi.MovieID)
			}
			defer unlock()
			// audit R3: witnesses are named by the CANONICAL movie ID while the
			// match surface is the matcher alias — probe BOTH spellings or an
			// alias-only fence misses the outstanding witness deterministically.
			fenceIDs := []string{strings.TrimSpace(rc.fmi.MovieID)}
			if rc.movie != nil {
				fenceIDs = append(fenceIDs, strings.TrimSpace(rc.movie.ID))
			}
			fenceHit := ""
			if rc.promoteWitnessFn != nil {
				seen := map[string]struct{}{}
				for _, fid := range fenceIDs {
					if fid == "" {
						continue
					}
					if _, dup := seen[strings.ToLower(fid)]; dup {
						continue
					}
					seen[strings.ToLower(fid)] = struct{}{}
					if rc.promoteWitnessFn(fid) {
						fenceHit = fid
						break
					}
				}
			}
			// codex P2-C/D: skip check runs UNDER the family key so a rekey that
			// won the lock before us is observed; callback check remains the net
			// for rekeys landing during the atomic write itself.
			if fenceHit != "" {
				logging.Warnf("[Recovery] skipping write-back for %s — promote witness for %s unresolved; restart reconciles", rc.filePath, fenceHit)
			} else if !writebackPreSkipped(rc.updater, rc.movie, rc.filePath, "Recovery") {
				errUp := rc.updater.AtomicUpdateFileResultWithProvenance(rc.filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
					if applyWritebackIdentityMismatch(rc.movie, current) {
						logging.Warnf("[Recovery] skipping write-back for %s — result rekeyed to %s mid-phase", rc.filePath, current.FileMatchInfo.MovieID)
						return current, prov, nil
					}
					current.FileMatchInfo = applyMatchFollowedByLiveIdentity(rc.fmi, current)
					current.Movie = mergeLiveReviewEdits(rc.movie, rc.movie, current.Movie)
					current.Status = models.JobStatusFailed
					current.Error = panicErr.Error()
					if !rc.startTime.IsZero() {
						current.StartedAt = rc.startTime
						current.EndedAt = &now
					}
					return current, mergeWriteBackProvenance(nil, prov), nil
				})
				if errUp != nil {
					mr := &resultstore.MovieResult{
						FileMatchInfo: rc.fmi,
						Status:        models.JobStatusFailed,
						Error:         panicErr.Error(),
					}
					if rc.movie != nil {
						mr.Movie = rc.movie
					}
					if !rc.startTime.IsZero() {
						mr.StartedAt = rc.startTime
						mr.EndedAt = &now
					}
					rc.updater.UpdateFileResult(rc.filePath, mr)
				}
			}

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
