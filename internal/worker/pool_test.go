package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTask is a simple task for testing
type mockTask struct {
	BaseTask
	duration   time.Duration
	shouldFail bool
	executed   *atomic.Int32
}

func newMockTask(id string, duration time.Duration, shouldFail bool) *mockTask {
	return &mockTask{
		BaseTask: BaseTask{
			id:          id,
			taskType:    TaskTypeScrape,
			description: "Mock task",
		},
		duration:   duration,
		shouldFail: shouldFail,
		executed:   &atomic.Int32{},
	}
}

func (t *mockTask) Execute(ctx context.Context) error {
	t.executed.Add(1)

	select {
	case <-time.After(t.duration):
		if t.shouldFail {
			return errors.New("task failed")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestNewPool(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(5, 10*time.Second, tracker)

	if pool == nil {
		t.Fatal("Expected pool to be created")
	}

	pool.Stop()
}

func TestPool_Submit(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(2, 10*time.Second, tracker)
	defer pool.Stop()

	task := newMockTask("task-1", 50*time.Millisecond, false)

	err := pool.Submit(task)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	err = pool.Wait()
	if err != nil {
		t.Fatalf("Expected no error from Wait, got %v", err)
	}

	if task.executed.Load() != 1 {
		t.Errorf("Expected task to be executed once, got %d", task.executed.Load())
	}
}

func TestPool_ConcurrentExecution(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	maxWorkers := 3
	pool := NewPool(maxWorkers, 10*time.Second, tracker)
	defer pool.Stop()

	numTasks := 10
	tasks := make([]*mockTask, numTasks)

	for i := 0; i < numTasks; i++ {
		tasks[i] = newMockTask(string(rune('A'+i)), 100*time.Millisecond, false)
		err := pool.Submit(tasks[i])
		if err != nil {
			t.Fatalf("Failed to submit task %d: %v", i, err)
		}
	}

	err := pool.Wait()
	if err != nil {
		t.Fatalf("Expected no error from Wait, got %v", err)
	}

	// Verify all tasks executed
	for i, task := range tasks {
		if task.executed.Load() != 1 {
			t.Errorf("Task %d: expected execution count 1, got %d", i, task.executed.Load())
		}
	}
}

func TestPool_FailedTasks(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(2, 10*time.Second, tracker)
	defer pool.Stop()

	task1 := newMockTask("task-1", 50*time.Millisecond, false)
	task2 := newMockTask("task-2", 50*time.Millisecond, true)
	task3 := newMockTask("task-3", 50*time.Millisecond, false)

	_ = pool.Submit(task1)
	_ = pool.Submit(task2)
	_ = pool.Submit(task3)

	err := pool.Wait()
	if err == nil {
		t.Fatal("Expected error from Wait due to failed task")
	}

	// All tasks should have been attempted
	if task1.executed.Load() != 1 {
		t.Errorf("Task 1: expected execution count 1, got %d", task1.executed.Load())
	}
	if task2.executed.Load() != 1 {
		t.Errorf("Task 2: expected execution count 1, got %d", task2.executed.Load())
	}
	if task3.executed.Load() != 1 {
		t.Errorf("Task 3: expected execution count 1, got %d", task3.executed.Load())
	}
}

func TestPool_Stop(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(2, 10*time.Second, tracker)

	// Submit long-running tasks
	task1 := newMockTask("task-1", 5*time.Second, false)
	task2 := newMockTask("task-2", 5*time.Second, false)

	_ = pool.Submit(task1)
	_ = pool.Submit(task2)

	// Stop immediately
	pool.Stop()

	// Tasks should be canceled
	time.Sleep(100 * time.Millisecond)

	// Note: We can't guarantee tasks won't execute at all,
	// but they should be canceled quickly
}

func TestPool_Timeout(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	// Short timeout
	pool := NewPool(2, 100*time.Millisecond, tracker)
	defer pool.Stop()

	// Task that takes longer than timeout
	task := newMockTask("task-1", 5*time.Second, false)

	err := pool.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	err = pool.Wait()
	if err == nil {
		t.Fatal("Expected timeout error")
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(2, 10*time.Second, tracker)

	// Submit tasks
	task1 := newMockTask("task-1", 2*time.Second, false)
	task2 := newMockTask("task-2", 2*time.Second, false)

	_ = pool.Submit(task1)
	_ = pool.Submit(task2)

	// Cancel via Stop
	pool.Stop()

	// Wait should return quickly due to cancellation
	err := pool.Wait()
	if err == nil {
		t.Log("Wait completed without error (context may have been canceled cleanly)")
	}
}

func TestPool_Stats(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(2, 10*time.Second, tracker)
	defer pool.Stop()

	// Submit some tasks
	for i := 0; i < 5; i++ {
		task := newMockTask(string(rune('A'+i)), 50*time.Millisecond, false)
		tracker.Start(task.ID(), task.Type(), "Starting")
		_ = pool.Submit(task)
	}

	// Wait a bit for tasks to start
	time.Sleep(10 * time.Millisecond)

	stats := pool.Stats()

	if stats.TotalTasks == 0 {
		t.Error("Expected total tasks > 0")
	}

	_ = pool.Wait()

	finalStats := pool.Stats()
	if finalStats.Success == 0 {
		t.Error("Expected some successful tasks")
	}
}

func TestPool_RaceConditions(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	pool := NewPool(5, 10*time.Second, tracker)
	defer pool.Stop()

	// Submit tasks concurrently from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	tasksPerGoroutine := 10

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				taskID := string(rune('A'+goroutineID)) + string(rune('0'+i))
				task := newMockTask(taskID, 10*time.Millisecond, false)
				_ = pool.Submit(task)
			}
		}(g)
	}

	wg.Wait()
	_ = pool.Wait()

	stats := pool.Stats()
	expectedTotal := numGoroutines * tasksPerGoroutine

	if stats.TotalTasks != expectedTotal {
		t.Errorf("Expected %d total tasks, got %d", expectedTotal, stats.TotalTasks)
	}
}

// panicTask is a task that panics when executed
type panicTask struct {
	BaseTask
	panicValue interface{}
	executed   *atomic.Int32
}

func newPanicTask(id string, panicValue interface{}) *panicTask {
	return &panicTask{
		BaseTask: BaseTask{
			id:          id,
			taskType:    TaskTypeScrape,
			description: "Panic task",
		},
		panicValue: panicValue,
		executed:   &atomic.Int32{},
	}
}

func (t *panicTask) Execute(ctx context.Context) error {
	t.executed.Add(1)
	panic(t.panicValue)
}

func TestPool_PanicRecovery(t *testing.T) {
	testCases := []struct {
		name        string
		panicValue  interface{}
		wantMessage string
	}{
		{"string panic", "something went wrong", "something went wrong"},
		{"error panic", errors.New("internal error"), "internal error"},
		{"int panic", 42, "42"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			progressChan := make(chan ProgressUpdate, 100)
			tracker := NewProgressTracker(progressChan)

			pool := NewPool(2, 10*time.Second, tracker)
			defer pool.Stop()

			task := newPanicTask("panic-task-1", tc.panicValue)
			if err := pool.Submit(task); err != nil {
				t.Fatalf("Failed to submit task: %v", err)
			}

			err := pool.Wait()
			if err == nil {
				t.Fatal("Expected error from Wait due to panicked task")
			}

			if !strings.Contains(err.Error(), "panicked") {
				t.Errorf("Expected error to contain 'panicked', got: %v", err)
			}

			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Errorf("Expected error to contain %q, got: %v", tc.wantMessage, err)
			}
		})
	}

	t.Run("pool continues after panic", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		panicT := newPanicTask("panic-task", "boom")
		normalT := newMockTask("normal-task", 10*time.Millisecond, false)

		if err := pool.Submit(panicT); err != nil {
			t.Fatalf("Failed to submit panic task: %v", err)
		}
		if err := pool.Submit(normalT); err != nil {
			t.Fatalf("Failed to submit normal task: %v", err)
		}

		err := pool.Wait()
		if err == nil {
			t.Fatal("Expected error from Wait due to panicked task")
		}

		if normalT.executed.Load() != 1 {
			t.Errorf("Normal task should have been executed, got %d executions", normalT.executed.Load())
		}
	})

	t.Run("progress tracker receives fail on panic", func(t *testing.T) {
		progressChan := make(chan ProgressUpdate, 100)
		tracker := NewProgressTracker(progressChan)

		pool := NewPool(2, 10*time.Second, tracker)
		defer pool.Stop()

		task := newPanicTask("panic-task-progress", "progress-test")
		if err := pool.Submit(task); err != nil {
			t.Fatalf("Failed to submit task: %v", err)
		}

		if err := pool.Wait(); err == nil {
			t.Fatal("Expected error from Wait")
		}

		progress, ok := tracker.Get("panic-task-progress")
		if !ok {
			t.Fatal("Expected progress entry for panicked task")
		}

		if progress.Status != TaskStatusFailed {
			t.Errorf("Expected task status to be failed, got %s", progress.Status)
		}

		if progress.Error == nil {
			t.Fatal("Expected progress error to be set")
		}

		if !strings.Contains(progress.Error.Error(), "panic") {
			t.Errorf("Expected progress error to contain 'panic', got: %v", progress.Error)
		}
	})
}

func TestPool_ActiveWorkers(t *testing.T) {
	progressChan := make(chan ProgressUpdate, 100)
	tracker := NewProgressTracker(progressChan)

	maxWorkers := 3
	pool := NewPool(maxWorkers, 10*time.Second, tracker)
	defer pool.Stop()

	// Submit tasks that will block
	for i := 0; i < 5; i++ {
		task := newMockTask(string(rune('A'+i)), 1*time.Second, false)
		tracker.Start(task.ID(), task.Type(), "Starting")
		_ = pool.Submit(task)
	}

	// Give tasks time to start
	time.Sleep(100 * time.Millisecond)

	activeWorkers := pool.ActiveWorkers()

	// Should have some workers active (but not necessarily max due to timing)
	if activeWorkers < 0 {
		t.Errorf("Expected non-negative active workers, got %d", activeWorkers)
	}

	// Note: We can't guarantee exactly maxWorkers due to timing,
	// but we can verify the method returns a reasonable value
	if activeWorkers > maxWorkers {
		t.Errorf("Active workers (%d) exceeds max workers (%d)", activeWorkers, maxWorkers)
	}
}
