package tui

import "time"

// SortEventPhase identifies which phase of a sort operation emitted an event.
// Mirrors worker.JobEventPhase — the TUI owns its own event vocabulary
// so it does not import the worker package directly.
type SortEventPhase string

// SortEventPhase values are the phases a sort operation reports events for.
const (
	SortEventPhaseScrape SortEventPhase = "scrape"
	SortEventPhaseApply  SortEventPhase = "apply"
)

// SortEventStep identifies which step within a phase emitted an event.
// Mirrors worker.JobEventStep — the TUI owns its own event vocabulary
// so it does not import the worker package directly.
type SortEventStep string

const (
	sortStepQueued   SortEventStep = "queued"
	sortStepScrape   SortEventStep = "scrape"
	sortStepOrganize SortEventStep = "organize"
	sortStepDownload SortEventStep = "download"
	sortStepNFO      SortEventStep = "nfo"
	sortStepApply    SortEventStep = "apply"
	sortStepComplete SortEventStep = "complete"
	sortStepFailed   SortEventStep = "failed"
)

// SortEvent represents a progress event emitted during a sort operation.
// This is the TUI's own event type, decoupled from worker.JobEvent.
// The adapter in sort_service.go converts between the two.
//
// Code and Args are optional structured localization handles. When Code is
// non-empty, the TUI translates it via its localizer (falling back to Message
// for unknown codes or missing locales). When Code is empty, the raw English
// Message is displayed as-is. This mirrors the API/frontend code+fallback
// pattern: known event codes localize locally, unknown/plugin events keep the
// English fallback.
type SortEvent struct {
	JobID     string
	MovieID   string
	Phase     SortEventPhase
	Step      SortEventStep
	Progress  float64
	Message   string
	Code      string
	Args      map[string]any
	Timestamp time.Time
}

// SortEventSubscriber provides access to a stream of SortEvents.
type SortEventSubscriber interface {
	// Events returns a read-only channel that emits SortEvents.
	Events() <-chan SortEvent
	// Close releases the subscriber's resources.
	Close()
	// Done returns a channel that is closed when the subscriber is finished.
	Done() <-chan struct{}
}
