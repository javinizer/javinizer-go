package tui

// State represents the testable business state of the TUI.
// This is separated from the Model struct to enable unit testing
// independent of Bubble Tea's event-driven framework.
//
// Philosophy: 60% testable (business logic), 40% excluded (UI rendering).
// This struct contains ONLY business state fields that can be tested
// with pure functions, NOT UI-specific fields like viewport, help, styles.
type State struct {
	// View state
	CurrentView ViewMode

	// File browser state
	Cursor       int
	FileCount    int
	SelectedIdx  int // Currently selected file index in browser

	// Modal states
	ShowingFolderPicker bool
	ShowingManualSearch bool
	EditingPath         bool

	// Processing state
	IsProcessing       bool
	IsPaused           bool
	ProcessingComplete bool
}

// NewState creates a new State with sensible defaults
func NewState() *State {
	return &State{
		CurrentView:         ViewBrowser,
		Cursor:              0,
		FileCount:           0,
		SelectedIdx:         0,
		ShowingFolderPicker: false,
		ShowingManualSearch: false,
		EditingPath:         false,
		IsProcessing:        false,
		IsPaused:            false,
		ProcessingComplete:  false,
	}
}

// SwitchToView switches to the specified view.
// Returns a new state with the updated view.
// This is a pure function with no side effects.
func SwitchToView(state State, view ViewMode) State {
	// Don't change if already on this view
	if state.CurrentView == view {
		return state
	}

	// Valid view types: ViewBrowser(0), ViewDashboard(1), ViewLogs(2), ViewSettings(3), ViewHelp(4)
	if !IsValidView(view) {
		// Invalid view - return unchanged state
		return state
	}

	newState := state
	newState.CurrentView = view

	// Reset cursor when switching views (fresh start on new view)
	newState.Cursor = 0

	return newState
}

// CycleView cycles to the next view in sequence.
// Order: Browser -> Dashboard -> Logs -> Settings -> Browser (skips Help view).
// Returns a new state with the updated view.
func CycleView(state State) State {
	// Cycle through views (Browser -> Dashboard -> Logs -> Settings -> Browser)
	// Skip Help view (accessed via '?' key, not tab)
	nextView := (state.CurrentView + 1) % 5

	// Skip Help view in cycling
	if nextView == ViewHelp {
		nextView = ViewBrowser
	}

	newState := state
	newState.CurrentView = nextView
	newState.Cursor = 0 // Reset cursor on view switch

	return newState
}

// IsValidView checks if the given view mode is valid.
// Valid views: Browser(0), Dashboard(1), Logs(2), Settings(3), Help(4)
func IsValidView(view ViewMode) bool {
	return view >= ViewBrowser && view <= ViewHelp
}

// ToggleHelp toggles between the current view and the Help view.
// Returns a new state with the toggled view.
func ToggleHelp(state State, previousView ViewMode) State {
	newState := state

	if state.CurrentView == ViewHelp {
		// Currently on Help - return to previous view
		newState.CurrentView = previousView
	} else {
		// Switch to Help view
		newState.CurrentView = ViewHelp
	}

	newState.Cursor = 0
	return newState
}

// MoveCursorUp moves the cursor up by one position.
// Returns a new state with the updated cursor (clamped to >= 0).
func MoveCursorUp(state State) State {
	newState := state
	newState.Cursor--

	// Clamp to minimum 0
	if newState.Cursor < 0 {
		newState.Cursor = 0
	}

	return newState
}

// MoveCursorDown moves the cursor down by one position.
// Returns a new state with the updated cursor (clamped to < maxItems).
func MoveCursorDown(state State, maxItems int) State {
	newState := state
	newState.Cursor++

	// Clamp to maximum (maxItems - 1)
	if maxItems > 0 && newState.Cursor >= maxItems {
		newState.Cursor = maxItems - 1
	}

	return newState
}

// SetFileCount updates the file count in the state.
// Used when files are loaded or rescanned.
func SetFileCount(state State, count int) State {
	newState := state
	newState.FileCount = count

	// If cursor is out of bounds, reset it
	if newState.Cursor >= count {
		newState.Cursor = 0
	}

	return newState
}
