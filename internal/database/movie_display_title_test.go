package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpsertDisplayTitleUsesHydratedCreditsAndInvalidatesOnce(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	canonical := models.Actress{FirstName: "Canonical", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&canonical).Error)
	movie, err := repo.Upsert(t.Context(), &models.Movie{ID: "TITLE-001", Title: "Base", DisplayTitle: "Old", Actresses: []models.Actress{canonical}})
	require.NoError(t, err)
	movie.SkipCreditReconcile = true
	beforeGeneration := movie.RenderGeneration
	render := func(_ context.Context, loaded *models.Movie) string {
		require.Len(t, loaded.Credits, 1)
		require.NotNil(t, loaded.Credits[0].Actress)
		return loaded.Credits[0].Actress.FullName() + " " + loaded.Title
	}
	saved, err := UpsertWithDisplayTitle(t.Context(), repo, movie, nil, nil, render)
	require.NoError(t, err)
	require.Equal(t, "Person Canonical Base", saved.DisplayTitle)
	require.Equal(t, saved.DisplayTitle, movie.DisplayTitle, "family envelope aliases the incoming movie")
	require.True(t, saved.RenderDirty)
	require.Equal(t, beforeGeneration+1, saved.RenderGeneration)
	stored, err := repo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, saved.DisplayTitle, stored.DisplayTitle)
	require.Equal(t, saved.RenderGeneration, stored.RenderGeneration)
	saved.SkipCreditReconcile = true
	saved.DisplayTitle = "Provisional title before credit hydration"
	again, err := UpsertWithDisplayTitle(t.Context(), repo, saved, nil, nil, render)
	require.NoError(t, err)
	require.Equal(t, beforeGeneration+1, again.RenderGeneration, "unchanged final title must not advance generation")
}

func TestUpsertDisplayTitleRollsBackEveryFinalizationFailure(t *testing.T) {
	for _, stage := range []string{"title write", "render invalidation", "render state reload"} {
		t.Run(stage, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewMovieRepository(db)
			original, err := repo.Upsert(t.Context(), &models.Movie{ID: "TITLE-FAIL", Title: "Before", DisplayTitle: "Before derived"})
			require.NoError(t, err)
			candidate := original.Clone()
			candidate.Title = "After"
			rendered := false
			injected := errors.New("injected " + stage)
			name := "test:title-finalization"
			hook := func(tx *gorm.DB) {
				if !rendered || tx.Statement.Table != "movies" {
					return
				}
				if stage == "title write" || (stage == "render invalidation" && len(tx.Statement.Selects) == 0) || (stage == "render state reload" && strings.Join(tx.Statement.Selects, ",") == "render_dirty,render_generation") {
					tx.AddError(injected)
				}
			}
			if stage == "title write" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(name, hook))
				defer db.Callback().Update().Remove(name)
			} else {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, hook))
				defer db.Callback().Query().Remove(name)
			}
			_, err = UpsertWithDisplayTitle(t.Context(), repo, candidate, nil, nil, func(context.Context, *models.Movie) string {
				rendered = true
				return "After derived"
			})
			require.ErrorIs(t, err, injected)
			rendered = false
			stored, err := repo.FindByID(t.Context(), original.ID)
			require.NoError(t, err)
			require.Equal(t, original.Title, stored.Title)
			require.Equal(t, original.DisplayTitle, stored.DisplayTitle)
			require.Equal(t, original.RenderGeneration, stored.RenderGeneration)
			require.Equal(t, original.DisplayTitle, candidate.DisplayTitle, "failed render must not be echoed as committed")
		})
	}
}

type titleLegacyRepository struct {
	MovieRepositoryInterface
	got *models.Movie
}

func (r *titleLegacyRepository) UpsertWithTranslations(_ context.Context, movie *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	r.got = movie.Clone()
	return movie, nil
}

func TestUpsertDisplayTitleLegacyRepository(t *testing.T) {
	repo := &titleLegacyRepository{}
	movie := &models.Movie{Title: "Base"}
	_, err := UpsertWithDisplayTitle(t.Context(), repo, movie, nil, nil, func(context.Context, *models.Movie) string { return "Derived" })
	require.NoError(t, err)
	require.Equal(t, "Derived", repo.got.DisplayTitle)
	_, err = UpsertWithDisplayTitle(t.Context(), repo, movie, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Derived", repo.got.DisplayTitle)
}
