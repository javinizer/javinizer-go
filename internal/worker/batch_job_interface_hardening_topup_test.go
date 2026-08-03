package worker

// Patch-coverage top-up for jobEditorImpl hardening legs: RestoreMovieResult's
// explicit nil-snapshot rejection and the multipart override compensation's
// "no pre-override snapshot for FAILING part" annotation.

import (
	"context"
	"errors"
	"testing"

	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestJobEditor_RestoreMovieResult_NilPriorRejected pins the explicit guard:
// RestoreMovieResult with a nil snapshot (a FAILED lookup, distinct from a
// legitimately nil stored Movie) is a hard error — never a silent no-op
// re-seat.
func TestJobEditor_RestoreMovieResult_NilPriorRejected(t *testing.T) {
	tracker := resultstore.New(1, []string{"np-1.mp4"})
	je := &jobEditorImpl{store: tracker, jobID: "job-np"}

	err := je.RestoreMovieResult(context.Background(), "np-1.mp4", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing pre-edit snapshot")
}

// TestApplyFieldOverride_FailingPartNoSnapshotSurfaced pins the compensation
// corner of jobEditorImpl's override fan-out: the FAILING part's snapshot
// lookup failed at plan time (nil prior), so its partial UpdateMovie write
// cannot be reverted — that must be SURFACED on the error ("no pre-override
// snapshot for failing part"), while the successfully updated sibling still
// restores cleanly through its own snapshot.
func TestApplyFieldOverride_FailingPartNoSnapshotSurfaced(t *testing.T) {
	const movieID = "FNS-1"
	const part1, part2 = "fns-1.mp4", "fns-2.mp4"

	tracker := resultstore.New(1, []string{part1, part2})
	tracker.UpdateFileResult(part1, &resultstore.MovieResult{
		ResultID:      "res-fns-1",
		FileMatchInfo: models.FileMatchInfo{Path: part1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Part One"},
	})
	tracker.UpdateFileResult(part2, &resultstore.MovieResult{
		ResultID:      "res-fns-2",
		FileMatchInfo: models.FileMatchInfo{Path: part2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Part Two"},
	})

	movieRepo := dbmocks.NewMockMovieRepositoryInterface(t)
	// Part 1 (updated first) is reverted through its snapshot after part 2
	// fails, so its Upsert leg runs for the revert too.
	movieRepo.EXPECT().Upsert(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, m *models.Movie) (*models.Movie, error) {
			return m, nil
		})

	part2Failed := false
	je := &jobEditorImpl{
		store: &overrideFailStore{
			Store: tracker,
			// The FAILING part had no readable snapshot at plan time: the
			// compensation cannot restore its partial write.
			lookupErrPath: part2,
			failUpdate: map[string]func(m *models.Movie) error{
				part2: func(m *models.Movie) error {
					if !part2Failed {
						part2Failed = true
						return errors.New("disk full")
					}
					return nil
				},
			},
		},
		jobID:     "job-fns",
		movieRepo: movieRepo,
	}

	_, _, applyErr := je.ApplyFieldOverride(context.Background(), "res-fns-1", "title", "scraper")
	require.Error(t, applyErr)
	assert.Contains(t, applyErr.Error(), "persist field override")
	assert.Contains(t, applyErr.Error(), "no pre-override snapshot for failing part "+part2,
		"the un-revertible failing part must be surfaced, not silently skipped")

	// The successfully updated sibling still reverts to its pre-edit title.
	got1, err := tracker.GetMovieResult(part1)
	require.NoError(t, err)
	assert.Equal(t, "Part One", got1.Movie.Title,
		"the sibling with a present snapshot still restores to its pre-override state")

	assertPosterSourceLockFree(t, "job-fns", movieID)
}
