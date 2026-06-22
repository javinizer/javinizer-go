package worker

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// PosterEditor handles poster-related mutations on job results.
// Extracted from BatchJob to isolate the poster update concern —
// BatchJob no longer directly implements poster editing logic.
//
// PosterEditor is held by BatchJob and used to satisfy JobEditor's
// UpdatePosterCrop and UpdatePosterFromURL methods.
//
// When movieRepo is provided, PosterEditor also persists poster updates to
// the database (best-effort). This concentrates the full poster update
// lifecycle — in-memory state and DB persistence — in one place, so that
// any caller using PosterEditor automatically gets DB persistence without
// risking a split between in-memory and persistent state.
type PosterEditor struct {
	lookup    MovieLookup
	updater   ResultUpdater
	movieRepo database.MovieRepositoryInterface // optional: when set, poster updates are persisted to DB
}

// NewPosterEditor creates a PosterEditor with the given lookup and updater.
// If movieRepo is non-nil, UpdatePosterFromURL will also persist the poster
// change to the database (best-effort: DB failures are logged, not returned).
func NewPosterEditor(lookup MovieLookup, updater ResultUpdater, movieRepo database.MovieRepositoryInterface) *PosterEditor {
	return &PosterEditor{lookup: lookup, updater: updater, movieRepo: movieRepo}
}

// UpdatePosterCrop updates the cropped poster URL for all files matching movieID.
func (pe *PosterEditor) UpdatePosterCrop(movieID string, croppedURL string) error {
	filePaths := pe.lookup.FindFilePathsForMovieID(movieID)
	for _, filePath := range filePaths {
		err := pe.updater.AtomicUpdateFileResult(filePath, func(current *MovieResult) (*MovieResult, error) {
			if current.Movie == nil {
				return current, nil // skip files with nil Movie
			}
			movie := current.Movie.Clone()
			backupPosterOriginals(movie)
			movie.Poster.CroppedPosterURL = croppedURL
			movie.Poster.ShouldCropPoster = false
			current.Movie = movie
			current.FileMatchInfo.MovieID = movie.ID
			return current, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdatePosterFromURL updates the poster URL and cropped poster URL for all files matching movieID.
// When a movieRepo is configured, the poster change is also persisted to the database.
// DB persistence is best-effort: failures are logged but do not propagate to the caller.
func (pe *PosterEditor) UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error {
	filePaths := pe.lookup.FindFilePathsForMovieID(movieID)
	for _, filePath := range filePaths {
		err := pe.updater.AtomicUpdateFileResult(filePath, func(current *MovieResult) (*MovieResult, error) {
			if current.Movie == nil {
				return current, nil // skip files with nil Movie
			}
			movie := current.Movie.Clone()
			backupPosterOriginals(movie)
			movie.Poster.PosterURL = posterURL
			movie.Poster.CroppedPosterURL = croppedURL
			movie.Poster.ShouldCropPoster = false
			current.Movie = movie
			current.FileMatchInfo.MovieID = movie.ID
			return current, nil
		})
		if err != nil {
			return err
		}
	}

	// Persist poster update to database. Best-effort: failures are logged but
	// do not fail the request, matching the previous adapter-level behavior.
	if pe.movieRepo != nil {
		posterID := movieID
		if mr, _ := pe.lookup.FindMovieResultForMovieID(movieID); mr != nil && mr.Movie != nil && mr.Movie.ID != "" {
			posterID = mr.Movie.ID
		}
		existing, dbErr := pe.movieRepo.FindByID(ctx, posterID)
		if dbErr == nil && existing != nil {
			existing.Poster.PosterURL = posterURL
			existing.Poster.CroppedPosterURL = croppedURL
			if _, upErr := pe.movieRepo.Upsert(ctx, existing); upErr != nil {
				logging.Warnf("Failed to update movie poster in database: %v", upErr)
			}
		} else if dbErr != nil {
			logging.Warnf("Failed to find movie %s for poster update: %v", posterID, dbErr)
		}
	}

	return nil
}

// backupPosterOriginals preserves the original poster URLs before they are overwritten.
func backupPosterOriginals(movie *models.Movie) {
	if movie.Poster.OriginalPosterURL == "" {
		shouldCrop := movie.Poster.ShouldCropPoster
		movie.Poster.OriginalPosterURL = movie.Poster.PosterURL
		movie.Poster.OriginalCroppedPosterURL = movie.Poster.CroppedPosterURL
		movie.Poster.OriginalShouldCropPoster = &shouldCrop
	}
}
