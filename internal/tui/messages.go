package tui

import (
	"time"
)

// Message types for Bubble Tea

// sortEventMsg represents a progress event from a sort operation
type sortEventMsg struct {
	Event SortEvent
}

// logMsg represents a log message
type logMsg struct {
	Level     string // debug, info, warn, error
	Message   string
	Timestamp time.Time
}

// errorMsg represents an error that occurred
type errorMsg struct {
	Error error
}

// tickMsg is sent periodically to update the UI
type tickMsg time.Time

// quitMsg signals the application should quit
type quitMsg struct{}

// rescanMsg signals to rescan a new folder path
type rescanMsg struct {
	Path string
}
