package database

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var errResidualMovieQuery = errors.New("forced residual movie query error")

func residualMovieAssociationFixture(t *testing.T) (*DB, models.Actress, models.Actress) {
	t.Helper()
	db := newDatabaseTestDB(t)
	source := models.Actress{JapaneseName: "residual source"}
	target := models.Actress{JapaneseName: "residual target"}
	repo := NewActressRepository(db)
	require.NoError(t, repo.Create(context.Background(), &source))
	require.NoError(t, repo.Create(context.Background(), &target))
	movie := &models.Movie{
		ContentID:    "residual-" + uuid.NewString(),
		ID:           "RESIDUAL-" + uuid.NewString(),
		DisplayTitle: "Residual association coverage",
		Actresses:    []models.Actress{source},
	}
	_, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	return db, source, target
}

func TestMoveMovieAssociationsResidualBranches(t *testing.T) {
	t.Run("movie load error", func(t *testing.T) {
		db, source, target := residualMovieAssociationFixture(t)
		name := "residual:movie-query:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*[]models.Movie); ok {
				tx.AddError(errResidualMovieQuery)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }()

		updated, err := moveMovieAssociations(db.DB, source.ID, target.ID)
		require.Zero(t, updated)
		require.ErrorIs(t, err, errResidualMovieQuery)
	})

	t.Run("association disappears after candidate lookup", func(t *testing.T) {
		db, source, target := residualMovieAssociationFixture(t)
		name := "residual:remove-association:" + uuid.NewString()
		removed := false
		require.NoError(t, db.DB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			if removed {
				return
			}
			if _, ok := tx.Statement.Dest.(*[]string); !ok {
				return
			}
			removed = true
			tx.Exec("DELETE FROM movie_actresses WHERE actress_id = ?", source.ID)
		}))
		defer func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }()

		updated, err := moveMovieAssociations(db.DB, source.ID, target.ID)
		require.NoError(t, err)
		require.Zero(t, updated)
		require.True(t, removed)
	})

	t.Run("association replacement error", func(t *testing.T) {
		db, source, target := residualMovieAssociationFixture(t)
		name := "residual:break-association:" + uuid.NewString()
		dropped := false
		require.NoError(t, db.DB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			if dropped || tx.Statement.Table != "actresses" {
				return
			}
			dropped = true
			tx.Exec("DROP TABLE movie_actresses")
		}))
		defer func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }()

		updated, err := moveMovieAssociations(db.DB, source.ID, target.ID)
		require.Zero(t, updated)
		require.Error(t, err)
		require.True(t, dropped)
	})
}
