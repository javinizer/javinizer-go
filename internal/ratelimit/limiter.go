package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu              sync.Mutex
	nextAllowedTime time.Time
	delay           time.Duration
}

func NewLimiter(delay time.Duration) *Limiter {
	return &Limiter{delay: delay}
}

func (l *Limiter) Wait(ctx context.Context) error {
	if l.delay <= 0 {
		return nil
	}

	l.mu.Lock()

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
