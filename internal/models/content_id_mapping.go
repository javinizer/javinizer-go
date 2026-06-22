package models

import (
	"context"
	"time"
)

// ContentIDMapping stores the mapping between search IDs and actual DMM content IDs
// This is used to cache the resolution of display IDs (like "MDB-087") to actual
// content IDs (like "61mdb087") that DMM uses internally
type ContentIDMapping struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	SearchID  string    `gorm:"uniqueIndex;not null" json:"search_id"` // e.g., "MDB-087"
	ContentID string    `gorm:"not null" json:"content_id"`            // e.g., "61mdb087"
	Source    string    `gorm:"not null" json:"source"`                // e.g., "dmm"
	CreatedAt time.Time `json:"created_at"`
}

// ContentIDMappingRepositoryInterface defines the contract for content ID mapping operations.
// Defined in models (not database) to avoid import cycles — scraperutil and scraper packages
// need this interface but must not depend on the database package.
type ContentIDMappingRepositoryInterface interface {
	FindBySearchID(ctx context.Context, searchID string) (*ContentIDMapping, error)
	Create(ctx context.Context, mapping *ContentIDMapping) error
	Delete(ctx context.Context, searchID string) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]ContentIDMapping, error)
	GetAll(ctx context.Context) ([]ContentIDMapping, error)
	GetAllChunked(ctx context.Context, chunkSize int) ([]ContentIDMapping, error)
}
