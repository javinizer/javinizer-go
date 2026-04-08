package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu              sync.Mutex
	lastRequestTime time.Time
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
	defer l.mu.Unlock()

	if !l.lastRequestTime.IsZero() {
		elapsed := time.Since(l.lastRequestTime)
		if elapsed < l.delay {
			select {
			case <-time.After(l.delay - elapsed):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	l.lastRequestTime = time.Now()
	return nil
}
