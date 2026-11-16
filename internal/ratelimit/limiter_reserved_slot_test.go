package ratelimit

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiterReservedSlotCancellationAfterSchedulingDelay(t *testing.T) {
	for _, laterReservation := range []bool{false, true} {
		name := "expired_tail_clamps_to_now"
		if laterReservation {
			name = "later_reservation_is_preserved"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// This helper-level fixture represents scheduling delay between the
				// reservation and timer creation. The captured duration remains positive,
				// although the reserved slot has elapsed. Do not claim this reproduces
				// that scheduler interleaving through Wait itself.
				now := time.Now()
				delay := time.Minute
				mySlot := now.Add(-time.Second)
				limiter := NewLimiter(delay)
				limiter.nextAllowedTime = mySlot.Add(delay)
				if laterReservation {
					limiter.nextAllowedTime = limiter.nextAllowedTime.Add(delay)
				}
				before := limiter.nextAllowedTime
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				select {
				case <-ctx.Done():
				default:
					t.Fatal("real cancellation was not delivered")
				}

				require.ErrorIs(t, limiter.waitForReservedSlot(ctx, mySlot, delay), context.Canceled)
				if laterReservation {
					require.Equal(t, before, limiterSchedule(limiter), "canceling an earlier waiter must not reclaim a later reservation")
				} else {
					require.Equal(t, now, limiterSchedule(limiter), "an expired tail must be reclaimed at now, not in the past or one delay ahead")
					require.NoError(t, limiter.Wait(context.Background()), "the reclaimed slot must be usable by a healthy caller")
					require.Equal(t, now.Add(delay), limiterSchedule(limiter))
				}
			})
		})
	}
}
