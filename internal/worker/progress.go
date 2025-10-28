package worker

import (
	"sync"
	"time"
)

// TaskProgress tracks the progress of a single task
type TaskProgress struct {
	ID         string
	Type       TaskType
	Status     TaskStatus
	Progress   float64 // 0.0 to 1.0
	Message    string
	BytesTotal int64
	BytesDone  int64
	StartTime  time.Time
	UpdatedAt  time.Time
	Error      error
}

// ProgressTracker tracks progress for all tasks
type ProgressTracker struct {
	tasks  map[string]*TaskProgress
	mu     sync.RWMutex
	notify chan<- ProgressUpdate
}

// ProgressUpdate represents a progress update event
type ProgressUpdate struct {
	TaskID    string
	Type      TaskType
	Status    TaskStatus
	Progress  float64
	Message   string
	BytesDone int64
	Error     error
	Timestamp time.Time
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(notify chan<- ProgressUpdate) *ProgressTracker {
	return &ProgressTracker{
		tasks:  make(map[string]*TaskProgress),
		notify: notify,
	}
}

// Start marks a task as started
func (pt *ProgressTracker) Start(taskID string, taskType TaskType, message string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now()
	pt.tasks[taskID] = &TaskProgress{
		ID:        taskID,
		Type:      taskType,
		Status:    TaskStatusRunning,
		Progress:  0.0,
		Message:   message,
		StartTime: now,
		UpdatedAt: now,
	}

	pt.sendUpdate(taskID)
}

// Update updates the progress of a task
func (pt *ProgressTracker) Update(taskID string, progress float64, message string, bytesDone int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return
	}

	task.Progress = progress
	task.Message = message
	task.BytesDone = bytesDone
	task.UpdatedAt = time.Now()

	pt.sendUpdate(taskID)
}

// SetTotal sets the total bytes for a task
func (pt *ProgressTracker) SetTotal(taskID string, bytesTotal int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return
	}

	task.BytesTotal = bytesTotal
	task.UpdatedAt = time.Now()

	pt.sendUpdate(taskID)
}

// Complete marks a task as completed successfully
func (pt *ProgressTracker) Complete(taskID string, message string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return
	}

	task.Status = TaskStatusSuccess
	task.Progress = 1.0
	task.Message = message
	task.UpdatedAt = time.Now()

	pt.sendUpdate(taskID)
}

// Fail marks a task as failed
func (pt *ProgressTracker) Fail(taskID string, err error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return
	}

	task.Status = TaskStatusFailed
	task.Error = err
	task.Message = err.Error()
	task.UpdatedAt = time.Now()

	pt.sendUpdate(taskID)
}

// Cancel marks a task as canceled
func (pt *ProgressTracker) Cancel(taskID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return
	}

	task.Status = TaskStatusCanceled
	task.Message = "Canceled"
	task.UpdatedAt = time.Now()

	pt.sendUpdate(taskID)
}

// Get retrieves the progress for a task
func (pt *ProgressTracker) Get(taskID string) (*TaskProgress, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	task, exists := pt.tasks[taskID]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	copy := *task
	return &copy, true
}

// GetAll retrieves all task progress
func (pt *ProgressTracker) GetAll() []*TaskProgress {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	tasks := make([]*TaskProgress, 0, len(pt.tasks))
	for _, task := range pt.tasks {
		copy := *task
		tasks = append(tasks, &copy)
	}

	return tasks
}

// GetByType retrieves tasks of a specific type
func (pt *ProgressTracker) GetByType(taskType TaskType) []*TaskProgress {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	tasks := make([]*TaskProgress, 0)
	for _, task := range pt.tasks {
		if task.Type == taskType {
			copy := *task
			tasks = append(tasks, &copy)
		}
	}

	return tasks
}

// GetByStatus retrieves tasks with a specific status
func (pt *ProgressTracker) GetByStatus(status TaskStatus) []*TaskProgress {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	tasks := make([]*TaskProgress, 0)
	for _, task := range pt.tasks {
		if task.Status == status {
			copy := *task
			tasks = append(tasks, &copy)
		}
	}

	return tasks
}

// Stats returns statistics about all tasks
func (pt *ProgressTracker) Stats() ProgressStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	stats := ProgressStats{
		Total:           len(pt.tasks),
		Pending:         0,
		Running:         0,
		Success:         0,
		Failed:          0,
		Canceled:        0,
		TotalBytes:      0,
		DoneBytes:       0,
		OverallProgress: 0.0,
	}

	for _, task := range pt.tasks {
		switch task.Status {
		case TaskStatusPending:
			stats.Pending++
		case TaskStatusRunning:
			stats.Running++
		case TaskStatusSuccess:
			stats.Success++
		case TaskStatusFailed:
			stats.Failed++
		case TaskStatusCanceled:
			stats.Canceled++
		}

		stats.TotalBytes += task.BytesTotal
		stats.DoneBytes += task.BytesDone
	}

	if stats.Total > 0 {
		stats.OverallProgress = float64(stats.Success+stats.Failed+stats.Canceled) / float64(stats.Total)
	}

	return stats
}

// Clear removes all task progress
func (pt *ProgressTracker) Clear() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.tasks = make(map[string]*TaskProgress)
}

// Remove removes a specific task
func (pt *ProgressTracker) Remove(taskID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	delete(pt.tasks, taskID)
}

// sendUpdate sends a progress update notification (must be called with lock held)
func (pt *ProgressTracker) sendUpdate(taskID string) {
	if pt.notify == nil {
		return
	}

	task := pt.tasks[taskID]
	update := ProgressUpdate{
		TaskID:    task.ID,
		Type:      task.Type,
		Status:    task.Status,
		Progress:  task.Progress,
		Message:   task.Message,
		BytesDone: task.BytesDone,
		Error:     task.Error,
		Timestamp: time.Now(),
	}

	// Non-blocking send
	select {
	case pt.notify <- update:
	default:
		// Channel full, skip this update
	}
}

// ProgressStats holds statistics about all tasks
type ProgressStats struct {
	Total           int
	Pending         int
	Running         int
	Success         int
	Failed          int
	Canceled        int
	TotalBytes      int64
	DoneBytes       int64
	OverallProgress float64
}
