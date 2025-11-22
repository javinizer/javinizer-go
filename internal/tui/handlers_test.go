package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestHandleProgressUpdate(t *testing.T) {
	baseTime := time.Now()

	tests := []struct {
		name     string
		tasks    map[string]*worker.TaskProgress
		update   worker.ProgressUpdate
		wantTask *worker.TaskProgress // Expected state of the updated task
	}{
		{
			name: "update existing task progress",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.0,
					Message:  "Starting",
					Status:   worker.TaskStatusPending,
				},
			},
			update: worker.ProgressUpdate{
				TaskID:    "task1",
				Progress:  0.5,
				Message:   "Halfway",
				Status:    worker.TaskStatusRunning,
				BytesDone: 1024,
				Timestamp: baseTime,
				Type:      worker.TaskTypeScrape,
			},
			wantTask: &worker.TaskProgress{
				ID:        "task1",
				Progress:  0.5,
				Message:   "Halfway",
				Status:    worker.TaskStatusRunning,
				BytesDone: 1024,
				UpdatedAt: baseTime,
				Type:      worker.TaskTypeScrape,
			},
		},
		{
			name: "update nonexistent task - no-op",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.5,
					Message:  "Existing",
				},
			},
			update: worker.ProgressUpdate{
				TaskID:   "task2",
				Progress: 0.8,
				Message:  "Should not appear",
			},
			wantTask: nil, // Task2 should not be added
		},
		{
			name: "progress boundary - 0.0",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.5,
				},
			},
			update: worker.ProgressUpdate{
				TaskID:   "task1",
				Progress: 0.0,
				Status:   worker.TaskStatusPending,
			},
			wantTask: &worker.TaskProgress{
				ID:       "task1",
				Progress: 0.0,
				Status:   worker.TaskStatusPending,
			},
		},
		{
			name: "progress boundary - 1.0 complete",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.9,
				},
			},
			update: worker.ProgressUpdate{
				TaskID:   "task1",
				Progress: 1.0,
				Status:   worker.TaskStatusSuccess,
				Message:  "Complete",
			},
			wantTask: &worker.TaskProgress{
				ID:       "task1",
				Progress: 1.0,
				Status:   worker.TaskStatusSuccess,
				Message:  "Complete",
			},
		},
		{
			name: "update one task, verify others unchanged",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.3,
					Message:  "Task 1",
				},
				"task2": {
					ID:       "task2",
					Progress: 0.7,
					Message:  "Task 2",
				},
			},
			update: worker.ProgressUpdate{
				TaskID:   "task1",
				Progress: 0.6,
				Message:  "Updated Task 1",
			},
			wantTask: &worker.TaskProgress{
				ID:       "task1",
				Progress: 0.6,
				Message:  "Updated Task 1",
			},
		},
		{
			name: "update with error",
			tasks: map[string]*worker.TaskProgress{
				"task1": {
					ID:       "task1",
					Progress: 0.5,
				},
			},
			update: worker.ProgressUpdate{
				TaskID:   "task1",
				Progress: 0.5, // Preserve existing progress
				Status:   worker.TaskStatusFailed,
				Message:  "Failed",
				Error:    errors.New("scrape failed"),
			},
			wantTask: &worker.TaskProgress{
				ID:       "task1",
				Progress: 0.5,
				Status:   worker.TaskStatusFailed,
				Message:  "Failed",
				Error:    errors.New("scrape failed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save copy of original task for immutability check
			var originalTask *worker.TaskProgress
			if task, exists := tt.tasks[tt.update.TaskID]; exists {
				copy := *task
				originalTask = &copy
			}

			// Run the handler
			got := HandleProgressUpdate(tt.tasks, tt.update)

			if tt.wantTask == nil {
				// Verify task was not added
				_, exists := got[tt.update.TaskID]
				assert.False(t, exists, "nonexistent task should not be added")
			} else {
				// Verify the updated task
				task, exists := got[tt.update.TaskID]
				assert.True(t, exists, "task should exist")
				assert.Equal(t, tt.wantTask.ID, task.ID)
				assert.InDelta(t, tt.wantTask.Progress, task.Progress, 0.001)
				assert.Equal(t, tt.wantTask.Message, task.Message)
				assert.Equal(t, tt.wantTask.Status, task.Status)
				assert.Equal(t, tt.wantTask.BytesDone, task.BytesDone)
				assert.Equal(t, tt.wantTask.Type, task.Type)

				if tt.wantTask.Error != nil {
					assert.EqualError(t, task.Error, tt.wantTask.Error.Error())
				}

				// Verify immutability - original task not mutated
				if originalTask != nil {
					origFromMap := tt.tasks[tt.update.TaskID]
					assert.Equal(t, originalTask.Progress, origFromMap.Progress, "original task should not be mutated")
					assert.Equal(t, originalTask.Message, origFromMap.Message, "original task should not be mutated")
				}
			}

			// Verify other tasks unchanged (if multiple tasks)
			if len(tt.tasks) > 1 && tt.wantTask != nil {
				for id, originalTask := range tt.tasks {
					if id != tt.update.TaskID {
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
		tasks map[string]*worker.TaskProgress
		want  float64
	}{
		{
			name:  "empty tasks map",
			tasks: map[string]*worker.TaskProgress{},
			want:  0.0,
		},
		{
			name: "single task at 50%",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.5},
			},
			want: 0.5,
		},
		{
			name: "single task at 100%",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 1.0},
			},
			want: 1.0,
		},
		{
			name: "single task at 0%",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.0},
			},
			want: 0.0,
		},
		{
			name: "two tasks - average 50%",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.0},
				"task2": {Progress: 1.0},
			},
			want: 0.5,
		},
		{
			name: "three tasks - mixed progress",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.0},
				"task2": {Progress: 0.5},
				"task3": {Progress: 1.0},
			},
			want: 0.5, // (0.0 + 0.5 + 1.0) / 3 = 0.5
		},
		{
			name: "multiple tasks all complete",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 1.0},
				"task2": {Progress: 1.0},
				"task3": {Progress: 1.0},
			},
			want: 1.0,
		},
		{
			name: "multiple tasks all pending",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.0},
				"task2": {Progress: 0.0},
				"task3": {Progress: 0.0},
			},
			want: 0.0,
		},
		{
			name: "four tasks - varying progress",
			tasks: map[string]*worker.TaskProgress{
				"task1": {Progress: 0.25},
				"task2": {Progress: 0.5},
				"task3": {Progress: 0.75},
				"task4": {Progress: 1.0},
			},
			want: 0.625, // (0.25 + 0.5 + 0.75 + 1.0) / 4 = 0.625
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTotalProgress(tt.tasks)
			assert.InDelta(t, tt.want, got, 0.001, "progress should match within 0.001 delta")
		})
	}
}

func TestHandleStats(t *testing.T) {
	tests := []struct {
		name         string
		currentStats worker.ProgressStats
		poolStats    worker.PoolStats
		want         worker.ProgressStats
	}{
		{
			name: "update all fields",
			currentStats: worker.ProgressStats{
				Total:   0,
				Pending: 0,
			},
			poolStats: worker.PoolStats{
				TotalTasks:      10,
				Pending:         5,
				Running:         3,
				Success:         1,
				Failed:          1,
				Canceled:        0,
				OverallProgress: 0.3,
			},
			want: worker.ProgressStats{
				Total:           10,
				Pending:         5,
				Running:         3,
				Success:         1,
				Failed:          1,
				Canceled:        0,
				OverallProgress: 0.3,
			},
		},
		{
			name:         "zero stats",
			currentStats: worker.ProgressStats{},
			poolStats: worker.PoolStats{
				TotalTasks: 0,
			},
			want: worker.ProgressStats{
				Total: 0,
			},
		},
		{
			name: "complete batch - 100% progress",
			currentStats: worker.ProgressStats{
				Total:   5,
				Running: 2,
			},
			poolStats: worker.PoolStats{
				TotalTasks:      5,
				Success:         5,
				OverallProgress: 1.0,
			},
			want: worker.ProgressStats{
				Total:           5,
				Success:         5,
				OverallProgress: 1.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleStats(tt.currentStats, tt.poolStats)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleLogMessage(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		message string
		want    LogEntry
	}{
		{
			name:    "info log",
			level:   "info",
			message: "Processing started",
			want: LogEntry{
				Level:   "info",
				Message: "Processing started",
			},
		},
		{
			name:    "warn log",
			level:   "warn",
			message: "Scraper timeout",
			want: LogEntry{
				Level:   "warn",
				Message: "Scraper timeout",
			},
		},
		{
			name:    "error log",
			level:   "error",
			message: "Failed to download",
			want: LogEntry{
				Level:   "error",
				Message: "Failed to download",
			},
		},
		{
			name:    "debug log",
			level:   "debug",
			message: "Cache hit",
			want: LogEntry{
				Level:   "debug",
				Message: "Cache hit",
			},
		},
		{
			name:    "empty message",
			level:   "info",
			message: "",
			want: LogEntry{
				Level:   "info",
				Message: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleLogMessage(tt.level, tt.message)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want LogEntry
	}{
		{
			name: "standard error",
			err:  errors.New("connection failed"),
			want: LogEntry{
				Level:   "error",
				Message: "connection failed",
			},
		},
		{
			name: "formatted error",
			err:  errors.New("scraper timeout: 30s exceeded"),
			want: LogEntry{
				Level:   "error",
				Message: "scraper timeout: 30s exceeded",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
