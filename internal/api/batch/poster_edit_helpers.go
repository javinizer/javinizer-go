package batch

import (
	"errors"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// Poster-edit identity/source helpers shared by the manual crop endpoint, the
// poster-from-URL endpoint, and the whole-movie edit/override paths. The crop
// handler file sits near the API file-size cap, so these cohesive helpers
// live here.

// errInvalidMovieIDForPoster is the rejection shared by every endpoint that
// resolves a poster-operation key from job state (pre-lock resolution and the
// post-lock convergence re-resolution alike).
var errInvalidMovieIDForPoster = errors.New("invalid movie ID for poster operation")

// resolvePosterID resolves the effective poster identifier for a movie within a
// batch job. It starts with the URL parameter movieID, then looks up the movie
// result to use the canonical Movie.ID if available. Returns an error if the
// resolved ID fails safe-filename validation (path traversal check).
func resolvePosterID(lookup resultstore.MovieLookup, movieID string) (string, error) {
	posterID := movieID
	movieResult, _ := lookup.FindMovieResultForMovieID(movieID)
	if movieResult != nil && movieResult.Movie != nil && movieResult.Movie.ID != "" {
		posterID = movieResult.Movie.ID
	}
	if !validPosterLockKey(posterID) {
		return "", errInvalidMovieIDForPoster
	}
	return posterID, nil
}

// effectivePosterSourceOf mirrors worker's effectivePosterSource (and the
// scrape generator's download-source resolution): PosterURL when set,
// CoverURL otherwise. The crop endpoint compares it pre/post lock-wait to
// detect a source swap that invalidated client-measured crop coordinates.
func effectivePosterSourceOf(movie *models.Movie) string {
	if movie == nil {
		return ""
	}
	if movie.Poster.PosterURL != "" {
		return movie.Poster.PosterURL
	}
	return movie.Poster.CoverURL
}

// posterLockKeyFor derives the shared poster-source lock key for a stored movie
// result: Movie.ID when set, FileMatchInfo.MovieID otherwise — the same
// precedence the temp poster cache and the override path key on, and the key
// updateBatchMovie's convergence loop re-resolves from fresh post-lock state.
func posterLockKeyFor(result *resultstore.MovieResult) string {
	key := result.FileMatchInfo.MovieID
	if result.Movie != nil && result.Movie.ID != "" {
		key = result.Movie.ID
	}
	return key
}

// validPosterLockKey is the safe-filename validation (path traversal check)
// shared by resolvePosterID and the post-lock re-resolved keys: the poster
// cache paths are built from this key, so it must be a plain file name.
func validPosterLockKey(posterID string) bool {
	return posterID == filepath.Base(posterID) && posterID != "" && posterID != "."
}
