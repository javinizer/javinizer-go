package tui

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/worker"
)

// Message types for Bubble Tea

// ProgressMsg represents a progress update from a worker
type ProgressMsg struct {
	TaskID    string
	Type      worker.TaskType
	Status    worker.TaskStatus
	Progress  float64
	Message   string
	BytesDone int64
	Error     error
	Timestamp time.Time
}

// TaskCompleteMsg indicates a task has completed
type TaskCompleteMsg struct {
	TaskID   string
	Type     worker.TaskType
	Success  bool
	Error    error
	Duration time.Duration
}

// LogMsg represents a log message
type LogMsg struct {
	Level     string // debug, info, warn, error
	Message   string
	Timestamp time.Time
}

// StatsMsg provides overall statistics update
type StatsMsg struct {
	TotalTasks      int
	Pending         int
	Running         int
	Success         int
	Failed          int
	Canceled        int
	TotalBytes      int64
	DoneBytes       int64
	OverallProgress float64
	ElapsedTime     time.Duration
}

// ErrorMsg represents an error that occurred
type ErrorMsg struct {
	Error error
}

// FileSelectedMsg indicates files were selected/deselected
type FileSelectedMsg struct {
	Path     string
	Selected bool
}

// ScanCompleteMsg indicates file scanning is complete
type ScanCompleteMsg struct {
	FilesFound int
	Skipped    int
	Errors     int
}

// TickMsg is sent periodically to update the UI
type TickMsg time.Time

// QuitMsg signals the application should quit
type QuitMsg struct{}

// StartProcessingMsg signals to start processing selected files
type StartProcessingMsg struct{}

// PauseMsg signals to pause processing
type PauseMsg struct{}

// ResumeMsg signals to resume processing
type ResumeMsg struct{}

// CancelTaskMsg signals to cancel a specific task
type CancelTaskMsg struct {
	TaskID string
}
