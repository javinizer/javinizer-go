package realtime

import (
	"sync"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// ProgressBroadcaster is an interface for broadcasting progress messages.
// This interface allows for dependency injection and testing with mocks.
type ProgressBroadcaster interface {
	BroadcastProgress(msg *websocket.ProgressMessage) error
}

// ProgressAdapter bridges worker.ProgressTracker updates to WebSocket broadcasts
// for API batch jobs. It provides thread-safe mapping between task IDs and
// file paths for real-time progress tracking.
//
// Usage Example:
//
//	// Create adapter for a batch job
//	adapter := NewProgressAdapter(job.ID, job, nil) // nil uses global wsHub
//
//	// Create progress tracker with adapter's channel
//	progressTracker := worker.NewProgressTracker(adapter.GetChannel())
//
//	// Start adapter (non-blocking)
//	adapter.Start()
//	defer adapter.Stop() // Safe to call multiple times
//
//	// Register each task before submitting to worker pool
//	for i, filePath := range job.Files {
//	    taskID := fmt.Sprintf("batch-scrape-%s-%d", job.ID, i)
//	    adapter.RegisterTask(taskID, i, filePath)
//
//	    task := NewBatchScrapeTask(taskID, filePath, progressTracker, ...)
//	    pool.Submit(task)
//	}
//
//	// Progress updates are automatically broadcast to WebSocket clients
type ProgressAdapter struct {
	jobID           string
	job             *worker.BatchJob
	broadcaster     ProgressBroadcaster
	updateChan      chan worker.ProgressUpdate
	stopChan        chan struct{}
	stopOnce        sync.Once      // Ensure Stop() is idempotent
	taskToFileIndex map[string]int // taskID -> file index
	mu              sync.RWMutex
	wg              sync.WaitGroup // Track goroutine lifecycle
}

// NewProgressAdapter creates a new progress adapter for a batch job.
// The adapter listens to progress updates and broadcasts them via WebSocket.
// If broadcaster is nil, the default runtime hub is used when available.
func NewProgressAdapter(jobID string, job *worker.BatchJob, broadcaster ProgressBroadcaster) *ProgressAdapter {
	if broadcaster == nil {
		if runtime := core.DefaultRuntimeState(); runtime != nil {
			broadcaster = runtime.WebSocketHub()
		}
	}
	return &ProgressAdapter{
		jobID:           jobID,
		job:             job,
		broadcaster:     broadcaster,
		updateChan:      make(chan worker.ProgressUpdate, 100),
		stopChan:        make(chan struct{}),
		taskToFileIndex: make(map[string]int),
	}
}

// RegisterTask maps a task ID to its corresponding file index and path.
// This mapping is used to correlate progress updates with specific files.
// Thread-safe for concurrent registration during task submission.
func (a *ProgressAdapter) RegisterTask(taskID string, fileIndex int, filePath string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.taskToFileIndex[taskID] = fileIndex
	logging.Debugf("Registered task %s for file %s (index %d)", taskID, filePath, fileIndex)
}

// Start launches the adapter's update processing goroutine.
// It listens for progress updates and converts them to WebSocket messages.
// This method is non-blocking; use Stop() to shut down gracefully.
func (a *ProgressAdapter) Start() {
	a.wg.Add(1)
	go a.processUpdates()
	logging.Debugf("Progress adapter started for job %s", a.jobID)
}

// Stop signals the adapter to shut down and waits for the processing
// goroutine to finish. This ensures graceful cleanup of resources.
// Stop is idempotent and safe to call multiple times.
func (a *ProgressAdapter) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopChan)
		a.wg.Wait()
		logging.Debugf("Progress adapter stopped for job %s", a.jobID)
	})
}

// GetChannel returns a write-only channel for ProgressTracker to send updates.
// This channel should be passed to worker.NewProgressTracker().
func (a *ProgressAdapter) GetChannel() chan<- worker.ProgressUpdate {
	return a.updateChan
}

// processUpdates is the main event loop that converts worker progress updates
// to WebSocket messages and broadcasts them to connected clients.
// Runs in its own goroutine until Stop() is called.
func (a *ProgressAdapter) processUpdates() {
	defer a.wg.Done()

	for {
		select {
		case update := <-a.updateChan:
			a.handleUpdate(update)

		case <-a.stopChan:
			// Drain remaining updates before shutting down
			a.drainRemainingUpdates()
			return
		}
	}
}

// handleUpdate processes a single progress update and broadcasts it via WebSocket.
func (a *ProgressAdapter) handleUpdate(update worker.ProgressUpdate) {
	// Map taskID to file index
	a.mu.RLock()
	fileIndex, exists := a.taskToFileIndex[update.TaskID]
	a.mu.RUnlock()

	if !exists {
		// Unknown task - this can happen if task completed before registration
		// or if task is not part of this batch job
		logging.Debugf("Received update for unknown task %s, skipping", update.TaskID)
		return
	}

	// Validate file index
	if fileIndex < 0 || fileIndex >= len(a.job.Files) {
		logging.Errorf("Invalid file index %d for task %s (total files: %d)",
			fileIndex, update.TaskID, len(a.job.Files))
		return
	}

	// Get file path from job
	filePath := a.job.Files[fileIndex]

	// Convert TaskStatus to string for WebSocket message
	status := string(update.Status)

	// Get overall job progress (lightweight, thread-safe)
	progress := a.job.GetProgress()

	// Create WebSocket message
	wsMsg := &websocket.ProgressMessage{
		JobID:     a.jobID,
		FileIndex: fileIndex,
		FilePath:  filePath,
		Status:    status,
		Progress:  progress,
		Message:   update.Message,
	}

	// Add error if present
	if update.Error != nil {
		wsMsg.Error = update.Error.Error()
	}

	// Broadcast to WebSocket clients (non-blocking)
	if a.broadcaster != nil {
		if err := a.broadcaster.BroadcastProgress(wsMsg); err != nil {
			logging.Errorf("Failed to broadcast progress for task %s: %v", update.TaskID, err)
		}
	}

	logging.Debugf("Broadcasted progress for task %s: status=%s, progress=%.1f%%, message=%s",
		update.TaskID, status, progress, update.Message)
}

// drainRemainingUpdates processes any updates that arrived after Stop() was called
// to ensure no progress information is lost during shutdown.
func (a *ProgressAdapter) drainRemainingUpdates() {
	for {
		select {
		case update := <-a.updateChan:
			a.handleUpdate(update)
		default:
			// Channel is empty, shutdown complete
			return
		}
	}
}

// UnregisterTask removes a task from the mapping (optional cleanup).
// Not strictly necessary as the adapter is typically short-lived per job,
// but provided for completeness and memory efficiency in long-running jobs.
func (a *ProgressAdapter) UnregisterTask(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.taskToFileIndex, taskID)
	logging.Debugf("Unregistered task %s", taskID)
}

// GetRegisteredTaskCount returns the number of currently registered tasks.
// Useful for debugging and monitoring.
func (a *ProgressAdapter) GetRegisteredTaskCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.taskToFileIndex)
}
