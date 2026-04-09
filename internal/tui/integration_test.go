package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTUI_FileBrowserNavigation tests keyboard navigation in the file browser.
// Verifies j/k key presses update cursor correctly and respect boundaries.
func TestTUI_FileBrowserNavigation(t *testing.T) {
	// Setup: create temp directory with 5 test video files
	tmpDir := t.TempDir()
	createTestVideos(t, tmpDir, 5)

	// Initialize model with test config
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)

	// Manually set files for navigation testing
	model.files = []FileItem{
		{Name: "IPX-100.mp4", Path: filepath.Join(tmpDir, "IPX-100.mp4")},
		{Name: "IPX-101.mp4", Path: filepath.Join(tmpDir, "IPX-101.mp4")},
		{Name: "IPX-102.mp4", Path: filepath.Join(tmpDir, "IPX-102.mp4")},
		{Name: "IPX-103.mp4", Path: filepath.Join(tmpDir, "IPX-103.mp4")},
		{Name: "IPX-104.mp4", Path: filepath.Join(tmpDir, "IPX-104.mp4")},
	}
	require.Len(t, model.files, 5, "Should have 5 test files")

	// Verify initial state
	assert.Equal(t, 0, model.cursor, "Cursor should start at 0")

	// Simulate 'j' key press (down arrow)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(*Model)
	assert.Equal(t, 1, model.cursor, "Cursor should move down to 1")

	// Simulate 'k' key press (up arrow)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(*Model)
	assert.Equal(t, 0, model.cursor, "Cursor should move up to 0")

	// Verify state preservation
	assert.Len(t, model.files, 5, "File list should be unchanged")
}

// TestTUI_ViewSwitching tests view transitions and state preservation.
// Verifies tab key cycles through views and data is preserved.
func TestTUI_ViewSwitching(t *testing.T) {
	// Setup: initialize model with test config
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)
	model.files = []FileItem{
		{Name: "IPX-100.mp4"},
		{Name: "IPX-101.mp4"},
		{Name: "IPX-102.mp4"},
	}

	// Verify initial view (should be Browser)
	assert.Equal(t, ViewBrowser, model.currentView, "Should start in Browser view")

	// Switch to Dashboard view (press tab)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, ViewDashboard, model.currentView, "Should switch to Dashboard view")

	// Verify file list preserved
	assert.Len(t, model.files, 3, "File list should be preserved across view switch")

	// Cycle through remaining views: Dashboard -> Logs -> Settings -> Browser
	// (Help view is skipped in tab cycling per state.go:76-81)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, ViewLogs, model.currentView, "Should switch to Logs view")

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, ViewSettings, model.currentView, "Should switch to Settings view")

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	assert.Equal(t, ViewBrowser, model.currentView, "Should cycle back to Browser view")
	assert.Len(t, model.files, 3, "File list should still be preserved after full cycle")
}

// TestTUI_ProgressTracking tests progress message handling and task state updates.
// Verifies progress messages update task state correctly via handlers integration.
func TestTUI_ProgressTracking(t *testing.T) {
	// Setup: initialize model
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)

	// Add initial task to model
	taskID := "test-task-1"
	model.tasks = map[string]*worker.TaskProgress{
		taskID: {
			ID:       taskID,
			Progress: 0.0,
			Message:  "Starting",
			Status:   worker.TaskStatusPending,
		},
	}
	model.taskOrder = []string{taskID}

	// Send progress update: 0.0 → 0.5
	progressMsg := ProgressMsg{
		TaskID:   taskID,
		Progress: 0.5,
		Message:  "Halfway",
	}
	updated, _ := model.Update(progressMsg)
	model = updated.(*Model)

	// Verify task state updated
	task, exists := model.tasks[taskID]
	require.True(t, exists, "Task should exist")
	assert.Equal(t, 0.5, task.Progress, "Progress should be 0.5")
	assert.Equal(t, "Halfway", task.Message, "Message should be 'Halfway'")
}

// TestTUI_TaskCompletionFlow tests task completion and state transitions.
// Verifies ProgressMsg with TaskStatusSuccess marks task as complete correctly.
func TestTUI_TaskCompletionFlow(t *testing.T) {
	// Setup: initialize model with task
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)

	taskID := "test-task-complete"
	model.tasks = map[string]*worker.TaskProgress{
		taskID: {
			ID:        taskID,
			Progress:  0.5,
			Message:   "Processing",
			Status:    worker.TaskStatusRunning,
			StartTime: time.Now(),
		},
	}
	model.taskOrder = []string{taskID}

	// Send completion message via ProgressMsg with TaskStatusSuccess
	completeMsg := ProgressMsg{
		TaskID:   taskID,
		Status:   worker.TaskStatusSuccess,
		Progress: 1.0,
		Message:  "Complete",
	}
	updated, _ := model.Update(completeMsg)
	model = updated.(*Model)

	// Verify task marked as complete
	task, exists := model.tasks[taskID]
	require.True(t, exists, "Task should exist")
	assert.Equal(t, worker.TaskStatusSuccess, task.Status, "Task status should be 'success'")
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
