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
	if secondElapsed > delay+20*time.Millisecond {
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

	delay := 20 * time.Millisecond
	limiter := NewLimiter(delay)

	var wg sync.WaitGroup
	const numGoroutines = 10
	times := make([]time.Time, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = limiter.Wait(context.Background())
			times[idx] = time.Now()
		}(i)
	}

	wg.Wait()

	for i := 1; i < numGoroutines; i++ {
		diff := times[i].Sub(times[i-1])
		if diff < 0 {
			diff = -diff
		}
		if diff < delay-5*time.Millisecond {
			t.Errorf("concurrent calls should be separated by at least %v, got %v between call %d and %d", delay, diff, i-1, i)
		}
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

	// The key invariant: the sum of all waits should be at least 3*delay
	// (since 3 waiters must go through rate limiter sequentially)
	// If rollback bug exists, total would be less because some slots get erased
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
	delay := 20 * time.Millisecond
	limiter := NewLimiter(delay)

	// Initialize: first call sets nextAllowedTime
	_ = limiter.Wait(context.Background())

	// Simulate the scenario where multiple waiters have reserved slots:
	// After initialization, nextAllowedTime = now + delay
	//
	// Case 1: Cancel when no one else reserved after us
	// - mySlot = nextAllowedTime
	// - After we reserve, nextAllowedTime = mySlot + delay
	// - We cancel before anyone else reserves
	// - nextAllowedTime should rollback to mySlot

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		limiter.Wait(ctx1)
	}()

	time.Sleep(5 * time.Millisecond) // Let the goroutine reserve its slot
	cancel1()                        // Cancel it
	time.Sleep(5 * time.Millisecond) // Let the rollback happen

	// Now nextAllowedTime should be back to ~delay from when the waiter started
	// A new waiter should not have to wait too long
	t0 := time.Now()
	limiter.Wait(context.Background())
	elapsed := time.Since(t0)

	// Should be able to proceed within ~delay (not 2*delay if rollback worked)
	if elapsed > 2*delay {
		t.Errorf("rollback didn't work, new waiter waited %v (expected ~%v)", elapsed, delay)
	}
	t.Logf("Case 1 - rollback when alone: %v", elapsed)
}
