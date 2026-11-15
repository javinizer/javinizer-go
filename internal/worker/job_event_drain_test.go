package worker

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReplaceOldestJobEventAfterSubscriberDrain(t *testing.T) {
	for _, drained := range []int{0, 1, 32, 63, 64} {
		t.Run(fmt.Sprintf("drained_%d", drained), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := newJobEventBroadcaster()
				sub := b.Subscribe()
				defer sub.Close()
				defer b.Close()
				ch := sub.(*channelSubscriber).ch
				events := make([]JobEvent, cap(ch))
				for i := range events {
					events[i] = JobEvent{MovieID: fmt.Sprintf("movie-%d", i), Message: fmt.Sprintf("event-%d", i), Progress: float64(i), Timestamp: time.Unix(int64(i), 0)}
					b.Send(events[i])
				}
				latest := JobEvent{MovieID: "latest", Message: "after full buffer", Progress: 100, Timestamp: time.Unix(100, 0)}

				// Pause the producer at the real failed-send boundary. Consumers
				// do not hold b.mu and can drain the channel before overflow recovery.
				b.mu.Lock()
				select {
				case ch <- latest:
					b.mu.Unlock()
					t.Fatal("subscriber buffer was not full")
				default:
				}
				consumed := make(chan []JobEvent)
				go func() {
					got := make([]JobEvent, 0, drained)
					for i := 0; i < drained; i++ {
						got = append(got, <-sub.Events())
					}
					consumed <- got
				}()
				gotConsumed := <-consumed
				replaceOldestJobEvent(ch, latest)
				b.mu.Unlock()

				// No consumer remains to rescue a blocking receive/send. synctest
				// detects a deadlock if overflow recovery stops making progress.
				require.Equal(t, events[:drained], gotConsumed)
				b.Close()
				var remaining []JobEvent
				for event := range sub.Events() {
					remaining = append(remaining, event)
				}
				want := append([]JobEvent{}, events[min(drained+1, len(events)):]...)
				want = append(want, latest)
				require.Equal(t, want, remaining, "drop at most one remaining event, keep order, and always deliver the newest event")
			})
		})
	}
}
