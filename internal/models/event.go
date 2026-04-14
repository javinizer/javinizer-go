package models

import "time"

// Event category constants for the event_type field
const (
	EventCategoryScraper  = "scraper"
	EventCategoryOrganize = "organize"
	EventCategorySystem   = "system"
)

// Severity constants matching slog/Logrus levels (per D-08)
const (
	SeverityDebug = "debug"
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// Event represents a structured event log entry for debugging and bug reporting
type Event struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	EventType string    `json:"event_type" gorm:"not null;index:idx_events_type;index:idx_events_type_severity;index:idx_events_type_source"`
	Severity  string    `json:"severity" gorm:"not null;index:idx_events_severity;index:idx_events_type_severity"`
	Message   string    `json:"message" gorm:"not null;type:text"`
	Context   string    `json:"context" gorm:"type:text"` // JSON-encoded details
	Source    string    `json:"source" gorm:"index:idx_events_source;index:idx_events_type_source"`
	CreatedAt time.Time `json:"created_at" gorm:"not null;index:idx_events_created_at"`
}

// TableName specifies the table name for Event
func (Event) TableName() string {
	return "events"
}
