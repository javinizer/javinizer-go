package worker

// r10 P1-3 regression: ApplyFieldOverride's failure compensation previously
// reverted only the SUCCESSFUL part writes. UpdateMovie commits DB side
// effects BEFORE the in-memory write (actress renames first, then the movie
// upsert, then store.UpdateMovie — batch_job_interface.go's documented
// order), so when the FAILING part fails on a leg after those commits, its
// side effects were never reverted (compensation only walked updatedParts).
// The compensation now restores the failing part FIRST, with the same
// RestoreMovieResult semantics as the successful parts: the re-upsert of
// the pre-edit movie restores the DB row and the snapshot re-seat undoes
// any in-memory leg.

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

// TestApplyFieldOverride_FailingPartCommittedUpsertReverted drives a
// multipart title override whose SECOND part commits its DB upsert and then
// fails the in-memory write ("disk full" seam). Exact compensation must
// re-upsert the pre-edit movies for BOTH parts — the failing one FIRST —
// and re-seat their stored snapshots; reverting only the successful part
// (pre-fix) would leave the failing part's movies-table row at the rejected
// title forever.
func TestApplyFieldOverride_FailingPartCommittedUpsertReverted(t *testing.T) {
	const movieID = "FPC-1"
	const part1, part2 = "fpc-1.mp4", "fpc-2.mp4"

	tracker := resultstore.New(1, []string{part1, part2})
	tracker.UpdateFileResult(part1, &resultstore.MovieResult{
		ResultID:      "res-fpc-1",
		FileMatchInfo: models.FileMatchInfo{Path: part1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Part One"},
	})
	tracker.UpdateFileResult(part2, &resultstore.MovieResult{
		ResultID:      "res-fpc-2",
		FileMatchInfo: models.FileMatchInfo{Path: part2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Part Two"},
	})
	tracker.SetProvenance(part1, &resultstore.ProvenanceData{})
	tracker.SetProvenance(part2, &resultstore.ProvenanceData{})

	// The movies-table trace: every committed upsert records the movie's
	// title. The failing part's forward upsert COMMITS before the in-memory
	// leg fails, so a pre-fix trace lacks part 2's re-upsert; the fix must
	// re-upsert the pre-edit movie for it (and for part 1).
	var dbTitles []string
	movieRepo := dbmocks.NewMockMovieRepositoryInterface(t)
	movieRepo.EXPECT().Upsert(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, m *models.Movie) (*models.Movie, error) {
			dbTitles = append(dbTitles, m.Title)
			return m, nil
		})

	part2Failed := false
	je := &jobEditorImpl{
		store: &overrideFailStore{
			Store: tracker,
			// The failing leg: part 2's in-memory write of the override
			// movie — AFTER its Upsert already committed. Fail ONCE: the
			// pre-edit restore writes the same stored movie back and must
			// pass through.
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
		jobID:     "job-fpc",
		movieRepo: movieRepo,
	}

	_, _, applyErr := je.ApplyFieldOverride(context.Background(), "res-fpc-1", "title", "scraper")
	require.Error(t, applyErr)
	assert.Contains(t, applyErr.Error(), "persist field override")
	assert.NotContains(t, applyErr.Error(), "could not be reverted",
		"the failing part had a snapshot and restores cleanly")

	// The FAILING part's committed upsert is reverted FIRST, then the
	// successful part's — both back at their pre-edit titles. (Each part's
	// per-part merge keeps its OWN title, so the forward and revert upserts
	// of one part read the same title; the revert ORDER is what pins the
	// fix: part 2's re-upsert of "Part Two" rides between the two forward
	// upserts and part 1's revert.)
	require.Equal(t, []string{
		"Part One", // part1 forward upsert
		"Part Two", // part2 forward upsert (COMMITTED before the failing leg)
		"Part Two", // part2 reverted FIRST — the pre-fix gap
		"Part One", // part1 reverted
	}, dbTitles)

	// Both parts' stored movies are back at their pre-override titles.
	got1, err := tracker.GetMovieResult(part1)
	require.NoError(t, err)
	assert.Equal(t, "Part One", got1.Movie.Title)
	got2, err := tracker.GetMovieResult(part2)
	require.NoError(t, err)
	assert.Equal(t, "Part Two", got2.Movie.Title,
		"the failing part's stored movie is re-seated verbatim from its snapshot")
	assertPosterSourceLockFree(t, "job-fpc", movieID)
}
