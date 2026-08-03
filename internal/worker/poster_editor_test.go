package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- PosterEditor.UpdatePosterFromURL: DB persistence path ---

// TestUpdatePosterFromURL_DBSuccess verifies that when movieRepo is set,
// UpdatePosterFromURL persists the poster change via FindByID + Upsert.
func TestUpdatePosterFromURL_DBSuccess(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &resultstore.MovieResult{
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
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

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
	job.results.UpdateFileResult("/test/file.mp4", &resultstore.MovieResult{
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
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

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
	job.results.UpdateFileResult("/test/file.mp4", &resultstore.MovieResult{
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
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

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
	job.results.UpdateFileResult("/test/file.mp4", &resultstore.MovieResult{
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
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

	// FindByID returns nil movie, no error — Upsert should NOT be called
	movieRepo.EXPECT().FindByID(context.TODO(), "ABC-004").Return(nil, nil)

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-004", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Poster.PosterURL)
}

// TestUpdatePosterFromURL_DBPersistsDerivedCropIntent pins Codex P2
// (poster_editor.go): the DB upsert writes the movie's DERIVED
// ShouldCropPoster together with the URLs — otherwise a later scrape loads
// the record through internal/scrape/cache.go and organizes the SAME URL
// with the stale crop decision. Each case seeds the DB row with the OPPOSITE
// flag to prove the write actually moves the value.
func TestUpdatePosterFromURL_DBPersistsDerivedCropIntent(t *testing.T) {
	const (
		filePath = "/test/intent-db.mp4"
		movieID  = "INT-DB1"
		newURL   = "https://example.com/new-poster.jpg"
		newCrop  = "https://example.com/new-cropped.jpg"
	)

	setup := func(t *testing.T, prior models.PosterState, prov *resultstore.ProvenanceData, dbStaleFlag bool) *models.Movie {
		t.Helper()
		job := newBatchJob([]string{filePath})
		job.results.UpdateFileResult(filePath, &resultstore.MovieResult{
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Poster: prior},
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		})
		if prov != nil {
			job.results.SetProvenance(filePath, prov)
		}
		movieRepo := mocks.NewMockMovieRepositoryInterface(t)
		job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

		// The DB row holds the STALE flag — the exact desync the fix closes.
		existing := &models.Movie{ID: movieID, Poster: models.PosterState{
			PosterURL:        "https://example.com/old.jpg",
			ShouldCropPoster: dbStaleFlag,
		}}
		movieRepo.EXPECT().FindByID(context.TODO(), movieID).Return(existing, nil)
		movieRepo.EXPECT().Upsert(context.TODO(), existing).Return(existing, nil)
		require.NoError(t, job.posterEditor.UpdatePosterFromURL(context.TODO(), movieID, newURL, newCrop))
		return existing // the captured upsert argument (mutated in place before Upsert)
	}

	t.Run("cover-backed prior persists true", func(t *testing.T) {
		existing := setup(t,
			models.PosterState{CoverURL: "https://example.com/cover.jpg", ShouldCropPoster: true},
			nil, false /* stale: flag said keep whole */)
		assert.Equal(t, newURL, existing.Poster.PosterURL)
		assert.Equal(t, newCrop, existing.Poster.CroppedPosterURL)
		assert.True(t, existing.Poster.ShouldCropPoster,
			"cover-backed intent travels with the URL write — Organize must still crop this image")
	})

	t.Run("poster-grade prior persists false", func(t *testing.T) {
		existing := setup(t,
			models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false},
			nil, true /* stale: flag said crop */)
		assert.False(t, existing.Poster.ShouldCropPoster,
			"poster-grade intent travels too — a stale true would make Organize crop a poster-grade image")
	})

	t.Run("provenance-match persists the source's intent", func(t *testing.T) {
		// A poster-grade prior falls back to false, but the selected URL is the
		// recorded source's landscape cover — its crop decision wins in memory
		// and must win in the DB write as well.
		existing := setup(t,
			models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "javdb", PosterURL: newURL, CoverURL: newURL, ShouldCropPoster: true},
			}}, false)
		assert.True(t, existing.Poster.ShouldCropPoster,
			"the source's own crop decision for this very image persists to the DB")
	})
}

// failingAtomicStore wraps a Store so the in-memory persist leg of
// UpdatePosterFromURL fails deterministically — mirroring overrideFailStore's
// predicate style but at the AtomicUpdateFileResult seam the fan-out uses.
type failingAtomicStore struct {
	resultstore.Store
	err error
}

func (s *failingAtomicStore) AtomicUpdateFileResult(string, func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	return s.err
}

// TestUpdatePosterFromURL_InMemoryPersistError verifies the fan-out error
// leg: a failed AtomicUpdateFileResult propagates and stops the fan-out.
func TestUpdatePosterFromURL_InMemoryPersistError(t *testing.T) {
	job := newBatchJob([]string{"/test/fail-update.mp4"})
	job.results.UpdateFileResult("/test/fail-update.mp4", &resultstore.MovieResult{
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ERR-001"},
		FileMatchInfo: models.FileMatchInfo{Path: "/test/fail-update.mp4", MovieID: "ERR-001"},
	})
	injectErr := errors.New("injected update failure")
	pe := NewPosterEditor(job.results, &failingAtomicStore{Store: job.results, err: injectErr}, nil)

	err := pe.UpdatePosterFromURL(context.TODO(), "ERR-001", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.ErrorIs(t, err, injectErr)
}

// TestUpdatePosterFromURL_NoInMemoryResultLeavesDBIntentAlone pins the guard
// half of the Codex P2 fix: when no in-memory result exists for the movie ID
// (nothing was fanned out), there is no derived intent to pair with the URLs
// — the DB write still carries the URLs but must NOT overwrite the recorded
// crop decision.
func TestUpdatePosterFromURL_NoInMemoryResultLeavesDBIntentAlone(t *testing.T) {
	job := newBatchJob([]string{"/test/db-only.mp4"})
	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)

	existing := &models.Movie{ID: "DBONLY-1", Poster: models.PosterState{
		PosterURL:        "https://example.com/old.jpg",
		ShouldCropPoster: true,
	}}
	movieRepo.EXPECT().FindByID(context.TODO(), "DBONLY-1").Return(existing, nil)
	movieRepo.EXPECT().Upsert(context.TODO(), existing).Return(existing, nil)

	// No job result references DBONLY-1 — the fan-out is a no-op.
	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "DBONLY-1", "https://example.com/new.jpg", "https://example.com/new-crop.jpg")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.jpg", existing.Poster.PosterURL)
	assert.True(t, existing.Poster.ShouldCropPoster,
		"no in-memory result ⇒ no derived intent ⇒ the DB row's crop decision survives")
}

// TestUpdatePosterFromURL_NilMovieRepoSkipsDB verifies that when movieRepo
// is nil, the DB persistence code path is entirely skipped.
func TestUpdatePosterFromURL_NilMovieRepoSkipsDB(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &resultstore.MovieResult{
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
