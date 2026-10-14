package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter enforces a minimum delay between successive Wait calls using a sliding-slot queue.
type Limiter struct {
	mu              sync.Mutex
	nextAllowedTime time.Time
	delay           time.Duration
}

// NewLimiter returns a Limiter that enforces the given delay between calls.
func NewLimiter(delay time.Duration) *Limiter {
	return &Limiter{delay: delay}
}

// Wait blocks until the next allowed slot, respecting ctx cancellation.
// Cancellation observed before reservation leaves the schedule unchanged. A
// cancelled pending waiter releases only a tail reservation, preserving later
// callers' slots. Cancellation racing a successful grant may consume that slot.
func (l *Limiter) Wait(ctx context.Context) error {
	if l.delay <= 0 {
		return nil
	}

	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		l.mu.Unlock()
		return err
	}

	now := time.Now()
	if l.nextAllowedTime.IsZero() || now.After(l.nextAllowedTime) {
		l.nextAllowedTime = now.Add(l.delay)
		l.mu.Unlock()
		return nil
	}

	waitDuration := l.nextAllowedTime.Sub(now)
	mySlot := l.nextAllowedTime // Save the slot we reserved (our wake-up time)
	l.nextAllowedTime = l.nextAllowedTime.Add(l.delay)
	l.mu.Unlock()

	select {
	case <-time.After(waitDuration):
		return nil
	case <-ctx.Done():
		l.mu.Lock()
		// Only rollback if nextAllowedTime hasn't been advanced past our slot's end.
		// This prevents erasing slots reserved by other waiters who came after us.
		if !l.nextAllowedTime.After(mySlot.Add(l.delay)) {
			newTime := l.nextAllowedTime.Add(-l.delay)
			// Clamp to not go before now - we can't give away slots in the past
			if now := time.Now(); newTime.Before(now) {
				newTime = now
			}
			l.nextAllowedTime = newTime
		}
		l.mu.Unlock()
		return ctx.Err()
	}
}
