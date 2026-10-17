package database

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
)

// DisplayTitleRenderer derives a title from an authoritative movie snapshot.
// It must not mutate the movie or perform database writes.
type DisplayTitleRenderer func(context.Context, *models.Movie) string

// displayTitleUpserter keeps title derivation inside the credit-resolution
// transaction without coupling the repository to template configuration.
type displayTitleUpserter interface {
	UpsertWithDisplayTitle(context.Context, *models.Movie, []models.GenreTranslationData, []models.ActressTranslationData, DisplayTitleRenderer) (*models.Movie, error)
}

// UpsertWithDisplayTitle derives the title after authoritative cast hydration.
// Repositories without credit resolution retain their ordinary upsert behavior.
func UpsertWithDisplayTitle(ctx context.Context, repo MovieRepositoryInterface, movie *models.Movie, genres []models.GenreTranslationData, actresses []models.ActressTranslationData, render DisplayTitleRenderer) (*models.Movie, error) {
	if render != nil {
		if capable, ok := repo.(displayTitleUpserter); ok {
			return capable.UpsertWithDisplayTitle(ctx, movie, genres, actresses, render)
		}
		movie.DisplayTitle = render(ctx, movie)
	}
	return repo.UpsertWithTranslations(ctx, movie, genres, actresses)
}

// UpsertWithDisplayTitle persists the rendered title in the same transaction as
// its resolved credits, including when this repository belongs to an edit unit.
func (r *MovieRepository) UpsertWithDisplayTitle(ctx context.Context, movie *models.Movie, genres []models.GenreTranslationData, actresses []models.ActressTranslationData, render DisplayTitleRenderer) (*models.Movie, error) {
	return r.upserter.upsertWithDisplayTitle(ctx, movie, genres, actresses, render)
}
