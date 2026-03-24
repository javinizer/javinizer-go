package realtime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWebSocketHub is a mock implementation of the WebSocket hub for testing
type mockWebSocketHub struct {
	messages []*websocket.ProgressMessage
	mu       sync.Mutex
	calls    int
}

func (m *mockWebSocketHub) BroadcastProgress(msg *websocket.ProgressMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.calls++
	return nil
}

func (m *mockWebSocketHub) GetMessages() []*websocket.ProgressMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to avoid race conditions
	messages := make([]*websocket.ProgressMessage, len(m.messages))
	copy(messages, m.messages)
	return messages
}

func (m *mockWebSocketHub) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockWebSocketHub) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
	m.calls = 0
}

// TestNewProgressAdapter verifies adapter initialization
func TestNewProgressAdapter(t *testing.T) {
	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4", "/path/to/file2.mp4"},
	}

	mockHub := &mockWebSocketHub{}
	adapter := NewProgressAdapter("test-job", job, mockHub)

	assert.NotNil(t, adapter)
	assert.Equal(t, "test-job", adapter.jobID)
	assert.Equal(t, job, adapter.job)
	assert.NotNil(t, adapter.broadcaster)
	assert.NotNil(t, adapter.updateChan)
	assert.NotNil(t, adapter.stopChan)
	assert.NotNil(t, adapter.taskToFileIndex)
	assert.Equal(t, 0, adapter.GetRegisteredTaskCount())
}

// TestRegisterTask verifies task registration and lookup
func TestRegisterTask(t *testing.T) {
	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4", "/path/to/file2.mp4"},
	}

	mockHub := &mockWebSocketHub{}
	adapter := NewProgressAdapter("test-job", job, mockHub)

	// Register tasks
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")
	adapter.RegisterTask("task-2", 1, "/path/to/file2.mp4")

	// Verify registration
	assert.Equal(t, 2, adapter.GetRegisteredTaskCount())

	// Verify internal mapping
	adapter.mu.RLock()
	assert.Equal(t, 0, adapter.taskToFileIndex["task-1"])
	assert.Equal(t, 1, adapter.taskToFileIndex["task-2"])
	adapter.mu.RUnlock()
}

// TestUnregisterTask verifies task unregistration
func TestUnregisterTask(t *testing.T) {
	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	mockHub := &mockWebSocketHub{}
	adapter := NewProgressAdapter("test-job", job, mockHub)

	// Register and unregister
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")
	assert.Equal(t, 1, adapter.GetRegisteredTaskCount())

	adapter.UnregisterTask("task-1")
	assert.Equal(t, 0, adapter.GetRegisteredTaskCount())

	// Verify internal mapping
	adapter.mu.RLock()
	_, exists := adapter.taskToFileIndex["task-1"]
	adapter.mu.RUnlock()
	assert.False(t, exists)
}

// TestUpdateConversion verifies correct conversion from ProgressUpdate to WebSocket message
func TestUpdateConversion(t *testing.T) {
	// Set up mock hub
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:         "test-job",
		Files:      []string{"/path/to/file1.mp4"},
		TotalFiles: 1,
		Completed:  0,
		Progress:   25.5,
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")

	// Start adapter
	adapter.Start()
	defer adapter.Stop()

	// Send update
	update := worker.ProgressUpdate{
		TaskID:   "task-1",
		Status:   worker.TaskStatusRunning,
		Progress: 0.5,
		Message:  "Processing file",
	}

	adapter.updateChan <- update

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Verify message was broadcast
	messages := mockHub.GetMessages()
	require.Len(t, messages, 1)

	msg := messages[0]
	assert.Equal(t, "test-job", msg.JobID)
	assert.Equal(t, 0, msg.FileIndex)
	assert.Equal(t, "/path/to/file1.mp4", msg.FilePath)
	assert.Equal(t, "running", msg.Status)
	assert.Equal(t, 25.5, msg.Progress)
	assert.Equal(t, "Processing file", msg.Message)
	assert.Empty(t, msg.Error)
}

// TestUpdateConversionWithError verifies error handling in messages
func TestUpdateConversionWithError(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:         "test-job",
		Files:      []string{"/path/to/file1.mp4"},
		TotalFiles: 1,
		Progress:   0,
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")

	adapter.Start()
	defer adapter.Stop()

	// Send update with error
	testError := errors.New("scraping failed")
	update := worker.ProgressUpdate{
		TaskID:  "task-1",
		Status:  worker.TaskStatusFailed,
		Message: "Failed to scrape",
		Error:   testError,
	}

	adapter.updateChan <- update
	time.Sleep(50 * time.Millisecond)

	messages := mockHub.GetMessages()
	require.Len(t, messages, 1)

	msg := messages[0]
	assert.Equal(t, "failed", msg.Status)
	assert.Equal(t, "scraping failed", msg.Error)
}

// TestUnknownTaskID verifies graceful handling of unknown task IDs
func TestUnknownTaskID(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	// Don't register any tasks

	adapter.Start()
	defer adapter.Stop()

	// Send update for unknown task
	update := worker.ProgressUpdate{
		TaskID:  "unknown-task",
		Status:  worker.TaskStatusRunning,
		Message: "This should be ignored",
	}

	adapter.updateChan <- update
	time.Sleep(50 * time.Millisecond)

	// Verify no message was broadcast
	messages := mockHub.GetMessages()
	assert.Len(t, messages, 0)
}

// TestInvalidFileIndex verifies handling of out-of-bounds file indices
func TestInvalidFileIndex(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)

	// Register task with invalid index
	adapter.RegisterTask("task-1", 999, "invalid-path")

	adapter.Start()
	defer adapter.Stop()

	// Send update
	update := worker.ProgressUpdate{
		TaskID:  "task-1",
		Status:  worker.TaskStatusRunning,
		Message: "Processing",
	}

	adapter.updateChan <- update
	time.Sleep(50 * time.Millisecond)

	// Verify no message was broadcast
	messages := mockHub.GetMessages()
	assert.Len(t, messages, 0)
}

// TestConcurrentTaskRegistration verifies thread safety of task registration
func TestConcurrentTaskRegistration(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: make([]string, 100),
	}

	for i := 0; i < 100; i++ {
		job.Files[i] = "/path/to/file.mp4"
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)

	// Register tasks concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			taskID := string(rune('a'+(index%26))) + string(rune('0'+(index/26)))
			adapter.RegisterTask(taskID, index, job.Files[index])
		}(i)
	}

	wg.Wait()

	// Verify all tasks were registered
	assert.Equal(t, 100, adapter.GetRegisteredTaskCount())
}

// TestConcurrentUpdates verifies thread safety during concurrent update processing
func TestConcurrentUpdates(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:         "test-job",
		Files:      make([]string, 50),
		TotalFiles: 50,
	}

	for i := 0; i < 50; i++ {
		job.Files[i] = "/path/to/file.mp4"
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)

	// Register tasks
	for i := 0; i < 50; i++ {
		taskID := string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		adapter.RegisterTask(taskID, i, job.Files[i])
	}

	adapter.Start()
	defer adapter.Stop()

	// Send updates concurrently
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			taskID := string(rune('a'+(index%26))) + string(rune('0'+(index/26)))
			update := worker.ProgressUpdate{
				TaskID:   taskID,
				Status:   worker.TaskStatusRunning,
				Progress: 0.5,
				Message:  "Processing",
			}
			adapter.updateChan <- update
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify all updates were broadcast
	assert.Equal(t, 50, mockHub.GetCallCount())
}

// TestGracefulShutdown verifies proper cleanup during Stop()
func TestGracefulShutdown(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")

	adapter.Start()

	// Send some updates
	for i := 0; i < 5; i++ {
		update := worker.ProgressUpdate{
			TaskID:  "task-1",
			Status:  worker.TaskStatusRunning,
			Message: "Update",
		}
		adapter.updateChan <- update
	}

	// Stop and verify graceful shutdown
	adapter.Stop()

	// Verify all updates were processed
	assert.Equal(t, 5, mockHub.GetCallCount())
}

// TestDrainRemainingUpdates verifies that pending updates are processed during shutdown
func TestDrainRemainingUpdates(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")

	adapter.Start()

	// Send updates without waiting
	for i := 0; i < 10; i++ {
		update := worker.ProgressUpdate{
			TaskID:  "task-1",
			Status:  worker.TaskStatusRunning,
			Message: "Update",
		}
		adapter.updateChan <- update
	}

	// Stop immediately (some updates may still be in channel)
	adapter.Stop()

	// Verify all updates were eventually processed
	// Note: Race detector will catch any issues here
	callCount := mockHub.GetCallCount()
	assert.Equal(t, 10, callCount, "All updates should be drained during shutdown")
}

// TestMultipleStatusTransitions verifies tracking through multiple status changes
func TestMultipleStatusTransitions(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:         "test-job",
		Files:      []string{"/path/to/file1.mp4"},
		TotalFiles: 1,
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.RegisterTask("task-1", 0, "/path/to/file1.mp4")

	adapter.Start()
	defer adapter.Stop()

	// Simulate task lifecycle
	statuses := []worker.TaskStatus{
		worker.TaskStatusPending,
		worker.TaskStatusRunning,
		worker.TaskStatusSuccess,
	}

	for _, status := range statuses {
		update := worker.ProgressUpdate{
			TaskID:  "task-1",
			Status:  status,
			Message: string(status),
		}
		adapter.updateChan <- update
		time.Sleep(20 * time.Millisecond)
	}

	// Verify all transitions were broadcast
	messages := mockHub.GetMessages()
	require.Len(t, messages, 3)

	assert.Equal(t, "pending", messages[0].Status)
	assert.Equal(t, "running", messages[1].Status)
	assert.Equal(t, "success", messages[2].Status)
}

// TestIdempotentStop verifies that Stop() can be called multiple times safely
func TestIdempotentStop(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	adapter.Start()

	// Call Stop() multiple times - should not panic
	adapter.Stop()
	adapter.Stop()
	adapter.Stop()

	// Verify no panic occurred
	assert.True(t, true, "Multiple Stop() calls should be safe")
}

// TestGetChannel verifies channel access
func TestGetChannel(t *testing.T) {
	mockHub := &mockWebSocketHub{}

	job := &worker.BatchJob{
		ID:    "test-job",
		Files: []string{"/path/to/file1.mp4"},
	}

	adapter := NewProgressAdapter("test-job", job, mockHub)
	ch := adapter.GetChannel()

	assert.NotNil(t, ch)

	// Verify we can send to the channel
	update := worker.ProgressUpdate{
		TaskID:  "task-1",
		Status:  worker.TaskStatusRunning,
		Message: "Test",
	}

	// Non-blocking send to verify channel works
	select {
	case ch <- update:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Failed to send to channel")
	}
}
