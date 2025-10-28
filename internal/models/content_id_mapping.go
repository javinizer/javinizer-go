package models

import "time"

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
