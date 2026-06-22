package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func calculateTotalProgress(tasks map[string]*taskState) float64 {
	if len(tasks) == 0 {
		return 0.0
	}

	total := 0.0
	for _, task := range tasks {
		total += task.Progress
	}

	return total / float64(len(tasks))
}

func handleLogMessage(level, message string) logEntry {
	return logEntry{
		Level:   level,
		Message: message,
	}
}

func handleError(err error) logEntry {
	return logEntry{
		Level:   "error",
		Message: err.Error(),
	}
}

func TestHandleSortEvent(t *testing.T) {
	baseTime := time.Now()

	tests := []struct {
		name     string
		tasks    map[string]*taskState
		event    SortEvent
		wantTask *taskState // Expected state of the updated task
	}{
		{
			name: "update existing task progress",
			tasks: map[string]*taskState{
				"IPX-123": {
					ID:       "IPX-123",
					Progress: 0.0,
					Message:  "Starting",
					Step:     sortStepScrape,
					Phase:    SortEventPhaseScrape,
				},
			},
			event: SortEvent{
				MovieID:   "IPX-123",
				Progress:  0.5,
				Message:   "Halfway",
				Step:      sortStepScrape,
				Phase:     SortEventPhaseScrape,
				Timestamp: baseTime,
			},
			wantTask: &taskState{
				ID:        "IPX-123",
				Progress:  0.5,
				Message:   "Halfway",
				Step:      sortStepScrape,
				Phase:     SortEventPhaseScrape,
				UpdatedAt: baseTime,
			},
		},
		{
			name:  "create new task from event",
			tasks: map[string]*taskState{},
			event: SortEvent{
				MovieID:   "IPX-456",
				Progress:  0.0,
				Message:   "Queued",
				Step:      sortStepQueued,
				Phase:     SortEventPhaseScrape,
				Timestamp: baseTime,
			},
			wantTask: &taskState{
				ID:        "IPX-456",
				Progress:  0.0,
				Message:   "Queued",
				Step:      sortStepQueued,
				Phase:     SortEventPhaseScrape,
				UpdatedAt: baseTime,
			},
		},
		{
			name: "progress boundary - 1.0 complete",
			tasks: map[string]*taskState{
				"IPX-123": {
					ID:       "IPX-123",
					Progress: 0.9,
					Step:     sortStepScrape,
				},
			},
			event: SortEvent{
				MovieID:   "IPX-123",
				Progress:  1.0,
				Step:      sortStepComplete,
				Message:   "Complete",
				Timestamp: baseTime,
			},
			wantTask: &taskState{
				ID:        "IPX-123",
				Progress:  1.0,
				Step:      sortStepComplete,
				Message:   "Complete",
				UpdatedAt: baseTime,
			},
		},
		{
			name: "update one task, verify others unchanged",
			tasks: map[string]*taskState{
				"IPX-123": {
					ID:       "IPX-123",
					Progress: 0.3,
					Message:  "Task 1",
				},
				"IPX-456": {
					ID:       "IPX-456",
					Progress: 0.7,
					Message:  "Task 2",
				},
			},
			event: SortEvent{
				MovieID:  "IPX-123",
				Progress: 0.6,
				Message:  "Updated Task 1",
			},
			wantTask: &taskState{
				ID:       "IPX-123",
				Progress: 0.6,
				Message:  "Updated Task 1",
			},
		},
		{
			name: "failed task",
			tasks: map[string]*taskState{
				"IPX-123": {
					ID:       "IPX-123",
					Progress: 0.5,
					Step:     sortStepScrape,
				},
			},
			event: SortEvent{
				MovieID:  "IPX-123",
				Progress: 0.5,
				Step:     sortStepFailed,
				Message:  "Scrape failed",
			},
			wantTask: &taskState{
				ID:       "IPX-123",
				Progress: 0.5,
				Step:     sortStepFailed,
				Message:  "Scrape failed",
			},
		},
		{
			name:     "empty MovieID is no-op",
			tasks:    map[string]*taskState{"IPX-123": {ID: "IPX-123"}},
			event:    SortEvent{MovieID: "", Message: "orphan"},
			wantTask: nil, // Should not add a task with empty MovieID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save copy of original task for immutability check
			var originalTask *taskState
			if task, exists := tt.tasks[tt.event.MovieID]; exists {
				copy := *task
				originalTask = &copy
			}

			// Run the handler
			got := handleSortEvent(tt.tasks, tt.event)

			if tt.wantTask == nil && tt.event.MovieID == "" {
				// Empty MovieID — original map should be unchanged
				assert.Equal(t, len(tt.tasks), len(got))
				return
			}

			if tt.wantTask == nil {
				// Verify task was not added
				_, exists := got[tt.event.MovieID]
				assert.False(t, exists, "nonexistent task should not be added")
			} else {
				// Verify the updated task
				task, exists := got[tt.event.MovieID]
				assert.True(t, exists, "task should exist")
				assert.Equal(t, tt.wantTask.ID, task.ID)
				assert.InDelta(t, tt.wantTask.Progress, task.Progress, 0.001)
				assert.Equal(t, tt.wantTask.Message, task.Message)
				assert.Equal(t, tt.wantTask.Step, task.Step)

				// Verify immutability - original task not mutated
				if originalTask != nil {
					origFromMap := tt.tasks[tt.event.MovieID]
					if origFromMap != nil {
						assert.Equal(t, originalTask.Progress, origFromMap.Progress, "original task should not be mutated")
						assert.Equal(t, originalTask.Message, origFromMap.Message, "original task should not be mutated")
					}
				}
			}

			// Verify other tasks unchanged (if multiple tasks)
			if len(tt.tasks) > 1 && tt.wantTask != nil {
				for id, originalTask := range tt.tasks {
					if id != tt.event.MovieID {
						assert.Same(t, originalTask, got[id],
							"other tasks should reference same pointers (not copied)")
					}
				}
			}
		})
	}
}

func TestCalculateTotalProgress(t *testing.T) {
	tests := []struct {
		name  string
		tasks map[string]*taskState
		want  float64
	}{
		{
			name:  "empty tasks map",
			tasks: map[string]*taskState{},
			want:  0.0,
		},
		{
			name: "single task at 50%",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.5},
			},
			want: 0.5,
		},
		{
			name: "single task at 100%",
			tasks: map[string]*taskState{
				"task1": {Progress: 1.0},
			},
			want: 1.0,
		},
		{
			name: "single task at 0%",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.0},
			},
			want: 0.0,
		},
		{
			name: "two tasks - average 50%",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.0},
				"task2": {Progress: 1.0},
			},
			want: 0.5,
		},
		{
			name: "three tasks - mixed progress",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.0},
				"task2": {Progress: 0.5},
				"task3": {Progress: 1.0},
			},
			want: 0.5,
		},
		{
			name: "multiple tasks all complete",
			tasks: map[string]*taskState{
				"task1": {Progress: 1.0},
				"task2": {Progress: 1.0},
				"task3": {Progress: 1.0},
			},
			want: 1.0,
		},
		{
			name: "multiple tasks all pending",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.0},
				"task2": {Progress: 0.0},
				"task3": {Progress: 0.0},
			},
			want: 0.0,
		},
		{
			name: "four tasks - varying progress",
			tasks: map[string]*taskState{
				"task1": {Progress: 0.25},
				"task2": {Progress: 0.5},
				"task3": {Progress: 0.75},
				"task4": {Progress: 1.0},
			},
			want: 0.625,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTotalProgress(tt.tasks)
			assert.InDelta(t, tt.want, got, 0.001, "progress should match within 0.001 delta")
		})
	}
}

func TestCalculateStats(t *testing.T) {
	tests := []struct {
		name  string
		tasks map[string]*taskState
		want  jobStats
	}{
		{
			name:  "empty tasks",
			tasks: map[string]*taskState{},
			want:  jobStats{},
		},
		{
			name: "all pending",
			tasks: map[string]*taskState{
				"task1": {Step: SortEventStep("queued"), Progress: 0},
				"task2": {Step: SortEventStep("queued"), Progress: 0},
			},
			want: jobStats{Total: 2, Pending: 2},
		},
		{
			name: "all running",
			tasks: map[string]*taskState{
				"task1": {Step: sortStepScrape, Progress: 0.5},
				"task2": {Step: sortStepApply, Progress: 0.3},
			},
			want: jobStats{Total: 2, Running: 2},
		},
		{
			name: "all complete",
			tasks: map[string]*taskState{
				"task1": {Step: sortStepComplete, Progress: 1.0},
				"task2": {Step: sortStepComplete, Progress: 1.0},
			},
			want: jobStats{Total: 2, success: 2, OverallProgress: 1.0},
		},
		{
			name: "all failed",
			tasks: map[string]*taskState{
				"task1": {Step: sortStepFailed, Progress: 0.5},
				"task2": {Step: sortStepFailed, Progress: 0.3},
			},
			want: jobStats{Total: 2, Failed: 2, OverallProgress: 1.0},
		},
		{
			name: "mixed states",
			tasks: map[string]*taskState{
				"task1": {Step: sortStepComplete, Progress: 1.0},
				"task2": {Step: sortStepFailed, Progress: 0.5},
				"task3": {Step: sortStepScrape, Progress: 0.3},
				"task4": {Step: SortEventStep("queued"), Progress: 0},
			},
			want: jobStats{Total: 4, success: 1, Failed: 1, Running: 1, Pending: 1, OverallProgress: 0.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateStats(tt.tasks)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleLogMessage(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		message string
		want    logEntry
	}{
		{
			name:    "info log",
			level:   "info",
			message: "Processing started",
			want: logEntry{
				Level:   "info",
				Message: "Processing started",
			},
		},
		{
			name:    "warn log",
			level:   "warn",
			message: "Scraper timeout",
			want: logEntry{
				Level:   "warn",
				Message: "Scraper timeout",
			},
		},
		{
			name:    "error log",
			level:   "error",
			message: "Failed to download",
			want: logEntry{
				Level:   "error",
				Message: "Failed to download",
			},
		},
		{
			name:    "debug log",
			level:   "debug",
			message: "Cache hit",
			want: logEntry{
				Level:   "debug",
				Message: "Cache hit",
			},
		},
		{
			name:    "empty message",
			level:   "info",
			message: "",
			want: logEntry{
				Level:   "info",
				Message: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handleLogMessage(tt.level, tt.message)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want logEntry
	}{
		{
			name: "standard error",
			err:  errors.New("connection failed"),
			want: logEntry{
				Level:   "error",
				Message: "connection failed",
			},
		},
		{
			name: "formatted error",
			err:  errors.New("scraper timeout: 30s exceeded"),
			want: logEntry{
				Level:   "error",
				Message: "scraper timeout: 30s exceeded",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handleError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
