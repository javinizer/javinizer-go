package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

type MovieTagRepository struct {
	db *DB
}

func NewMovieTagRepository(db *DB) *MovieTagRepository {
	return &MovieTagRepository{db: db}
}

func (r *MovieTagRepository) AddTag(ctx context.Context, movieID, tag string) error {
	movieTag := &models.MovieTag{
		MovieID: movieID,
		Tag:     tag,
	}
	if err := r.db.WithContext(ctx).Create(movieTag).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("tag %s for movie %s", tag, movieID), err)
	}
	return nil
}

func (r *MovieTagRepository) RemoveTag(ctx context.Context, movieID, tag string) error {
	if err := r.db.WithContext(ctx).Where("movie_id = ? AND tag = ?", movieID, tag).Delete(&models.MovieTag{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("tag %s for movie %s", tag, movieID), err)
	}
	return nil
}

func (r *MovieTagRepository) RemoveAllTags(ctx context.Context, movieID string) error {
	if err := r.db.WithContext(ctx).Where("movie_id = ?", movieID).Delete(&models.MovieTag{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movieID), err)
	}
	return nil
}

func (r *MovieTagRepository) GetTagsForMovie(ctx context.Context, movieID string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.WithContext(ctx).Where("movie_id = ?", movieID).Order("tag ASC").Find(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("tags for movie %s", movieID), err)
	}

	tags := make([]string, len(movieTags))
	for i, mt := range movieTags {
		tags[i] = mt.Tag
	}
	return tags, nil
}

func (r *MovieTagRepository) GetMoviesWithTag(ctx context.Context, tag string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.WithContext(ctx).Where("tag = ?", tag).Order("movie_id ASC").Find(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movies with tag %s", tag), err)
	}

	movieIDs := make([]string, len(movieTags))
	for i, mt := range movieTags {
		movieIDs[i] = mt.MovieID
	}
	return movieIDs, nil
}

// ListTagsPaginated returns a page of movie tags ordered by movie_id.
// Use this instead of ListAll for large libraries where loading all tags
// into memory at once would be prohibitively expensive.
func (r *MovieTagRepository) ListTagsPaginated(ctx context.Context, limit, offset int) ([]models.MovieTag, error) {
	var movieTags []models.MovieTag
	err := r.db.WithContext(ctx).Order("movie_id ASC, tag ASC").Limit(limit).Offset(offset).Find(&movieTags).Error
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
	err := r.db.WithContext(ctx).Order("movie_id ASC, tag ASC").Find(&movieTags).Error
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

func (r *MovieTagRepository) GetUniqueTagsList(ctx context.Context) ([]string, error) {
	var tags []string
	err := r.db.WithContext(ctx).Model(&models.MovieTag{}).Distinct("tag").Order("tag ASC").Pluck("tag", &tags).Error
	if err != nil {
		return nil, wrapDBErr("find", "unique tags", err)
	}
	return tags, nil
}
