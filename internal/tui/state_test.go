package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewState(t *testing.T) {
	state := NewState()

	assert.Equal(t, ViewBrowser, state.CurrentView, "Should default to Browser view")
	assert.Equal(t, 0, state.Cursor, "Should default cursor to 0")
	assert.Equal(t, 0, state.FileCount, "Should default file count to 0")
	assert.Equal(t, 0, state.SelectedIdx, "Should default selected index to 0")
	assert.False(t, state.ShowingFolderPicker, "Should default folder picker to hidden")
	assert.False(t, state.ShowingManualSearch, "Should default manual search to hidden")
	assert.False(t, state.EditingPath, "Should default path editing to false")
	assert.False(t, state.IsProcessing, "Should default processing to false")
	assert.False(t, state.IsPaused, "Should default paused to false")
	assert.False(t, state.ProcessingComplete, "Should default processing complete to false")
}

func TestSwitchToView(t *testing.T) {
	tests := []struct {
		name            string
		initialView     ViewMode
		targetView      ViewMode
		expectedView    ViewMode
		expectedCursor  int
		shouldChange    bool
	}{
		{
			name:           "switch from Browser to Dashboard",
			initialView:    ViewBrowser,
			targetView:     ViewDashboard,
			expectedView:   ViewDashboard,
			expectedCursor: 0,
			shouldChange:   true,
		},
		{
			name:           "switch from Dashboard to Logs",
			initialView:    ViewDashboard,
			targetView:     ViewLogs,
			expectedView:   ViewLogs,
			expectedCursor: 0,
			shouldChange:   true,
		},
		{
			name:           "switch from Logs to Settings",
			initialView:    ViewLogs,
			targetView:     ViewSettings,
			expectedView:   ViewSettings,
			expectedCursor: 0,
			shouldChange:   true,
		},
		{
			name:           "switch from Settings to Help",
			initialView:    ViewSettings,
			targetView:     ViewHelp,
			expectedView:   ViewHelp,
			expectedCursor: 0,
			shouldChange:   true,
		},
		{
			name:           "switch from Help to Browser",
			initialView:    ViewHelp,
			targetView:     ViewBrowser,
			expectedView:   ViewBrowser,
			expectedCursor: 0,
			shouldChange:   true,
		},
		{
			name:           "stay on same view (Browser)",
			initialView:    ViewBrowser,
			targetView:     ViewBrowser,
			expectedView:   ViewBrowser,
			expectedCursor: 5, // Cursor should be preserved
			shouldChange:   false,
		},
		{
			name:           "stay on same view (Dashboard)",
			initialView:    ViewDashboard,
			targetView:     ViewDashboard,
			expectedView:   ViewDashboard,
			expectedCursor: 3,
			shouldChange:   false,
		},
		{
			name:           "invalid view (negative)",
			initialView:    ViewBrowser,
			targetView:     ViewMode(-1),
			expectedView:   ViewBrowser, // No change
			expectedCursor: 2,
			shouldChange:   false,
		},
		{
			name:           "invalid view (too large)",
			initialView:    ViewDashboard,
			targetView:     ViewMode(10),
			expectedView:   ViewDashboard, // No change
			expectedCursor: 4,
			shouldChange:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create initial state
			state := State{
				CurrentView: tt.initialView,
				Cursor:      tt.expectedCursor,
			}

			// Switch to target view
			newState := SwitchToView(state, tt.targetView)

			// Verify view changed correctly
			assert.Equal(t, tt.expectedView, newState.CurrentView)

			if tt.shouldChange {
				// Cursor should reset to 0 when view changes
				assert.Equal(t, 0, newState.Cursor, "Cursor should reset to 0 on view change")
			} else {
				// Cursor should be preserved when view doesn't change
				assert.Equal(t, tt.expectedCursor, newState.Cursor, "Cursor should be preserved when view unchanged")
			}
		})
	}
}

func TestCycleView(t *testing.T) {
	tests := []struct {
		name         string
		initialView  ViewMode
		expectedView ViewMode
	}{
		{
			name:         "cycle from Browser to Dashboard",
			initialView:  ViewBrowser,
			expectedView: ViewDashboard,
		},
		{
			name:         "cycle from Dashboard to Logs",
			initialView:  ViewDashboard,
			expectedView: ViewLogs,
		},
		{
			name:         "cycle from Logs to Settings",
			initialView:  ViewLogs,
			expectedView: ViewSettings,
		},
		{
			name:         "cycle from Settings back to Browser (skip Help)",
			initialView:  ViewSettings,
			expectedView: ViewBrowser,
		},
		{
			name:         "cycle from Help to Browser (Help not in normal cycle)",
			initialView:  ViewHelp,
			expectedView: ViewBrowser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := State{
				CurrentView: tt.initialView,
				Cursor:      5, // Set non-zero cursor
			}

			newState := CycleView(state)

			assert.Equal(t, tt.expectedView, newState.CurrentView)
			assert.Equal(t, 0, newState.Cursor, "Cursor should reset to 0 on cycle")
		})
	}
}

func TestIsValidView(t *testing.T) {
	tests := []struct {
		name     string
		view     ViewMode
		expected bool
	}{
		{
			name:     "Browser view is valid",
			view:     ViewBrowser,
			expected: true,
		},
		{
			name:     "Dashboard view is valid",
			view:     ViewDashboard,
			expected: true,
		},
		{
			name:     "Logs view is valid",
			view:     ViewLogs,
			expected: true,
		},
		{
			name:     "Settings view is valid",
			view:     ViewSettings,
			expected: true,
		},
		{
			name:     "Help view is valid",
			view:     ViewHelp,
			expected: true,
		},
		{
			name:     "negative view is invalid",
			view:     ViewMode(-1),
			expected: false,
		},
		{
			name:     "view beyond Help is invalid",
			view:     ViewMode(5),
			expected: false,
		},
		{
			name:     "large view number is invalid",
			view:     ViewMode(100),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidView(tt.view)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToggleHelp(t *testing.T) {
	tests := []struct {
		name         string
		initialView  ViewMode
		previousView ViewMode
		expectedView ViewMode
	}{
		{
			name:         "toggle Help on from Browser",
			initialView:  ViewBrowser,
			previousView: ViewBrowser,
			expectedView: ViewHelp,
		},
		{
			name:         "toggle Help off to Browser",
			initialView:  ViewHelp,
			previousView: ViewBrowser,
			expectedView: ViewBrowser,
		},
		{
			name:         "toggle Help on from Dashboard",
			initialView:  ViewDashboard,
			previousView: ViewDashboard,
			expectedView: ViewHelp,
		},
		{
			name:         "toggle Help off to Dashboard",
			initialView:  ViewHelp,
			previousView: ViewDashboard,
			expectedView: ViewDashboard,
		},
		{
			name:         "toggle Help on from Logs",
			initialView:  ViewLogs,
			previousView: ViewLogs,
			expectedView: ViewHelp,
		},
		{
			name:         "toggle Help off to Logs",
			initialView:  ViewHelp,
			previousView: ViewLogs,
			expectedView: ViewLogs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := State{
				CurrentView: tt.initialView,
				Cursor:      3, // Non-zero cursor
			}

			newState := ToggleHelp(state, tt.previousView)

			assert.Equal(t, tt.expectedView, newState.CurrentView)
			assert.Equal(t, 0, newState.Cursor, "Cursor should reset to 0 on toggle")
		})
	}
}

func TestMoveCursorUp(t *testing.T) {
	tests := []struct {
		name           string
		initialCursor  int
		expectedCursor int
	}{
		{
			name:           "move cursor up from 5 to 4",
			initialCursor:  5,
			expectedCursor: 4,
		},
		{
			name:           "move cursor up from 1 to 0",
			initialCursor:  1,
			expectedCursor: 0,
		},
		{
			name:           "move cursor up from 0 stays at 0 (boundary)",
			initialCursor:  0,
			expectedCursor: 0,
		},
		{
			name:           "move cursor up from 10 to 9",
			initialCursor:  10,
			expectedCursor: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := State{
				Cursor: tt.initialCursor,
			}

			newState := MoveCursorUp(state)

			assert.Equal(t, tt.expectedCursor, newState.Cursor)
		})
	}
}

func TestMoveCursorDown(t *testing.T) {
	tests := []struct {
		name           string
		initialCursor  int
		maxItems       int
		expectedCursor int
	}{
		{
			name:           "move cursor down from 0 to 1",
			initialCursor:  0,
			maxItems:       10,
			expectedCursor: 1,
		},
		{
			name:           "move cursor down from 5 to 6",
			initialCursor:  5,
			maxItems:       10,
			expectedCursor: 6,
		},
		{
			name:           "move cursor down at boundary (9 -> stays at 9 for maxItems=10)",
			initialCursor:  9,
			maxItems:       10,
			expectedCursor: 9,
		},
		{
			name:           "move cursor down from 8 to 9 (last valid)",
			initialCursor:  8,
			maxItems:       10,
			expectedCursor: 9,
		},
		{
			name:           "move cursor down with empty list (maxItems=0)",
			initialCursor:  0,
			maxItems:       0,
			expectedCursor: 1, // Increments but won't clamp (maxItems=0 is edge case)
		},
		{
			name:           "move cursor down with single item (maxItems=1)",
			initialCursor:  0,
			maxItems:       1,
			expectedCursor: 0, // Clamped to 0 (max is 1-1=0)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := State{
				Cursor: tt.initialCursor,
			}

			newState := MoveCursorDown(state, tt.maxItems)

			assert.Equal(t, tt.expectedCursor, newState.Cursor)
		})
	}
}

func TestSetFileCount(t *testing.T) {
	tests := []struct {
		name           string
		initialCursor  int
		initialCount   int
		newCount       int
		expectedCursor int
		expectedCount  int
	}{
		{
			name:           "set file count with cursor in bounds",
			initialCursor:  3,
			initialCount:   10,
			newCount:       20,
			expectedCursor: 3,
			expectedCount:  20,
		},
		{
			name:           "set file count with cursor out of bounds (reset cursor)",
			initialCursor:  15,
			initialCount:   20,
			newCount:       5,
			expectedCursor: 0, // Reset cursor when out of bounds
			expectedCount:  5,
		},
		{
			name:           "set file count to 0 (empty list)",
			initialCursor:  5,
			initialCount:   10,
			newCount:       0,
			expectedCursor: 0, // Reset cursor
			expectedCount:  0,
		},
		{
			name:           "set file count with cursor at exact boundary",
			initialCursor:  10,
			initialCount:   15,
			newCount:       10,
			expectedCursor: 0, // Cursor 10 is out of bounds for count=10 (max index 9)
			expectedCount:  10,
		},
		{
			name:           "increase file count (cursor stays valid)",
			initialCursor:  2,
			initialCount:   5,
			newCount:       10,
			expectedCursor: 2,
			expectedCount:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := State{
				Cursor:    tt.initialCursor,
				FileCount: tt.initialCount,
			}

			newState := SetFileCount(state, tt.newCount)

			assert.Equal(t, tt.expectedCount, newState.FileCount)
			assert.Equal(t, tt.expectedCursor, newState.Cursor)
		})
	}
}

// Test that all state functions are pure (no side effects)
func TestStateFunctionsPurity(t *testing.T) {
	t.Run("SwitchToView does not modify original state", func(t *testing.T) {
		original := State{CurrentView: ViewBrowser, Cursor: 5}
		originalCopy := original

		_ = SwitchToView(original, ViewDashboard)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})

	t.Run("CycleView does not modify original state", func(t *testing.T) {
		original := State{CurrentView: ViewBrowser, Cursor: 3}
		originalCopy := original

		_ = CycleView(original)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})

	t.Run("MoveCursorUp does not modify original state", func(t *testing.T) {
		original := State{Cursor: 5}
		originalCopy := original

		_ = MoveCursorUp(original)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})

	t.Run("MoveCursorDown does not modify original state", func(t *testing.T) {
		original := State{Cursor: 5}
		originalCopy := original

		_ = MoveCursorDown(original, 10)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})
}
