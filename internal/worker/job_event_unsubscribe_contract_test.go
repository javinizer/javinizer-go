package worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// unsubscribe promises false for an absent channel independently of the public
// subscriber Close method's idempotence guard. Exercise that private contract
// without changing the broadcaster's internal subscriber list.
func TestJobEventBroadcasterUnsubscribeUnknownChannelPreservesSubscribers(t *testing.T) {
	b := newJobEventBroadcaster()
	defer b.Close()
	first := b.Subscribe()
	second := b.Subscribe()
	defer first.Close()
	defer second.Close()

	unknown := make(chan JobEvent, 1)
	defer close(unknown)
	require.False(t, b.unsubscribe(unknown))
	require.Len(t, b.subscribers, 2)

	assertReceives := func(sub JobEventSubscriber, want JobEvent) {
		t.Helper()
		select {
		case got, ok := <-sub.Events():
			require.True(t, ok, "an unrelated removal must not close a live subscription")
			require.Equal(t, want, got)
		default:
			t.Fatal("live subscriber did not receive the synchronously broadcast event")
		}
	}
	event := JobEvent{Message: "after unknown removal"}
	b.Send(event)
	assertReceives(first, event)
	assertReceives(second, event)

	// Public Close removes and closes its own channel. The private removal
	// operation must then report absence without disturbing the other owner.
	removed := first.(*channelSubscriber).ch
	first.Close()
	require.False(t, b.unsubscribe(removed))
	require.Len(t, b.subscribers, 1)
	select {
	case _, ok := <-first.Events():
		require.False(t, ok)
	default:
		t.Fatal("Close did not close the removed subscriber's channel")
	}
	event = JobEvent{Message: "after repeated removal"}
	b.Send(event)
	assertReceives(second, event)
}
