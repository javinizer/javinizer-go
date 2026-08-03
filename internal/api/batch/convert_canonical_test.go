package batch

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestMovieResultToResponse_EmitsCanonicalMovieID pins Codex round-9 P1-A:
// the API must expose the CANONICAL movie identity (posterLockKeyFor
// precedence: Movie.ID when set, FileMatchInfo.MovieID otherwise) as
// movie_id — the same key the poster cache paths, the crop UI's
// {movie_id}-full.jpg URL, and the PATCH/crop/from-URL endpoints resolve.
// Pre-fix both converters emitted the stale FileMatchInfo.MovieID, so a
// refetched re-keyed result (FMI=OLDK, Movie.ID=NEWK) made the crop modal
// request OLDK-full.jpg while the cache was written at NEWK.
func TestMovieResultToResponse_EmitsCanonicalMovieID(t *testing.T) {
	divergent := &resultstore.MovieResult{
		ResultID:      "res-canonical-1",
		FileMatchInfo: models.FileMatchInfo{Path: "/x/target.mp4", MovieID: "OLDK-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "NEWK-001", Title: "Effective"},
	}
	full := movieResultToResponse(divergent, nil)
	assert.Equal(t, "NEWK-001", full.MovieID,
		"full response movie_id must be the canonical Movie.ID, not the stale FileMatchInfo.MovieID")
	slim := movieResultToSlimResponse(divergent, nil)
	assert.Equal(t, "NEWK-001", slim.MovieID,
		"slim response movie_id must match the full response (same canonical precedence)")

	// Legacy/nil-movie fallback: FileMatchInfo.MovieID stands when no stored
	// movie (or an empty Movie.ID) can win precedence.
	legacy := &resultstore.MovieResult{
		ResultID:      "res-canonical-2",
		FileMatchInfo: models.FileMatchInfo{Path: "/x/legacy.mp4", MovieID: "LEGC-001"},
		Status:        models.JobStatusCompleted,
	}
	assert.Equal(t, "LEGC-001", movieResultToResponse(legacy, nil).MovieID)
	assert.Equal(t, "LEGC-001", movieResultToSlimResponse(legacy, nil).MovieID)

	emptyID := &resultstore.MovieResult{
		ResultID:      "res-canonical-3",
		FileMatchInfo: models.FileMatchInfo{Path: "/x/empty-id.mp4", MovieID: "EMPT-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "", Title: "No ID"},
	}
	assert.Equal(t, "EMPT-001", movieResultToResponse(emptyID, nil).MovieID)
	assert.Equal(t, "EMPT-001", movieResultToSlimResponse(emptyID, nil).MovieID)

	// Nil result stays nil (no panic).
	assert.Nil(t, movieResultToResponse(nil, nil))
	assert.Nil(t, movieResultToSlimResponse(nil, nil))
}
