package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMovie_BackupCoverOriginal(t *testing.T) {
	t.Run("snapshots existing cover when cover changes", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "original-cover.jpg"}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		updated := &models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "new-cover.jpg"}}
		err := ej.UpdateMovie(context.Background(), "file1.mp4", updated)
		require.NoError(t, err)

		result, _ := job.results.GetMovieResult("file1.mp4")
		assert.Equal(t, "new-cover.jpg", result.Movie.Poster.CoverURL)
		assert.Equal(t, "original-cover.jpg", result.Movie.Poster.OriginalCoverURL,
			"changing the cover should snapshot the prior cover as the original")
	})

	t.Run("does not snapshot when cover unchanged", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "cover.jpg"}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		// Edit an unrelated field (Title); cover stays the same.
		updated := &models.Movie{ID: "ABC-001", Title: "Renamed", Poster: models.PosterState{CoverURL: "cover.jpg"}}
		err := ej.UpdateMovie(context.Background(), "file1.mp4", updated)
		require.NoError(t, err)

		result, _ := job.results.GetMovieResult("file1.mp4")
		assert.Equal(t, "", result.Movie.Poster.OriginalCoverURL,
			"editing a non-cover field must not capture an original cover snapshot")
	})

	t.Run("carries forward existing original across subsequent edits", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "original-cover.jpg"}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		// First change: snapshot the original.
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "cover-1.jpg"}}))

		// Second change: the incoming movie carries no OriginalCoverURL (as if
		// reloaded from a client that round-trips only the edited fields), but
		// the in-memory state still holds the captured original.
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "cover-2.jpg"}}))

		result, _ := job.results.GetMovieResult("file1.mp4")
		assert.Equal(t, "cover-2.jpg", result.Movie.Poster.CoverURL)
		assert.Equal(t, "original-cover.jpg", result.Movie.Poster.OriginalCoverURL,
			"the original snapshot from the first change must survive subsequent edits")
	})

	t.Run("preserves already-captured original from existing state", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		// Simulate a restart: the persisted movie already carries an original.
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: "ABC-001", Poster: models.PosterState{
				CoverURL:         "current-cover.jpg",
				OriginalCoverURL: "scraped-cover.jpg",
			}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		// A client edit that drops OriginalCoverURL from the payload must not
		// erase the persisted original.
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Poster: models.PosterState{CoverURL: "new-cover.jpg"}}))

		result, _ := job.results.GetMovieResult("file1.mp4")
		assert.Equal(t, "new-cover.jpg", result.Movie.Poster.CoverURL)
		assert.Equal(t, "scraped-cover.jpg", result.Movie.Poster.OriginalCoverURL,
			"an original captured before a restart must be carried forward, not overwritten")
	})
}
