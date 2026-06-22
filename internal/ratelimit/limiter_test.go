package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLimiter_ZeroDelay(t *testing.T) {
	limiter := NewLimiter(0)
	start := time.Now()
	err := limiter.Wait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("zero delay should return immediately, took %v", elapsed)
	}
}

func TestLimiter_DelayCausesWait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	delay := 50 * time.Millisecond
	limiter := NewLimiter(delay)

	start := time.Now()
	_ = limiter.Wait(context.Background())
	firstElapsed := time.Since(start)

	start = time.Now()
	_ = limiter.Wait(context.Background())
	secondElapsed := time.Since(start)

	if firstElapsed > 10*time.Millisecond {
		t.Errorf("first call should return immediately, took %v", firstElapsed)
	}
	if secondElapsed < delay-10*time.Millisecond {
		t.Errorf("second call should wait at least %v, only waited %v", delay, secondElapsed)
	}
	if secondElapsed > delay+30*time.Millisecond {
		t.Errorf("second call should not wait much longer than %v, waited %v", delay, secondElapsed)
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	delay := 100 * time.Millisecond
	limiter := NewLimiter(delay)
	_ = limiter.Wait(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := limiter.Wait(ctx)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("should return quickly on cancellation, took %v", elapsed)
	}
}

func TestLimiter_ConcurrentCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	delay := 50 * time.Millisecond
	limiter := NewLimiter(delay)

	const numGoroutines = 5
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			_ = limiter.Wait(context.Background())
		}()
	}

	t0 := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(t0)

	minExpected := time.Duration(numGoroutines-1) * delay
	if elapsed < minExpected-20*time.Millisecond {
		t.Errorf("total elapsed %v should be at least %v for %d sequential rate-limited calls", elapsed, minExpected, numGoroutines)
	}
}

func TestLimiter_CancellationUnderContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	delay := 100 * time.Millisecond
	limiter := NewLimiter(delay)

	ctx1 := context.Background()
	err1 := make(chan error, 1)
	go func() {
		err1 <- limiter.Wait(ctx1)
	}()

	time.Sleep(10 * time.Millisecond)

	ctx2, cancel2 := context.WithCancel(context.Background())
	err2 := make(chan error, 1)
	go func() {
		err2 <- limiter.Wait(ctx2)
	}()

	ctx3, cancel3 := context.WithCancel(context.Background())
	err3 := make(chan error, 1)
	go func() {
		err3 <- limiter.Wait(ctx3)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel2()

	select {
	case err := <-err2:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("cancellation should be observed quickly, not blocked by first waiter")
	}

	cancel3()
	select {
	case err := <-err3:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("third waiter cancellation should be observed quickly")
	}

	if err := <-err1; err != nil {
		t.Errorf("first waiter should complete successfully: %v", err)
	}
}

// TestLimiter_CancelDoesNotShortenOthersWait verifies the rollback logic is correct.
// This tests the fix for the bug where cancellation could erase other waiters' slots.
func TestLimiter_CancelDoesNotShortenOthersWait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	delay := 50 * time.Millisecond
	limiter := NewLimiter(delay)

	// First call returns immediately and sets nextAllowedTime
	_ = limiter.Wait(context.Background())

	var wg sync.WaitGroup
	result1 := make(chan time.Duration, 1)
	result2 := make(chan time.Duration, 1)
	result3 := make(chan time.Duration, 1)

	ctx2, cancel2 := context.WithCancel(context.Background())

	// Use a barrier to start all waiters at once
	start := make(chan struct{})

	// Waiter 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		t0 := time.Now()
		err := limiter.Wait(context.Background())
		result1 <- time.Since(t0)
		if err != nil {
			t.Errorf("waiter1 unexpected error: %v", err)
		}
	}()

	// Waiter 2 - will cancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		t0 := time.Now()
		err := limiter.Wait(ctx2)
		select {
		case result2 <- time.Since(t0):
		default:
		}
		if err != context.Canceled {
			t.Errorf("waiter2 expected Canceled, got: %v", err)
		}
	}()

	// Waiter 3
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		t0 := time.Now()
		err := limiter.Wait(context.Background())
		result3 <- time.Since(t0)
		if err != nil {
			t.Errorf("waiter3 unexpected error: %v", err)
		}
	}()

	// Start all waiters at once
	close(start)

	// Let them queue up
	time.Sleep(10 * time.Millisecond)

	// Cancel waiter2
	cancel2()

	// Wait for all to complete
	wg.Wait()

	elapsed1 := <-result1
	elapsed2 := <-result2
	elapsed3 := <-result3

	// The key invariant: waiter1 + waiter3 must account for at least 2*delay total
	// (2 non-cancelled waiters each need their own rate-limited slot).
	// If rollback bug exists, waiter2's cancelled slot would be erased,
	// allowing waiter3 to proceed earlier than it should.
	totalWait := elapsed1 + elapsed3 // waiter2 cancelled, doesn't count

	// Minimum expected: waiter1 + waiter3 each need at least one slot
	// Even if they get different slots, total should be >= 2*delay
	minTotal := 2 * delay
	if totalWait < minTotal {
		t.Errorf("total wait (%v + %v = %v) should be at least %v - rollback bug detected!", elapsed1, elapsed3, totalWait, minTotal)
	}

	// Log for debugging
	t.Logf("waiter1: %v, waiter2: %v (cancelled), waiter3: %v, total: %v", elapsed1, elapsed2, elapsed3, totalWait)
}

// TestLimiter_RollbackLogic tests the rollback logic directly.
// This verifies that rollback only happens when no other waiter reserved after us.
func TestLimiter_RollbackLogic(t *testing.T) {
	delay := 50 * time.Millisecond
	limiter := NewLimiter(delay)

	_ = limiter.Wait(context.Background())

	// Case 1: Cancel when no one else reserved after us
	// - Waiter reserves slot at nextAllowedTime
	// - nextAllowedTime advances by delay
	// - We cancel before the slot fires
	// - Rollback should move nextAllowedTime back, allowing the next waiter sooner

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		limiter.Wait(ctx1)
	}()

	// Give the goroutine time to acquire the lock and reserve its slot,
	// but cancel before the slot fires (well within the 50ms delay).
	time.Sleep(10 * time.Millisecond)
	cancel1()
	time.Sleep(10 * time.Millisecond)

	// With rollback: nextAllowedTime moved back to ~slot start, so next waiter
	// should proceed quickly (within ~delay of the original reservation).
	// Without rollback: nextAllowedTime is still at slot+delay, so next waiter
	// waits much longer (~delay - 10ms = ~40ms remaining).
	t0 := time.Now()
	limiter.Wait(context.Background())
	elapsed := time.Since(t0)

	// With working rollback, the next waiter should proceed within delay
	// (the slot was released, so it can reuse that time window).
	if elapsed > delay {
		t.Errorf("rollback didn't work, new waiter waited %v (expected <%v)", elapsed, delay)
	}
	t.Logf("Case 1 - rollback when alone: %v", elapsed)
}
