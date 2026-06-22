package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func moveCursorUp(state state) state {
	newState := state
	newState.Cursor--

	if newState.Cursor < 0 {
		newState.Cursor = 0
	}

	return newState
}

func moveCursorDown(state state, maxItems int) state {
	newState := state
	newState.Cursor++

	if maxItems > 0 && newState.Cursor >= maxItems {
		newState.Cursor = maxItems - 1
	}

	return newState
}

func setFileCount(state state, count int) state {
	newState := state
	newState.FileCount = count

	if newState.Cursor >= count {
		newState.Cursor = 0
	}

	return newState
}

func TestNewState(t *testing.T) {
	state := newState()

	assert.Equal(t, 0, state.Cursor, "Should default cursor to 0")
	assert.Equal(t, 0, state.FileCount, "Should default file count to 0")
	assert.Equal(t, 0, state.SelectedIdx, "Should default selected index to 0")
	assert.False(t, state.EditingPath, "Should default path editing to false")
	assert.False(t, state.IsProcessing, "Should default processing to false")
	assert.False(t, state.IsPaused, "Should default paused to false")
	assert.False(t, state.ProcessingComplete, "Should default processing complete to false")
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
			state := state{
				Cursor: tt.initialCursor,
			}

			newState := moveCursorUp(state)

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
			state := state{
				Cursor: tt.initialCursor,
			}

			newState := moveCursorDown(state, tt.maxItems)

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
			state := state{
				Cursor:    tt.initialCursor,
				FileCount: tt.initialCount,
			}

			newState := setFileCount(state, tt.newCount)

			assert.Equal(t, tt.expectedCount, newState.FileCount)
			assert.Equal(t, tt.expectedCursor, newState.Cursor)
		})
	}
}

// Test that state helper functions are pure (no side effects)
func TestStateFunctionsPurity(t *testing.T) {
	t.Run("moveCursorUp does not modify original state", func(t *testing.T) {
		original := state{Cursor: 5}
		originalCopy := original

		_ = moveCursorUp(original)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})

	t.Run("moveCursorDown does not modify original state", func(t *testing.T) {
		original := state{Cursor: 5}
		originalCopy := original

		_ = moveCursorDown(original, 10)

		assert.Equal(t, originalCopy, original, "Original state should be unchanged")
	})
}

// --- viewManager tests (migrated from state_test.go) ---

func TestNewViewManager(t *testing.T) {
	vm := newViewManager()
	assert.Equal(t, viewBrowser, vm.currentView(), "Should default to browser view")
}

func TestViewManager_SwitchTo(t *testing.T) {
	tests := []struct {
		name         string
		initialView  viewMode
		targetView   viewMode
		expectedView viewMode
	}{
		{
			name:         "switch from browser to dashboard",
			initialView:  viewBrowser,
			targetView:   viewDashboard,
			expectedView: viewDashboard,
		},
		{
			name:         "switch from dashboard to Logs",
			initialView:  viewDashboard,
			targetView:   viewLogs,
			expectedView: viewLogs,
		},
		{
			name:         "switch from Logs to Settings",
			initialView:  viewLogs,
			targetView:   viewSettings,
			expectedView: viewSettings,
		},
		{
			name:         "switch from Settings to Help",
			initialView:  viewSettings,
			targetView:   viewHelp,
			expectedView: viewHelp,
		},
		{
			name:         "switch from Help to browser",
			initialView:  viewHelp,
			targetView:   viewBrowser,
			expectedView: viewBrowser,
		},
		{
			name:         "stay on same view (browser)",
			initialView:  viewBrowser,
			targetView:   viewBrowser,
			expectedView: viewBrowser,
		},
		{
			name:         "stay on same view (dashboard)",
			initialView:  viewDashboard,
			targetView:   viewDashboard,
			expectedView: viewDashboard,
		},
		{
			name:         "invalid view (negative)",
			initialView:  viewBrowser,
			targetView:   viewMode(-1),
			expectedView: viewBrowser, // No change
		},
		{
			name:         "invalid view (too large)",
			initialView:  viewDashboard,
			targetView:   viewMode(10),
			expectedView: viewDashboard, // No change
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := newViewManager()
			// Set initial view by switching from default browser
			if tt.initialView != viewBrowser {
				vm.switchTo(tt.initialView)
			}

			vm.switchTo(tt.targetView)
			assert.Equal(t, tt.expectedView, vm.currentView())
		})
	}
}

func TestViewManager_Cycle(t *testing.T) {
	tests := []struct {
		name         string
		initialView  viewMode
		expectedView viewMode
	}{
		{
			name:         "cycle from browser to dashboard",
			initialView:  viewBrowser,
			expectedView: viewDashboard,
		},
		{
			name:         "cycle from dashboard to Logs",
			initialView:  viewDashboard,
			expectedView: viewLogs,
		},
		{
			name:         "cycle from Logs to Settings",
			initialView:  viewLogs,
			expectedView: viewSettings,
		},
		{
			name:         "cycle from Settings back to browser (skip Help)",
			initialView:  viewSettings,
			expectedView: viewBrowser,
		},
		{
			name:         "cycle from Help to browser (Help not in normal cycle)",
			initialView:  viewHelp,
			expectedView: viewBrowser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := newViewManager()
			if tt.initialView != viewBrowser {
				vm.switchTo(tt.initialView)
			}
			vm.cycle()
			assert.Equal(t, tt.expectedView, vm.currentView())
		})
	}
}

func TestViewManager_ToggleHelp(t *testing.T) {
	tests := []struct {
		name         string
		initialView  viewMode
		expectedView viewMode
	}{
		{
			name:         "toggle Help on from browser",
			initialView:  viewBrowser,
			expectedView: viewHelp,
		},
		{
			name:         "toggle Help on from dashboard",
			initialView:  viewDashboard,
			expectedView: viewHelp,
		},
		{
			name:         "toggle Help on from Logs",
			initialView:  viewLogs,
			expectedView: viewHelp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := newViewManager()
			if tt.initialView != viewBrowser {
				vm.switchTo(tt.initialView)
			}
			vm.toggleHelp()
			assert.Equal(t, tt.expectedView, vm.currentView())
		})
	}

	// Test toggle off: browser → Help → browser
	t.Run("toggle Help off to browser", func(t *testing.T) {
		vm := newViewManager()
		vm.toggleHelp() // browser → Help
		assert.Equal(t, viewHelp, vm.currentView())
		vm.toggleHelp() // Help → browser
		assert.Equal(t, viewBrowser, vm.currentView())
	})

	// Test toggle off: dashboard → Help → dashboard
	t.Run("toggle Help off to dashboard", func(t *testing.T) {
		vm := newViewManager()
		vm.switchTo(viewDashboard)
		vm.toggleHelp() // dashboard → Help
		assert.Equal(t, viewHelp, vm.currentView())
		vm.toggleHelp() // Help → dashboard
		assert.Equal(t, viewDashboard, vm.currentView())
	})

	// Test toggle off: logs → Help → logs
	t.Run("toggle Help off to logs", func(t *testing.T) {
		vm := newViewManager()
		vm.switchTo(viewLogs)
		vm.toggleHelp() // logs → Help
		assert.Equal(t, viewHelp, vm.currentView())
		vm.toggleHelp() // Help → logs
		assert.Equal(t, viewLogs, vm.currentView())
	})
}

func TestIsValidView(t *testing.T) {
	tests := []struct {
		name     string
		view     viewMode
		expected bool
	}{
		{
			name:     "browser view is valid",
			view:     viewBrowser,
			expected: true,
		},
		{
			name:     "dashboard view is valid",
			view:     viewDashboard,
			expected: true,
		},
		{
			name:     "Logs view is valid",
			view:     viewLogs,
			expected: true,
		},
		{
			name:     "Settings view is valid",
			view:     viewSettings,
			expected: true,
		},
		{
			name:     "Help view is valid",
			view:     viewHelp,
			expected: true,
		},
		{
			name:     "negative view is invalid",
			view:     viewMode(-1),
			expected: false,
		},
		{
			name:     "view beyond Help is invalid",
			view:     viewMode(5),
			expected: false,
		},
		{
			name:     "large view number is invalid",
			view:     viewMode(100),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidView(tt.view)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- settingsManager tests ---

func TestNewSettingsManager(t *testing.T) {
	sm := newSettingsManager(settingsManagerDeps{}, false, false)
	s := sm.get()

	assert.True(t, s.ScrapeEnabled, "Scrape should default to true")
	assert.True(t, s.DownloadEnabled, "Download should default to true")
	assert.True(t, s.OrganizeEnabled, "Organize should default to true")
	assert.True(t, s.NFOEnabled, "NFO should default to true")
	assert.False(t, s.DryRun, "DryRun should default to false")
	assert.False(t, s.ForceUpdate, "ForceUpdate should default to false")
	assert.False(t, s.ForceRefresh, "ForceRefresh should default to false")
	assert.False(t, s.MoveFiles, "MoveFiles should default to false")
	assert.False(t, s.UpdateMode, "UpdateMode should default to false")
	assert.Equal(t, 0, sm.cursorPos(), "Cursor should default to 0")
}

func TestSettingsManager_MoveCursor(t *testing.T) {
	sm := newSettingsManager(settingsManagerDeps{}, false, false)

	// Move down
	sm.moveCursor(1)
	assert.Equal(t, 1, sm.cursorPos())

	sm.moveCursor(1)
	assert.Equal(t, 2, sm.cursorPos())

	// Move up
	sm.moveCursor(-1)
	assert.Equal(t, 1, sm.cursorPos())

	// Can't go below 0
	sm.moveCursor(-1)
	sm.moveCursor(-1)
	assert.Equal(t, 0, sm.cursorPos())

	// Can't go above maxSettings (9)
	for i := 0; i < 20; i++ {
		sm.moveCursor(1)
	}
	assert.Equal(t, 9, sm.cursorPos())
}

func TestSettingsManager_Toggle(t *testing.T) {
	// Track apply calls
	var applyCount int
	sm := newSettingsManager(settingsManagerDeps{
		apply: func(s settingsSnapshot) { applyCount++ },
		log:   func(level, message string) {},
	}, false, false)

	// Toggle DryRun (cursor 0)
	sm.moveCursor(0)
	desc := sm.toggle()
	assert.Contains(t, desc, "Dry run mode enabled")
	assert.True(t, sm.get().DryRun)
	assert.Equal(t, 1, applyCount)

	// Toggle again to disable
	desc = sm.toggle()
	assert.Contains(t, desc, "Dry run mode disabled")
	assert.False(t, sm.get().DryRun)
	assert.Equal(t, 2, applyCount)
}

func TestSettingsManager_ToggleUpdateMode(t *testing.T) {
	_ = settingsSnapshot{} // verify type is usable
	sm := newSettingsManager(settingsManagerDeps{
		apply: func(s settingsSnapshot) {},
		log:   func(level, message string) {},
	}, false, false)

	// Toggle UpdateMode (cursor 9)
	sm.moveCursor(9)
	desc := sm.toggle()
	assert.Contains(t, desc, "Update mode enabled")
	assert.True(t, sm.get().UpdateMode)
	assert.False(t, sm.get().OrganizeEnabled, "Organize should be disabled when update mode is on")

	// Toggle again to disable
	desc = sm.toggle()
	assert.Contains(t, desc, "Update mode disabled")
	assert.False(t, sm.get().UpdateMode)
	assert.True(t, sm.get().OrganizeEnabled, "Organize should be re-enabled when update mode is off")
}

func TestSettingsManager_SetDryRun(t *testing.T) {
	var applyCount int
	sm := newSettingsManager(settingsManagerDeps{
		apply: func(s settingsSnapshot) { applyCount++ },
		log:   func(level, message string) {},
	}, false, false)

	sm.setDryRun(true)
	assert.True(t, sm.get().DryRun)
	assert.Equal(t, 1, applyCount)

	sm.setDryRun(false)
	assert.False(t, sm.get().DryRun)
	assert.Equal(t, 2, applyCount)
}

func TestSettingsManager_SetUpdateMode(t *testing.T) {
	sm := newSettingsManager(settingsManagerDeps{
		apply: func(s settingsSnapshot) {},
		log:   func(level, message string) {},
	}, false, false)

	sm.setUpdateMode(true)
	assert.True(t, sm.get().UpdateMode)
	assert.False(t, sm.get().OrganizeEnabled, "Organize should be disabled when update mode is on")

	sm.setUpdateMode(false)
	assert.False(t, sm.get().UpdateMode)
	assert.True(t, sm.get().OrganizeEnabled, "Organize should be re-enabled when update mode is off")
}

func TestSettingsManager_ExtrafanartConfig(t *testing.T) {
	sm := newSettingsManager(settingsManagerDeps{}, true, false)
	assert.True(t, sm.get().DownloadExtrafanart, "Extrafanart should match config")

	sm2 := newSettingsManager(settingsManagerDeps{}, false, false)
	assert.False(t, sm2.get().DownloadExtrafanart, "Extrafanart should match config")
}
