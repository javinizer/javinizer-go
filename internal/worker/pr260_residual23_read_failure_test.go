package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPR260ResidualNilReadAllowsNeverPersistedReview(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.EXPECT().FindByContentID(ctx, "new-cast-row").Return(nil, nil)
	edit := &models.Movie{ID: "new-cast-row", Title: "fresh"}
	require.NoError(t, validateActressEdit(ctx, database.EditUnit{Movies: repo}, edit, &ActressEditGuard{KnownPersisted: false}))
	require.Equal(t, "fresh", edit.Title)
}

func TestPR260ResidualCachedCastReadFailureDoesNotPublish(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockMovieRepositoryInterface(t)
	fault := errors.New("repository cast read failed")
	repo.EXPECT().FindByContentID(ctx, "cache-read-failure").Return(nil, fault)
	const path = "/f/read-failure.mp4"
	store := resultstore.New(1, []string{path})
	baseline := &models.Movie{ID: "CACHE-ERR", ContentID: "cache-read-failure", Title: "baseline", Actresses: []models.Actress{{ID: 7}}}
	store.UpdateFileResult(path, &resultstore.MovieResult{ResultID: "read-failure-result", Status: models.JobStatusCompleted, Movie: baseline, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: baseline.ID}})
	incoming := &models.Movie{ID: baseline.ID, ContentID: baseline.ContentID, Title: "unpublished", Actresses: []models.Actress{{ID: 8}}}
	pe := NewPosterEditor(store, store, repo)
	_, _, err := pe.UpdateMovieFamilyWithEcho(ctx, baseline.ID, "read-failure-result", incoming, FamilySaveOptions{PreserveCachedActresses: true})
	require.ErrorIs(t, err, fault)
	stored, _, ok := store.GetFileResultByResultID("read-failure-result")
	require.True(t, ok)
	require.Equal(t, baseline.ContentID, stored.Movie.ContentID)
	require.Equal(t, baseline.ID, stored.Movie.ID)
	require.Equal(t, "baseline", stored.Movie.Title)
	require.Equal(t, uint(7), stored.Movie.Actresses[0].ID)
	require.Equal(t, "unpublished", incoming.Title)
}
