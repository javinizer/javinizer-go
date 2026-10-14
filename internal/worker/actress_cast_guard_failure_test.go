package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260CastGuardRejectsStaleReviewWithoutChangingPersistedMovie(t *testing.T) {
	db := newActressEditTestDB(t)
	movie := &models.Movie{ContentID: "PR260-cast-guard", ID: "PR260-cast-guard", Title: "persisted"}
	require.NoError(t, db.Create(movie).Error)
	repo := database.NewMovieRepository(db)
	unit := database.EditUnit{Movies: repo}
	edit := &models.Movie{ContentID: movie.ContentID, Title: "uncommitted"}
	err := validateActressEdit(context.Background(), unit, edit, &ActressEditGuard{ExpectedCastVersion: "obsolete"})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
	current, err := repo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "persisted", current.Title)
	require.NoError(t, validateActressEdit(context.Background(), unit, edit, &ActressEditGuard{ExpectedCastVersion: models.ActressCastVersion(current.Actresses)}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, validateActressEdit(ctx, unit, edit, &ActressEditGuard{ExpectedCastVersion: "obsolete"}), context.Canceled)
	current, err = repo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "persisted", current.Title)
}
func TestPR260CastGuardMissingRowDistinguishesFreshFromDeletedPersisted(t *testing.T) {
	db := newActressEditTestDB(t)
	unit := database.EditUnit{Movies: database.NewMovieRepository(db)}
	edit := &models.Movie{ID: "PR260-never-persisted-cast", Title: "new review"}
	ctx := context.Background()
	require.NoError(t, validateActressEdit(ctx, unit, edit, nil), "legacy save without an explicit cast guard stays valid")
	require.NoError(t, validateActressEdit(ctx, unit, edit, &ActressEditGuard{KnownPersisted: false}), "fresh movie has no cast row yet")
	err := validateActressEdit(ctx, unit, edit, &ActressEditGuard{KnownPersisted: true})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict, "a deleted persisted row must not be recreated by review")
	require.Contains(t, conflict.Error(), "persisted movie missing")
	missing, err := unit.Movies.FindByContentID(ctx, edit.ID)
	require.Error(t, err)
	require.Nil(t, missing)
}
