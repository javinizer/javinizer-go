package worker

import (
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobEventBroadcaster_SubscribeAndSend(t *testing.T) {
	b := newJobEventBroadcaster()
	defer b.Close()

	sub := b.Subscribe()
	defer sub.Close()

	b.Send(JobEvent{JobID: models.JobID("j1"), Message: "hello"})

	select {
	case evt := <-sub.Events():
		assert.Equal(t, models.JobID("j1"), evt.JobID)
		assert.Equal(t, "hello", evt.Message)
	default:
		t.Fatal("expected to receive event")
	}
}

func TestJobEventBroadcaster_UnsubscribeRemovesFromList(t *testing.T) {
	b := newJobEventBroadcaster()

	sub1 := b.Subscribe()
	sub2 := b.Subscribe()

	require.Len(t, b.subscribers, 2)

	sub1.Close()

	require.Len(t, b.subscribers, 1)

	b.Send(JobEvent{JobID: models.JobID("j1"), Message: "after-close"})

	evt := <-sub2.Events()
	assert.Equal(t, "after-close", evt.Message)

	b.Close()
}

func TestJobEventBroadcaster_UnsubscribeClosesChannel(t *testing.T) {
	b := newJobEventBroadcaster()

	sub := b.Subscribe()
	sub.Close()

	_, ok := <-sub.Events()
	assert.False(t, ok, "unsubscribed channel should be closed")
}

func TestJobEventBroadcaster_UnsubscribeIdempotent(t *testing.T) {
	b := newJobEventBroadcaster()

	sub := b.Subscribe()
	require.Len(t, b.subscribers, 1)

	sub.Close()
	assert.Len(t, b.subscribers, 0)

	sub.Close()
	assert.Len(t, b.subscribers, 0)

	b.Close()
}

func TestJobEventBroadcaster_CloseClosesAllSubscribers(t *testing.T) {
	b := newJobEventBroadcaster()

	sub1 := b.Subscribe()
	sub2 := b.Subscribe()

	b.Close()

	_, ok1 := <-sub1.Events()
	assert.False(t, ok1)

	_, ok2 := <-sub2.Events()
	assert.False(t, ok2)
}

func TestJobEventBroadcaster_SendAfterUnsubscribeNoPanic(t *testing.T) {
	b := newJobEventBroadcaster()

	sub := b.Subscribe()
	sub.Close()

	assert.NotPanics(t, func() {
		b.Send(JobEvent{JobID: models.JobID("j1"), Message: "should-not-panic"})
	})

	b.Close()
}

func TestJobEventBroadcaster_ConcurrentSendAndUnsubscribe(t *testing.T) {
	b := newJobEventBroadcaster()

	const numSubscribers = 10
	const numEvents = 100

	subs := make([]JobEventSubscriber, numSubscribers)
	for i := range subs {
		subs[i] = b.Subscribe()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < numEvents; i++ {
			b.Send(JobEvent{JobID: models.JobID("j1"), Progress: float64(i)})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < numSubscribers; i++ {
			subs[i].Close()
		}
	}()

	wg.Wait()

	b.Close()
}

func TestJobEventBroadcaster_SendAfterBroadcasterCloseNoPanic(t *testing.T) {
	b := newJobEventBroadcaster()
	_ = b.Subscribe()

	b.Close()

	assert.NotPanics(t, func() {
		b.Send(JobEvent{JobID: models.JobID("j1"), Message: "after-close"})
	})
}

func TestJobEventBroadcaster_SubscribeAfterClose(t *testing.T) {
	b := newJobEventBroadcaster()
	b.Close()

	sub := b.Subscribe()

	_, ok := <-sub.Events()
	assert.False(t, ok, "subscriber from closed broadcaster should have closed channel")
}

func TestJobEventBroadcaster_UnsubscribeThenBroadcasterClose(t *testing.T) {
	b := newJobEventBroadcaster()

	sub1 := b.Subscribe()
	sub2 := b.Subscribe()

	sub1.Close()

	_, ok := <-sub1.Events()
	assert.False(t, ok, "unsubscribed channel should be closed")

	b.Close()

	_, ok = <-sub2.Events()
	assert.False(t, ok, "remaining subscriber channel should be closed by broadcaster")
}

func TestChannelSubscriber_NilBroadcasterClose(t *testing.T) {
	ch := make(chan JobEvent)
	close(ch)

	sub := &channelSubscriber{ch: ch}

	assert.NotPanics(t, func() {
		sub.Close()
	})

	assert.True(t, sub.closed)
}
