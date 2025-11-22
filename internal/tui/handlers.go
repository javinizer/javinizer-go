package tui

import (
	"github.com/javinizer/javinizer-go/internal/worker"
)

// HandleProgressUpdate updates task progress state based on a progress message.
// This is a pure function with no side effects - it creates a new tasks map
// with the updated progress data for the specified task.
//
// Parameters:
//   - tasks: Current map of task IDs to TaskProgress
//   - update: Progress update containing task ID, status, and progress data
//
// Returns:
//   - New map with updated task progress (immutable transformation)
func HandleProgressUpdate(tasks map[string]*worker.TaskProgress, update worker.ProgressUpdate) map[string]*worker.TaskProgress {
	// Create new tasks map to ensure immutability
	newTasks := make(map[string]*worker.TaskProgress)
	for id, task := range tasks {
		if id == update.TaskID {
			// Create a copy of the task being updated
			updatedTask := *task
			updatedTask.Status = update.Status
			updatedTask.Progress = update.Progress
			updatedTask.Message = update.Message
			updatedTask.BytesDone = update.BytesDone
			updatedTask.Error = update.Error
			updatedTask.UpdatedAt = update.Timestamp
			updatedTask.Type = update.Type
			newTasks[id] = &updatedTask
		} else {
			// Keep other tasks unchanged
			newTasks[id] = task
		}
	}

	return newTasks
}

// HandleStats updates overall statistics based on worker pool stats.
// This is a pure function that creates a new ProgressStats with updated values.
//
// Parameters:
//   - currentStats: Current progress statistics
//   - poolStats: New statistics from worker pool
//
// Returns:
//   - New ProgressStats with updated values
func HandleStats(currentStats worker.ProgressStats, poolStats worker.PoolStats) worker.ProgressStats {
	return worker.ProgressStats{
		Total:           poolStats.TotalTasks,
		Pending:         poolStats.Pending,
		Running:         poolStats.Running,
		Success:         poolStats.Success,
		Failed:          poolStats.Failed,
		Canceled:        poolStats.Canceled,
		OverallProgress: poolStats.OverallProgress,
	}
}

// CalculateTotalProgress computes the aggregate progress across all tasks.
// This is a pure function with no side effects.
//
// Parameters:
//   - tasks: Map of task IDs to TaskProgress
//
// Returns:
//   - Average progress (0.0 to 1.0), or 0.0 if no tasks exist
func CalculateTotalProgress(tasks map[string]*worker.TaskProgress) float64 {
	if len(tasks) == 0 {
		return 0.0
	}

	total := 0.0
	for _, task := range tasks {
		total += task.Progress
	}

	return total / float64(len(tasks))
}

// HandleLogMessage processes a log message and creates a log entry.
// This is a pure function that returns a formatted log entry.
//
// Parameters:
//   - level: Log level (info, warn, error, etc.)
//   - message: Log message content
//
// Returns:
//   - Formatted log entry struct
func HandleLogMessage(level, message string) LogEntry {
	return LogEntry{
		Level:   level,
		Message: message,
	}
}

// HandleError processes an error message and creates a log entry.
// This is a convenience function that wraps HandleLogMessage with "error" level.
//
// Parameters:
//   - err: Error to process
//
// Returns:
//   - Log entry with error level and error message
func HandleError(err error) LogEntry {
	return LogEntry{
		Level:   "error",
		Message: err.Error(),
	}
}
