package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type MovieRepository struct {
	*BaseRepository[models.Movie, string]
	upserter *MovieUpserter
}

func NewMovieRepository(db *DB) *MovieRepository {
	repo := &MovieRepository{
		BaseRepository: NewBaseRepository[models.Movie, string](
			db, "movie",
			func(m models.Movie) string { return movieEntityID(&m) },
			WithNewEntity[models.Movie, string](func() models.Movie { return models.Movie{} }),
		),
	}
	repo.upserter = NewMovieUpserter(repo)
	return repo
}

func movieEntityID(movie *models.Movie) string {
	if movie.ContentID != "" {
		return movie.ContentID
	}
	return movie.ID
}

func (r *MovieRepository) Create(ctx context.Context, movie *models.Movie) error {
	return r.BaseRepository.Create(ctx, movie)
}

func (r *MovieRepository) Update(ctx context.Context, movie *models.Movie) error {
	if err := r.GetDB().WithContext(ctx).Save(movie).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("movie %s", movieEntityID(movie)), err)
	}
	return nil
}

func (r *MovieRepository) Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error) {
	return r.upserter.Upsert(ctx, movie)
}

func (r *MovieRepository) UpsertWithTranslations(ctx context.Context, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*models.Movie, error) {
	return r.upserter.UpsertWithTranslations(ctx, movie, genreTranslations, actressTranslations)
}

func (r *MovieRepository) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie by id %s: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie by id %s", id), err)
	}
	return &movie, nil
}

func (r *MovieRepository) FindByContentID(ctx context.Context, contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie %s: %w", contentID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s", contentID), err)
	}
	return &movie, nil
}

func (r *MovieRepository) Delete(ctx context.Context, id string) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var movie models.Movie
		if err := tx.Model(&models.Movie{}).
			Select("content_id").
			Where("id = ?", id).
			First(&movie).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return wrapDBErr("find", fmt.Sprintf("movie for delete %s", id), err)
		}

		if movie.ContentID == "" {
			return nil
		}

		stub := &models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(stub).Association("Actresses").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("actresses for movie %s", movie.ContentID), err)
		}
		if err := tx.Model(stub).Association("Genres").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("genres for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTranslation{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("translations for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTag{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.Movie{}, "content_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		return nil
	})
}

func (r *MovieRepository) List(ctx context.Context, limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	if err != nil {
		return nil, wrapDBErr("find", "movies", err)
	}
	return movies, nil
}
