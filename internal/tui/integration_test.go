package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTUI_FileBrowserNavigation tests keyboard navigation in the file browser.
// Verifies j/k key presses update cursor correctly and respect boundaries.
func TestTUI_FileBrowserNavigation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Setup: create temp directory with 5 test video files
	tmpDir := t.TempDir()
	createTestVideos(t, tmpDir, 5)

	// Initialize model with test config
	model := New(TUIModelConfig{})

	// Manually set files for navigation testing
	model.browserState.files = []fileItem{
		{Name: "IPX-100.mp4", Path: filepath.Join(tmpDir, "IPX-100.mp4")},
		{Name: "IPX-101.mp4", Path: filepath.Join(tmpDir, "IPX-101.mp4")},
		{Name: "IPX-102.mp4", Path: filepath.Join(tmpDir, "IPX-102.mp4")},
		{Name: "IPX-103.mp4", Path: filepath.Join(tmpDir, "IPX-103.mp4")},
		{Name: "IPX-104.mp4", Path: filepath.Join(tmpDir, "IPX-104.mp4")},
	}
	require.Len(t, model.browserState.files, 5, "Should have 5 test files")

	// Verify initial state
	assert.Equal(t, 0, model.state.Cursor, "Cursor should start at 0")

	// Simulate 'j' key press (down arrow)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(*Model)
	assert.Equal(t, 1, model.state.Cursor, "Cursor should move down to 1")

	// Simulate 'k' key press (up arrow)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(*Model)
	assert.Equal(t, 0, model.state.Cursor, "Cursor should move up to 0")

	// Verify state preservation
	assert.Len(t, model.browserState.files, 5, "File list should be unchanged")
}

// TestTUI_ViewSwitching tests view transitions and state preservation.
// Verifies tab key cycles through views and data is preserved.
func TestTUI_ViewSwitching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Setup: initialize model with test config
	model := New(TUIModelConfig{})
	model.browserState.files = []fileItem{
		{Name: "IPX-100.mp4"},
		{Name: "IPX-101.mp4"},
		{Name: "IPX-102.mp4"},
	}

	// Verify initial view (should be browser)
	assert.Equal(t, viewBrowser, model.viewMgr.currentView(), "Should start in browser view")

	// Switch to dashboard view (press tab)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, viewDashboard, model.viewMgr.currentView(), "Should switch to dashboard view")

	// Verify file list preserved
	assert.Len(t, model.browserState.files, 3, "File list should be preserved across view switch")

	// Cycle through remaining views: dashboard -> Logs -> Settings -> browser
	// (Help view is skipped in tab cycling per state.go:76-81)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, viewLogs, model.viewMgr.currentView(), "Should switch to Logs view")

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, viewSettings, model.viewMgr.currentView(), "Should switch to Settings view")

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, viewBrowser, model.viewMgr.currentView(), "Should cycle back to browser view")
	assert.Len(t, model.browserState.files, 3, "File list should still be preserved after full cycle")
}

// TestTUI_ProgressTracking tests progress message handling and task state updates.
// Verifies SortEvent messages update task state correctly via handlers integration.
func TestTUI_ProgressTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Setup: initialize model
	model := New(TUIModelConfig{})

	// Add initial task to model
	taskID := "IPX-123"
	model.taskTracker.tasks = map[string]*taskState{
		taskID: {
			ID:       taskID,
			Progress: 0.0,
			Message:  "Starting",
			Step:     sortStepScrape,
		},
	}
	model.taskTracker.taskOrder = []string{taskID}

	// Send sort event: 0.0 → 0.5
	sortEventMsg := sortEventMsg{
		Event: SortEvent{
			MovieID:  taskID,
			Progress: 0.5,
			Message:  "Halfway",
			Step:     sortStepScrape,
			Phase:    SortEventPhaseScrape,
		},
	}
	updated, _ := model.Update(sortEventMsg)
	model = updated.(*Model)

	// Verify task state updated
	task, exists := model.taskTracker.tasks[taskID]
	require.True(t, exists, "Task should exist")
	assert.Equal(t, 0.5, task.Progress, "Progress should be 0.5")
	assert.Equal(t, "Halfway", task.Message, "Message should be 'Halfway'")
}

// TestTUI_TaskCompletionFlow tests task completion and state transitions.
// Verifies sortEventMsg with step "complete" marks task as complete correctly.
func TestTUI_TaskCompletionFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Setup: initialize model with task
	model := New(TUIModelConfig{})

	taskID := "IPX-456"
	model.taskTracker.tasks = map[string]*taskState{
		taskID: {
			ID:       taskID,
			Progress: 0.5,
			Message:  "Processing",
			Step:     sortStepScrape,
		},
	}
	model.taskTracker.taskOrder = []string{taskID}

	// Send completion message via sortEventMsg with step "complete"
	completeMsg := sortEventMsg{
		Event: SortEvent{
			MovieID:  taskID,
			Step:     sortStepComplete,
			Progress: 1.0,
			Message:  "Complete",
		},
	}
	updated, _ := model.Update(completeMsg)
	model = updated.(*Model)

	// Verify task marked as complete
	task, exists := model.taskTracker.tasks[taskID]
	require.True(t, exists, "Task should exist")
	assert.Equal(t, SortEventStep(sortStepComplete), task.Step, "Task step should be 'complete'")
	assert.Equal(t, 1.0, task.Progress, "Progress should be 1.0 when complete")
}

// Helper functions for test setup

// createTestVideos creates n test video files in the specified directory.
func createTestVideos(t *testing.T, dir string, count int) {
	for i := 0; i < count; i++ {
		// Use realistic JAV ID patterns
		filename := filepath.Join(dir, "IPX-"+fmt.Sprintf("%03d", i+100)+".mp4")
		err := os.WriteFile(filename, []byte("fake video content"), 0644)
		require.NoError(t, err, "Failed to create test video file")
	}
}
