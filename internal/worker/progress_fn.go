package worker

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// makeProgressFn returns a scrape.ProgressFunc closure that broadcasts
// step progress as JobEvents on the given broadcaster. Shared between
// scrapeFile and applyFile, which differ only in Phase and MovieID source.
func makeProgressFn(broadcaster progressBroadcaster, jobID models.JobID, movieID string, phase JobEventPhase) scrape.ProgressFunc {
	return func(step scrape.ProgressStep, pct float64, msg string) {
		broadcaster.Send(JobEvent{
			JobID:     jobID,
			MovieID:   movieID,
			Phase:     phase,
			Step:      JobEventStep(string(step)),
			Progress:  pct,
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}
