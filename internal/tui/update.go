package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Update handles messages and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case ProgressMsg:
		m.UpdateProgress(worker.ProgressUpdate{
			TaskID:    msg.TaskID,
			Type:      msg.Type,
			Status:    msg.Status,
			Progress:  msg.Progress,
			Message:   msg.Message,
			BytesDone: msg.BytesDone,
			Error:     msg.Error,
			Timestamp: msg.Timestamp,
		})
		// Continue waiting for more progress updates
		cmds = append(cmds, waitForProgress(m.progressChan))
		return m, tea.Batch(cmds...)

	case TickMsg:
		// Update elapsed time and stats
		if m.workerPool != nil {
			stats := m.workerPool.Stats()
			m.UpdateStats(worker.ProgressStats{
				Total:           stats.TotalTasks,
				Pending:         stats.Pending,
				Running:         stats.Running,
				Success:         stats.Success,
				Failed:          stats.Failed,
				Canceled:        stats.Canceled,
				OverallProgress: stats.OverallProgress,
			})
		}
		// Schedule next tick
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case LogMsg:
		m.AddLog(msg.Level, msg.Message)
		return m, nil

	case ErrorMsg:
		m.err = msg.Error
		m.AddLog("error", msg.Error.Error())
		return m, nil

	case QuitMsg:
		m.quitting = true
		if m.workerPool != nil {
			m.workerPool.Stop()
		}
		return m, tea.Quit

	case RescanMsg:
		// Update source path
		m.SetSourcePath(msg.Path)
		m.AddLog("warn", "Path updated. Please restart TUI to rescan new folder.")
		return m, nil
	}

	// Update active view component
	// Note: Components handle their own updates internally
	// We don't need to reassign since they're pointers

	return m, tea.Batch(cmds...)
}

// handleKeyPress handles keyboard input
func (m *Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Global keybindings
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		if m.workerPool != nil {
			m.workerPool.Stop()
		}
		return m, tea.Quit

	case "?":
		// Toggle help view
		if m.currentView == ViewHelp {
			m.currentView = ViewBrowser
		} else {
			m.currentView = ViewHelp
		}
		return m, nil

	case "1":
		m.currentView = ViewBrowser
		return m, nil

	case "2":
		m.currentView = ViewDashboard
		return m, nil

	case "3":
		m.currentView = ViewLogs
		return m, nil

	case "tab":
		// Cycle through views
		m.currentView = (m.currentView + 1) % 4
		if m.currentView == ViewHelp {
			m.currentView = ViewBrowser
		}
		return m, nil
	}

	// View-specific keybindings
	switch m.currentView {
	case ViewBrowser:
		return m.handleBrowserKeys(msg)

	case ViewDashboard:
		return m.handleDashboardKeys(msg)

	case ViewLogs:
		return m.handleLogsKeys(msg)
	}

	return m, tea.Batch(cmds...)
}

// handleBrowserKeys handles browser view keybindings
func (m *Model) handleBrowserKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// If editing path, handle text input
	if m.editingPath {
		switch msg.String() {
		case "enter":
			// Confirm path change
			m.editingPath = false
			newPath := m.pathInput.Value()
			if newPath != "" && newPath != m.sourcePath {
				m.AddLog("info", "Path changed to: "+newPath)
				// Trigger rescan by sending a RescanMsg
				return m, func() tea.Msg {
					return RescanMsg{Path: newPath}
				}
			}
			return m, nil

		case "esc":
			// Cancel editing
			m.editingPath = false
			m.pathInput.SetValue(m.sourcePath)
			return m, nil

		default:
			// Update text input
			m.pathInput, cmd = m.pathInput.Update(msg)
			return m, cmd
		}
	}

	// Normal browser navigation
	switch msg.String() {
	case "f":
		// Toggle folder editing mode
		m.editingPath = true
		m.pathInput.Focus()
		return m, nil

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		if m.browser != nil {
			m.browser.CursorUp()
		}

	case "down", "j":
		if m.cursor < len(m.files)-1 {
			m.cursor++
		}
		if m.browser != nil {
			m.browser.CursorDown()
		}

	case " ", "space":
		// Toggle selection of current file
		if m.cursor < len(m.files) {
			m.ToggleFileSelection(m.files[m.cursor].Path)
		}

	case "a":
		// Select all
		for i := range m.files {
			m.files[i].Selected = true
			m.selectedFiles[m.files[i].Path] = true
		}
		if m.browser != nil {
			m.browser.SelectAll()
		}

	case "A":
		// Deselect all
		for i := range m.files {
			m.files[i].Selected = false
		}
		m.selectedFiles = make(map[string]bool)
		if m.browser != nil {
			m.browser.DeselectAll()
		}

	case "enter":
		// Start processing
		if len(m.selectedFiles) > 0 && !m.isProcessing {
			ctx := context.Background()
			if err := m.StartProcessing(ctx); err != nil {
				m.AddLog("error", err.Error())
			}
		}

	case "p":
		// Pause/resume processing
		if m.isProcessing {
			m.isPaused = !m.isPaused
			if m.isPaused {
				m.AddLog("info", "Processing paused")
			} else {
				m.AddLog("info", "Processing resumed")
			}
		}
	}

	return m, nil
}

// handleDashboardKeys handles dashboard view keybindings
func (m *Model) handleDashboardKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		// Refresh/reset stats
		m.startTime = m.startTime
	}

	return m, nil
}

// handleLogsKeys handles logs view keybindings
func (m *Model) handleLogsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.logScroll > 0 {
			m.logScroll--
		}
		if m.logViewer != nil {
			m.logViewer.ScrollUp()
		}

	case "down", "j":
		maxScroll := len(m.logs) - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.logScroll < maxScroll {
			m.logScroll++
		}
		if m.logViewer != nil {
			m.logViewer.ScrollDown()
		}

	case "g":
		// Go to top
		m.logScroll = 0
		if m.logViewer != nil {
			m.logViewer.ScrollToTop()
		}

	case "G":
		// Go to bottom
		maxScroll := len(m.logs) - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		m.logScroll = maxScroll
		if m.logViewer != nil {
			m.logViewer.ScrollToBottom()
		}

	case "a":
		// Toggle auto-scroll
		m.autoScroll = !m.autoScroll
		if m.logViewer != nil {
			m.logViewer.ToggleAutoScroll()
		}
	}

	return m, nil
}
