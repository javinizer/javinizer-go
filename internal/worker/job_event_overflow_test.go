package worker

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJobEventBroadcasterOverflowRetainsNewestEvents(t *testing.T) {
	for _, count := range []int{0, 1, 63, 64, 65, 128, 193} {
		t.Run(fmt.Sprintf("events_%d", count), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := newJobEventBroadcaster()
				defer b.Close()
				sub := b.Subscribe()
				defer sub.Close()
				events := make([]JobEvent, count)
				for i := range events {
					events[i] = JobEvent{MovieID: fmt.Sprintf("movie-%d", i), Message: fmt.Sprintf("event-%d", i), Progress: float64(i), Timestamp: time.Unix(int64(i), 0)}
					b.Send(events[i])
				}
				b.Close()
				got := make([]JobEvent, 0)
				for event := range sub.Events() {
					got = append(got, event)
				}
				start := max(0, count-64)
				require.Equal(t, events[start:], got)
			})
		})
	}
}

func TestJobEventBroadcasterOverflowIsolatesSlowSubscribers(t *testing.T) {
	for _, stalledFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("stalled_first_%t", stalledFirst), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := newJobEventBroadcaster()
				defer b.Close()
				fast, stalled := b.Subscribe(), b.Subscribe()
				if stalledFirst {
					fast, stalled = stalled, fast
				}
				defer fast.Close()
				defer stalled.Close()
				events := make([]JobEvent, 193)
				for i := range events {
					events[i] = JobEvent{Message: fmt.Sprintf("event-%d", i)}
					b.Send(events[i])
					select {
					case got, ok := <-fast.Events():
						require.True(t, ok)
						require.Equal(t, events[i], got)
					default:
						t.Fatal("stalled subscriber prevented delivery to fast subscriber")
					}
				}
				b.Close()
				got := make([]JobEvent, 0, 64)
				for event := range stalled.Events() {
					got = append(got, event)
				}
				require.Equal(t, events[len(events)-64:], got)
				_, ok := <-fast.Events()
				require.False(t, ok)
			})
		})
	}
}
