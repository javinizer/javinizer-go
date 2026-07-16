package worker

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/progress"
)

// makeProgressReporter returns a progress.ProgressReporter closure that
// broadcasts step progress as JobEvents on the given broadcaster. Shared
// between scrapeFile and applyFile, which differ only in Phase and MovieID
// source. The returned reporter is injected into the per-file task context
// via progress.WithReporter so downstream emitters call
// progress.FromContext(ctx).Report(...).
func makeProgressReporter(broadcaster progressBroadcaster, jobID models.JobID, movieID string, phase JobEventPhase) progress.ProgressReporter {
	return progress.ReporterFunc(func(step progress.ProgressStep, pct float64, msg string) {
		broadcaster.Send(JobEvent{
			JobID:     jobID,
			MovieID:   movieID,
			Phase:     phase,
			Step:      JobEventStep(string(step)),
			Progress:  pct,
			Message:   msg,
			Timestamp: time.Now(),
		})
	})
}
