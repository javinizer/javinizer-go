package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// MovieRepository persists movies and their associated records using a GORM
// base repository plus a dedicated upserter for insert-or-update semantics.
type MovieRepository struct {
	*BaseRepository[models.Movie, string]
	upserter *MovieUpserter
}

// NewMovieRepository returns a MovieRepository backed by the given database.
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

// Create inserts a new movie into the database.
func (r *MovieRepository) Create(ctx context.Context, movie *models.Movie) error {
	return r.BaseRepository.Create(ctx, movie)
}

// Update saves all fields of an existing movie.
func (r *MovieRepository) Update(ctx context.Context, movie *models.Movie) error {
	contentID := movieEntityID(movie)
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return mutateMovieRenderInputsTx(tx, []string{contentID}, func() error {
			if err := tx.Save(movie).Error; err != nil {
				return wrapDBErr("update", fmt.Sprintf("movie %s", contentID), err)
			}
			return nil
		})
	})
}

// Upsert inserts or updates a movie, returning the persisted record.
func (r *MovieRepository) Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error) {
	return r.upserter.Upsert(ctx, movie)
}

// UpsertWithTranslations upserts a movie together with its genre and actress translations.
func (r *MovieRepository) UpsertWithTranslations(ctx context.Context, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*models.Movie, error) {
	return r.upserter.UpsertWithTranslations(ctx, movie, genreTranslations, actressTranslations)
}

// FindByID loads a movie by its primary id, preloading actresses, genres, and translations.
func (r *MovieRepository) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	var movie models.Movie
	find := func(condition string) error {
		return r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).
			First(&movie, condition, id).Error
	}
	err := find("id = ?")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = find("content_id = ?")
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie by id %s: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie by id %s", id), err)
	}
	credits, err := NewMovieCreditRepository(r.GetDB()).ListByMovie(ctx, movie.ContentID)
	if err != nil {
		return nil, err
	}
	movie.Credits = credits
	return &movie, nil
}

// ErrApplyPublicationStale indicates the expected render generation no longer matches.
var ErrApplyPublicationStale = errors.New("apply publication generation changed")

// WithApplyPublicationFence invokes publish with a loaded movie under a generation-checked transaction.
func (r *MovieRepository) WithApplyPublicationFence(ctx context.Context, contentID string, expectedGeneration int64, publish func(*models.Movie) error) error {
	if strings.TrimSpace(contentID) == "" {
		return fmt.Errorf("apply publication fence: empty content id")
	}
	if publish == nil {
		return fmt.Errorf("apply publication fence: nil publisher")
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked := tx.Model(&models.Movie{}).Where("content_id = ?", contentID).
			UpdateColumn("render_generation", gorm.Expr("render_generation"))
		if locked.Error != nil {
			return wrapDBErr("lock", fmt.Sprintf("movie %s for apply publication", contentID), locked.Error)
		}
		if locked.RowsAffected != 1 {
			return fmt.Errorf("apply publication fence: movie %s: %w", contentID, ErrNotFound)
		}

		var movie models.Movie
		if err := tx.WithContext(ctx).
			Preload("Actresses").
			Preload("Genres").
			Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).
			First(&movie, "content_id = ?", contentID).Error; err != nil {
			return wrapDBErr("find", fmt.Sprintf("movie %s for apply publication", contentID), err)
		}
		credits, err := NewMovieCreditRepository(r.GetDB()).ListByMovieTx(tx, movie.ContentID)
		if err != nil {
			return err
		}
		movie.Credits = credits
		if movie.RenderGeneration != expectedGeneration {
			return fmt.Errorf("%w: movie %s expected %d, found %d", ErrApplyPublicationStale, contentID, expectedGeneration, movie.RenderGeneration)
		}
		return publish(&movie)
	})
}

// ErrApplyArtifactPublicationBlocked indicates an open collision prevents artifact publication.
var ErrApplyArtifactPublicationBlocked = errors.New("apply artifact publication blocked by open collision")

// WithApplyArtifactPublicationFence admits and finalizes publication in short
// database transactions. The publisher runs outside either transaction so its
// filesystem work and durable journal writes never hold or contend with a
// long-lived SQLite writer.
func (r *MovieRepository) WithApplyArtifactPublicationFence(ctx context.Context, contentID string, expectedGeneration int64, publish func(*models.Movie) error) error {
	if strings.TrimSpace(contentID) == "" {
		return fmt.Errorf("apply artifact publication fence: empty content id")
	}
	if publish == nil {
		return fmt.Errorf("apply artifact publication fence: nil publisher")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Commit admission before touching the filesystem. Journal appends made by
	// publish then commit independently before any destructive destination move.
	if err := r.admitArtifactPublication(ctx, contentID, expectedGeneration); err != nil {
		return err
	}
	var authoritative *models.Movie
	if err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		movie, err := r.lockAndLoadArtifactPublicationMovie(ctx, tx, contentID)
		if err != nil {
			return err
		}
		if movie.RenderGeneration != expectedGeneration {
			return fmt.Errorf("%w: movie %s expected %d, found %d", ErrApplyPublicationStale, contentID, expectedGeneration, movie.RenderGeneration)
		}
		var openCollisions int64
		if err := tx.WithContext(ctx).Model(&models.CreditCollision{}).Where("movie_content_id = ? AND status = ?", contentID, models.CollisionStatusOpen).Count(&openCollisions).Error; err != nil {
			return wrapDBErr("count", fmt.Sprintf("open collisions for movie %s", contentID), err)
		}
		if openCollisions != 0 {
			return fmt.Errorf("%w: movie %s", ErrApplyArtifactPublicationBlocked, contentID)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		authoritative = movie
		return nil
	}); err != nil {
		return err
	}
	if err := publish(authoritative); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if authoritative.RenderGeneration != expectedGeneration {
		return fmt.Errorf("%w: movie %s expected %d after publication, found %d", ErrApplyPublicationStale, contentID, expectedGeneration, authoritative.RenderGeneration)
	}
	// A concurrent generation or collision change rejects finalization. The
	// caller then compensates the already-journaled filesystem transaction.
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		movie, err := r.lockAndLoadArtifactPublicationMovie(ctx, tx, contentID)
		if err != nil {
			return err
		}
		if movie.RenderGeneration != expectedGeneration {
			return fmt.Errorf("%w: movie %s expected %d, found %d", ErrApplyPublicationStale, contentID, expectedGeneration, movie.RenderGeneration)
		}
		var openCollisions int64
		if err := tx.WithContext(ctx).Model(&models.CreditCollision{}).
			Where("movie_content_id = ? AND status = ?", contentID, models.CollisionStatusOpen).
			Count(&openCollisions).Error; err != nil {
			return wrapDBErr("count", fmt.Sprintf("open collisions for movie %s", contentID), err)
		}
		if openCollisions != 0 {
			return fmt.Errorf("%w: movie %s", ErrApplyArtifactPublicationBlocked, contentID)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		cleaned := tx.WithContext(ctx).Model(&models.Movie{}).
			Where("content_id = ? AND render_generation = ?", contentID, expectedGeneration).
			UpdateColumn("render_dirty", false)
		if cleaned.Error != nil {
			return wrapDBErr("clean", fmt.Sprintf("movie %s after artifact publication", contentID), cleaned.Error)
		}
		if cleaned.RowsAffected != 1 {
			return fmt.Errorf("%w: movie %s could not be cleaned at generation %d", ErrApplyPublicationStale, contentID, expectedGeneration)
		}
		return nil
	})
}

func (r *MovieRepository) admitArtifactPublication(ctx context.Context, contentID string, expectedGeneration int64) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		movie, err := r.lockAndLoadArtifactPublicationMovie(ctx, tx, contentID)
		if err != nil {
			return err
		}
		if movie.RenderGeneration != expectedGeneration {
			return fmt.Errorf("%w: movie %s expected %d, found %d", ErrApplyPublicationStale, contentID, expectedGeneration, movie.RenderGeneration)
		}
		var openCollisions int64
		if err := tx.WithContext(ctx).Model(&models.CreditCollision{}).
			Where("movie_content_id = ? AND status = ?", contentID, models.CollisionStatusOpen).
			Count(&openCollisions).Error; err != nil {
			return wrapDBErr("count", fmt.Sprintf("open collisions for movie %s", contentID), err)
		}
		if openCollisions != 0 {
			return fmt.Errorf("%w: movie %s", ErrApplyArtifactPublicationBlocked, contentID)
		}
		if movie.RenderDirty {
			return nil
		}
		admitted := tx.WithContext(ctx).Model(&models.Movie{}).
			Where("content_id = ? AND render_generation = ?", contentID, expectedGeneration).
			UpdateColumn("render_dirty", true)
		if admitted.Error != nil {
			return wrapDBErr("admit", fmt.Sprintf("movie %s for artifact publication", contentID), admitted.Error)
		}
		if admitted.RowsAffected != 1 {
			return fmt.Errorf("%w: movie %s could not be admitted at generation %d", ErrApplyPublicationStale, contentID, expectedGeneration)
		}
		return ctx.Err()
	})
}

func (r *MovieRepository) lockAndLoadArtifactPublicationMovie(ctx context.Context, tx *gorm.DB, contentID string) (*models.Movie, error) {
	locked := tx.WithContext(ctx).Model(&models.Movie{}).Where("content_id = ?", contentID).
		UpdateColumn("render_generation", gorm.Expr("render_generation"))
	if locked.Error != nil {
		return nil, wrapDBErr("lock", fmt.Sprintf("movie %s for artifact publication", contentID), locked.Error)
	}
	if locked.RowsAffected != 1 {
		return nil, fmt.Errorf("apply artifact publication fence: movie %s: %w", contentID, ErrNotFound)
	}
	var movie models.Movie
	if err := tx.WithContext(ctx).
		Preload("Actresses").
		Preload("Genres").
		Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).
		First(&movie, "content_id = ?", contentID).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s for artifact publication", contentID), err)
	}
	credits, err := NewMovieCreditRepository(r.GetDB()).ListByMovieTx(tx, movie.ContentID)
	if err != nil {
		return nil, err
	}
	movie.Credits = credits
	return &movie, nil
}

// FindByContentID loads a movie by its content_id, preloading actresses, genres, and translations.
func (r *MovieRepository) FindByContentID(ctx context.Context, contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie %s: %w", contentID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s", contentID), err)
	}
	credits, err := NewMovieCreditRepository(r.GetDB()).ListByMovie(ctx, movie.ContentID)
	if err != nil {
		return nil, err
	}
	movie.Credits = credits
	return &movie, nil
}

// Delete removes a movie and its associated actresses, genres, translations, and tags.
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

		if err := deleteCreditReassignmentsTx(tx, "movie_content_id = ?", "movie "+movie.ContentID, movie.ContentID); err != nil {
			return err
		}
		if err := deleteCreditRecordsTx(tx, "movie_content_id = ?", "movie_content_id = ?", movie.ContentID, "movie "+movie.ContentID); err != nil {
			return err
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

// List returns a page of movies with actresses and genres preloaded.
func (r *MovieRepository) List(ctx context.Context, limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.GetDB().WithContext(ctx).Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	if err != nil {
		return nil, wrapDBErr("find", "movies", err)
	}
	return movies, nil
}
