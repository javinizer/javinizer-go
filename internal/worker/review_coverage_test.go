package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
)

func TestTrackApplyResults_PanicCountsAsFailed(t *testing.T) {
	t.Run("panic without Failed flag counts as failure", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true, Failed: false, Panic: false},
			{FilePath: "b.mp4", Success: false, Failed: false, Panic: true, PanicMsg: "boom"},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(1), organized)
		assert.Equal(t, int64(1), failed, "panicked outcome must count toward failed")
	})

	t.Run("all success with no panic", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true},
			{FilePath: "b.mp4", Success: true},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(2), organized)
		assert.Equal(t, int64(0), failed)
	})

	t.Run("mixed failed and panic", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true},
			{FilePath: "b.mp4", Success: false, Failed: true},
			{FilePath: "c.mp4", Success: false, Panic: true},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(1), organized)
		assert.Equal(t, int64(2), failed)
	})
}

// TestUpdateFileResult_ExcludedDecrementSkip verifies that UpdateFileResult
// does NOT decrement counters when replacing a result for an excluded file.
// Without the !Excluded guard, a file that was Completed (counted), then
// excluded (MarkExcluded decrements), then updated again would double-decrement.
func TestUpdateFileResult_ExcludedDecrementSkip(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// file1 completes — counter incremented
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{Status: models.JobStatusCompleted})
	assert.Equal(t, 1, job.prog().Completed)

	// file1 is excluded — MarkExcluded decrements the counter
	job.results.MarkExcluded("file1.mp4")
	assert.Equal(t, 0, job.prog().Completed, "MarkExcluded should decrement Completed")
	assert.True(t, job.snap().Excluded["file1.mp4"])

	// Now UpdateFileResult is called again for the excluded file.
	// Without the !Excluded guard on decrement, this would decrement
	// Completed from 0 to -1 (counter drift).
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{Status: models.JobStatusFailed})
	assert.Equal(t, 0, job.prog().Completed, "excluded file must NOT decrement Completed")
	assert.Equal(t, 0, job.prog().Failed, "excluded file must NOT increment Failed")
}
