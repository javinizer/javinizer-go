package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPR260UpsertInvalidatesStagedRenderAndFreshRetryCleans(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	ctx := context.Background()
	movie := &models.Movie{ContentID: "upsert-render-fence", ID: "UPSERT-RENDER", Title: "Before", RenderDirty: true, RenderGeneration: 41}
	stored, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	require.Zero(t, stored.RenderGeneration)
	require.False(t, stored.RenderDirty)

	stagedGeneration := stored.RenderGeneration
	stored.Title = "After"
	stored.RenderDirty = false
	stored.RenderGeneration = 0
	updated, err := repo.Upsert(ctx, stored)
	require.NoError(t, err)
	require.Equal(t, int64(1), updated.RenderGeneration)
	require.True(t, updated.RenderDirty)

	published := false
	err = repo.WithApplyArtifactPublicationFence(ctx, movie.ContentID, stagedGeneration, func(*models.Movie) error {
		published = true
		return nil
	})
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	require.False(t, published)

	persisted, err := repo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "After", persisted.Title)
	require.True(t, persisted.RenderDirty)
	require.Equal(t, int64(1), persisted.RenderGeneration)

	require.NoError(t, repo.WithApplyArtifactPublicationFence(ctx, movie.ContentID, persisted.RenderGeneration, func(authoritative *models.Movie) error {
		published = true
		require.Equal(t, "After", authoritative.Title)
		return nil
	}))
	require.True(t, published)
	persisted, err = repo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.False(t, persisted.RenderDirty)
	require.Equal(t, int64(1), persisted.RenderGeneration)

	// A stale write-back copy carrying the pre-clean flags must be a no-op.
	updated.RenderDirty = true
	_, err = repo.Upsert(ctx, updated)
	require.NoError(t, err)
	persisted, err = repo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.False(t, persisted.RenderDirty)
	require.Equal(t, int64(1), persisted.RenderGeneration)
}

func TestPR260UpsertRenderGenerationNoopCreditsAndRollback(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	ctx := context.Background()
	actressA := models.Actress{FirstName: "Alpha", LastName: "Actor", Verified: true, Origin: ActressOriginUser}
	actressB := models.Actress{FirstName: "Beta", LastName: "Actor", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actressA).Error)
	require.NoError(t, db.Create(&actressB).Error)

	movie := &models.Movie{ContentID: "upsert-render-credits", ID: "UPSERT-CREDITS", Title: "Stable", Credits: []models.MovieCredit{{ActressID: actressA.ID, Actress: &actressA, CreditedName: "Alpha Actor", Origin: "scrape"}}}
	stored, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	require.Zero(t, stored.RenderGeneration)

	noop := stored.Clone()
	noop.SourceName = "provenance-only"
	noop.RenderDirty = true
	noop.RenderGeneration = 99
	stored, err = repo.Upsert(ctx, noop)
	require.NoError(t, err)
	require.Zero(t, stored.RenderGeneration)
	require.False(t, stored.RenderDirty)

	added := stored.Clone()
	added.Credits = append(added.Credits, models.MovieCredit{ActressID: actressB.ID, Actress: &actressB, CreditedName: "Actor Beta", Origin: "scrape"})
	stored, err = repo.Upsert(ctx, added)
	require.NoError(t, err)
	require.Equal(t, int64(1), stored.RenderGeneration)
	require.True(t, stored.RenderDirty)
	require.Len(t, stored.Credits, 2)

	removed := stored.Clone()
	removed.Credits = removed.Credits[:1]
	stored, err = repo.Upsert(ctx, removed)
	require.NoError(t, err)
	require.Equal(t, int64(2), stored.RenderGeneration)
	require.True(t, stored.RenderDirty)
	require.Len(t, stored.Credits, 1)

	require.NoError(t, db.Exec(`CREATE TRIGGER fail_upsert_render_generation BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'generation failed'); END`).Error)
	failed := stored.Clone()
	failed.Title = "Must Roll Back"
	failed.Credits = append(failed.Credits, models.MovieCredit{ActressID: actressB.ID, Actress: &actressB, CreditedName: "Actor Beta", Origin: "scrape"})
	_, err = repo.Upsert(ctx, failed)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrApplyPublicationStale))

	persisted, findErr := repo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, findErr)
	require.Equal(t, "Stable", persisted.Title)
	require.Equal(t, int64(2), persisted.RenderGeneration)
	require.Len(t, persisted.Credits, 1)
}

func TestPR260MarkMovieRenderInputsChangedRejectsLostRow(t *testing.T) {
	db := newCreditTestDB(t)
	before := &models.Movie{ContentID: "lost-render-row", Title: "Before", RenderGeneration: 3}
	after := before.Clone()
	after.Title = "After"
	err := invalidateMovieRenderGenerationTx(db.DB, before, after)
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	require.Equal(t, int64(3), after.RenderGeneration)
	require.False(t, after.RenderDirty)
}

func TestPR260UpsertRenderCreditReadFailuresRollback(t *testing.T) {
	t.Run("pre-state snapshot", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieRepository(db)
		movie := models.Movie{ContentID: "render-snapshot-credit-error", ID: "SNAPSHOT-ERROR", Title: "Before"}
		require.NoError(t, db.Create(&movie).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		movie.Title = "After"
		_, err := repo.Upsert(t.Context(), &movie)
		require.ErrorContains(t, err, "snapshot render credits")
		require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
		require.Equal(t, "Before", movie.Title)
	})

	t.Run("post-reconcile reload", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewMovieRepository(db)
		movie := models.Movie{ContentID: "render-final-credit-error", ID: "FINAL-ERROR", Title: "Before"}
		require.NoError(t, db.Create(&movie).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 2)
		movie.Title = "After"
		movie.Credits = []models.MovieCredit{}
		movie.SkipCreditReconcile = true
		_, err := repo.Upsert(t.Context(), &movie)
		require.Error(t, err)
		require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
		require.Equal(t, "Before", movie.Title)
	})

}

func loadCreditsIntoTx(tx *gorm.DB, movie *models.Movie) {
	if movie == nil {
		return
	}
	var credits []models.MovieCredit
	if err := tx.Preload("Actress").Where("movie_content_id = ?", movie.ContentID).Order("order_index ASC, id ASC").Find(&credits).Error; err == nil {
		movie.Credits = credits
	}
}
