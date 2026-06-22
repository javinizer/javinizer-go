package worker

import (
	"sync"
)

// batchJobEventSource encapsulates event broadcasting for a BatchJob.
// Extracted from BatchJob to isolate the event-streaming concern:
// Subscribe, SendJobEvent, and CloseEventBroadcaster all operate
// exclusively on eventBroadcaster and keepBroadcasterOpen.
// BatchJob embeds this struct so callers see no API change.
type batchJobEventSource struct {
	mu                  sync.RWMutex         `json:"-"`
	eventBroadcaster    *jobEventBroadcaster `json:"-"` // Event streaming for subscribers
	keepBroadcasterOpen bool                 `json:"-"` // When true, phase methods won't close the broadcaster
}

// newBatchJobEventSource creates a batchJobEventSource with a fresh broadcaster.
func newBatchJobEventSource() batchJobEventSource {
	return batchJobEventSource{
		eventBroadcaster: newJobEventBroadcaster(),
	}
}

// Subscribe returns a new subscriber for job events.
// If the broadcaster is nil, returns a closed-channel subscriber.
func (es *batchJobEventSource) Subscribe() JobEventSubscriber {
	es.mu.RLock()
	broadcaster := es.eventBroadcaster
	es.mu.RUnlock()

	if broadcaster == nil {
		ch := make(chan JobEvent)
		close(ch)
		return &channelSubscriber{ch: ch, closed: true}
	}
	return broadcaster.Subscribe()
}

// SendJobEvent broadcasts an event to all current subscribers.
// No-op when the broadcaster is nil.
func (es *batchJobEventSource) SendJobEvent(event JobEvent) {
	es.mu.RLock()
	broadcaster := es.eventBroadcaster
	es.mu.RUnlock()

	if broadcaster != nil {
		broadcaster.Send(event)
	}
}

// CloseEventBroadcaster shuts down the broadcaster and all subscribers.
// No-op when the broadcaster is nil.
func (es *batchJobEventSource) CloseEventBroadcaster() {
	es.mu.RLock()
	broadcaster := es.eventBroadcaster
	es.mu.RUnlock()

	if broadcaster != nil {
		broadcaster.Close()
	}
}

// SetKeepOpen sets the keepBroadcasterOpen flag. When true, phase methods
// won't close the broadcaster — the caller is responsible for closing it.
// This consolidates broadcaster lifecycle management into the event source.
func (es *batchJobEventSource) SetKeepOpen(keepOpen bool) {
	es.mu.Lock()
	es.keepBroadcasterOpen = keepOpen
	es.mu.Unlock()
}
