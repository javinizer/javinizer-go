package eventlog

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// EventEmitter allows any code path to emit typed structured events
// that are persisted to the events table. Per D-09, v1 delivers the
// interface only — concrete event emission in scrapers/organizer/system is v2.
//
// Events are fire-and-forget: a failure to emit an event must NOT block
// the calling operation. The Emit methods return error for logging purposes,
// but callers should not treat it as fatal. Log the error and continue.
type EventEmitter interface {
	// EmitScraperEvent records a scraper-related event.
	// source identifies the scraper (e.g., "r18dev", "dmm").
	// message is a human-readable description.
	// severity is one of SeverityDebug/Info/Warn/Error.
	// context is optional structured detail (movie_id, url, error detail, etc).
	EmitScraperEvent(source, message, severity string, context map[string]interface{}) error

	// EmitOrganizeEvent records an organize-related event.
	// source identifies the operation stage (e.g., "file_move", "nfo_gen", "image_download").
	EmitOrganizeEvent(source, message, severity string, context map[string]interface{}) error

	// EmitSystemEvent records a system lifecycle event.
	// source identifies the subsystem (e.g., "server", "config", "database").
	EmitSystemEvent(source, message, severity string, context map[string]interface{}) error
}

// eventEmitter is the concrete implementation of EventEmitter.
// It persists events via EventRepositoryInterface following the DI pattern.
type eventEmitter struct {
	repo database.EventRepositoryInterface
}

// NewEmitter creates a new EventEmitter that persists events through the given repository.
// Per D-09: v1 only delivers the interface + concrete implementation.
// No integration with actual scraper/organizer/system code yet.
func NewEmitter(repo database.EventRepositoryInterface) EventEmitter {
	return &eventEmitter{repo: repo}
}

// EmitScraperEvent records a scraper-related event with event_type="scraper"
func (e *eventEmitter) EmitScraperEvent(source, message, severity string, context map[string]interface{}) error {
	return e.emit(models.EventCategoryScraper, source, message, severity, context)
}

// EmitOrganizeEvent records an organize-related event with event_type="organize"
func (e *eventEmitter) EmitOrganizeEvent(source, message, severity string, context map[string]interface{}) error {
	return e.emit(models.EventCategoryOrganize, source, message, severity, context)
}

// EmitSystemEvent records a system lifecycle event with event_type="system"
func (e *eventEmitter) EmitSystemEvent(source, message, severity string, context map[string]interface{}) error {
	return e.emit(models.EventCategorySystem, source, message, severity, context)
}

// emit is the shared implementation that creates an Event model and persists it.
// If context is nil, Context field is set to empty string.
// If json.Marshal fails for context, a graceful degradation message is stored instead.
func (e *eventEmitter) emit(eventType, source, message, severity string, context map[string]interface{}) error {
	if e.repo == nil {
		return fmt.Errorf("event emitter: repository is nil, cannot persist event")
	}

	contextJSON := ""
	if context != nil {
		bytes, err := json.Marshal(context)
		if err != nil {
			// Graceful degradation: store error message instead of failing
			contextJSON = `{"error":"failed to marshal context"}`
		} else {
			contextJSON = string(bytes)
		}
	}

	event := &models.Event{
		EventType: eventType,
		Severity:  severity,
		Message:   message,
		Context:   contextJSON,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}

	return e.repo.Create(event)
}
