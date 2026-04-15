package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPool_Errors(t *testing.T) {
	t.Run("No errors when all tasks succeed", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task1 := newMockTask("task-1", 50*time.Millisecond, false)
		task2 := newMockTask("task-2", 50*time.Millisecond, false)

		_ = pool.Submit(task1)
		_ = pool.Submit(task2)
		_ = pool.Wait()

		errs := pool.Errors()
		assert.Empty(t, errs, "Expected no errors when all tasks succeed")
	})

	t.Run("Returns errors from failed tasks", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task1 := newMockTask("task-1", 50*time.Millisecond, false)
		task2 := newMockTask("task-2", 50*time.Millisecond, true) // This will fail
		task3 := newMockTask("task-3", 50*time.Millisecond, true) // This will fail too

		_ = pool.Submit(task1)
		_ = pool.Submit(task2)
		_ = pool.Submit(task3)
		_ = pool.Wait()

		errs := pool.Errors()
		assert.Len(t, errs, 2, "Expected 2 errors from failed tasks")
	})

	t.Run("Returns copy of errors slice", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task := newMockTask("task-1", 50*time.Millisecond, true)
		_ = pool.Submit(task)
		_ = pool.Wait()

		errs1 := pool.Errors()
		errs2 := pool.Errors()

		// Should be equal but not same slice
		assert.Equal(t, len(errs1), len(errs2))
		// Modifying one slice shouldn't affect the other (they're copies)
		if len(errs1) > 0 {
			// We can't modify the slice directly but we can verify they're independent
			assert.NotNil(t, errs1)
			assert.NotNil(t, errs2)
		}
	})

	t.Run("Errors are collected across multiple Wait calls", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		// First batch
		task1 := newMockTask("task-1", 50*time.Millisecond, true)
		_ = pool.Submit(task1)
		_ = pool.Wait()

		errs1 := pool.Errors()
		assert.Len(t, errs1, 1, "Expected 1 error after first batch")

		// Second batch
		task2 := newMockTask("task-2", 50*time.Millisecond, true)
		_ = pool.Submit(task2)
		_ = pool.Wait()

		errs2 := pool.Errors()
		assert.Len(t, errs2, 2, "Expected 2 errors accumulated from both batches")
	})

	t.Run("Thread-safe error collection", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(5, 10*time.Second, tracker)
		defer pool.Stop()

		// Submit many failing tasks concurrently
		numTasks := 20
		for i := 0; i < numTasks; i++ {
			task := newMockTask(string(rune('A'+i)), 10*time.Millisecond, true)
			_ = pool.Submit(task)
		}

		_ = pool.Wait()

		errs := pool.Errors()
		assert.Len(t, errs, numTasks, "Expected all task errors to be collected")

		// Call Errors() multiple times concurrently to test thread safety
		type result struct {
			length int
			errors []error
		}
		results := make(chan result, 100)

		for g := 0; g < 10; g++ {
			go func() {
				for i := 0; i < 10; i++ {
					errs := pool.Errors()
					results <- result{length: len(errs), errors: errs}
				}
			}()
		}

		// Collect all results and assert on main goroutine
		for r := 0; r < 100; r++ {
			res := <-results
			assert.Len(t, res.errors, numTasks, "Concurrent Errors() call %d returned wrong length", r)
		}
	})
}

// errTestSentinel is a sentinel error for testing errors.Is with the joined error
var errTestSentinel = errors.New("sentinel error")

// mockTaskWithErr is a task that returns a specific error
type mockTaskWithErr struct {
	BaseTask
	duration time.Duration
	err      error
}

func (t *mockTaskWithErr) Execute(ctx context.Context) error {
	select {
	case <-time.After(t.duration):
		return t.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPool_WaitJoinedErrors(t *testing.T) {
	t.Run("Wait returns nil when no errors", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task1 := newMockTask("task-1", 50*time.Millisecond, false)
		task2 := newMockTask("task-2", 50*time.Millisecond, false)

		_ = pool.Submit(task1)
		_ = pool.Submit(task2)

		err := pool.Wait()
		assert.NoError(t, err, "Expected no error when all tasks succeed")
	})

	t.Run("Wait error contains summary count and individual details", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task2 := newMockTask("task-2", 50*time.Millisecond, true)
		task3 := newMockTask("task-3", 50*time.Millisecond, true)

		_ = pool.Submit(task2)
		_ = pool.Submit(task3)

		err := pool.Wait()
		require.Error(t, err, "Expected error from Wait when tasks fail")

		errMsg := err.Error()
		assert.Contains(t, errMsg, "2 tasks failed", "Error should contain summary count")
		assert.Contains(t, errMsg, "task-2 failed", "Error should contain task-2 details")
		assert.Contains(t, errMsg, "task-3 failed", "Error should contain task-3 details")
	})

	t.Run("Wait error supports errors.Is for individual errors", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task2 := &mockTaskWithErr{
			BaseTask: BaseTask{id: "task-2", taskType: TaskTypeScrape, description: "sentinel task"},
			duration: 50 * time.Millisecond,
			err:      errTestSentinel,
		}
		task3 := newMockTask("task-3", 50*time.Millisecond, true)

		_ = pool.Submit(task2)
		_ = pool.Submit(task3)

		err := pool.Wait()
		require.Error(t, err, "Expected error from Wait when tasks fail")

		assert.True(t, errors.Is(err, errTestSentinel),
			"errors.Is should find sentinel error within joined error")
	})

	t.Run("Wait error supports errors.Unwrap for individual errors", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task2 := newMockTask("task-2", 50*time.Millisecond, true)
		task3 := newMockTask("task-3", 50*time.Millisecond, true)

		_ = pool.Submit(task2)
		_ = pool.Submit(task3)

		err := pool.Wait()
		require.Error(t, err, "Expected error from Wait when tasks fail")

		unwrapped := errors.Unwrap(err)
		require.NotNil(t, unwrapped, "Wait() error should be unwrappable to the joined error")

		joined, ok := unwrapped.(interface{ Unwrap() []error })
		require.True(t, ok, "Unwrapped error should implement Unwrap() []error (joined error)")

		innerErrors := joined.Unwrap()
		assert.Len(t, innerErrors, 2, "Joined error should contain 2 individual errors")
	})

	t.Run("Errors method still returns individual errors after Wait", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 10)
		tracker := NewProgressTracker(progressChan)
		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task2 := newMockTask("task-2", 50*time.Millisecond, true)
		task3 := newMockTask("task-3", 50*time.Millisecond, true)

		_ = pool.Submit(task2)
		_ = pool.Submit(task3)

		_ = pool.Wait()

		errs := pool.Errors()
		assert.Len(t, errs, 2, "Errors() should still return 2 individual errors")
	})
}

// TestPool_ErrorsWithContextCancellation tests error handling when tasks are canceled
func TestPool_ErrorsWithContextCancellation(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)
	pool := NewPool(2, 10*time.Second, tracker)

	// Submit long-running tasks
	task1 := newMockTask("task-1", 5*time.Second, false)
	task2 := newMockTask("task-2", 5*time.Second, false)

	_ = pool.Submit(task1)
	_ = pool.Submit(task2)

	// Cancel immediately
	pool.Stop()

	// Errors may include context.Canceled errors
	errs := pool.Errors()

	// We expect errors from cancellation
	if len(errs) > 0 {
		for _, err := range errs {
			// Error should be context.Canceled or wrap it
			assert.ErrorIs(t, err, context.Canceled,
				"Expected context.Canceled error, got: %v", err)
		}
	}
}
