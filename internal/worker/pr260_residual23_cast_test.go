package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ResidualNilMovieReadRejectsPersistedCastEdit(t *testing.T) {
	repo := mocks.NewMockMovieRepositoryInterface(t)
	ctx := context.Background()
	repo.EXPECT().FindByContentID(ctx, "cast-nil-row").Return(nil, nil)
	edit := &models.Movie{ID: "cast-nil-row", Title: "not committed"}
	err := validateActressEdit(ctx, database.EditUnit{Movies: repo}, edit, &ActressEditGuard{KnownPersisted: true})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
	require.ErrorContains(t, err, "persisted movie missing")
	require.Equal(t, "not committed", edit.Title)
}
