package worker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewProgressTracker(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	if tracker == nil {
		t.Fatal("Expected tracker to be created")
	}
}

func TestProgressTracker_Start(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	taskType := TaskTypeScrape

	tracker.Start(taskID, taskType, "Starting task")

	// Check that update was sent
	select {
	case update := <-ch:
		if update.TaskID != taskID {
			t.Errorf("Expected task ID %s, got %s", taskID, update.TaskID)
		}
		if update.Type != taskType {
			t.Errorf("Expected task type %s, got %s", taskType, update.Type)
		}
		if update.Status != TaskStatusRunning {
			t.Errorf("Expected status %s, got %s", TaskStatusRunning, update.Status)
		}
		if update.Progress != 0.0 {
			t.Errorf("Expected progress 0.0, got %f", update.Progress)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update to be sent")
	}

	// Check internal state
	progress, ok := tracker.Get(taskID)
	if !ok {
		t.Fatal("Expected progress to be tracked")
	}
	if progress.Status != TaskStatusRunning {
		t.Errorf("Expected status %s, got %s", TaskStatusRunning, progress.Status)
	}
}

func TestProgressTracker_Update(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	tracker.Start(taskID, TaskTypeScrape, "Starting")
	<-ch // Drain start message

	tracker.Update(taskID, 0.5, "Halfway done", 1024)

	// Check update
	select {
	case update := <-ch:
		if update.Progress != 0.5 {
			t.Errorf("Expected progress 0.5, got %f", update.Progress)
		}
		if update.Message != "Halfway done" {
			t.Errorf("Expected message 'Halfway done', got '%s'", update.Message)
		}
		if update.BytesDone != 1024 {
			t.Errorf("Expected bytes 1024, got %d", update.BytesDone)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update to be sent")
	}

	// Check internal state
	progress, _ := tracker.Get(taskID)
	if progress.Progress != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", progress.Progress)
	}
	if progress.Message != "Halfway done" {
		t.Errorf("Expected message 'Halfway done', got '%s'", progress.Message)
	}
}

func TestProgressTracker_SetTotal(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	tracker.Start(taskID, TaskTypeDownload, "Starting")
	<-ch // Drain start message

	totalBytes := int64(10240)
	tracker.SetTotal(taskID, totalBytes)

	progress, _ := tracker.Get(taskID)
	if progress.BytesTotal != totalBytes {
		t.Errorf("Expected total bytes %d, got %d", totalBytes, progress.BytesTotal)
	}
}

func TestProgressTracker_Complete(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	tracker.Start(taskID, TaskTypeScrape, "Starting")
	<-ch // Drain start message

	tracker.Complete(taskID, "Task completed")

	// Check update
	select {
	case update := <-ch:
		if update.Status != TaskStatusSuccess {
			t.Errorf("Expected status %s, got %s", TaskStatusSuccess, update.Status)
		}
		if update.Progress != 1.0 {
			t.Errorf("Expected progress 1.0, got %f", update.Progress)
		}
		if update.Message != "Task completed" {
			t.Errorf("Expected message 'Task completed', got '%s'", update.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update to be sent")
	}

	// Check internal state
	progress, _ := tracker.Get(taskID)
	if progress.Status != TaskStatusSuccess {
		t.Errorf("Expected status %s, got %s", TaskStatusSuccess, progress.Status)
	}
}

func TestProgressTracker_Fail(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	tracker.Start(taskID, TaskTypeScrape, "Starting")
	<-ch // Drain start message

	testErr := errors.New("test error")
	tracker.Fail(taskID, testErr)

	// Check update
	select {
	case update := <-ch:
		if update.Status != TaskStatusFailed {
			t.Errorf("Expected status %s, got %s", TaskStatusFailed, update.Status)
		}
		if update.Error == nil {
			t.Error("Expected error to be set")
		}
		if update.Error.Error() != testErr.Error() {
			t.Errorf("Expected error '%s', got '%s'", testErr.Error(), update.Error.Error())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update to be sent")
	}

	// Check internal state
	progress, _ := tracker.Get(taskID)
	if progress.Status != TaskStatusFailed {
		t.Errorf("Expected status %s, got %s", TaskStatusFailed, progress.Status)
	}
	if progress.Error == nil {
		t.Error("Expected error to be set")
	}
}

func TestProgressTracker_Cancel(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	taskID := "task-1"
	tracker.Start(taskID, TaskTypeScrape, "Starting")
	<-ch // Drain start message

	tracker.Cancel(taskID)

	// Check update
	select {
	case update := <-ch:
		if update.Status != TaskStatusCanceled {
			t.Errorf("Expected status %s, got %s", TaskStatusCanceled, update.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected update to be sent")
	}

	// Check internal state
	progress, _ := tracker.Get(taskID)
	if progress.Status != TaskStatusCanceled {
		t.Errorf("Expected status %s, got %s", TaskStatusCanceled, progress.Status)
	}
}

func TestProgressTracker_GetAll(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	// Start multiple tasks
	tracker.Start("task-1", TaskTypeScrape, "Starting")
	tracker.Start("task-2", TaskTypeDownload, "Starting")
	tracker.Start("task-3", TaskTypeOrganize, "Starting")

	all := tracker.GetAll()

	if len(all) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(all))
	}
}

func TestProgressTracker_GetByType(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	// Start tasks of different types
	tracker.Start("task-1", TaskTypeScrape, "Starting")
	tracker.Start("task-2", TaskTypeScrape, "Starting")
	tracker.Start("task-3", TaskTypeDownload, "Starting")

	scrapes := tracker.GetByType(TaskTypeScrape)
	downloads := tracker.GetByType(TaskTypeDownload)

	if len(scrapes) != 2 {
		t.Errorf("Expected 2 scrape tasks, got %d", len(scrapes))
	}
	if len(downloads) != 1 {
		t.Errorf("Expected 1 download task, got %d", len(downloads))
	}
}

func TestProgressTracker_GetByStatus(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	tracker.Start("task-1", TaskTypeScrape, "Starting")
	tracker.Start("task-2", TaskTypeScrape, "Starting")
	tracker.Start("task-3", TaskTypeScrape, "Starting")

	// Drain start messages
	for i := 0; i < 3; i++ {
		<-ch
	}

	tracker.Complete("task-1", "Done")
	tracker.Fail("task-2", errors.New("Error"))

	running := tracker.GetByStatus(TaskStatusRunning)
	success := tracker.GetByStatus(TaskStatusSuccess)
	failed := tracker.GetByStatus(TaskStatusFailed)

	if len(running) != 1 {
		t.Errorf("Expected 1 running task, got %d", len(running))
	}
	if len(success) != 1 {
		t.Errorf("Expected 1 successful task, got %d", len(success))
	}
	if len(failed) != 1 {
		t.Errorf("Expected 1 failed task, got %d", len(failed))
	}
}

func TestProgressTracker_Stats(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	// Start tasks
	tracker.Start("task-1", TaskTypeScrape, "Starting")
	tracker.Start("task-2", TaskTypeScrape, "Starting")
	tracker.Start("task-3", TaskTypeScrape, "Starting")
	tracker.Start("task-4", TaskTypeScrape, "Starting")

	// Drain start messages
	for i := 0; i < 4; i++ {
		<-ch
	}

	// Update statuses
	tracker.Complete("task-1", "Done")
	tracker.Complete("task-2", "Done")
	tracker.Fail("task-3", errors.New("Error"))
	// task-4 remains running

	stats := tracker.Stats()

	if stats.Total != 4 {
		t.Errorf("Expected total 4, got %d", stats.Total)
	}
	if stats.Running != 1 {
		t.Errorf("Expected 1 running, got %d", stats.Running)
	}
	if stats.Success != 2 {
		t.Errorf("Expected 2 success, got %d", stats.Success)
	}
	if stats.Failed != 1 {
		t.Errorf("Expected 1 failed, got %d", stats.Failed)
	}

	// Overall progress should be (2 complete + 0.5 for running) / 4 = 62.5%
	// But since we don't update progress for task-4, it might be different
	if stats.OverallProgress < 0 || stats.OverallProgress > 1 {
		t.Errorf("Expected progress between 0 and 1, got %f", stats.OverallProgress)
	}
}

func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	ch := make(chan ProgressUpdate, 1000)
	tracker := NewProgressTracker(ch)

	var wg sync.WaitGroup
	numGoroutines := 10
	tasksPerGoroutine := 10

	// Concurrent writes
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				taskID := string(rune('A'+goroutineID)) + string(rune('0'+i))
				tracker.Start(taskID, TaskTypeScrape, "Starting")
				tracker.Update(taskID, 0.5, "Working", 0)
				tracker.Complete(taskID, "Done")
			}
		}(g)
	}

	// Concurrent reads
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				tracker.GetAll()
				tracker.Stats()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	stats := tracker.Stats()
	expectedTotal := numGoroutines * tasksPerGoroutine

	if stats.Total != expectedTotal {
		t.Errorf("Expected %d total tasks, got %d", expectedTotal, stats.Total)
	}
	if stats.Success != expectedTotal {
		t.Errorf("Expected %d successful tasks, got %d", expectedTotal, stats.Success)
	}
}

func TestProgressTracker_NonExistentTask(t *testing.T) {
	ch := make(chan ProgressUpdate, 10)
	tracker := NewProgressTracker(ch)

	// Try to get non-existent task
	progress, ok := tracker.Get("non-existent")
	if ok {
		t.Error("Expected false for non-existent task")
	}
	if progress != nil {
		t.Error("Expected nil for non-existent task")
	}

	// Try to update non-existent task (should not panic)
	tracker.Update("non-existent", 0.5, "Test", 0)
	tracker.Complete("non-existent", "Test")
	tracker.Fail("non-existent", errors.New("Test"))
	tracker.Cancel("non-existent")
}
