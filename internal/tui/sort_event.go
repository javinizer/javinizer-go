package tui

import "time"

// SortEventPhase identifies which phase of a sort operation emitted an event.
// Mirrors worker.JobEventPhase — the TUI owns its own event vocabulary
// so it does not import the worker package directly.
type SortEventPhase string

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
type SortEvent struct {
	JobID     string
	MovieID   string
	Phase     SortEventPhase
	Step      SortEventStep
	Progress  float64
	Message   string
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
