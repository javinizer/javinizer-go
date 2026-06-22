package tui

// state represents the testable business state of the TUI.
// This is separated from the Model struct to enable unit testing
// independent of Bubble Tea's event-driven framework.
//
// Philosophy: 60% testable (business logic), 40% excluded (UI rendering).
// This struct contains ONLY business state fields that can be tested
// with pure functions, NOT UI-specific fields like viewport, help, styles.
type state struct {
	// File browser state
	Cursor      int
	FileCount   int
	SelectedIdx int // Currently selected file index in browser

	// Modal states
	EditingPath bool

	// Processing state
	IsProcessing       bool
	IsPaused           bool
	ProcessingComplete bool
}

// newState creates a new state with sensible defaults
func newState() *state {
	return &state{
		Cursor:             0,
		FileCount:          0,
		SelectedIdx:        0,
		EditingPath:        false,
		IsProcessing:       false,
		IsPaused:           false,
		ProcessingComplete: false,
	}
}

// isValidView checks if the given view mode is valid.
func isValidView(view viewMode) bool {
	return view >= viewBrowser && view <= viewHelp
}
