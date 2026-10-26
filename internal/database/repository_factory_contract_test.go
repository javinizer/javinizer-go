package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestRepositoryCreateConstraintContext(t *testing.T) {
	t.Run("genre name", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := newGenreRepository(db)
		original := models.Genre{Name: "Existing genre"}
		require.NoError(t, repo.Create(t.Context(), &original))
		err := repo.Create(t.Context(), &models.Genre{Name: original.Name})
		var constraint sqlite3.Error
		require.ErrorAs(t, err, &constraint)
		require.Equal(t, sqlite3.ErrConstraintUnique, constraint.ExtendedCode)
		require.ErrorContains(t, err, "create genre Existing genre")
		stored, err := repo.FindByID(t.Context(), original.ID)
		require.NoError(t, err)
		require.Equal(t, original, *stored)
		count, err := repo.Count(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
	})
	t.Run("history ID", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewHistoryRepository(db)
		original := models.History{MovieID: "original", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess}
		require.NoError(t, repo.Create(t.Context(), &original))
		err := repo.Create(t.Context(), &models.History{ID: original.ID, MovieID: "replacement"})
		var constraint sqlite3.Error
		require.ErrorAs(t, err, &constraint)
		require.Equal(t, sqlite3.ErrConstraintPrimaryKey, constraint.ExtendedCode)
		require.ErrorContains(t, err, fmt.Sprintf("create history %d", original.ID))
		stored, err := repo.FindByID(t.Context(), original.ID)
		require.NoError(t, err)
		require.Equal(t, original, *stored)
		count, err := repo.Count(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
	})
	t.Run("job ID", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewJobRepository(db)
		original := models.Job{ID: "existing-job", Status: models.JobStatusCompleted, TotalFiles: 4}
		require.NoError(t, repo.Create(t.Context(), &original))
		err := repo.Create(t.Context(), &models.Job{ID: original.ID, Status: models.JobStatusFailed})
		var constraint sqlite3.Error
		require.ErrorAs(t, err, &constraint)
		require.Equal(t, sqlite3.ErrConstraintPrimaryKey, constraint.ExtendedCode)
		require.ErrorContains(t, err, "create job existing-job")
		stored, err := repo.FindByID(t.Context(), original.ID)
		require.NoError(t, err)
		require.Equal(t, original, *stored)
		count, err := repo.Count(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
	})
}

func TestRepositoryCountUsesEntityTable(t *testing.T) {
	db, _, _ := moviePersistenceFixture(t)
	operationRepo := NewBatchFileOperationRepository(db)
	for i := 0; i < 3; i++ {
		require.NoError(t, operationRepo.Create(t.Context(), &models.BatchFileOperation{
			MovieID: "persistence-target", OriginalPath: fmt.Sprintf("source-%d.mp4", i), NewPath: fmt.Sprintf("organized-%d.mp4", i),
			OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
		}))
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name  string
		want  int64
		count func(context.Context) (int64, error)
	}{
		{"movies", 2, NewMovieRepository(db).Count},
		{"genres", 1, newGenreRepository(db).Count},
		{"batch file operations", 3, operationRepo.Count},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, err := tc.count(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, count, "count must select the repository's entity table")
			count, err = tc.count(cancelled)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorContains(t, err, "count "+tc.name)
			require.Zero(t, count)
			count, err = tc.count(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, count, "cancellation must not poison later queries")
		})
	}
}
