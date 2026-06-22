package tui

// handleSortEvent updates task progress state based on a sort event.
// This is a pure function with no side effects - it creates a new tasks map
// with the updated progress data for the specified task.
//
// Parameters:
//   - tasks: Current map of task IDs to taskState
//   - event: SortEvent containing movie ID, step, progress, and message
//
// Returns:
//   - New map with updated task state (immutable transformation)
func handleSortEvent(tasks map[string]*taskState, event SortEvent) map[string]*taskState {
	taskID := event.MovieID
	if taskID == "" {
		return tasks
	}

	// Create new tasks map to ensure immutability
	newTasks := make(map[string]*taskState)
	for id, task := range tasks {
		if id == taskID {
			// Create a copy of the task being updated
			updatedTask := *task
			updatedTask.Phase = event.Phase
			updatedTask.Step = event.Step
			updatedTask.Progress = event.Progress
			updatedTask.Message = event.Message
			updatedTask.UpdatedAt = event.Timestamp
			newTasks[id] = &updatedTask
		} else {
			// Keep other tasks unchanged
			newTasks[id] = task
		}
	}

	// If task didn't exist yet, create it
	if _, exists := newTasks[taskID]; !exists {
		newTasks[taskID] = &taskState{
			ID:        taskID,
			Phase:     event.Phase,
			Step:      event.Step,
			Progress:  event.Progress,
			Message:   event.Message,
			UpdatedAt: event.Timestamp,
		}
	}

	return newTasks
}

// calculateStats computes aggregate statistics from the task state map.
// This replaces HandleStats which previously read from worker.PoolStats.
//
// Parameters:
//   - tasks: Map of task IDs to taskState
//
// Returns:
//   - jobStats with counts and overall progress
func calculateStats(tasks map[string]*taskState) jobStats {
	stats := jobStats{
		Total: len(tasks),
	}

	for _, task := range tasks {
		switch task.Step {
		case sortStepComplete:
			stats.success++
		case sortStepFailed:
			stats.Failed++
		default:
			if task.Progress > 0 {
				stats.Running++
			} else {
				stats.Pending++
			}
		}
	}

	if stats.Total > 0 {
		stats.OverallProgress = float64(stats.success+stats.Failed) / float64(stats.Total)
	}

	return stats
}
