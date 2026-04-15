package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MovieTagRepository handles movie tag operations
type MovieTagRepository struct {
	db *DB
}

// NewMovieTagRepository creates a new movie tag repository
func NewMovieTagRepository(db *DB) *MovieTagRepository {
	return &MovieTagRepository{db: db}
}

// AddTag adds a tag to a movie
// Returns error if tag already exists (UNIQUE constraint violation)
func (r *MovieTagRepository) AddTag(movieID, tag string) error {
	movieTag := &models.MovieTag{
		MovieID: movieID,
		Tag:     tag,
	}
	if err := r.db.Create(movieTag).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("tag %s for movie %s", tag, movieID), err)
	}
	return nil
}

// RemoveTag removes a specific tag from a movie
func (r *MovieTagRepository) RemoveTag(movieID, tag string) error {
	if err := r.db.Where("movie_id = ? AND tag = ?", movieID, tag).Delete(&models.MovieTag{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("tag %s for movie %s", tag, movieID), err)
	}
	return nil
}

// RemoveAllTags removes all tags for a movie
func (r *MovieTagRepository) RemoveAllTags(movieID string) error {
	if err := r.db.Where("movie_id = ?", movieID).Delete(&models.MovieTag{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movieID), err)
	}
	return nil
}

// GetTagsForMovie returns all tags for a specific movie
func (r *MovieTagRepository) GetTagsForMovie(movieID string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Where("movie_id = ?", movieID).Order("tag ASC").Find(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("tags for movie %s", movieID), err)
	}

	tags := make([]string, len(movieTags))
	for i, mt := range movieTags {
		tags[i] = mt.Tag
	}
	return tags, nil
}

// GetMoviesWithTag returns all movie IDs that have the specified tag
func (r *MovieTagRepository) GetMoviesWithTag(tag string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Where("tag = ?", tag).Order("movie_id ASC").Find(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movies with tag %s", tag), err)
	}

	movieIDs := make([]string, len(movieTags))
	for i, mt := range movieTags {
		movieIDs[i] = mt.MovieID
	}
	return movieIDs, nil
}

// ListAll returns a map of all movie IDs to their tags
func (r *MovieTagRepository) ListAll() (map[string][]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Order("movie_id ASC, tag ASC").Find(&movieTags).Error
	if err != nil {
		return nil, wrapDBErr("find", "movie tags", err)
	}

	result := make(map[string][]string)
	for _, mt := range movieTags {
		result[mt.MovieID] = append(result[mt.MovieID], mt.Tag)
	}
	return result, nil
}

// GetUniqueTagsList returns all unique tags in the database
func (r *MovieTagRepository) GetUniqueTagsList() ([]string, error) {
	var tags []string
	err := r.db.Model(&models.MovieTag{}).Distinct("tag").Order("tag ASC").Pluck("tag", &tags).Error
	if err != nil {
		return nil, wrapDBErr("find", "unique tags", err)
	}
	return tags, nil
}
