package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// GenreReplacementRepository provides database operations for genre replacements
type GenreReplacementRepository struct {
	db *DB
}

// NewGenreReplacementRepository creates a new genre replacement repository
func NewGenreReplacementRepository(db *DB) *GenreReplacementRepository {
	return &GenreReplacementRepository{db: db}
}

// Create adds a new genre replacement
func (r *GenreReplacementRepository) Create(replacement *models.GenreReplacement) error {
	if err := r.db.Create(replacement).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("genre replacement %s", replacement.Original), err)
	}
	return nil
}

// Upsert creates or updates a genre replacement
func (r *GenreReplacementRepository) Upsert(replacement *models.GenreReplacement) error {
	existing, err := r.FindByOriginal(replacement.Original)
	if err != nil {
		if !isRecordNotFound(err) {
			return err
		}
		// Doesn't exist, create it
		return r.Create(replacement)
	}

	// Exists, update it
	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	if err := r.db.Save(replacement).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("genre replacement %s", replacement.Original), err)
	}
	return nil
}

// FindByOriginal finds a replacement by original genre name
func (r *GenreReplacementRepository) FindByOriginal(original string) (*models.GenreReplacement, error) {
	var replacement models.GenreReplacement
	err := r.db.First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre replacement %s", original), err)
	}
	return &replacement, nil
}

// List returns all genre replacements
func (r *GenreReplacementRepository) List() ([]models.GenreReplacement, error) {
	var replacements []models.GenreReplacement
	err := r.db.Find(&replacements).Error
	if err != nil {
		return nil, wrapDBErr("find", "genre replacements", err)
	}
	return replacements, nil
}

// Delete removes a genre replacement
func (r *GenreReplacementRepository) Delete(original string) error {
	if err := r.db.Delete(&models.GenreReplacement{}, "original = ?", original).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("genre replacement %s", original), err)
	}
	return nil
}

// GetReplacementMap returns all replacements as a map[original]replacement
func (r *GenreReplacementRepository) GetReplacementMap() (map[string]string, error) {
	replacements, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}
