package worker

import (
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// JobEventPhase identifies which phase of a batch job emitted an event.
type JobEventPhase string

const (
	JobEventPhaseScrape JobEventPhase = "scrape"
	jobEventPhaseApply  JobEventPhase = "apply"
)

// JobEventStep identifies which step within a phase emitted an event.
type JobEventStep string

const (
	StepQueued   JobEventStep = "queued"
	StepScrape   JobEventStep = "scrape"
	stepOrganize JobEventStep = "organize"
	stepDownload JobEventStep = "download"
	stepNFO      JobEventStep = "nfo"
	StepApply    JobEventStep = "apply"
	StepComplete JobEventStep = "complete"
	StepFailed   JobEventStep = "failed"
)

// JobEvent represents a progress event emitted during batch job execution.
// Events are broadcast to all subscribers via the JobEventSubscriber channel.
type JobEvent struct {
	JobID     models.JobID  `json:"job_id"`
	MovieID   string        `json:"movie_id"`
	Phase     JobEventPhase `json:"phase"`
	Step      JobEventStep  `json:"step"`
	Progress  float64       `json:"progress"`
	Message   string        `json:"message"`
	Timestamp time.Time     `json:"timestamp"`
}

// JobEventSubscriber provides access to a stream of JobEvents.
// Callers receive a subscriber by calling BatchJob.Subscribe().
type JobEventSubscriber interface {
	// Events returns a read-only channel that emits JobEvents.
	// The channel is closed when Close is called on the subscriber
	// or when the broadcaster is closed.
	Events() <-chan JobEvent
	// Close releases the subscriber's resources.
	// It is safe to call Close multiple times.
	Close()
}

// jobEventBroadcaster implements fan-out event distribution to subscribers.
// It is mutex-protected for concurrent access from producers and consumers.
type jobEventBroadcaster struct {
	mu          sync.Mutex
	subscribers []chan JobEvent
	closed      bool
}

// newJobEventBroadcaster creates a new broadcaster ready to accept
// subscribers and broadcast events.
func newJobEventBroadcaster() *jobEventBroadcaster {
	return &jobEventBroadcaster{}
}

// Subscribe creates a new subscriber with a buffered channel.
// The returned subscriber receives events until Close is called
// on the subscriber or the broadcaster.
// If the broadcaster is already closed, returns a subscriber with
// a pre-closed channel.
func (b *jobEventBroadcaster) Subscribe() JobEventSubscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		ch := make(chan JobEvent)
		close(ch)
		return &channelSubscriber{ch: ch, closed: true}
	}

	ch := make(chan JobEvent, 64)
	b.subscribers = append(b.subscribers, ch)
	return &channelSubscriber{ch: ch, broadcaster: b}
}

// Send broadcasts a JobEvent to all subscriber channels.
// Uses non-blocking send with drop-oldest on full buffer:
// if a subscriber's buffer is full, the oldest event is discarded
// to make room for the new one. This prevents slow consumers from
// blocking the producer.
func (b *jobEventBroadcaster) Send(event JobEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}

// Close closes all subscriber channels and clears the subscriber list.
// After Close, no more events can be sent and all subscriber channels
// will be drained by their receivers.
func (b *jobEventBroadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = nil
}

// unsubscribe removes a subscriber's channel from the broadcaster's list.
// Called by channelSubscriber.Close() before closing the channel.
// Returns true if the channel was found and removed, false otherwise.
func (b *jobEventBroadcaster) unsubscribe(ch chan JobEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return false
	}

	for i, subCh := range b.subscribers {
		if subCh == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return true
		}
	}
	return false
}

// channelSubscriber is the concrete implementation of JobEventSubscriber.
// It wraps a buffered channel and provides idempotent Close.
type channelSubscriber struct {
	ch          chan JobEvent
	broadcaster *jobEventBroadcaster
	closed      bool
	mu          sync.Mutex
}

// Events returns the read-only event channel.
func (s *channelSubscriber) Events() <-chan JobEvent {
	return s.ch
}

// Close is idempotent — safe to call multiple times.
// It removes the subscriber's channel from the broadcaster's list
// and closes the channel. After unsubscribe removes the channel from
// the list, no concurrent Send() can be touching it, so closing is safe.
func (s *channelSubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.broadcaster != nil {
		if s.broadcaster.unsubscribe(s.ch) {
			close(s.ch)
		}
	}
}
