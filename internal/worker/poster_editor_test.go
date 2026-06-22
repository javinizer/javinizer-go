package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- PosterEditor.UpdatePosterFromURL: DB persistence path ---

// TestUpdatePosterFromURL_DBSuccess verifies that when movieRepo is set,
// UpdatePosterFromURL persists the poster change via FindByID + Upsert.
func TestUpdatePosterFromURL_DBSuccess(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-001",
			Poster: models.PosterState{
				PosterURL: "https://example.com/old.jpg",
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "ABC-001",
		},
	})

	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	job.posterEditor = NewPosterEditor(job.resultIndex, job.results, movieRepo)

	existingMovie := &models.Movie{
		ID: "ABC-001",
		Poster: models.PosterState{
			PosterURL: "https://example.com/old.jpg",
		},
	}

	movieRepo.EXPECT().FindByID(context.TODO(), "ABC-001").Return(existingMovie, nil)
	movieRepo.EXPECT().Upsert(context.TODO(), existingMovie).Return(existingMovie, nil)

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	// Verify in-memory state was updated
	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/new-crop.jpg", result.Movie.Poster.CroppedPosterURL)

	// Verify DB movie was updated before Upsert
	assert.Equal(t, "https://example.com/new.jpg", existingMovie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/new-crop.jpg", existingMovie.Poster.CroppedPosterURL)
}

// TestUpdatePosterFromURL_DBUpsertError verifies that DB upsert failures
// are logged but do not propagate to the caller (best-effort semantics).
func TestUpdatePosterFromURL_DBUpsertError(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-002",
			Poster: models.PosterState{
				PosterURL: "https://example.com/old.jpg",
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "ABC-002",
		},
	})

	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	job.posterEditor = NewPosterEditor(job.resultIndex, job.results, movieRepo)

	existingMovie := &models.Movie{
		ID: "ABC-002",
		Poster: models.PosterState{
			PosterURL: "https://example.com/old.jpg",
		},
	}

	movieRepo.EXPECT().FindByID(context.TODO(), "ABC-002").Return(existingMovie, nil)
	movieRepo.EXPECT().Upsert(context.TODO(), existingMovie).Return(nil, errors.New("db connection lost"))

	// Best-effort: error should NOT propagate
	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-002", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	// In-memory state should still be updated
	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}

// TestUpdatePosterFromURL_DBFindByIDError verifies that FindByID failures
// are logged but do not propagate to the caller (best-effort semantics).
func TestUpdatePosterFromURL_DBFindByIDError(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-003",
			Poster: models.PosterState{
				PosterURL: "https://example.com/old.jpg",
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "ABC-003",
		},
	})

	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	job.posterEditor = NewPosterEditor(job.resultIndex, job.results, movieRepo)

	movieRepo.EXPECT().FindByID(context.TODO(), "ABC-003").Return(nil, errors.New("not found"))

	// Best-effort: error should NOT propagate
	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-003", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	// In-memory state should still be updated
	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}

// TestUpdatePosterFromURL_DBFindByIDReturnsNil verifies that if FindByID
// returns nil without error, Upsert is not called (movie not in DB).
func TestUpdatePosterFromURL_DBFindByIDReturnsNil(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-004",
			Poster: models.PosterState{
				PosterURL: "https://example.com/old.jpg",
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "ABC-004",
		},
	})

	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	job.posterEditor = NewPosterEditor(job.resultIndex, job.results, movieRepo)

	// FindByID returns nil movie, no error — Upsert should NOT be called
	movieRepo.EXPECT().FindByID(context.TODO(), "ABC-004").Return(nil, nil)

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-004", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}

// TestUpdatePosterFromURL_NilMovieRepoSkipsDB verifies that when movieRepo
// is nil, the DB persistence code path is entirely skipped.
func TestUpdatePosterFromURL_NilMovieRepoSkipsDB(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-005",
			Poster: models.PosterState{
				PosterURL: "https://example.com/old.jpg",
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "ABC-005",
		},
	})

	// movieRepo is nil by default — no DB calls should happen
	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-005", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}
