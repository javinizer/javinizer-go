package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// MovieTagRepository persists movie-to-tag associations in the database.
type MovieTagRepository struct {
	db *DB
}

type movieTagKey struct {
	canonical string
	legacy    string
	found     bool
}

const canonicalMovieTagsSQL = `
SELECT keeper.id, grouped.movie_id, keeper.tag, keeper.created_at
FROM movie_tags keeper
JOIN (
	SELECT MIN(resolved.id) AS id, resolved.movie_id, resolved.tag
	FROM (
		SELECT mt.id,
			CASE
				WHEN EXISTS (SELECT 1 FROM movies exact_movie WHERE exact_movie.content_id = mt.movie_id) THEN mt.movie_id
				WHEN 1 = (
					SELECT COUNT(*)
					FROM movies mapped
					WHERE mapped.id = mt.movie_id
						AND mapped.id IS NOT NULL AND mapped.id <> ''
						AND mapped.content_id IS NOT NULL AND mapped.content_id <> ''
				) THEN (
					SELECT mapped.content_id
					FROM movies mapped
					WHERE mapped.id = mt.movie_id
						AND mapped.id IS NOT NULL AND mapped.id <> ''
						AND mapped.content_id IS NOT NULL AND mapped.content_id <> ''
				)
				ELSE mt.movie_id
			END AS movie_id,
			mt.tag
		FROM movie_tags mt
	) resolved
	GROUP BY resolved.movie_id, resolved.tag
) grouped ON grouped.id = keeper.id`

// NewMovieTagRepository returns a MovieTagRepository backed by db.
func NewMovieTagRepository(db *DB) *MovieTagRepository {
	return &MovieTagRepository{db: db}
}

func resolveMovieTagKey(tx *gorm.DB, supplied string) (movieTagKey, error) {
	if supplied == "" {
		return movieTagKey{canonical: supplied}, nil
	}
	type candidate struct {
		ContentID  string
		Legacy     string
		Precedence int
	}
	var matches []candidate
	err := tx.Raw(`SELECT m.content_id,
		CASE
			WHEN m.id IS NOT NULL AND m.id <> '' AND m.id <> m.content_id
				AND NOT EXISTS (SELECT 1 FROM movies collision WHERE collision.content_id = m.id)
			THEN m.id ELSE ''
		END AS legacy,
		CASE WHEN m.content_id = ? THEN 0 ELSE 1 END AS precedence
		FROM movies m
		WHERE m.content_id = ?
			OR (m.id = ? AND m.id IS NOT NULL AND m.id <> '' AND m.content_id IS NOT NULL AND m.content_id <> '')
		ORDER BY precedence ASC, m.content_id ASC
		LIMIT 2`, supplied, supplied, supplied).Scan(&matches).Error
	if err != nil {
		return movieTagKey{}, err
	}
	if len(matches) == 0 {
		return movieTagKey{canonical: supplied}, nil
	}
	if matches[0].Precedence == 0 {
		return movieTagKey{canonical: matches[0].ContentID, legacy: matches[0].Legacy, found: true}, nil
	}
	if len(matches) > 1 {
		return movieTagKey{}, fmt.Errorf("movie display ID %q is ambiguous", supplied)
	}
	return movieTagKey{canonical: matches[0].ContentID, legacy: matches[0].Legacy, found: true}, nil
}

func normalizeMovieTagKeyTx(tx *gorm.DB, key movieTagKey) error {
	if key.legacy == "" || key.legacy == key.canonical {
		return nil
	}
	if err := tx.Exec(`DELETE FROM movie_tags
		WHERE movie_id = ? AND EXISTS (
			SELECT 1 FROM movie_tags canonical
			WHERE canonical.movie_id = ? AND canonical.tag = movie_tags.tag
		)`, key.legacy, key.canonical).Error; err != nil {
		return err
	}
	return tx.Model(&models.MovieTag{}).Where("movie_id = ?", key.legacy).Update("movie_id", key.canonical).Error
}

func invalidateMovieTagRenderTx(tx *gorm.DB, movieID string) error {
	result := tx.Model(&models.Movie{}).Where("content_id = ?", movieID).Updates(map[string]any{
		"render_dirty": true, "render_generation": gorm.Expr("render_generation + 1"),
	})
	if result.Error != nil {
		return wrapDBErr("invalidate render", fmt.Sprintf("movie %s after tag mutation", movieID), result.Error)
	}
	if result.RowsAffected != 1 {
		return wrapDBErr("invalidate render", fmt.Sprintf("movie %s after tag mutation", movieID), gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *MovieTagRepository) mutate(ctx context.Context, supplied string, mutation func(*gorm.DB, string) (bool, error)) error {
	return retryOnLocked(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			key, err := resolveMovieTagKey(tx, supplied)
			if err != nil {
				return wrapDBErr("resolve", fmt.Sprintf("movie tag key %s", supplied), err)
			}
			if err := normalizeMovieTagKeyTx(tx, key); err != nil {
				return wrapDBErr("normalize", fmt.Sprintf("movie tag key %s", supplied), err)
			}
			changed, err := mutation(tx, key.canonical)
			if err != nil || !changed || !key.found {
				return err
			}
			return invalidateMovieTagRenderTx(tx, key.canonical)
		})
	})
}

// AddTag associates tag with the movie identified by movieID.
func (r *MovieTagRepository) AddTag(ctx context.Context, movieID, tag string) error {
	return retryOnLocked(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			key, err := resolveMovieTagKey(tx, movieID)
			if err != nil {
				return wrapDBErr("resolve", fmt.Sprintf("movie tag key %s", movieID), err)
			}
			var legacyMatch int64
			if key.legacy != "" {
				if err := tx.Model(&models.MovieTag{}).Where("movie_id = ? AND tag = ?", key.legacy, tag).Count(&legacyMatch).Error; err != nil {
					return wrapDBErr("find", fmt.Sprintf("legacy tag %s for movie %s", tag, movieID), err)
				}
			}
			if err := normalizeMovieTagKeyTx(tx, key); err != nil {
				return wrapDBErr("normalize", fmt.Sprintf("movie tag key %s", movieID), err)
			}
			if legacyMatch != 0 {
				return nil
			}
			movieTag := &models.MovieTag{MovieID: key.canonical, Tag: tag}
			if err := tx.Create(movieTag).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("tag %s for movie %s", tag, movieID), err)
			}
			if !key.found {
				return nil
			}
			return invalidateMovieTagRenderTx(tx, key.canonical)
		})
	})
}

// RemoveTag deletes the association between movieID and tag.
func (r *MovieTagRepository) RemoveTag(ctx context.Context, movieID, tag string) error {
	return r.mutate(ctx, movieID, func(tx *gorm.DB, canonical string) (bool, error) {
		result := tx.Where("movie_id = ? AND tag = ?", canonical, tag).Delete(&models.MovieTag{})
		if result.Error != nil {
			return false, wrapDBErr("delete", fmt.Sprintf("tag %s for movie %s", tag, movieID), result.Error)
		}
		return result.RowsAffected != 0, nil
	})
}

// RemoveAllTags deletes every tag associated with the given movieID.
func (r *MovieTagRepository) RemoveAllTags(ctx context.Context, movieID string) error {
	return r.mutate(ctx, movieID, func(tx *gorm.DB, canonical string) (bool, error) {
		result := tx.Where("movie_id = ?", canonical).Delete(&models.MovieTag{})
		if result.Error != nil {
			return false, wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movieID), result.Error)
		}
		return result.RowsAffected != 0, nil
	})
}

func (r *MovieTagRepository) canonicalKey(ctx context.Context, supplied string) (string, error) {
	key, err := resolveMovieTagKey(r.db.WithContext(ctx), supplied)
	if err != nil {
		return "", wrapDBErr("resolve", fmt.Sprintf("movie tag key %s", supplied), err)
	}
	return key.canonical, nil
}

// GetTagsForMovie returns the tags associated with movieID, ordered by name.
func (r *MovieTagRepository) GetTagsForMovie(ctx context.Context, movieID string) ([]string, error) {
	canonical, err := r.canonicalKey(ctx, movieID)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0)
	err = r.db.WithContext(ctx).Raw("SELECT tag FROM ("+canonicalMovieTagsSQL+") WHERE movie_id = ? ORDER BY tag ASC", canonical).Scan(&tags).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("tags for movie %s", movieID), err)
	}
	return tags, nil
}

// GetMoviesWithTag returns the IDs of movies associated with tag, ordered by movie ID.
func (r *MovieTagRepository) GetMoviesWithTag(ctx context.Context, tag string) ([]string, error) {
	movieIDs := make([]string, 0)
	err := r.db.WithContext(ctx).Raw("SELECT movie_id FROM ("+canonicalMovieTagsSQL+") WHERE tag = ? ORDER BY movie_id ASC", tag).Scan(&movieIDs).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movies with tag %s", tag), err)
	}
	return movieIDs, nil
}

// ListTagsPaginated returns a page of movie tags ordered by movie_id.
// Use this instead of ListAll for large libraries where loading all tags
// into memory at once would be prohibitively expensive.
func (r *MovieTagRepository) ListTagsPaginated(ctx context.Context, limit, offset int) ([]models.MovieTag, error) {
	var movieTags []models.MovieTag
	err := r.db.WithContext(ctx).Raw("SELECT id, movie_id, tag, created_at FROM ("+canonicalMovieTagsSQL+") ORDER BY movie_id ASC, tag ASC LIMIT ? OFFSET ?", limit, offset).Scan(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", "movie tags", err)
	}
	return movieTags, nil
}

// ListAll loads all movie tags into a movie_id→[]tag map.
//
// Deprecated: for large libraries, use ListTagsPaginated with chunked loading
// to avoid loading the entire table into memory. ListAllChunked provides a
// drop-in replacement that loads in configurable chunk sizes.
func (r *MovieTagRepository) ListAll(ctx context.Context) (map[string][]string, error) {
	var movieTags []models.MovieTag
	err := r.db.WithContext(ctx).Raw("SELECT id, movie_id, tag, created_at FROM (" + canonicalMovieTagsSQL + ") ORDER BY movie_id ASC, tag ASC").Scan(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", "movie tags", err)
	}

	result := make(map[string][]string)
	for _, mt := range movieTags {
		result[mt.MovieID] = append(result[mt.MovieID], mt.Tag)
	}
	return result, nil
}

// ListAllChunked loads all movie tags into a movie_id→[]tag map using chunked
// queries. This avoids loading the entire movie_tags table into memory at once.
// A chunkSize of 1000 is recommended for most libraries.
func (r *MovieTagRepository) ListAllChunked(ctx context.Context, chunkSize int) (map[string][]string, error) {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	result := make(map[string][]string)
	offset := 0
	for {
		tags, err := r.ListTagsPaginated(ctx, chunkSize, offset)
		if err != nil {
			return nil, err
		}
		if len(tags) == 0 {
			break
		}
		for _, t := range tags {
			result[t.MovieID] = append(result[t.MovieID], t.Tag)
		}
		offset += chunkSize
	}
	return result, nil
}

// GetUniqueTagsList returns the distinct set of all tags, ordered by name.
func (r *MovieTagRepository) GetUniqueTagsList(ctx context.Context) ([]string, error) {
	var tags []string
	err := r.db.WithContext(ctx).Model(&models.MovieTag{}).Distinct("tag").Order("tag ASC").Pluck("tag", &tags).Error
	if err != nil {
		return nil, wrapDBErr("find", "unique tags", err)
	}
	return tags, nil
}
