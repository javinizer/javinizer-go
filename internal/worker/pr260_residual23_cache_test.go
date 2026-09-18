package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPR260ResidualCachedCastFallback(t *testing.T) {
	const path = "/f/cached.mp4"
	store := resultstore.New(1, []string{path})
	old := &models.Movie{ID: "CACHE-1", ContentID: "cache-1", Title: "baseline", Actresses: []models.Actress{{ID: 7, FirstName: "Persisted"}}}
	store.UpdateFileResult(path, &resultstore.MovieResult{ResultID: "cached-result", Status: models.JobStatusCompleted, Movie: old, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: old.ID}})
	pe := NewPosterEditor(store, store, nil) // valid legacy in-memory editor with no repository
	incoming := &models.Movie{ID: old.ID, ContentID: old.ContentID, Title: "review", Actresses: []models.Actress{{ID: 8, FirstName: "Stale"}}}
	_, _, err := pe.UpdateMovieFamilyWithEcho(context.Background(), old.ID, "cached-result", incoming, FamilySaveOptions{PreserveCachedActresses: true})
	require.NoError(t, err)
	result, _, ok := store.GetFileResultByResultID("cached-result")
	require.True(t, ok)
	require.NotNil(t, result.Movie)
	require.Equal(t, "review", result.Movie.Title)
	require.Len(t, result.Movie.Actresses, 1)
	require.Equal(t, uint(7), result.Movie.Actresses[0].ID)
	require.Equal(t, uint(7), incoming.Actresses[0].ID)
}
