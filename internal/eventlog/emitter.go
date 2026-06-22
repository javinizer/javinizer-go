package eventlog

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
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
	EmitScraperEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error

	// EmitOrganizeEvent records an organize-related event.
	// source identifies the operation stage (e.g., "file_move", "nfo_gen", "image_download").
	EmitOrganizeEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error

	// EmitSystemEvent records a system lifecycle event.
	// source identifies the subsystem (e.g., "server", "config", "database").
	EmitSystemEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error

	// Stats returns the number of events successfully emitted and failed since startup.
	// Useful for diagnostics and health checks — does not block.
	Stats() (emitted, failed int64)
}

// eventEmitter is the concrete implementation of EventEmitter.
// It persists events via EventRepositoryInterface following the DI pattern.
type eventEmitter struct {
	repo      database.EventRepositoryInterface
	emitCount atomic.Int64
	failCount atomic.Int64
}

// NewEmitter creates a new EventEmitter that persists events through the given repository.
// Per D-09: v1 only delivers the interface + concrete implementation.
// No integration with actual scraper/organizer/system code yet.
func NewEmitter(repo database.EventRepositoryInterface) EventEmitter {
	return &eventEmitter{repo: repo}
}

// EmitScraperEvent records a scraper-related event with event_type="scraper"
func (e *eventEmitter) EmitScraperEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	return e.emit(ctx, models.EventCategoryScraper, source, message, severity, eventCtx)
}

// EmitOrganizeEvent records an organize-related event with event_type="organize"
func (e *eventEmitter) EmitOrganizeEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	return e.emit(ctx, models.EventCategoryOrganize, source, message, severity, eventCtx)
}

// EmitSystemEvent records a system lifecycle event with event_type="system"
func (e *eventEmitter) EmitSystemEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	return e.emit(ctx, models.EventCategorySystem, source, message, severity, eventCtx)
}

// emit is the shared implementation that creates an Event model and persists it.
// If eventCtx is nil, Context field is set to empty string.
// If json.Marshal fails for eventCtx, a graceful degradation message is stored instead.
func (e *eventEmitter) emit(ctx context.Context, eventType models.EventCategory, source, message string, severity models.EventSeverity, eventCtx map[string]any) error {
	if e.repo == nil {
		e.failCount.Add(1)
		return fmt.Errorf("event emitter: repository is nil, cannot persist event")
	}

	// If the caller's context is already cancelled, skip the emit —
	// the request is done and nobody will read the event.
	if ctx.Err() != nil {
		e.failCount.Add(1)
		return fmt.Errorf("event emitter: caller context cancelled: %w", ctx.Err())
	}

	// Use a timeout to prevent a slow DB write from blocking the caller.
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	contextJSON := ""
	if eventCtx != nil {
		bytes, err := json.Marshal(eventCtx)
		if err != nil {
			e.failCount.Add(1)
			logging.Debugf("[eventlog] Failed to marshal context for %s event: %v", source, err)
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

	if err := e.repo.Create(emitCtx, event); err != nil {
		e.failCount.Add(1)
		logging.Debugf("[eventlog] Failed to persist %s event from %s: %v", eventType, source, err)
		return err
	}

	e.emitCount.Add(1)
	return nil
}

// Stats returns the number of events successfully emitted and failed.
func (e *eventEmitter) Stats() (emitted, failed int64) {
	return e.emitCount.Load(), e.failCount.Load()
}
