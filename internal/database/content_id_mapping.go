package database

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ContentIDMappingRepository provides database operations for content ID mappings
type ContentIDMappingRepository struct {
	db *DB
}

// NewContentIDMappingRepository creates a new content ID mapping repository
func NewContentIDMappingRepository(db *DB) *ContentIDMappingRepository {
	return &ContentIDMappingRepository{db: db}
}

// FindBySearchID looks up a content ID mapping by the search ID
// Search IDs are normalized to uppercase for case-insensitive matching
func (r *ContentIDMappingRepository) FindBySearchID(searchID string) (*models.ContentIDMapping, error) {
	var mapping models.ContentIDMapping

	// Normalize search ID to uppercase for consistent lookups
	normalizedID := strings.ToUpper(searchID)

	err := r.db.Where("search_id = ?", normalizedID).First(&mapping).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("content ID mapping %s", normalizedID), err)
	}

	return &mapping, nil
}

// Create saves a new content ID mapping to the database
// If a mapping with the same search ID already exists, it will be updated
func (r *ContentIDMappingRepository) Create(mapping *models.ContentIDMapping) error {
	// Normalize search ID to uppercase
	mapping.SearchID = strings.ToUpper(mapping.SearchID)

	// Use upsert to handle duplicates gracefully
	// This will update the existing record if search_id already exists
	if err := r.db.Where(models.ContentIDMapping{SearchID: mapping.SearchID}).
		Assign(models.ContentIDMapping{
			ContentID: mapping.ContentID,
			Source:    mapping.Source,
		}).
		FirstOrCreate(mapping).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("content ID mapping %s", mapping.SearchID), err)
	}
	return nil
}

// Delete removes a content ID mapping from the database
func (r *ContentIDMappingRepository) Delete(searchID string) error {
	normalizedID := strings.ToUpper(searchID)
	if err := r.db.Where("search_id = ?", normalizedID).Delete(&models.ContentIDMapping{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("content ID mapping %s", normalizedID), err)
	}
	return nil
}

// GetAll retrieves all content ID mappings
func (r *ContentIDMappingRepository) GetAll() ([]models.ContentIDMapping, error) {
	var mappings []models.ContentIDMapping
	err := r.db.Find(&mappings).Error
	if err != nil {
		return nil, wrapDBErr("find", "content ID mappings", err)
	}
	return mappings, nil
}
