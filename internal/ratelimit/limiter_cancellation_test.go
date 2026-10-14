package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiter_CancelledBeforeReservationLeavesScheduleUnchanged(t *testing.T) {
	for _, state := range []string{"unused", "available", "queued"} {
		for _, expired := range []bool{false, true} {
			name := state + "/cancelled"
			if expired {
				name = state + "/expired"
			}
			t.Run(name, func(t *testing.T) {
				limiter := NewLimiter(time.Hour)
				switch state {
				case "available":
					limiter.nextAllowedTime = time.Now().Add(-time.Hour)
				case "queued":
					limiter.nextAllowedTime = time.Now().Add(time.Hour)
				}
				before := limiter.nextAllowedTime
				ctx, cancel := context.WithCancel(context.Background())
				wantErr := context.Canceled
				if expired {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
					wantErr = context.DeadlineExceeded
				}
				cancel()

				require.ErrorIs(t, limiter.Wait(ctx), wantErr)
				require.Equal(t, before, limiter.nextAllowedTime, "a rejected call must not reserve or rewrite a slot")
				if state != "queued" {
					require.NoError(t, limiter.Wait(context.Background()), "the next valid caller still has an immediate slot")
				}
			})
		}
	}
}

func TestLimiter_CancellationBeforeLockAcquisitionLeavesScheduleUnchanged(t *testing.T) {
	limiter := NewLimiter(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	result := make(chan error, 1)
	limiter.mu.Lock()
	go func() {
		close(started)
		result <- limiter.Wait(ctx)
	}()
	<-started
	cancel()
	limiter.mu.Unlock()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled waiter did not return after acquiring the lock")
	}
	require.True(t, limiter.nextAllowedTime.IsZero())
	require.NoError(t, limiter.Wait(context.Background()))
}

type limiterObservedWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *limiterObservedWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func startObservedLimiterWait(t *testing.T, limiter *Limiter) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	observed := &limiterObservedWaitContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result <- limiter.Wait(observed)
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	select {
	case <-observed.waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not reserve a pending slot")
	}
	return cancel, result
}

func limiterSchedule(limiter *Limiter) time.Time {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.nextAllowedTime
}

func TestLimiter_CancelledPendingTailReleasesOnlyItsReservation(t *testing.T) {
	limiter := NewLimiter(time.Hour)
	require.NoError(t, limiter.Wait(context.Background()))
	before := limiterSchedule(limiter)
	cancel, result := startObservedLimiterWait(t, limiter)
	require.Equal(t, before.Add(time.Hour), limiterSchedule(limiter))

	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.Equal(t, before, limiterSchedule(limiter))
}

func TestLimiter_CancelledPendingWaitPreservesLaterReservations(t *testing.T) {
	limiter := NewLimiter(time.Hour)
	require.NoError(t, limiter.Wait(context.Background()))
	before := limiterSchedule(limiter)
	cancelFirst, first := startObservedLimiterWait(t, limiter)
	cancelSecond, second := startObservedLimiterWait(t, limiter)
	require.Equal(t, before.Add(2*time.Hour), limiterSchedule(limiter))

	cancelFirst()
	require.ErrorIs(t, <-first, context.Canceled)
	require.Equal(t, before.Add(2*time.Hour), limiterSchedule(limiter), "an earlier cancellation must not move the later caller's slot")
	cancelSecond()
	require.ErrorIs(t, <-second, context.Canceled)
	require.Equal(t, before.Add(time.Hour), limiterSchedule(limiter), "only the cancelled tail is reclaimed; earlier holes are not shifted")
}

func TestLimiter_CancellationAfterSuccessfulGrantKeepsSlot(t *testing.T) {
	limiter := NewLimiter(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, limiter.Wait(ctx))
	granted := limiterSchedule(limiter)
	cancel()

	// Once Wait has granted a slot, later cancellation must not make another
	// caller bypass the minimum delay. Only that caller's pending tail is freed.
	cancelNext, next := startObservedLimiterWait(t, limiter)
	require.Equal(t, granted.Add(time.Hour), limiterSchedule(limiter))
	cancelNext()
	require.ErrorIs(t, <-next, context.Canceled)
	require.Equal(t, granted, limiterSchedule(limiter))
}
