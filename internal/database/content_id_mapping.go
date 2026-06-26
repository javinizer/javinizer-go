package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

type ContentIDMappingRepository struct {
	db *DB
}

func NewContentIDMappingRepository(db *DB) *ContentIDMappingRepository {
	return &ContentIDMappingRepository{db: db}
}

func (r *ContentIDMappingRepository) FindBySearchID(ctx context.Context, searchID string) (*models.ContentIDMapping, error) {
	var mapping models.ContentIDMapping

	normalizedID := strings.ToUpper(searchID)

	err := r.db.WithContext(ctx).Where("search_id = ?", normalizedID).First(&mapping).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("content ID mapping %s", normalizedID), err)
	}

	return &mapping, nil
}

func (r *ContentIDMappingRepository) Create(ctx context.Context, mapping *models.ContentIDMapping) error {
	mapping.SearchID = strings.ToUpper(mapping.SearchID)

	if err := r.db.WithContext(ctx).Where(models.ContentIDMapping{SearchID: mapping.SearchID}).
		Assign(models.ContentIDMapping{
			ContentID: mapping.ContentID,
			Source:    mapping.Source,
		}).
		FirstOrCreate(mapping).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("content ID mapping %s", mapping.SearchID), err)
	}
	return nil
}

func (r *ContentIDMappingRepository) Delete(ctx context.Context, searchID string) error {
	normalizedID := strings.ToUpper(searchID)
	if err := r.db.WithContext(ctx).Where("search_id = ?", normalizedID).Delete(&models.ContentIDMapping{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("content ID mapping %s", normalizedID), err)
	}
	return nil
}

// GetAllPaginated returns a page of content ID mappings.
// Use this instead of GetAll for large tables where loading all mappings
// into memory at once would be prohibitively expensive.
func (r *ContentIDMappingRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]models.ContentIDMapping, error) {
	var mappings []models.ContentIDMapping
	err := r.db.WithContext(ctx).Order("search_id ASC").Limit(limit).Offset(offset).Find(&mappings).Error
	if err != nil {
		return nil, wrapDBErr("find", "content ID mappings", err)
	}
	return mappings, nil
}

// GetAll loads all content ID mappings from the database.
//
// Deprecated: for large tables, use GetAllPaginated with chunked loading
// to avoid loading the entire table into memory. GetAllChunked provides a
// drop-in replacement that loads in configurable chunk sizes.
func (r *ContentIDMappingRepository) GetAll(ctx context.Context) ([]models.ContentIDMapping, error) {
	var mappings []models.ContentIDMapping
	// Order by search_id ASC to match GetAllPaginated/GetAllChunked so the
	// deprecated path and its replacements produce the same row sequence.
	err := r.db.WithContext(ctx).Order("search_id ASC").Find(&mappings).Error
	if err != nil {
		return nil, wrapDBErr("find", "content ID mappings", err)
	}
	return mappings, nil
}

// GetAllChunked loads all content ID mappings using chunked queries.
// This avoids loading the entire content_id_mappings table into memory at once.
// A chunkSize of 1000 is recommended for most libraries.
func (r *ContentIDMappingRepository) GetAllChunked(ctx context.Context, chunkSize int) ([]models.ContentIDMapping, error) {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	var all []models.ContentIDMapping
	offset := 0
	for {
		chunk, err := r.GetAllPaginated(ctx, chunkSize, offset)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			break
		}
		all = append(all, chunk...)
		offset += chunkSize
	}
	return all, nil
}
